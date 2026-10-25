package store

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveIsPrivateAndRoundTrips(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	st := State{Version: 1, Active: "azure"}
	st.Upsert(Profile{Name: "azure", Label: "Azure", BaseURL: "https://deeprouter.top", APIKey: "sk-secret"})
	if err := Save(path, st); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("mode %o", info.Mode().Perm())
	}
	dirInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatal(err)
	}
	// The temp dir itself may be 0700 from the test harness; the parent of
	// config.json is dir, which we MkdirAll only if needed. Create a nested dir.
	nested := filepath.Join(dir, "nested", "config.json")
	if err := Save(nested, st); err != nil {
		t.Fatal(err)
	}
	parent, err := os.Stat(filepath.Dir(nested))
	if err != nil {
		t.Fatal(err)
	}
	if parent.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %o", parent.Mode().Perm())
	}
	_ = dirInfo
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	p, ok := loaded.Find("azure")
	if !ok || p.APIKey != "sk-secret" || loaded.Active != "azure" {
		t.Fatalf("%+v", loaded)
	}
}

func TestRemoveRefusesActive(t *testing.T) {
	st := State{Active: "openai"}
	st.Upsert(Profile{Name: "openai"})
	if err := st.Remove("openai"); err == nil {
		t.Fatal("expected error")
	}
	st.Active = "other"
	if err := st.Remove("openai"); err != nil {
		t.Fatal(err)
	}
	if _, ok := st.Find("openai"); ok {
		t.Fatal("still present")
	}
}

func TestValidName(t *testing.T) {
	if err := ValidName("azure"); err != nil {
		t.Fatal(err)
	}
	if err := ValidName("-bad"); err == nil {
		t.Fatal("expected error")
	}
}
