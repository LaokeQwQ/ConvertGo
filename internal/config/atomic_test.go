package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicReplacementFailureRemovesTemporaryFile(t *testing.T) {
	dir := t.TempDir()
	destination := filepath.Join(dir, "settings.json")
	if err := os.Mkdir(destination, 0700); err != nil {
		t.Fatal(err)
	}
	sentinel := filepath.Join(destination, "keep")
	if err := os.WriteFile(sentinel, []byte("unchanged"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := atomicJSON(destination, DefaultSettings()); err == nil {
		t.Fatal("expected replacement of a nonempty directory to fail")
	}
	got, err := os.ReadFile(sentinel)
	if err != nil || string(got) != "unchanged" {
		t.Fatalf("failed write modified destination: %q %v", got, err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "settings.json" {
		t.Fatalf("temporary configuration leaked: %v err=%v", entries, err)
	}
}
