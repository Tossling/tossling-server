package main

import (
	"context"
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

//go:embed web
var webFiles embed.FS

var assetVersion = func() string {
	h := sha256.New()
	for _, name := range []string{"web/static/tossling.css", "web/static/tossling.js", "web/static/logo.svg"} {
		data, err := webFiles.ReadFile(name)
		if err != nil {
			panic(err)
		}
		h.Write(data)
	}
	return hex.EncodeToString(h.Sum(nil))[:12]
}()

var pages = template.Must(template.New("").Funcs(template.FuncMap{
	"dict":  dict,
	"asset": func(name string) string { return "/static/" + name + "?v=" + assetVersion },
	"when": func(t time.Time) template.HTML {
		if t.IsZero() || t.Unix() <= 0 {
			return "—"
		}
		return template.HTML(fmt.Sprintf(`<time datetime="%s">%s</time>`, t.UTC().Format(time.RFC3339), template.HTMLEscapeString(formatTime(t))))
	},
	"size": formatSize,
}).ParseFS(webFiles, "web/templates/*.html"))

func dict(pairs ...any) map[string]any {
	m := make(map[string]any, len(pairs)/2)
	for i := 0; i+1 < len(pairs); i += 2 {
		m[fmt.Sprint(pairs[i])] = pairs[i+1]
	}
	return m
}

const contentSecurityPolicy = "default-src 'none'; style-src 'self'; script-src 'self'; img-src 'self'; font-src 'self'; " +
	"connect-src 'self'; form-action 'self'; base-uri 'none'; frame-ancestors 'none'"

type check struct {
	OK   bool
	Text string
}

type pageData struct {
	T             Texts
	Version       string
	BaseURL       string
	Token         string
	Command       string
	Secret        string
	Checks        []check
	AdminSet      bool
	Nav           string
	Wide          bool
	Flash         string
	Error         string
	Overview      *overview
	Firebase      bool
	Projects      []project
	Project       *project
	Publishers    []publisherInfo
	Events        []event
	DeviceChannel bool
	SetupLink     string
	Form          url.Values
}

func newFrontend(socket string, opts options, store *settingsStore, service *projectService) http.Handler {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", socket)
		},
		MaxIdleConns:    100,
		IdleConnTimeout: 90 * time.Second,
	}
	backend := &url.URL{Scheme: "http", Host: "ntfy"}
	proxy := &httputil.ReverseProxy{
		Rewrite: func(pr *httputil.ProxyRequest) {
			pr.SetURL(backend)
			pr.Out.Host = pr.In.Host
			pr.Out.Header.Set(clientIPHeader, clientIP(pr.In, opts.behindProxy))
		},
		Transport:     transport,
		FlushInterval: -1,
	}
	static, err := fs.Sub(webFiles, "web/static")
	if err != nil {
		panic(err)
	}
	files := http.StripPrefix("/static/", http.FileServerFS(static))
	panel := newAdminPanel(store, service, opts)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, http.StatusOK, "index.html", pageData{T: textsFor(r), Version: version})
	})
	mux.HandleFunc("GET /static/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("v") == assetVersion || strings.HasPrefix(r.URL.Path, "/static/fonts/") {
			w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
		} else {
			w.Header().Set("Cache-Control", "no-cache")
		}
		files.ServeHTTP(w, r)
	})
	mux.HandleFunc("GET /setup/{secret}", func(w http.ResponseWriter, r *http.Request) {
		renderSetup(w, r, store, opts, http.StatusOK, "")
	})
	mux.HandleFunc("POST /setup/{secret}/admin", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		texts := textsFor(r)
		if _, ok := store.checkSetupSecret(r.PathValue("secret")); !ok {
			render(w, http.StatusNotFound, "lost.html", pageData{T: texts, Version: version})
			return
		}
		if r.FormValue("password") != r.FormValue("repeat") {
			renderSetup(w, r, store, opts, http.StatusBadRequest, texts["PasswordMismatch"])
			return
		}
		if err := store.setAdminPassword(r.FormValue("password")); err != nil {
			problem := err.Error()
			if errors.Is(err, errShortPassword) {
				problem = texts["PasswordShort"]
			}
			renderSetup(w, r, store, opts, http.StatusBadRequest, problem)
			return
		}
		log.Print("setup: panel password set")
		http.Redirect(w, r, "/setup/"+url.PathEscape(r.PathValue("secret")), http.StatusSeeOther)
	})
	mux.HandleFunc("POST /setup/{secret}/done", func(w http.ResponseWriter, r *http.Request) {
		if !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		if !store.finishSetup(r.PathValue("secret")) {
			render(w, http.StatusNotFound, "lost.html", pageData{T: textsFor(r), Version: version})
			return
		}
		log.Print("setup finished, the setup link no longer works")
		http.Redirect(w, r, "/", http.StatusSeeOther)
	})
	for _, prefix := range apiPrefixes {
		mux.HandleFunc("GET "+prefix+"/health", func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"server": "tossling-server", "version": version, "push": opts.firebaseKey != ""})
		})
	}
	panel.routes(mux)
	panel.apiRoutes(mux)
	mux.Handle("/", proxy)
	return mux
}

func renderSetup(w http.ResponseWriter, r *http.Request, store *settingsStore, opts options, status int, problem string) {
	texts := textsFor(r)
	settings, ok := store.checkSetupSecret(r.PathValue("secret"))
	if !ok {
		render(w, http.StatusNotFound, "lost.html", pageData{T: texts, Version: version})
		return
	}
	render(w, status, "setup.html", pageData{
		T:        texts,
		Version:  version,
		BaseURL:  opts.baseURL,
		Token:    settings.Token,
		Command:  "tossling setup " + opts.baseURL,
		Secret:   settings.SetupSecret,
		Checks:   setupChecks(r, opts.baseURL, texts),
		AdminSet: settings.AdminHash != "",
		Error:    problem,
	})
}

func render(w http.ResponseWriter, status int, name string, data pageData) {
	h := w.Header()
	h.Set("Content-Type", "text/html; charset=utf-8")
	h.Set("Content-Security-Policy", contentSecurityPolicy)
	h.Set("Referrer-Policy", "same-origin")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("X-Frame-Options", "DENY")
	h.Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := pages.ExecuteTemplate(w, name, data); err != nil {
		log.Printf("page %s: %v", name, err)
	}
}

func setupChecks(r *http.Request, baseURL string, texts Texts) []check {
	base, err := url.Parse(baseURL)
	if err != nil {
		return nil
	}
	checks := []check{{OK: true, Text: texts["Address"] + ": " + baseURL}}
	if base.Scheme != "https" && !isLocal(base.Hostname()) {
		checks = append(checks, check{Text: texts["NoHTTPS"]})
	}
	if r.Host != "" && !strings.EqualFold(r.Host, base.Host) {
		checks = append(checks, check{Text: texts.f("OtherHost", r.Host, base.Host)})
	}
	return append(checks, check{OK: true, Text: texts["Files"]})
}

func isLocal(host string) bool {
	if host == "localhost" {
		return true
	}
	addr, err := netip.ParseAddr(host)
	return err == nil && addr.IsLoopback()
}

func clientIP(r *http.Request, behindProxy bool) string {
	if behindProxy {
		parts := strings.Split(r.Header.Get("X-Forwarded-For"), ",")
		if addr, err := netip.ParseAddr(strings.TrimSpace(parts[len(parts)-1])); err == nil {
			if addr.String() == internalIP {
				return "0.0.0.0"
			}
			return addr.String()
		}
	}
	if addrPort, err := netip.ParseAddrPort(r.RemoteAddr); err == nil {
		return addrPort.Addr().String()
	}
	return "0.0.0.0"
}
