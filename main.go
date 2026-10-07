package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

var version = "dev"

type options struct {
	listen      string
	dataDir     string
	baseURL     string
	behindProxy bool
	firebaseKey string
}

func parseOptions(args []string) options {
	flags := flag.NewFlagSet("tossling-server", flag.ExitOnError)
	listen := flags.String("listen", env("TOSSLING_LISTEN", env("TOSSY_LISTEN", ":8090")), "address to listen on (TOSSLING_LISTEN)")
	dataDir := flags.String("data", env("TOSSLING_DATA", env("TOSSY_DATA", "data")), "directory for the database, attachments and settings (TOSSLING_DATA)")
	baseURL := flags.String("base-url", env("TOSSLING_BASE_URL", env("TOSSY_BASE_URL", "")), "public address of the server, e.g. https://tossling.example.com (TOSSLING_BASE_URL)")
	behindProxy := flags.Bool("behind-proxy", env("TOSSLING_BEHIND_PROXY", env("TOSSY_BEHIND_PROXY", "")) == "true", "take the client address from X-Forwarded-For set by a reverse proxy in front (TOSSLING_BEHIND_PROXY=true)")
	firebaseKey := flags.String("firebase-key", env("TOSSLING_FIREBASE_KEY_FILE", env("TOSSY_FIREBASE_KEY_FILE", "")), "Firebase service account JSON, for instant delivery to the Tossling app (TOSSLING_FIREBASE_KEY_FILE)")
	flags.Usage = func() {
		fmt.Fprintln(flags.Output(), "Usage: tossling-server [flags]            run the server")
		fmt.Fprintln(flags.Output(), "       tossling-server setup-link [flags] print a new link to the setup page")
		fmt.Fprintln(flags.Output(), "       tossling-server project add <channel> [name] | list | remove <channel> [flags]")
		fmt.Fprintln(flags.Output(), "       tossling-server admin-password [flags]  set the password of the web panel (asks, or reads stdin)")
		flags.PrintDefaults()
	}
	_ = flags.Parse(args)
	if *baseURL == "" {
		log.Fatal("-base-url or TOSSLING_BASE_URL is required, e.g. https://tossling.example.com")
	}
	if err := os.MkdirAll(*dataDir, 0o700); err != nil {
		log.Fatal(err)
	}
	dir, err := filepath.Abs(*dataDir)
	if err != nil {
		log.Fatal(err)
	}
	return options{listen: *listen, dataDir: dir, baseURL: strings.TrimRight(*baseURL, "/"), behindProxy: *behindProxy, firebaseKey: *firebaseKey}
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "setup-link" {
		opts := parseOptions(os.Args[2:])
		secret, err := newSettingsStore(opts.dataDir).newSetupLink()
		if err != nil {
			log.Fatal(err)
		}
		fmt.Println(setupURL(opts.baseURL, secret))
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "admin-password" {
		if err := runAdminPassword(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	if len(os.Args) > 1 && os.Args[1] == "project" {
		if err := runProject(os.Args[2:]); err != nil {
			log.Fatal(err)
		}
		return
	}
	opts := parseOptions(os.Args[1:])
	lock, err := os.OpenFile(filepath.Join(opts.dataDir, "tossling-server.lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		log.Fatal(err)
	}
	if err := syscall.Flock(int(lock.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		log.Fatalf("another tossling-server already uses %s", opts.dataDir)
	}
	store := newSettingsStore(opts.dataDir)
	settings, _, err := store.load()
	if err != nil {
		log.Fatal(err)
	}
	if settings.SetupSecret != "" {
		fmt.Printf("\nFinish the setup on this page:\n\n    %s\n\n", setupURL(opts.baseURL, settings.SetupSecret))
	}

	backend, err := startNtfy(opts, settings)
	if err != nil {
		log.Fatal(err)
	}
	manager, err := openUserManager(opts.dataDir)
	if err != nil {
		log.Fatal(err)
	}
	service := newServiceOverSocket(manager, backend.socket, opts, settings.Token)
	server := &http.Server{
		Addr:              opts.listen,
		Handler:           newFrontend(backend.socket, opts, store, service),
		ReadHeaderTimeout: 30 * time.Second,
	}
	go func() {
		stop := make(chan os.Signal, 1)
		signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
		<-stop
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
		backend.stop()
	}()
	go service.adoptBaseURL()
	log.Printf("tossling-server %s on %s, data in %s", version, opts.listen, opts.dataDir)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		log.Fatal(err)
	}
}

func setupURL(baseURL, secret string) string {
	return baseURL + "/setup/" + secret
}

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
