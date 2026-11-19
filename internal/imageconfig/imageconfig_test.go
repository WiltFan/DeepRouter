package imageconfig

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const fakeKey = "sk-test-imageCfgFAKE0000000000000000"

func TestParseBase(t *testing.T) {
	good := map[string]string{
		"https://deeprouter.top":              "https://deeprouter.top",
		"https://wolfai.top/":                 "https://wolfai.top",
		"https://www.linkmesh.org/v1":         "https://www.linkmesh.org",
		"https://www.linkmesh.org/v1/":        "https://www.linkmesh.org",
		"  https://Img.Example.com:8443/  ":   "https://img.example.com:8443",
		"https://img.example.com/api/v1":      "https://img.example.com/api",
		"https://img.example.com/api/v1beta/": "https://img.example.com/api",
		"https://img.example.com/api":         "https://img.example.com/api",
		"HTTPS://img.example.com":             "https://img.example.com",
	}
	for in, want := range good {
		got, err := ParseBase(in)
		if err != nil || got != want {
			t.Errorf("ParseBase(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	bad := map[string]string{
		"":                                   "empty",
		"http://img.example.com":             "https://",
		"img.example.com":                    "https://",
		"ftp://img.example.com":              "https://",
		"https://user:pw@img.example.com":    "username",
		"https://user@img.example.com":       "username",
		"https://img.example.com/?a=1":       "query",
		"https://img.example.com/#frag":      "query",
		"https://":                           "host",
		"https:///path":                      "host",
		"https://img.example.com:99999999":   "valid URL",
		"https://img.example.com:port":       "valid URL",
		"https://img example.com":            "valid URL",
		"https://img.example.com/a\nb":       "valid URL",
		"https://img.example.com\\@evil.com": "valid URL",
	}
	for in, want := range bad {
		if _, err := ParseBase(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("ParseBase(%q) err %v, want %q", in, err, want)
		}
	}
}

func TestProtocolMigrationAndGeminiConfig(t *testing.T) {
	path := Path(t.TempDir())
	legacy, err := json.Marshal(map[string]any{"version": 1, "base_url": "https://img.example.com/prefix", "model": "gpt-image-2", "api_key": fakeKey})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, legacy, 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := Load(path)
	if err != nil || !ok || cfg.Protocol != ProtocolImages || cfg.Model != "gpt-image-2" || cfg.BaseURL != "https://img.example.com/prefix" {
		t.Fatal("legacy config was not read as Images")
	}
	if current, err := os.ReadFile(path); err != nil || string(current) != string(legacy) {
		t.Fatal("loading changed the legacy config")
	}
	cfg.Protocol = ProtocolGemini
	for _, wrong := range []string{"", "gpt-image-2", "gemini-2.5-pro", "models/gemini-image:generateContent", "models/..", "models/x/y"} {
		cfg.Model = wrong
		if err := Save(path, cfg); err == nil {
			t.Errorf("saved invalid Gemini image ID %q", wrong)
		}
	}
	cfg.Model = "models/gemini-nano-banana-2.1"
	cfg.BaseURL += "/v1beta/"
	if err := Save(path, cfg); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err = Load(path)
	if err != nil || !ok || cfg.Protocol != ProtocolGemini || cfg.Model != "gemini-nano-banana-2.1" || cfg.BaseURL != "https://img.example.com/prefix" {
		t.Fatal("native config did not round-trip")
	}
}

func TestSaveLoadModesAndReplace(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".deeprouter")
	path := Path(dir)
	if _, ok, err := Load(path); ok || err != nil {
		t.Fatalf("missing file: ok=%v err=%v", ok, err)
	}
	if err := Save(path, Config{Provider: "custom", BaseURL: "https://img.example.com/v1/", APIKey: " " + fakeKey + " "}); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := Load(path)
	if err != nil || !ok || cfg.BaseURL != "https://img.example.com" || cfg.APIKey != fakeKey || cfg.Version != 1 || cfg.Provider != "custom" || !cfg.Complete() {
		t.Fatalf("load %+v ok=%v err=%v", cfg, ok, err)
	}
	if runtime.GOOS != "windows" {
		if m := mode(t, path); m != 0o600 {
			t.Fatalf("file mode %o", m)
		}
		if m := mode(t, dir); m != 0o700 {
			t.Fatalf("dir mode %o", m)
		}
	}
	var raw map[string]any
	b, _ := os.ReadFile(path)
	if err := json.Unmarshal(b, &raw); err != nil || raw["base_url"] != "https://img.example.com" || raw["api_key"] != fakeKey {
		t.Fatalf("file %s", b)
	}
	if err := Save(path, Config{Provider: "wolfai", BaseURL: "https://wolfai.top", APIKey: "sk-test-second0000000000000"}); err != nil {
		t.Fatal(err)
	}
	cfg, _, _ = Load(path)
	if cfg.Provider != "wolfai" || cfg.APIKey != "sk-test-second0000000000000" {
		t.Fatalf("replace %+v", cfg)
	}
	assertNoTemp(t, dir)
}

func TestSaveRejectsBadInputWithoutWriting(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".deeprouter")
	path := Path(dir)
	for _, c := range []Config{
		{BaseURL: "http://img.example.com", APIKey: fakeKey},
		{BaseURL: "https://img.example.com", APIKey: ""},
		{BaseURL: "https://img.example.com", APIKey: "a\nb"},
	} {
		if err := Save(path, c); err == nil {
			t.Fatalf("saved %+v", c)
		}
	}
	if _, err := os.Lstat(dir); !os.IsNotExist(err) {
		t.Fatalf("bad input created the folder: %v", err)
	}
}

func TestFailedSaveKeepsPreviousFile(t *testing.T) {
	dir := filepath.Join(t.TempDir(), ".deeprouter")
	path := Path(dir)
	if err := Save(path, Config{Provider: "deeprouter", BaseURL: "https://deeprouter.top", APIKey: fakeKey}); err != nil {
		t.Fatal(err)
	}
	before, _ := os.ReadFile(path)
	boom := errors.New("boom")
	prevW, prevR := writeFile, rename
	t.Cleanup(func() { writeFile, rename = prevW, prevR })

	writeFile = func(f *os.File, b []byte) error { _, _ = f.Write(b[:3]); return boom }
	if err := Save(path, Config{BaseURL: "https://img.example.com", APIKey: "sk-test-new00000000000000"}); !errors.Is(err, boom) {
		t.Fatalf("write failure: %v", err)
	}
	writeFile = prevW
	rename = func(string, string) error { return boom }
	if err := Save(path, Config{BaseURL: "https://img.example.com", APIKey: "sk-test-new00000000000000"}); !errors.Is(err, boom) {
		t.Fatalf("rename failure: %v", err)
	}
	after, _ := os.ReadFile(path)
	if string(after) != string(before) {
		t.Fatalf("previous file changed:\n%s\n%s", before, after)
	}
	assertNoTemp(t, dir)
}

func TestSymlinksRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	root := t.TempDir()
	outside := filepath.Join(root, "outside.json")
	if err := os.WriteFile(outside, []byte(`{"base_url":"https://x.example","api_key":"sk-test-outside000000"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, ".deeprouter")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	path := Path(dir)
	if err := os.Symlink(outside, path); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("load through link: %v", err)
	}
	if err := Save(path, Config{BaseURL: "https://img.example.com", APIKey: fakeKey}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("save through link: %v", err)
	}
	b, _ := os.ReadFile(outside)
	if strings.Contains(string(b), fakeKey) {
		t.Fatal("wrote through the link")
	}

	// A linked config folder is refused too.
	real := filepath.Join(root, "real")
	if err := os.Mkdir(real, 0o700); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(root, "home")
	if err := os.Mkdir(home, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(home, ".deeprouter")); err != nil {
		t.Fatal(err)
	}
	if err := Save(Path(filepath.Join(home, ".deeprouter")), Config{BaseURL: "https://img.example.com", APIKey: fakeKey}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("save through linked folder: %v", err)
	}
	if entries, _ := os.ReadDir(real); len(entries) != 0 {
		t.Fatalf("wrote into the linked folder: %v", entries)
	}
}

func TestLoadRejectsBadFiles(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(path); err == nil {
		t.Fatal("bad JSON loaded")
	}
	if err := os.WriteFile(path, []byte(`{"base_url":"https://x.example"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg, ok, err := Load(path)
	if err != nil || !ok || cfg.Complete() {
		t.Fatalf("incomplete file: %+v ok=%v err=%v", cfg, ok, err)
	}
	if err := os.WriteFile(path, []byte(`{"base_url":"http://x.example","api_key":"`+fakeKey+`"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if cfg, _, _ := Load(path); cfg.Complete() {
		t.Fatal("http base counted as complete")
	}
}

func TestHost(t *testing.T) {
	if h := Host("https://img.example.com:8443/api"); h != "img.example.com:8443" {
		t.Fatal(h)
	}
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	info, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}

func assertNoTemp(t *testing.T, dir string) {
	t.Helper()
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".tmp-") {
			t.Fatalf("temporary file left: %s", e.Name())
		}
	}
}

func TestParseModel(t *testing.T) {
	for in, want := range map[string]string{
		"gpt-image-2.6-flare":        "gpt-image-2.6-flare",
		"  grok-3-image ":            "grok-3-image",
		"vendor/flux-kontext-pro":    "vendor/flux-kontext-pro",
		"doubao-seedream-5-0-260128": "doubao-seedream-5-0-260128",
		"模型-1":                       "模型-1",
		"gemini-3-pro-image-preview": "gemini-3-pro-image-preview",
		"my.model_v2:latest":         "my.model_v2:latest",
	} {
		if got, err := ParseModel(in); err != nil || got != want {
			t.Errorf("ParseModel(%q) = %q, %v", in, got, err)
		}
	}
	for _, in := range []string{"", "   ", "a b", "a\nb", "a\tb", "a\x00b", "a\\b", "/etc/passwd", "../x", "a/../b",
		"https://evil.example", strings.Repeat("x", 201), "\xff\xfe"} {
		if _, err := ParseModel(in); err == nil {
			t.Errorf("ParseModel(%q) accepted", in)
		}
	}
}

func TestSaveModel(t *testing.T) {
	path := Path(filepath.Join(t.TempDir(), ".deeprouter"))
	if err := Save(path, Config{Provider: "deeprouter", BaseURL: "https://deeprouter.top", APIKey: fakeKey, Model: " grok-3-image "}); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := Load(path)
	if err != nil || cfg.Model != "grok-3-image" {
		t.Fatalf("%+v %v", cfg, err)
	}
	before, _ := os.ReadFile(path)
	if err := Save(path, Config{Provider: "deeprouter", BaseURL: "https://deeprouter.top", APIKey: fakeKey, Model: "a\nb"}); err == nil {
		t.Fatal("saved a bad model id")
	}
	if after, _ := os.ReadFile(path); string(after) != string(before) {
		t.Fatal("a refused save changed the file")
	}
	// No model: the field is left out and the Skill uses DefaultModel.
	if err := Save(path, Config{Provider: "deeprouter", BaseURL: "https://deeprouter.top", APIKey: fakeKey}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(path); strings.Contains(string(b), `"model"`) {
		t.Fatalf("empty model written: %s", b)
	}
	if DefaultModel != "gpt-image-2.6-flare" {
		t.Fatal(DefaultModel)
	}
}
