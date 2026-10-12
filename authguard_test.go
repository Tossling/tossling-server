package main

import (
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/netip"
	"net/url"
	"sync/atomic"
	"testing"
	"time"

	"heckel.io/ntfy/v2/user"
)

type guardedServer struct {
	url   string
	calls *atomic.Int32
	guard *authGuard
}

func guardedBackend(t *testing.T, m *user.Manager) guardedServer {
	t.Helper()
	calls := &atomic.Int32{}
	ntfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		value := credential(r)
		if value == "" {
			w.WriteHeader(http.StatusOK)
			return
		}
		g := authGuard{manager: m}
		if !g.valid(value) {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/account/phone" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(ntfy.Close)
	backend, _ := url.Parse(ntfy.URL)
	guard := newAuthGuard(m)
	proxy := &httputil.ReverseProxy{
		Rewrite:        func(pr *httputil.ProxyRequest) { pr.SetURL(backend) },
		ModifyResponse: guard.watch,
	}
	front := httptest.NewServer(guard.wrap(proxy))
	t.Cleanup(front.Close)
	return guardedServer{url: front.URL, calls: calls, guard: guard}
}

func status(t *testing.T, method, target string, header map[string]string) int {
	t.Helper()
	req, _ := http.NewRequest(method, target, nil)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	return resp.StatusCode
}

func TestARefusedTokenIsAnsweredWithoutAskingNtfyAgain(t *testing.T) {
	m, _ := deviceManager(t)
	s := guardedBackend(t, m)
	dead := map[string]string{"Authorization": "Bearer " + user.GenerateToken()}
	for i := 0; i < 40; i++ {
		if got := status(t, "GET", s.url+"/tossling-room/json?poll=1", dead); got != http.StatusUnauthorized {
			t.Fatalf("request %d: %d", i, got)
		}
	}
	if n := s.calls.Load(); n != 1 {
		t.Fatalf("ntfy was asked %d times, want once", n)
	}
}

func TestEveryNewTokenStillReachesNtfy(t *testing.T) {
	m, _ := deviceManager(t)
	s := guardedBackend(t, m)
	for i := 0; i < 5; i++ {
		status(t, "GET", s.url+"/tossling-room/json?poll=1", map[string]string{"Authorization": "Bearer " + user.GenerateToken()})
	}
	if n := s.calls.Load(); n != 5 {
		t.Fatalf("ntfy was asked %d times, want 5: guessing must stay limited by ntfy", n)
	}
}

func TestAValidTokenRefusedForOtherReasonsIsNotRemembered(t *testing.T) {
	m, _ := deviceManager(t)
	token, err := issueRoomToken(m, "tossling", neverExpires, netip.IPv4Unspecified())
	if err != nil {
		t.Fatal(err)
	}
	s := guardedBackend(t, m)
	auth := map[string]string{"Authorization": "Bearer " + token.Value}
	if got := status(t, "POST", s.url+"/v1/account/phone", auth); got != http.StatusUnauthorized {
		t.Fatalf("phone: %d", got)
	}
	if got := status(t, "GET", s.url+"/tossling-room/json?poll=1", auth); got != http.StatusOK {
		t.Fatalf("the token was remembered as refused: %d", got)
	}
}

func TestTheQueryParameterAndBasicAuthCountAsCredentials(t *testing.T) {
	m, _ := deviceManager(t)
	s := guardedBackend(t, m)
	dead := user.GenerateToken()
	param := base64.RawURLEncoding.EncodeToString([]byte("Bearer " + dead))
	basic := map[string]string{"Authorization": "Basic " + base64.StdEncoding.EncodeToString([]byte(":"+dead))}
	for i := 0; i < 3; i++ {
		status(t, "GET", s.url+"/tossling-room/ws?auth="+param, nil)
		status(t, "GET", s.url+"/tossling-room/json?poll=1", basic)
	}
	if n := s.calls.Load(); n != 2 {
		t.Fatalf("ntfy was asked %d times, want once per form", n)
	}
	if got := status(t, "GET", s.url+"/tossling-inv-x/json?poll=1", nil); got != http.StatusOK {
		t.Fatalf("anonymous: %d", got)
	}
}

func TestARefusalIsForgottenAfterAnHour(t *testing.T) {
	m, _ := deviceManager(t)
	s := guardedBackend(t, m)
	now := time.Now()
	s.guard.now = func() time.Time { return now }
	dead := map[string]string{"Authorization": "Bearer " + user.GenerateToken()}
	status(t, "GET", s.url+"/tossling-room/json?poll=1", dead)
	status(t, "GET", s.url+"/tossling-room/json?poll=1", dead)
	now = now.Add(refusedFor + time.Minute)
	status(t, "GET", s.url+"/tossling-room/json?poll=1", dead)
	if n := s.calls.Load(); n != 2 {
		t.Fatalf("ntfy was asked %d times, want 2", n)
	}
}
