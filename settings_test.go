package main

import (
	"os"
	"path/filepath"
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
