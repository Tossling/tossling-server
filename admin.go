package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "tossy_admin"
	sessionTTL    = 30 * 24 * time.Hour
	loginAttempts = 5
	loginLockout  = 15 * time.Minute
)

type loginLimiter struct {
	mu    sync.Mutex
	fails map[string]*loginFails
}

type loginFails struct {
	count int
	until time.Time
	first time.Time
}

func (l *loginLimiter) blocked(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	f := l.fails[ip]
	return f != nil && time.Now().Before(f.until)
}

func (l *loginLimiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	now := time.Now()
	f := l.fails[ip]
	if f == nil || now.Sub(f.first) > loginLockout {
		f = &loginFails{first: now}
		l.fails[ip] = f
	}
	f.count++
	if f.count >= loginAttempts {
		f.until = now.Add(loginLockout)
	}
}

func (l *loginLimiter) reset(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.fails, ip)
}

type adminPanel struct {
	store    *settingsStore
	service  *projectService
	opts     options
	limiter  *loginLimiter
	firebase bool
}

func newAdminPanel(store *settingsStore, service *projectService, opts options) *adminPanel {
	return &adminPanel{store: store, service: service, opts: opts, limiter: &loginLimiter{fails: map[string]*loginFails{}}, firebase: opts.firebaseKey != ""}
}

func (a *adminPanel) sign(key string, expires int64) string {
	mac := hmac.New(sha256.New, []byte(key))
	mac.Write([]byte("tossy-admin:" + strconv.FormatInt(expires, 10)))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (a *adminPanel) secure() bool {
	return strings.HasPrefix(a.opts.baseURL, "https://")
}

func (a *adminPanel) startSession(w http.ResponseWriter) error {
	settings, _, err := a.store.load()
	if err != nil || settings.SessionKey == "" {
		return errors.New("no session key")
	}
	expires := time.Now().Add(sessionTTL).Unix()
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: strconv.FormatInt(expires, 10) + "." + a.sign(settings.SessionKey, expires),
		Path: "/", MaxAge: int(sessionTTL.Seconds()), HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteStrictMode})
	return nil
}

func (a *adminPanel) endSession(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1, HttpOnly: true, Secure: a.secure(), SameSite: http.SameSiteStrictMode})
}

func (a *adminPanel) authed(r *http.Request) bool {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return false
	}
	exp, sig, ok := strings.Cut(c.Value, ".")
	if !ok {
		return false
	}
	expires, err := strconv.ParseInt(exp, 10, 64)
	if err != nil || time.Now().Unix() > expires {
		return false
	}
	settings, _, err := a.store.load()
	if err != nil || settings.SessionKey == "" || settings.AdminHash == "" {
		return false
	}
	return hmac.Equal([]byte(sig), []byte(a.sign(settings.SessionKey, expires)))
}

func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		origin = r.Header.Get("Referer")
	}
	u, err := url.Parse(origin)
	return err == nil && u.Host != "" && strings.EqualFold(u.Host, r.Host)
}

func (a *adminPanel) page(r *http.Request, nav string) pageData {
	return pageData{T: textsFor(r), Version: version, Nav: nav, Wide: true, BaseURL: a.opts.baseURL}
}

func (a *adminPanel) guard(next func(http.ResponseWriter, *http.Request)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && !sameOrigin(r) {
			http.Error(w, "cross-site request refused", http.StatusForbidden)
			return
		}
		settings, _, err := a.store.load()
		if err != nil {
			http.Error(w, "settings unavailable", http.StatusInternalServerError)
			return
		}
		if settings.AdminHash == "" {
			render(w, http.StatusOK, "admin_none.html", a.page(r, ""))
			return
		}
		if !a.authed(r) {
			http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
			return
		}
		next(w, r)
	}
}

func (a *adminPanel) routes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
	})
	mux.HandleFunc("GET /admin/login", a.loginPage)
	mux.HandleFunc("POST /admin/login", a.login)
	mux.HandleFunc("POST /admin/logout", a.guard(func(w http.ResponseWriter, r *http.Request) {
		a.endSession(w)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	}))
	mux.HandleFunc("GET /admin/{$}", a.guard(a.overviewPage))
	mux.HandleFunc("GET /admin/projects", a.guard(a.projectsPage))
	mux.HandleFunc("POST /admin/projects", a.guard(a.createProject))
	mux.HandleFunc("GET /admin/projects/{topic}", a.guard(a.projectPage))
	mux.HandleFunc("POST /admin/projects/{topic}/{action}", a.guard(a.projectAction))
	mux.HandleFunc("GET /admin/projects/{topic}/events", a.guard(a.projectEvents))
	mux.HandleFunc("GET /admin/security", a.guard(a.securityPage))
	mux.HandleFunc("POST /admin/security/{action}", a.guard(a.securityAction))
}

func (a *adminPanel) loginPage(w http.ResponseWriter, r *http.Request) {
	if a.authed(r) {
		http.Redirect(w, r, "/admin/", http.StatusSeeOther)
		return
	}
	data := a.page(r, "")
	settings, _, err := a.store.load()
	if err == nil && settings.AdminHash == "" {
		render(w, http.StatusOK, "admin_none.html", data)
		return
	}
	render(w, http.StatusOK, "admin_login.html", data)
}

func (a *adminPanel) login(w http.ResponseWriter, r *http.Request) {
	data := a.page(r, "")
	if !sameOrigin(r) {
		http.Error(w, "cross-site request refused", http.StatusForbidden)
		return
	}
	ip := clientIP(r, a.opts.behindProxy)
	if a.limiter.blocked(ip) {
		data.Error = data.T["LoginLimited"]
		render(w, http.StatusTooManyRequests, "admin_login.html", data)
		return
	}
	if !a.store.checkAdminPassword(r.FormValue("password")) {
		a.limiter.fail(ip)
		log.Printf("panel: wrong password from %s", ip)
		data.Error = data.T["LoginWrong"]
		render(w, http.StatusUnauthorized, "admin_login.html", data)
		return
	}
	a.limiter.reset(ip)
	if err := a.startSession(w); err != nil {
		data.Error = data.T["Wrong"]
		render(w, http.StatusInternalServerError, "admin_login.html", data)
		return
	}
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (a *adminPanel) overviewPage(w http.ResponseWriter, r *http.Request) {
	data := a.page(r, "overview")
	o := a.service.overview()
	data.Overview = &o
	data.Firebase = a.firebase
	render(w, http.StatusOK, "admin_overview.html", data)
}

func (a *adminPanel) projectsPage(w http.ResponseWriter, r *http.Request) {
	a.renderProjects(w, r, http.StatusOK, "", url.Values{})
}

func (a *adminPanel) renderProjects(w http.ResponseWriter, r *http.Request, status int, problem string, form url.Values) {
	data := a.page(r, "projects")
	projects, err := a.service.projects()
	if err != nil {
		problem = err.Error()
	}
	data.Projects = projects
	if pubs, err := a.service.publishers(); err == nil {
		data.Publishers = pubs
	}
	data.Error = problem
	data.Form = form
	render(w, status, "admin_projects.html", data)
}

func (a *adminPanel) createProject(w http.ResponseWriter, r *http.Request) {
	topic := strings.TrimSpace(r.FormValue("topic"))
	name := strings.TrimSpace(r.FormValue("name"))
	publisher := strings.TrimSpace(r.FormValue("publisher"))
	token, err := a.service.add(topic, name, publisher)
	if err != nil {
		a.renderProjects(w, r, http.StatusBadRequest, err.Error(), r.PostForm)
		return
	}
	log.Printf("panel: project %s created", topic)
	if token == "" {
		http.Redirect(w, r, "/admin/projects/"+url.PathEscape(topic), http.StatusSeeOther)
		return
	}
	a.showToken(w, r, topic, token)
}

func (a *adminPanel) showToken(w http.ResponseWriter, r *http.Request, topic, token string) {
	data := a.page(r, "projects")
	if p, err := a.service.project(topic); err == nil {
		data.Project = p
	}
	data.Token = token
	data.Command = curlExample(a.opts.baseURL, topic, token)
	render(w, http.StatusOK, "admin_token.html", data)
}

func (a *adminPanel) projectPage(w http.ResponseWriter, r *http.Request) {
	a.renderProject(w, r, http.StatusOK, "", "")
}

func (a *adminPanel) renderProject(w http.ResponseWriter, r *http.Request, status int, flash, problem string) {
	topic := r.PathValue("topic")
	data := a.page(r, "projects")
	p, err := a.service.project(topic)
	if err != nil {
		http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
		return
	}
	data.Project = p
	data.Events = a.service.events(topic, 50)
	data.DeviceChannel = deviceChannels[topic]
	data.Command = curlExample(a.opts.baseURL, topic, "<token>")
	data.Flash = flash
	data.Error = problem
	render(w, status, "admin_project.html", data)
}

func (a *adminPanel) projectAction(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	texts := textsFor(r)
	p, err := a.service.project(topic)
	if err != nil {
		http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
		return
	}
	switch r.PathValue("action") {
	case "rename":
		if err := a.service.rename(topic, r.FormValue("name")); err != nil {
			a.renderProject(w, r, http.StatusBadRequest, "", err.Error())
			return
		}
		http.Redirect(w, r, "/admin/projects/"+url.PathEscape(topic), http.StatusSeeOther)
	case "test":
		if err := a.service.testEvent(topic, texts["TestTitle"], texts["TestMessage"]); err != nil {
			a.renderProject(w, r, http.StatusBadRequest, "", err.Error())
			return
		}
		a.renderProject(w, r, http.StatusOK, texts["TestSent"], "")
	case "token":
		publisher := strings.Split(p.Publisher, ", ")[0]
		token, err := a.service.newToken(publisher)
		if err != nil {
			a.renderProject(w, r, http.StatusBadRequest, "", err.Error())
			return
		}
		log.Printf("panel: new token for publisher %s", publisher)
		a.showToken(w, r, topic, token)
	case "delete":
		if err := a.service.remove(topic); err != nil {
			a.renderProject(w, r, http.StatusBadRequest, "", err.Error())
			return
		}
		log.Printf("panel: project %s removed", topic)
		http.Redirect(w, r, "/admin/projects", http.StatusSeeOther)
	default:
		http.NotFound(w, r)
	}
}

func (a *adminPanel) securityPage(w http.ResponseWriter, r *http.Request) {
	a.renderSecurity(w, r, http.StatusOK, "", "", "")
}

func (a *adminPanel) renderSecurity(w http.ResponseWriter, r *http.Request, status int, flash, problem, link string) {
	data := a.page(r, "security")
	if pubs, err := a.service.publishers(); err == nil {
		data.Publishers = pubs
	}
	data.Flash = flash
	data.Error = problem
	data.SetupLink = link
	render(w, status, "admin_security.html", data)
}

func (a *adminPanel) securityAction(w http.ResponseWriter, r *http.Request) {
	texts := textsFor(r)
	switch r.PathValue("action") {
	case "setup-link":
		secret, err := a.store.newSetupLink()
		if err != nil {
			a.renderSecurity(w, r, http.StatusInternalServerError, "", err.Error(), "")
			return
		}
		log.Print("panel: new setup link issued")
		a.renderSecurity(w, r, http.StatusOK, "", "", setupURL(a.opts.baseURL, secret))
	case "password":
		if !a.store.checkAdminPassword(r.FormValue("current")) {
			a.renderSecurity(w, r, http.StatusBadRequest, "", texts["LoginWrong"], "")
			return
		}
		if r.FormValue("password") != r.FormValue("repeat") {
			a.renderSecurity(w, r, http.StatusBadRequest, "", texts["PasswordMismatch"], "")
			return
		}
		if err := a.store.setAdminPassword(r.FormValue("password")); err != nil {
			problem := err.Error()
			if errors.Is(err, errShortPassword) {
				problem = texts["PasswordShort"]
			}
			a.renderSecurity(w, r, http.StatusBadRequest, "", problem, "")
			return
		}
		log.Print("panel: password changed")
		_ = a.startSession(w)
		a.renderSecurity(w, r, http.StatusOK, texts["PasswordChanged"], "", "")
	case "logout-all":
		if err := a.store.rotateSessions(); err != nil {
			a.renderSecurity(w, r, http.StatusInternalServerError, "", err.Error(), "")
			return
		}
		log.Print("panel: all sessions ended")
		a.endSession(w)
		http.Redirect(w, r, "/admin/login", http.StatusSeeOther)
	default:
		http.NotFound(w, r)
	}
}

func (a *adminPanel) projectEvents(w http.ResponseWriter, r *http.Request) {
	topic := r.PathValue("topic")
	if _, err := a.service.project(topic); err != nil {
		http.NotFound(w, r)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-store")
	h.Set("X-Accel-Buffering", "no")
	w.WriteHeader(http.StatusOK)
	fmt.Fprint(w, "retry: 5000\n\n")
	flusher.Flush()
	ctx := r.Context()
	lines := make(chan string, 16)
	go func() {
		defer close(lines)
		_ = a.service.follow(ctx, topic, func(e *liveEvent) error {
			line := ": keepalive\n\n"
			if e != nil {
				data, err := json.Marshal(e)
				if err != nil {
					return err
				}
				line = "data: " + string(data) + "\n\n"
			}
			select {
			case lines <- line:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
	}()
	ticker := time.NewTicker(25 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case line, open := <-lines:
			if !open {
				return
			}
			if _, err := fmt.Fprint(w, line); err != nil {
				return
			}
			flusher.Flush()
		case <-ticker.C:
			if _, err := fmt.Fprint(w, ": keepalive\n\n"); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}
