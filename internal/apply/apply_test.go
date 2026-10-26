package apply

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter-setup/internal/backup"
	"github.com/zfg5118/deeprouter-setup/internal/claudedesktop"
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
	if info.Mode().Perm() != 0o600 {
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
	if codexInfo.Mode().Perm() != 0o600 {
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
		if info.Mode().Perm() != 0o600 {
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

func mustReadApply(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
