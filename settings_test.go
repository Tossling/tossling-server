package main

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
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

func TestASetupLinkWorksForADay(t *testing.T) {
	store := newSettingsStore(t.TempDir())
	settings, _, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.checkSetupSecret(settings.SetupSecret); !ok {
		t.Fatal("a new link does not work")
	}
	if _, err := store.update(func(s *Settings) error { s.SetupIssued = time.Now().Add(-setupLinkLife - time.Minute).Unix(); return nil }); err != nil {
		t.Fatal(err)
	}
	if _, ok := store.checkSetupSecret(settings.SetupSecret); ok {
		t.Fatal("a link older than a day still works")
	}
	secret, err := store.newSetupLink()
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := store.checkSetupSecret(secret); !ok {
		t.Fatal("a new link from setup-link does not work")
	}
}

func TestALinkFromAnOlderVersionGetsItsDayFromNow(t *testing.T) {
	store := newSettingsStore(t.TempDir())
	settings, _, err := store.load()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.update(func(s *Settings) error { s.SetupIssued = 0; return nil }); err != nil {
		t.Fatal(err)
	}
	if err := store.startSetupClock(); err != nil {
		t.Fatal(err)
	}
	after, _, _ := store.load()
	if after.SetupIssued == 0 || time.Since(time.Unix(after.SetupIssued, 0)) > time.Minute {
		t.Fatalf("setup_issued = %d", after.SetupIssued)
	}
	if _, ok := store.checkSetupSecret(settings.SetupSecret); !ok {
		t.Fatal("the old link stopped working at once")
	}
}
