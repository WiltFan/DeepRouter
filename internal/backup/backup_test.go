package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOriginalIsNotOverwritten(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "settings.json")
	if err := os.WriteFile(path, []byte("original\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	store := Store{Dir: filepath.Join(home, "backups")}
	if err := store.EnsureOriginal(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("changed\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := store.EnsureOriginal(path); err != nil {
		t.Fatal(err)
	}
	id, err := store.SaveHistory([]string{path})
	if err != nil || id == "" {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("newer\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore("original"); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "original\n" {
		t.Fatalf("got %q", got)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	if _, err := store.Restore("latest"); err != nil {
		t.Fatal(err)
	}
	got, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "changed\n" {
		t.Fatalf("latest restored %q", got)
	}
}

func TestRestoreRemovesCreatedFile(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "missing.toml")
	store := Store{Dir: filepath.Join(home, "backups")}
	if err := store.EnsureOriginal(path); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("created\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file still exists: %v", err)
	}
}
