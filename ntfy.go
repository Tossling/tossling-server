package main

import (
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"time"

	ntfylog "heckel.io/ntfy/v2/log"
	"heckel.io/ntfy/v2/server"
	"heckel.io/ntfy/v2/user"
)

const (
	clientIPHeader = "X-Tossy-Client-Ip"
	internalIP     = "192.0.2.1"
)

type ntfyBackend struct {
	socket string
	server *server.Server
}

func (b *ntfyBackend) stop() {
	b.server.Stop()
}

func startNtfy(opts options, settings *Settings) (*ntfyBackend, error) {
	dir := opts.dataDir
	conf := server.NewConfig()
	conf.BaseURL = opts.baseURL
	conf.FirebaseKeyFile = opts.firebaseKey
	conf.ListenHTTP = ""
	socket, err := socketPath(dir)
	if err != nil {
		return nil, err
	}
	conf.ListenUnix = socket
	conf.ListenUnixMode = 0o600
	conf.WebRoot = ""
	conf.DisallowedTopics = append(append([]string{}, server.DefaultDisallowedTopics...), "setup")
	conf.CacheFile = filepath.Join(dir, "cache.db")
	conf.AuthFile = filepath.Join(dir, "user.db")
	conf.AuthDefault = user.PermissionDenyAll
	conf.AuthUsers = []*user.User{{Name: deviceUser, Hash: settings.PasswordHash, Role: user.RoleUser, Provisioned: true}}
	conf.AuthAccess = map[string][]*user.Grant{
		deviceUser: {
			{TopicPattern: "tossy-*", Permission: user.PermissionReadWrite, Provisioned: true},
			{TopicPattern: "mac", Permission: user.PermissionReadWrite, Provisioned: true},
			{TopicPattern: "claude", Permission: user.PermissionReadWrite, Provisioned: true},
			{TopicPattern: "*", Permission: user.PermissionRead, Provisioned: true},
		},
		user.Everyone: {
			{TopicPattern: "tossy-inv-*", Permission: user.PermissionRead, Provisioned: true},
		},
	}
	conf.AuthTokens = map[string][]*user.Token{deviceUser: {{Value: settings.Token, Label: "tossling-server", Provisioned: true}}}
	conf.AttachmentCacheDir = filepath.Join(dir, "attachments")
	conf.AttachmentFileSizeLimit = 520 * 1024 * 1024
	conf.AttachmentTotalSizeLimit = 5 * 1024 * 1024 * 1024
	conf.AttachmentExpiryDuration = 3 * time.Hour
	conf.VisitorAttachmentTotalSizeLimit = 5 * 1024 * 1024 * 1024
	conf.VisitorAttachmentDailyBandwidthLimit = 20 * 1024 * 1024 * 1024
	conf.BehindProxy = true
	conf.VisitorRequestExemptPrefixes = []netip.Prefix{netip.MustParsePrefix(internalIP + "/32")}
	conf.ProxyForwardedHeader = clientIPHeader
	conf.BuildVersion = "tossling-server " + version
	ntfylog.SetLevel(ntfylog.InfoLevel)

	s, err := server.New(conf)
	if err != nil {
		return nil, err
	}
	errs := make(chan error, 1)
	go func() { errs <- s.Run() }()
	for i := 0; i < 100; i++ {
		select {
		case err := <-errs:
			return nil, fmt.Errorf("ntfy stopped: %w", err)
		default:
		}
		if c, err := net.Dial("unix", conf.ListenUnix); err == nil {
			c.Close()
			return &ntfyBackend{socket: conf.ListenUnix, server: s}, nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return nil, fmt.Errorf("ntfy did not open %s", conf.ListenUnix)
}

func socketPath(dir string) (string, error) {
	path := filepath.Join(dir, "ntfy.sock")
	if len(path) < 100 {
		return path, nil
	}
	tmp, err := os.MkdirTemp("", "tossling-server-")
	if err != nil {
		return "", err
	}
	return filepath.Join(tmp, "ntfy.sock"), nil
}
