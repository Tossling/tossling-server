package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"net/netip"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"heckel.io/ntfy/v2/user"
)

var deviceChannels = map[string]bool{"mac": true, "claude": true}

type projectService struct {
	manager *user.Manager
	client  *http.Client
	stream  *http.Client
	ntfyURL string
	baseURL string
	token   string
	dataDir string
}

type project struct {
	Topic     string
	Name      string
	Publisher string
	Events    int
	LastEvent time.Time
}

type publisherInfo struct {
	Name       string
	Topics     []string
	LastUsed   time.Time
	LastOrigin string
}

type event struct {
	Time     time.Time
	Title    string
	Message  string
	Priority int
}

func openUserManager(dataDir string) (*user.Manager, error) {
	path := filepath.Join(dataDir, "user.db")
	return user.NewSQLiteManager(path, "", &user.Config{
		Filename:      path,
		DefaultAccess: user.PermissionDenyAll,
		BcryptCost:    user.DefaultUserPasswordBcryptCost,
	})
}

func newServiceOverSocket(manager *user.Manager, socket string, opts options, token string) *projectService {
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		var d net.Dialer
		return d.DialContext(ctx, "unix", socket)
	}}
	return &projectService{manager: manager, client: &http.Client{Transport: transport, Timeout: 15 * time.Second},
		stream: &http.Client{Transport: transport}, ntfyURL: "http://ntfy", baseURL: opts.baseURL, token: token, dataDir: opts.dataDir}
}

func newServiceOverHTTP(manager *user.Manager, opts options, token string) *projectService {
	host, port, err := net.SplitHostPort(opts.listen)
	if err != nil || host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return &projectService{manager: manager, client: &http.Client{Timeout: 15 * time.Second}, stream: &http.Client{},
		ntfyURL: "http://" + net.JoinHostPort(host, port), baseURL: opts.baseURL, token: token, dataDir: opts.dataDir}
}

var apiPrefixes = []string{"/v1/tossling", "/v1/tossy"}

func roomTopic(topic string) bool {
	return strings.HasPrefix(topic, "tossling-") || strings.HasPrefix(topic, "tossy-")
}

func checkProjectTopic(topic string) error {
	if !projectTopic.MatchString(topic) || roomTopic(topic) || topic == "setup" {
		return fmt.Errorf("%q cannot be a project channel: use letters, digits, - and _, not starting with tossling- or tossy-", topic)
	}
	return nil
}

func (p *projectService) request(method, path, token string, body any, headers map[string]string) ([]byte, error) {
	var reader io.Reader = http.NoBody
	switch b := body.(type) {
	case nil:
	case string:
		reader = strings.NewReader(b)
	default:
		data, err := json.Marshal(b)
		if err != nil {
			return nil, err
		}
		reader = bytes.NewReader(data)
	}
	req, err := http.NewRequest(method, p.ntfyURL+path, reader)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		if _, ok := body.(string); !ok {
			req.Header.Set("Content-Type", "application/json")
		}
	}
	req.Header.Set(clientIPHeader, internalIP)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("is the server running? %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server answered %s", resp.Status)
	}
	return data, nil
}

func (p *projectService) subscriptions() (map[string]string, error) {
	data, err := p.request(http.MethodGet, "/v1/account", p.token, nil, nil)
	if err != nil {
		return nil, err
	}
	var account struct {
		Subscriptions []struct {
			Topic       string  `json:"topic"`
			DisplayName *string `json:"display_name"`
		} `json:"subscriptions"`
	}
	if err := json.Unmarshal(data, &account); err != nil {
		return nil, err
	}
	names := map[string]string{}
	for _, s := range account.Subscriptions {
		if roomTopic(s.Topic) {
			continue
		}
		names[s.Topic] = s.Topic
		if s.DisplayName != nil && *s.DisplayName != "" {
			names[s.Topic] = *s.DisplayName
		}
	}
	return names, nil
}

func (p *projectService) subscribe(topic, name string) error {
	_, err := p.request(http.MethodPost, "/v1/account/subscription", p.token,
		map[string]any{"base_url": p.baseURL, "topic": topic, "display_name": name}, nil)
	return err
}

func (p *projectService) rename(topic, name string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("empty name")
	}
	_, err := p.request(http.MethodPatch, "/v1/account/subscription", p.token,
		map[string]any{"base_url": p.baseURL, "topic": topic, "display_name": strings.TrimSpace(name)}, nil)
	return err
}

func (p *projectService) unsubscribe(topic string) error {
	_, err := p.request(http.MethodDelete, "/v1/account/subscription", p.token, nil,
		map[string]string{"X-BaseURL": p.baseURL, "X-Topic": topic})
	return err
}

func (p *projectService) publishers() ([]publisherInfo, error) {
	users, err := p.manager.Users()
	if err != nil {
		return nil, err
	}
	var out []publisherInfo
	for _, u := range users {
		if !strings.HasPrefix(u.Name, projectUserPrefix) {
			continue
		}
		info := publisherInfo{Name: strings.TrimPrefix(u.Name, projectUserPrefix)}
		grants, err := p.manager.Grants(u.Name)
		if err != nil {
			return nil, err
		}
		for _, g := range grants {
			info.Topics = append(info.Topics, g.TopicPattern)
		}
		sort.Strings(info.Topics)
		tokens, err := p.manager.Tokens(u.ID)
		if err != nil {
			return nil, err
		}
		for _, t := range tokens {
			if t.LastAccess.After(info.LastUsed) && t.LastAccess.Unix() > 0 {
				info.LastUsed = t.LastAccess
				if t.LastOrigin.IsValid() && !t.LastOrigin.IsUnspecified() {
					info.LastOrigin = t.LastOrigin.String()
				}
			}
		}
		out = append(out, info)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (p *projectService) publisherOf(topic string, pubs []publisherInfo) string {
	var names []string
	for _, pub := range pubs {
		for _, t := range pub.Topics {
			if t == topic {
				names = append(names, pub.Name)
			}
		}
	}
	return strings.Join(names, ", ")
}

func (p *projectService) projects() ([]project, error) {
	names, err := p.subscriptions()
	if err != nil {
		return nil, err
	}
	pubs, err := p.publishers()
	if err != nil {
		return nil, err
	}
	for _, pub := range pubs {
		for _, t := range pub.Topics {
			if _, ok := names[t]; !ok {
				names[t] = t
			}
		}
	}
	stats := p.eventStats()
	var out []project
	for topic, name := range names {
		s := stats[topic]
		out = append(out, project{Topic: topic, Name: name, Publisher: p.publisherOf(topic, pubs), Events: s.count, LastEvent: s.last})
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out, nil
}

func (p *projectService) project(topic string) (*project, error) {
	all, err := p.projects()
	if err != nil {
		return nil, err
	}
	for i := range all {
		if all[i].Topic == topic {
			return &all[i], nil
		}
	}
	return nil, fmt.Errorf("no project on channel %s", topic)
}

func (p *projectService) createToken(username string) (string, error) {
	u, err := p.manager.User(username)
	if err != nil {
		return "", err
	}
	token, err := p.manager.CreateToken(u.ID, "tossling-server project", time.Unix(0, 0), netip.IPv4Unspecified(), false)
	if err != nil {
		return "", err
	}
	return token.Value, nil
}

func (p *projectService) add(topic, name, publisher string) (string, error) {
	if err := checkProjectTopic(topic); err != nil {
		return "", err
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = topic
	}
	publisher = strings.TrimSpace(publisher)
	if publisher == "" {
		publisher = topic
	}
	if err := checkProjectTopic(publisher); err != nil {
		return "", fmt.Errorf("publisher name: %w", err)
	}
	username := projectUserPrefix + publisher
	token := ""
	if _, err := p.manager.User(username); err == nil {
		grants, err := p.manager.Grants(username)
		if err != nil {
			return "", err
		}
		for _, g := range grants {
			if g.TopicPattern == topic {
				return "", fmt.Errorf("%s already publishes to %s", publisher, topic)
			}
		}
		if err := p.manager.AllowAccess(username, topic, user.PermissionWrite); err != nil {
			return "", err
		}
	} else {
		password, err := randomString(32)
		if err != nil {
			return "", err
		}
		if err := p.manager.AddUser(username, password, user.RoleUser, false); err != nil {
			return "", err
		}
		if err := p.manager.AllowAccess(username, topic, user.PermissionWrite); err != nil {
			return "", err
		}
		if token, err = p.createToken(username); err != nil {
			return "", err
		}
	}
	if names, err := p.subscriptions(); err == nil {
		if _, ok := names[topic]; ok {
			return token, p.rename(topic, name)
		}
	}
	return token, p.subscribe(topic, name)
}

func (p *projectService) remove(topic string) error {
	pubs, err := p.publishers()
	if err != nil {
		return err
	}
	for _, pub := range pubs {
		username := projectUserPrefix + pub.Name
		has := false
		for _, t := range pub.Topics {
			if t == topic {
				has = true
			}
		}
		if !has {
			continue
		}
		if err := p.manager.ResetAccess(username, topic); err != nil {
			return err
		}
		if len(pub.Topics) == 1 {
			if err := p.manager.RemoveUser(username); err != nil {
				return err
			}
		}
	}
	return p.unsubscribe(topic)
}

func (p *projectService) newToken(publisher string) (string, error) {
	username := projectUserPrefix + publisher
	u, err := p.manager.User(username)
	if err != nil {
		return "", fmt.Errorf("no publisher %s", publisher)
	}
	tokens, err := p.manager.Tokens(u.ID)
	if err != nil {
		return "", err
	}
	for _, t := range tokens {
		if err := p.manager.RemoveToken(u.ID, t.Value); err != nil {
			return "", err
		}
	}
	return p.createToken(username)
}

func (p *projectService) testEvent(topic, title, message string) error {
	headers := map[string]string{"Title": title, "Priority": "2"}
	if deviceChannels[topic] {
		_, err := p.request(http.MethodPost, "/"+topic, p.token, message, headers)
		return err
	}
	pubs, err := p.publishers()
	if err != nil {
		return err
	}
	name := strings.Split(p.publisherOf(topic, pubs), ", ")[0]
	if name == "" {
		return errors.New("the project has no publisher")
	}
	u, err := p.manager.User(projectUserPrefix + name)
	if err != nil {
		return err
	}
	token, err := p.manager.CreateToken(u.ID, "tossling-server test", time.Now().Add(time.Minute), netip.IPv4Unspecified(), false)
	if err != nil {
		return err
	}
	defer p.manager.RemoveToken(u.ID, token.Value)
	_, err = p.request(http.MethodPost, "/"+topic, token.Value, message, headers)
	return err
}

type topicStats struct {
	count int
	last  time.Time
}

func (p *projectService) cache() (*sql.DB, error) {
	return sql.Open("sqlite3", "file:"+filepath.Join(p.dataDir, "cache.db")+"?mode=ro&_busy_timeout=2000")
}

func (p *projectService) eventStats() map[string]topicStats {
	stats := map[string]topicStats{}
	db, err := p.cache()
	if err != nil {
		return stats
	}
	defer db.Close()
	rows, err := db.Query("SELECT topic, COUNT(*), MAX(time) FROM messages WHERE event = 'message' AND time >= ? AND topic NOT LIKE 'tossy-%' AND topic NOT LIKE 'tossling-%' GROUP BY topic",
		time.Now().Add(-24*time.Hour).Unix())
	if err != nil {
		return stats
	}
	defer rows.Close()
	for rows.Next() {
		var topic string
		var count int
		var last int64
		if rows.Scan(&topic, &count, &last) == nil {
			stats[topic] = topicStats{count: count, last: time.Unix(last, 0)}
		}
	}
	return stats
}

func (p *projectService) events(topic string, limit int) []event {
	db, err := p.cache()
	if err != nil {
		return nil
	}
	defer db.Close()
	rows, err := db.Query("SELECT time, title, message, priority FROM messages WHERE topic = ? AND event = 'message' ORDER BY time DESC LIMIT ?", topic, limit)
	if err != nil {
		return nil
	}
	defer rows.Close()
	var out []event
	for rows.Next() {
		var e event
		var t int64
		if rows.Scan(&t, &e.Title, &e.Message, &e.Priority) == nil {
			e.Time = time.Unix(t, 0)
			if e.Priority == 0 {
				e.Priority = 3
			}
			out = append(out, e)
		}
	}
	return out
}

type overview struct {
	Rooms          int
	RoomActivity   time.Time
	RoomMessages   int
	ProjectEvents  int
	Attachments    int64
	CacheSize      int64
	FirebaseOn     bool
	ProjectsCount  int
	PublisherCount int
}

func (p *projectService) overview() overview {
	var o overview
	if db, err := p.cache(); err == nil {
		defer db.Close()
		var last sql.NullInt64
		_ = db.QueryRow("SELECT COUNT(DISTINCT topic), COUNT(*), MAX(time) FROM messages WHERE (topic LIKE 'tossy-%' OR topic LIKE 'tossling-%') AND topic NOT LIKE 'tossy-inv-%' AND topic NOT LIKE 'tossling-inv-%'").Scan(&o.Rooms, &o.RoomMessages, &last)
		if last.Valid {
			o.RoomActivity = time.Unix(last.Int64, 0)
		}
		_ = db.QueryRow("SELECT COUNT(*) FROM messages WHERE event = 'message' AND topic NOT LIKE 'tossy-%' AND topic NOT LIKE 'tossling-%' AND time >= ?", time.Now().Add(-24*time.Hour).Unix()).Scan(&o.ProjectEvents)
	}
	_ = filepath.WalkDir(filepath.Join(p.dataDir, "attachments"), func(_ string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if info, err := d.Info(); err == nil {
				o.Attachments += info.Size()
			}
		}
		return nil
	})
	for _, name := range []string{"cache.db", "cache.db-wal"} {
		if info, err := os.Stat(filepath.Join(p.dataDir, name)); err == nil {
			o.CacheSize += info.Size()
		}
	}
	if projects, err := p.projects(); err == nil {
		o.ProjectsCount = len(projects)
	}
	if pubs, err := p.publishers(); err == nil {
		o.PublisherCount = len(pubs)
	}
	return o
}

type liveEvent struct {
	Time     int64  `json:"time"`
	Title    string `json:"title"`
	Message  string `json:"message"`
	Priority int    `json:"priority"`
}

func (p *projectService) follow(ctx context.Context, topic string, emit func(*liveEvent) error) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, p.ntfyURL+"/"+topic+"/json", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set(clientIPHeader, internalIP)
	resp, err := p.stream.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("server answered %s", resp.Status)
	}
	decoder := json.NewDecoder(resp.Body)
	for {
		var e struct {
			Event    string `json:"event"`
			Time     int64  `json:"time"`
			Title    string `json:"title"`
			Message  string `json:"message"`
			Priority int    `json:"priority"`
		}
		if err := decoder.Decode(&e); err != nil {
			return err
		}
		var out *liveEvent
		if e.Event == "message" {
			if e.Priority == 0 {
				e.Priority = 3
			}
			out = &liveEvent{Time: e.Time, Title: e.Title, Message: e.Message, Priority: e.Priority}
		}
		if err := emit(out); err != nil {
			return err
		}
	}
}
