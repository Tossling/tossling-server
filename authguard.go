package main

import (
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"

	"heckel.io/ntfy/v2/user"
)

const (
	refusedFor   = time.Hour
	refusedLimit = 10000
)

type authGuard struct {
	manager *user.Manager
	now     func() time.Time
	mu      sync.Mutex
	refused map[[32]byte]time.Time
}

func newAuthGuard(manager *user.Manager) *authGuard {
	return &authGuard{manager: manager, now: time.Now, refused: map[[32]byte]time.Time{}}
}

func credential(r *http.Request) string {
	value := strings.TrimSpace(r.Header.Get("Authorization"))
	query := r.URL.Query()
	param := query.Get("authorization")
	if param == "" {
		param = query.Get("auth")
	}
	if param != "" {
		decoded, err := base64.RawURLEncoding.DecodeString(param)
		if err != nil {
			return ""
		}
		value = strings.TrimSpace(string(decoded))
	}
	lower := strings.ToLower(value)
	if !strings.HasPrefix(lower, "bearer ") && !strings.HasPrefix(lower, "basic ") {
		return ""
	}
	return value
}

func (g *authGuard) isRefused(value string) bool {
	if value == "" {
		return false
	}
	key := sha256.Sum256([]byte(value))
	g.mu.Lock()
	defer g.mu.Unlock()
	until, ok := g.refused[key]
	if ok && g.now().After(until) {
		delete(g.refused, key)
		return false
	}
	return ok
}

func (g *authGuard) valid(value string) bool {
	if strings.HasPrefix(strings.ToLower(value), "bearer ") {
		_, err := g.manager.AuthenticateToken(strings.TrimSpace(value[len("bearer "):]))
		return err == nil
	}
	r := &http.Request{Header: http.Header{"Authorization": {value}}}
	name, password, ok := r.BasicAuth()
	if !ok {
		return false
	}
	var err error
	if name == "" {
		_, err = g.manager.AuthenticateToken(password)
	} else {
		_, err = g.manager.Authenticate(name, password)
	}
	return err == nil
}

func (g *authGuard) refusedByServer(value string) {
	if value == "" || g.valid(value) {
		return
	}
	now := g.now()
	key := sha256.Sum256([]byte(value))
	g.mu.Lock()
	defer g.mu.Unlock()
	if len(g.refused) >= refusedLimit {
		for k, until := range g.refused {
			if now.After(until) {
				delete(g.refused, k)
			}
		}
		if len(g.refused) >= refusedLimit {
			g.refused = map[[32]byte]time.Time{}
		}
	}
	g.refused[key] = now.Add(refusedFor)
}

func (g *authGuard) wrap(proxy http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if g.isRefused(credential(r)) {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Cache-Control", "no-store")
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":40101,"http":401,"error":"unauthorized","link":"https://ntfy.sh/docs/publish/#authentication"}` + "\n"))
			return
		}
		proxy.ServeHTTP(w, r)
	})
}

func (g *authGuard) watch(resp *http.Response) error {
	if resp.StatusCode == http.StatusUnauthorized && resp.Request != nil {
		g.refusedByServer(credential(resp.Request))
	}
	return nil
}
