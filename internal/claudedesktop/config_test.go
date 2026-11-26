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
	// Claude Desktop on 3P keeps its library under %LOCALAPPDATA%\Claude-3p.
	local := filepath.Join(t.TempDir(), "AppData", "Local")
	got := DirFor(home, "windows", local)
	if want := filepath.Join(local, "Claude-3p", "configLibrary"); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
	if strings.Contains(got, "Roaming") || strings.Contains(got, filepath.Join("Claude", "configLibrary")) {
		t.Fatal(got)
	}
	fallback := DirFor(home, "windows", "")
	if want := filepath.Join(home, "AppData", "Local", "Claude-3p", "configLibrary"); fallback != want {
		t.Fatalf("got %s, want %s", fallback, want)
	}
}

func TestDirForWindowsMSIXPackage(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "u")
	local := t.TempDir()
	packaged := filepath.Join(local, "Packages", msixPackage, "LocalCache", "Local", "Claude-3p")
	plain := filepath.Join(local, "Claude-3p")
	want := func(root string) string { return filepath.Join(root, "configLibrary") }

	if got := DirFor(home, "windows", local); got != want(plain) {
		t.Fatalf("no data yet: got %s, want %s", got, want(plain))
	}
	if err := os.MkdirAll(packaged, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := DirFor(home, "windows", local); got != want(packaged) {
		t.Fatalf("package data only: got %s, want %s", got, want(packaged))
	}
	if err := os.MkdirAll(plain, 0o700); err != nil {
		t.Fatal(err)
	}
	if got := DirFor(home, "windows", local); got != want(plain) {
		t.Fatalf("both exist: got %s, want %s", got, want(plain))
	}
	// Only Windows looks at the package directory.
	if got := DirFor(home, "linux", local); strings.Contains(got, "Packages") {
		t.Fatal(got)
	}
}

// Windows editors save JSON with a UTF-8 byte order mark. Reading it must not
// fail, or the whole apply (including Claude Code settings) is aborted.
func TestPlanAcceptsUTF8BOM(t *testing.T) {
	bom := []byte{0xEF, 0xBB, 0xBF}
	dir := t.TempDir()
	id := "11111111-2222-4333-8444-555555555555"
	meta := append(append([]byte{}, bom...), []byte("{\r\n  \"appliedId\": \""+id+"\",\r\n  \"entries\": [{\"id\": \""+id+"\", \"name\": \"Mine\"}]\r\n}\r\n")...)
	profile := append(append([]byte{}, bom...), []byte("{\r\n  \"inferenceGatewayApiKey\": \"sk-old\",\r\n  \"keepMe\": true\r\n}\r\n")...)
	if err := os.WriteFile(filepath.Join(dir, "_meta.json"), meta, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".json"), profile, 0o600); err != nil {
		t.Fatal(err)
	}
	if key := ReadAPIKey(dir); key != "sk-old" {
		t.Fatalf("ReadAPIKey = %q", key)
	}
	if path, wired := Applied(dir, "https://deeprouter.top"); path != filepath.Join(dir, id+".json") || wired {
		t.Fatalf("Applied = %s %v", path, wired)
	}
	files, err := Plan(dir, Update{BaseURL: "https://deeprouter.top", APIKey: "sk-new"})
	if err != nil {
		t.Fatal(err)
	}
	if files.ProfilePath != filepath.Join(dir, id+".json") {
		t.Fatal(files.ProfilePath)
	}
	var doc map[string]any
	if err := json.Unmarshal(files.Profile, &doc); err != nil {
		t.Fatalf("output is not plain JSON: %v\n%s", err, files.Profile)
	}
	if doc["inferenceGatewayApiKey"] != "sk-new" || doc["keepMe"] != true {
		t.Fatalf("profile: %s", files.Profile)
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

func TestClearModelKeepsAListTheUserWrote(t *testing.T) {
	for _, tc := range []struct {
		models    string
		wantList  int
		discovery bool
	}{
		{`[{"name": "claude-sonnet-4-6"}]`, 0, true},
		{`[{"name": "claude-sonnet-4-6"}, {"name": "claude-opus-4-6"}]`, 2, false},
	} {
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "mine.json"), []byte(`{"inferenceProvider": "gateway", "inferenceModels": `+tc.models+`}`), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "_meta.json"), []byte(`{"appliedId": "mine", "entries": [{"id": "mine", "name": "Mine"}]}`), 0o600); err != nil {
			t.Fatal(err)
		}
		files, err := Plan(dir, Update{BaseURL: "https://deeprouter.top", APIKey: "sk-desktop-key", ClearModel: true})
		if err != nil {
			t.Fatal(err)
		}
		var profile map[string]any
		if err := json.Unmarshal(files.Profile, &profile); err != nil {
			t.Fatal(err)
		}
		list, _ := profile["inferenceModels"].([]any)
		_, discovery := profile["modelDiscoveryEnabled"]
		if len(list) != tc.wantList || discovery != tc.discovery {
			t.Errorf("%s: models %#v, modelDiscoveryEnabled %#v", tc.models, profile["inferenceModels"], profile["modelDiscoveryEnabled"])
		}
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

func TestPlanRenamesPresetAndKeepsCustomName(t *testing.T) {
	dir := t.TempDir()
	first, err := Plan(dir, Update{BaseURL: "https://deeprouter.top", APIKey: "sk-desktop-key", Name: "DeepRouter"})
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
	next, err := Plan(dir, Update{BaseURL: "https://wolfai.top", APIKey: "sk-desktop-key", Name: "WolfAI"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(next.Meta), `"name": "WolfAI"`) || strings.Contains(string(next.Meta), `"name": "DeepRouter"`) {
		t.Fatalf("meta:\n%s", next.Meta)
	}
	var profile map[string]any
	if err := json.Unmarshal(next.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["inferenceProvider"] != "gateway" {
		t.Fatalf("inference provider %#v", profile["inferenceProvider"])
	}
	if err := os.WriteFile(next.ProfilePath, next.Profile, 0o600); err != nil {
		t.Fatal(err)
	}
	meta := strings.Replace(string(next.Meta), `"name": "WolfAI"`, `"name": "Personal"`, 1)
	if err := os.WriteFile(next.MetaPath, []byte(meta), 0o600); err != nil {
		t.Fatal(err)
	}
	kept, err := Plan(dir, Update{BaseURL: "https://www.linkmesh.org", APIKey: "sk-desktop-key", Name: "LinkMesh"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(kept.Meta), `"name": "Personal"`) || strings.Contains(string(kept.Meta), `"name": "LinkMesh"`) {
		t.Fatalf("custom name replaced:\n%s", kept.Meta)
	}
	if err := json.Unmarshal(kept.Profile, &profile); err != nil {
		t.Fatal(err)
	}
	if profile["inferenceProvider"] != "gateway" {
		t.Fatalf("inference provider %#v", profile["inferenceProvider"])
	}
}
