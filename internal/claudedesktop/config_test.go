package claudedesktop

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDirFor(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "u")
	if got := DirFor(home, "darwin", ""); !strings.HasSuffix(got, filepath.Join("Library", "Application Support", "Claude-3p", "configLibrary")) {
		t.Fatal(got)
	}
	if got := DirFor(home, "linux", ""); !strings.HasSuffix(got, filepath.Join(".config", "Claude-3p", "configLibrary")) {
		t.Fatal(got)
	}
	roaming := filepath.Join(string(filepath.Separator), "Users", "u", "AppData", "Roaming")
	got := DirFor(home, "windows", roaming)
	if !strings.HasPrefix(got, roaming) || !strings.HasSuffix(got, filepath.Join("Claude", "configLibrary")) {
		t.Fatal(got)
	}
	if strings.Contains(got, "Claude-3p") || strings.Contains(got, filepath.Join("AppData", "Local")) {
		t.Fatal(got)
	}
	fallback := DirFor(home, "windows", "")
	if !strings.Contains(fallback, filepath.Join("AppData", "Roaming", "Claude", "configLibrary")) {
		t.Fatal(fallback)
	}
}

func TestPlanCreatesAndReplacesKey(t *testing.T) {
	dir := t.TempDir()
	upd := Update{BaseURL: "https://deeprouter.top", APIKey: "sk-old-key-1111", Model: "claude-sonnet-4-6", SetModel: true}
	first, err := Plan(dir, upd)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.ProfilePath, first.Profile, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(first.MetaPath, first.Meta, 0o600); err != nil {
		t.Fatal(err)
	}
	again, err := Plan(dir, upd)
	if err != nil {
		t.Fatal(err)
	}
	if string(again.Profile) != string(first.Profile) || string(again.Meta) != string(first.Meta) {
		t.Fatalf("not idempotent\n%s\n%s", again.Profile, again.Meta)
	}
	var profile map[string]any
	if err := json.Unmarshal(first.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["inferenceProvider"] != "gateway" || profile["inferenceGatewayBaseUrl"] != "https://deeprouter.top" || profile["inferenceGatewayApiKey"] != "sk-old-key-1111" || profile["inferenceCredentialKind"] != "static" || profile["inferenceGatewayAuthScheme"] != "bearer" {
		t.Fatalf("profile %#v", profile)
	}
	if strings.Contains(string(first.Profile), "/v1") {
		t.Fatalf("base URL should not include /v1:\n%s", first.Profile)
	}
	models, _ := profile["inferenceModels"].([]any)
	if len(models) != 1 {
		t.Fatalf("models %#v", profile["inferenceModels"])
	}

	if err := os.WriteFile(first.ProfilePath, first.Profile, 0o600); err != nil {
		t.Fatal(err)
	}
	upd.APIKey = "sk-new-key-2222"
	upd.Model = "claude-opus-4-6"
	next, err := Plan(dir, upd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(next.Profile), "sk-old-key-1111") || !strings.Contains(string(next.Profile), "sk-new-key-2222") || !strings.Contains(string(next.Profile), "claude-opus-4-6") {
		t.Fatalf("replace:\n%s", next.Profile)
	}
	if strings.Contains(string(next.Meta), "sk-new-key-2222") {
		t.Fatalf("meta holds the key:\n%s", next.Meta)
	}
}

func TestApplied(t *testing.T) {
	dir := t.TempDir()
	path, wired := Applied(dir, "https://deeprouter.top")
	if wired || filepath.Base(path) != ProfileID+".json" {
		t.Fatalf("path %s wired %v", path, wired)
	}
	files, err := Plan(dir, Update{BaseURL: "https://deeprouter.top", APIKey: "sk-desktop-key"})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.ProfilePath, files.Profile, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(files.MetaPath, files.Meta, 0o600); err != nil {
		t.Fatal(err)
	}
	path, wired = Applied(dir, "https://deeprouter.top")
	if !wired || path != files.ProfilePath {
		t.Fatalf("path %s wired %v", path, wired)
	}
	if _, wired = Applied(dir, "https://other.example"); wired {
		t.Fatal("other base URL counted as wired")
	}
	if _, wired = Applied("", "https://deeprouter.top"); wired {
		t.Fatal("empty dir")
	}
}

func TestPlanUpdatesAppliedConfigAndKeepsOthers(t *testing.T) {
	dir := t.TempDir()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	applied := "other-config"
	if err := os.WriteFile(filepath.Join(dir, applied+".json"), []byte("{\n  \"keep\": true,\n  \"inferenceProvider\": \"bedrock\"\n}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	meta := "{\n  \"appliedId\": \"other-config\",\n  \"entries\": [{\"id\": \"other-config\", \"name\": \"Work\"}],\n  \"note\": \"leave me\"\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "_meta.json"), []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	files, err := Plan(dir, Update{BaseURL: "https://deeprouter.top", APIKey: "sk-desktop-key"})
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(files.ProfilePath) != applied+".json" {
		t.Fatal(files.ProfilePath)
	}
	var profile map[string]any
	if err := json.Unmarshal(files.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["keep"] != true || profile["inferenceProvider"] != "gateway" || profile["inferenceGatewayApiKey"] != "sk-desktop-key" {
		t.Fatalf("%#v", profile)
	}
	if profile["modelDiscoveryEnabled"] != true {
		t.Fatalf("discovery %#v", profile["modelDiscoveryEnabled"])
	}
	var index map[string]any
	if err := json.Unmarshal(files.Meta, &index); err != nil {
		t.Fatal(err)
	}
	if index["appliedId"] != applied || index["note"] != "leave me" {
		t.Fatalf("%#v", index)
	}
	entries, _ := index["entries"].([]any)
	if len(entries) != 1 {
		t.Fatalf("entries %#v", index["entries"])
	}
}
