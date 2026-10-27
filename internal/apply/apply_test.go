package apply

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/backup"
	"github.com/zfg5118/deeprouter/internal/claudedesktop"
	"github.com/zfg5118/deeprouter/internal/userenv"
)

func TestApplyIdempotentAndRestore(t *testing.T) {
	home := t.TempDir()
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "config.toml")
	rc := filepath.Join(home, ".zshrc")
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(codex), 0o700); err != nil {
		t.Fatal(err)
	}
	originalSettings := []byte("{\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"theme\": \"dark\"\n}\n")
	originalCodex := []byte("# keep\nmodel = \"gpt-old\"\n\n[sandbox]\nmode = \"read-only\"\n")
	originalRC := []byte("export KEEP_ME=1\n")
	if err := os.WriteFile(claude, originalSettings, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(codex, originalCodex, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, originalRC, 0o644); err != nil {
		t.Fatal(err)
	}

	backups := backup.Store{Dir: filepath.Join(home, ".deeprouter", "backups")}
	req := Request{
		ClaudePath:     claude,
		CodexPath:      codex,
		RCFile:         rc,
		Shell:          "zsh",
		BaseURL:        "https://deeprouter.top/v1",
		APIKey:         "sk-test-value",
		Label:          "Officially",
		ClaudeModel:    "claude-sonnet-4-6",
		CodexModel:     "gpt-4.1",
		WireAPI:        "responses",
		TouchClaude:    true,
		TouchCodex:     true,
		TouchShell:     true,
		SetClaudeModel: true,
		SetCodexModel:  true,
		Backups:        backups,
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}

	settings, err := os.ReadFile(claude)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), `"permissions": {"allow": ["Bash"]}`) {
		t.Fatalf("settings lost permissions:\n%s", settings)
	}
	if strings.Count(string(settings), "ANTHROPIC_AUTH_TOKEN") != 1 {
		t.Fatalf("token duplicated:\n%s", settings)
	}
	info, err := os.Stat(claude)
	if err != nil {
		t.Fatal(err)
	}
	if !permOK(info.Mode().Perm(), 0o600) {
		t.Fatalf("settings mode %o", info.Mode().Perm())
	}

	codexText, err := os.ReadFile(codex)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(codexText), "[model_providers.deeprouter]") != 1 {
		t.Fatalf("provider duplicated:\n%s", codexText)
	}
	if !strings.Contains(string(codexText), "mode = \"read-only\"") {
		t.Fatalf("sandbox lost:\n%s", codexText)
	}
	if !strings.Contains(string(codexText), "base_url = \"https://deeprouter.top/v1\"") {
		t.Fatalf("base url:\n%s", codexText)
	}
	if !strings.Contains(string(codexText), `experimental_bearer_token = "sk-test-value"`) {
		t.Fatalf("bearer token missing:\n%s", codexText)
	}
	if strings.Contains(string(codexText), "env_key") {
		t.Fatalf("deeprouter still requires an env var:\n%s", codexText)
	}
	codexInfo, err := os.Stat(codex)
	if err != nil {
		t.Fatal(err)
	}
	if !permOK(codexInfo.Mode().Perm(), 0o600) {
		t.Fatalf("codex mode %o", codexInfo.Mode().Perm())
	}

	rcText, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(rcText), "# >>> deeprouter-setup >>>") != 1 {
		t.Fatalf("shell block duplicated:\n%s", rcText)
	}
	if !strings.Contains(string(rcText), "KEEP_ME=1") {
		t.Fatalf("rc lost user export:\n%s", rcText)
	}
	if !strings.Contains(string(rcText), "DEEPROUTER_API_KEY=") || !strings.Contains(string(rcText), "ANTHROPIC_MODEL=") {
		t.Fatalf("rc missing exports:\n%s", rcText)
	}

	restored, err := backups.Restore("original")
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 3 {
		t.Fatalf("restored %v", restored)
	}
	gotSettings, _ := os.ReadFile(claude)
	gotCodex, _ := os.ReadFile(codex)
	gotRC, _ := os.ReadFile(rc)
	if string(gotSettings) != string(originalSettings) || string(gotCodex) != string(originalCodex) || string(gotRC) != string(originalRC) {
		t.Fatalf("restore mismatch\nsettings:\n%s\ncodex:\n%s\nrc:\n%s", gotSettings, gotCodex, gotRC)
	}
}

func TestApplyCreatesMissingFilesAndRestoreDeletesThem(t *testing.T) {
	home := t.TempDir()
	backups := backup.Store{Dir: filepath.Join(home, "backups")}
	req := Request{
		ClaudePath:     filepath.Join(home, ".claude", "settings.json"),
		CodexPath:      filepath.Join(home, ".codex", "config.toml"),
		RCFile:         filepath.Join(home, ".bashrc"),
		Shell:          "bash",
		BaseURL:        "http://127.0.0.1:9",
		APIKey:         "good-key",
		Label:          "OpenAI",
		TouchClaude:    true,
		TouchCodex:     true,
		TouchShell:     true,
		SetClaudeModel: false,
		SetCodexModel:  false,
		Backups:        backups,
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	if _, err := backups.Restore("original"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{req.ClaudePath, req.CodexPath, req.RCFile} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s still exists (%v)", path, err)
		}
	}
}

func TestApplyWritesDesktopLibrary(t *testing.T) {
	home := t.TempDir()
	desktop := filepath.Join(home, "Claude-3p", "configLibrary")
	backups := backup.Store{Dir: filepath.Join(home, "backups")}
	req := Request{
		ClaudePath:     filepath.Join(home, ".claude", "settings.json"),
		CodexPath:      filepath.Join(home, ".codex", "config.toml"),
		RCFile:         filepath.Join(home, ".zshrc"),
		Shell:          "zsh",
		DesktopDir:     desktop,
		BaseURL:        "https://deeprouter.top/v1",
		APIKey:         "sk-old-key-1111",
		Label:          "Officially",
		ClaudeModel:    "claude-sonnet-4-6",
		CodexModel:     "gpt-5.4",
		TouchClaude:    true,
		TouchCodex:     true,
		TouchShell:     true,
		SetClaudeModel: true,
		SetCodexModel:  true,
		Backups:        backups,
	}
	report, err := Apply(req)
	if err != nil {
		t.Fatal(err)
	}
	if report.DesktopProfile == "" || !strings.HasSuffix(report.DesktopProfile, claudedesktop.ProfileID+".json") {
		t.Fatal(report.DesktopProfile)
	}
	profile := mustReadApply(t, report.DesktopProfile)
	meta := mustReadApply(t, filepath.Join(desktop, "_meta.json"))
	if !strings.Contains(profile, "sk-old-key-1111") || strings.Contains(profile, "/v1") || !strings.Contains(profile, "claude-sonnet-4-6") {
		t.Fatalf("profile:\n%s", profile)
	}
	if strings.Contains(meta, "sk-old-key-1111") || strings.Count(meta, `"appliedId"`) != 1 {
		t.Fatalf("meta:\n%s", meta)
	}
	for _, path := range []string{report.DesktopProfile, filepath.Join(desktop, "_meta.json")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if !permOK(info.Mode().Perm(), 0o600) {
			t.Fatalf("%s mode %o", path, info.Mode().Perm())
		}
	}
	again, err := Apply(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changed) != 0 {
		t.Fatalf("second apply changed %v", again.Changed)
	}

	req.APIKey = "sk-new-key-2222"
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	profile = mustReadApply(t, report.DesktopProfile)
	if strings.Contains(profile, "sk-old-key-1111") || !strings.Contains(profile, "sk-new-key-2222") {
		t.Fatalf("replaced profile:\n%s", profile)
	}

	req.TouchClaude = false
	req.APIKey = "sk-codex-only-3333"
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(mustReadApply(t, report.DesktopProfile), "sk-codex-only-3333") {
		t.Fatal("codex-only apply rewrote the desktop profile")
	}

	restored, err := backups.Restore("original")
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) < 5 {
		t.Fatalf("restored %v", restored)
	}
	if _, err := os.Stat(report.DesktopProfile); !os.IsNotExist(err) {
		t.Fatalf("desktop profile remains: %v", err)
	}
}

func TestApplyUserEnvSkipsShellFile(t *testing.T) {
	home := t.TempDir()
	st := userenv.NewMem()
	if err := st.Set("ANTHROPIC_MODEL", "kept-by-user"); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("export KEEP_ME=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	record := userenv.RecordPath(filepath.Join(home, ".deeprouter"))
	backups := backup.Store{Dir: filepath.Join(home, ".deeprouter", "backups")}
	req := Request{
		ClaudePath:     filepath.Join(home, ".claude", "settings.json"),
		CodexPath:      filepath.Join(home, ".codex", "config.toml"),
		RCFile:         rc,
		Shell:          "zsh",
		UserEnv:        st,
		UserEnvRecord:  record,
		BaseURL:        "https://deeprouter.top",
		APIKey:         "sk-test-value",
		Label:          "Officially",
		ClaudeModel:    "claude-sonnet-4-6",
		CodexModel:     "gpt-5.4",
		TouchClaude:    true,
		TouchCodex:     true,
		TouchShell:     true,
		SetClaudeModel: true,
		SetCodexModel:  true,
		Backups:        backups,
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "export KEEP_ME=1\n" {
		t.Fatalf("shell file changed:\n%s", body)
	}
	got, ok, err := st.Get("ANTHROPIC_AUTH_TOKEN")
	if err != nil || !ok || got != "sk-test-value" {
		t.Fatalf("token %q ok %v err %v", got, ok, err)
	}
	if _, ok, _ := st.Get("DEEPROUTER_API_KEY"); !ok {
		t.Fatal("shared key was not stored")
	}
	rec, err := userenv.Load(record)
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := rec.Get("ANTHROPIC_MODEL"); v != "claude-sonnet-4-6" {
		t.Fatalf("record:\n%s", rec.Text())
	}
	again, err := Apply(req)
	if err != nil {
		t.Fatal(err)
	}
	if len(again.Changed) != 0 {
		t.Fatalf("second apply changed %v", again.Changed)
	}

	req.APIKey = "sk-next-value"
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.Get("DEEPROUTER_API_KEY"); got != "sk-next-value" {
		t.Fatalf("switched key %q", got)
	}
	if err := userenv.Reconcile(st, mustLoad(t, record), true, userenv.Record{}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get("ANTHROPIC_AUTH_TOKEN"); ok {
		t.Fatal("restore left the token")
	}
	model, ok, err := st.Get("ANTHROPIC_MODEL")
	if err != nil || !ok || model != "kept-by-user" {
		t.Fatalf("model %q ok %v", model, ok)
	}
}

func TestApplyGeminiKeepsOtherSettingsAndShellLines(t *testing.T) {
	home := t.TempDir()
	settings := filepath.Join(home, ".gemini", "settings.json")
	rc := filepath.Join(home, ".zshrc")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	original := []byte("{\n  \"ui\": {\"theme\": \"dark\"},\n  \"model\": {\"maxSessionTurns\": 4}\n}\n")
	if err := os.WriteFile(settings, original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(rc, []byte("export KEEP_ME=1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	backups := backup.Store{Dir: filepath.Join(home, ".deeprouter", "backups")}
	req := Request{
		GeminiPath:     settings,
		RCFile:         rc,
		Shell:          "zsh",
		BaseURL:        "https://deeprouter.top/v1",
		APIKey:         "sk-test-value",
		GeminiModel:    "gemini-2.5-pro",
		TouchGemini:    true,
		SetGeminiModel: true,
		Backups:        backups,
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	if _, err := Apply(req); err != nil {
		t.Fatal(err)
	}
	text := mustReadApply(t, settings)
	if !strings.Contains(text, `"theme": "dark"`) || !strings.Contains(text, `"maxSessionTurns": 4`) || !strings.Contains(text, `"selectedType": "gemini-api-key"`) || !strings.Contains(text, `"name": "gemini-2.5-pro"`) {
		t.Fatalf("settings:\n%s", text)
	}
	shell := mustReadApply(t, rc)
	if !strings.Contains(shell, "export KEEP_ME=1") || !strings.Contains(shell, "GOOGLE_GEMINI_BASE_URL='https://deeprouter.top'") || strings.Contains(shell, "/v1") || strings.Contains(shell, "ANTHROPIC_") {
		t.Fatalf("shell:\n%s", shell)
	}
	if !strings.Contains(shell, "GEMINI_API_KEY='sk-test-value'") {
		t.Fatalf("key line:\n%s", shell)
	}
	restored, err := backups.Restore("original")
	if err != nil {
		t.Fatal(err)
	}
	if len(restored) != 2 {
		t.Fatalf("restored %v", restored)
	}
	if mustReadApply(t, settings) != string(original) || mustReadApply(t, rc) != "export KEEP_ME=1\n" {
		t.Fatalf("settings:\n%s\nrc:\n%s", mustReadApply(t, settings), mustReadApply(t, rc))
	}
}

func mustLoad(t *testing.T, path string) userenv.Record {
	t.Helper()
	rec, err := userenv.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	return rec
}

func permOK(got, want os.FileMode) bool {
	if runtime.GOOS != "windows" {
		return got == want
	}
	return got&0o200 != 0
}

func mustReadApply(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
