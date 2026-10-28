package backup

import (
	"os"
	"path/filepath"
	"runtime"
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
	if !permOK(info.Mode().Perm(), 0o644) {
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

func permOK(got, want os.FileMode) bool {
	if runtime.GOOS != "windows" {
		return got == want
	}
	return got&0o200 != 0
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

func TestRestoreRemovesEmptyDirsItCreated(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, ".codex", "config.toml")
	store := Store{Dir: filepath.Join(home, "backups")}
	if err := store.EnsureOriginal(path); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("created\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore("original"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatalf(".codex still exists: %v", err)
	}

	keep := filepath.Join(home, "keep")
	if err := os.MkdirAll(keep, 0o700); err != nil {
		t.Fatal(err)
	}
	nested := filepath.Join(keep, "new", "file.txt")
	store = Store{Dir: filepath.Join(home, "backups-2")}
	if err := store.EnsureOriginal(nested); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(nested), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(nested, []byte("x\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore(""); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(keep); err != nil {
		t.Fatalf("pre-existing dir removed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(keep, "new")); !os.IsNotExist(err) {
		t.Fatalf("created dir remains: %v", err)
	}

	busy := filepath.Join(home, ".grok", "config.toml")
	store = Store{Dir: filepath.Join(home, "backups-3")}
	if err := store.EnsureOriginal(busy); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(busy), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(busy, []byte("a\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".grok", "other.txt"), []byte("stay\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Restore("original"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(busy); !os.IsNotExist(err) {
		t.Fatalf("config still exists: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".grok", "other.txt")); err != nil {
		t.Fatalf("sibling removed: %v", err)
	}
}
