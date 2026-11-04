package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
)

func TestNewServersUseTheTosslingSettingsFile(t *testing.T) {
	dir := t.TempDir()
	if got := newSettingsStore(dir).path; got != filepath.Join(dir, "tossling-server.json") {
		t.Fatalf("path = %s", got)
	}
}

func TestServersFromBeforeTheRenameKeepTheirSettingsFile(t *testing.T) {
	dir := t.TempDir()
	old := filepath.Join(dir, "tossy-server.json")
	if err := os.WriteFile(old, []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := newSettingsStore(dir).path; got != old {
		t.Fatalf("path = %s, want %s", got, old)
	}
}

func TestChangesAtOnceAllLand(t *testing.T) {
	store := newSettingsStore(t.TempDir())
	if _, _, err := store.load(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := store.update(func(s *Settings) error { s.SessionKey += "x"; return nil }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	settings, _, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if len(settings.SessionKey) != 50 {
		t.Fatalf("%d of 50 changes landed", len(settings.SessionKey))
	}
}
