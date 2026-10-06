package main

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/crypto/bcrypt"
	"heckel.io/ntfy/v2/user"
)

var errShortPassword = errors.New("the password must be at least 10 characters")

const deviceUser = "tossy"

type Settings struct {
	PasswordHash string `json:"password_hash"`
	Token        string `json:"token"`
	SetupSecret  string `json:"setup_secret,omitempty"`
	AdminHash    string `json:"admin_hash,omitempty"`
	SessionKey   string `json:"session_key,omitempty"`
}

type settingsStore struct {
	path string
	mu   sync.Mutex
}

func newSettingsStore(dir string) *settingsStore {
	path := filepath.Join(dir, "tossling-server.json")
	if old := filepath.Join(dir, "tossy-server.json"); fileExists(old) && !fileExists(path) {
		path = old
	}
	return &settingsStore{path: path}
}

func randomString(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

func (s *settingsStore) load() (*Settings, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.path)
	if err == nil {
		var settings Settings
		if err := json.Unmarshal(data, &settings); err != nil {
			return nil, false, err
		}
		if !user.ValidToken(settings.Token) || settings.PasswordHash == "" {
			return nil, false, errors.New(s.path + " is damaged: no token or password hash")
		}
		return &settings, false, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return nil, false, err
	}
	password, err := randomString(32)
	if err != nil {
		return nil, false, err
	}
	hash, err := user.HashPassword(password, user.DefaultUserPasswordBcryptCost)
	if err != nil {
		return nil, false, err
	}
	secret, err := randomString(24)
	if err != nil {
		return nil, false, err
	}
	settings := &Settings{PasswordHash: hash, Token: user.GenerateToken(), SetupSecret: secret}
	if err := s.write(settings); err != nil {
		return nil, false, err
	}
	return settings, true, nil
}

func (s *settingsStore) write(settings *Settings) error {
	data, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *settingsStore) update(change func(*Settings) error) (*Settings, error) {
	settings, _, err := s.load()
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := change(settings); err != nil {
		return nil, err
	}
	return settings, s.write(settings)
}

func (s *settingsStore) newSetupLink() (string, error) {
	settings, err := s.update(func(settings *Settings) error {
		secret, err := randomString(24)
		settings.SetupSecret = secret
		return err
	})
	if err != nil {
		return "", err
	}
	return settings.SetupSecret, nil
}

func (s *settingsStore) checkSetupSecret(secret string) (*Settings, bool) {
	settings, _, err := s.load()
	if err != nil || settings.SetupSecret == "" || secret == "" {
		return nil, false
	}
	return settings, subtle.ConstantTimeCompare([]byte(settings.SetupSecret), []byte(secret)) == 1
}

func (s *settingsStore) setAdminPassword(password string) error {
	if len([]rune(password)) < 10 {
		return errShortPassword
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	key, err := randomString(32)
	if err != nil {
		return err
	}
	_, err = s.update(func(settings *Settings) error {
		settings.AdminHash = string(hash)
		settings.SessionKey = key
		return nil
	})
	return err
}

func (s *settingsStore) checkAdminPassword(password string) bool {
	settings, _, err := s.load()
	if err != nil || settings.AdminHash == "" {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(settings.AdminHash), []byte(password)) == nil
}

func (s *settingsStore) rotateSessions() error {
	key, err := randomString(32)
	if err != nil {
		return err
	}
	_, err = s.update(func(settings *Settings) error {
		settings.SessionKey = key
		return nil
	})
	return err
}

func (s *settingsStore) finishSetup(secret string) bool {
	if _, ok := s.checkSetupSecret(secret); !ok {
		return false
	}
	_, err := s.update(func(settings *Settings) error {
		settings.SetupSecret = ""
		return nil
	})
	return err == nil
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
