package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter-setup/internal/testgate"
)

func TestHelpAndVersion(t *testing.T) {
	out, _, code := run(t, "", nil, "help")
	if code != 0 || !strings.Contains(out, "  setup") || !strings.Contains(out, "restore") {
		t.Fatalf("help code %d\n%s", code, out)
	}
	out, _, code = run(t, "", nil, "version")
	if code != 0 || !strings.Contains(out, "deeprouter-setup 0.1.0") {
		t.Fatalf("version %d %s", code, out)
	}
	_, errOut, code := run(t, "", nil, "nope")
	if code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown %d %s", code, errOut)
	}
}

func TestSetupRequiresKey(t *testing.T) {
	home := t.TempDir()
	_, errOut, code := run(t, home, nil, "setup", "--group", "officially")
	if code != 2 || !strings.Contains(errOut, "API key required") {
		t.Fatalf("code %d\n%s", code, errOut)
	}
}

func TestFlowSetupTestSwitchRestore(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()

	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "config.toml")
	rc := filepath.Join(home, ".zshrc")
	mustMkdir(t, filepath.Dir(claude))
	mustMkdir(t, filepath.Dir(codex))
	originalSettings := "{\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"theme\": \"dark\"\n}\n"
	originalCodex := "# keep\nmodel = \"gpt-old\"\n\n[sandbox]\nmode = \"read-only\"\n"
	originalRC := "export KEEP_ME=1\n"
	mustWrite(t, claude, originalSettings, 0o644)
	mustWrite(t, codex, originalCodex, 0o644)
	mustWrite(t, rc, originalRC, 0o644)

	setupArgs := []string{
		"setup",
		"--base-url", srv.URL,
		"--group", "officially",
		"--api-key", testgate.GoodKey,
		"--claude-model", "claude-sonnet-4-6",
		"--codex-model", "gpt-4.1",
	}
	out, errOut, code := run(t, home, nil, setupArgs...)
	if code != 0 {
		t.Fatalf("setup code %d\n%s\n%s", code, out, errOut)
	}
	assertNoKey(t, out+errOut)
	if !strings.Contains(out, "officially") || !strings.Contains(out, "source ") {
		t.Fatalf("setup output:\n%s", out)
	}

	settings := mustRead(t, claude)
	if !strings.Contains(settings, `"permissions": {"allow": ["Bash"]}`) || !strings.Contains(settings, testgate.GoodKey) {
		t.Fatalf("settings:\n%s", settings)
	}
	if !strings.Contains(settings, srv.URL) || strings.Contains(settings, srv.URL+"/v1\"") {
		t.Fatalf("anthropic base should not keep /v1:\n%s", settings)
	}
	if strings.Count(settings, "ANTHROPIC_AUTH_TOKEN") != 1 || !strings.Contains(settings, "ANTHROPIC_MODEL") {
		t.Fatalf("env keys:\n%s", settings)
	}
	if mode(t, claude) != 0o600 {
		t.Fatalf("settings mode %o", mode(t, claude))
	}

	codexText := mustRead(t, codex)
	if !strings.Contains(codexText, `experimental_bearer_token = "`+testgate.GoodKey+`"`) {
		t.Fatalf("bearer token missing:\n%s", codexText)
	}
	if strings.Contains(codexText, "env_key") {
		t.Fatalf("deeprouter still requires an env var:\n%s", codexText)
	}
	if mode(t, codex) != 0o600 {
		t.Fatalf("codex mode %o", mode(t, codex))
	}
	if strings.Count(codexText, "[model_providers.deeprouter]") != 1 {
		t.Fatalf("provider section:\n%s", codexText)
	}
	if !strings.Contains(codexText, `base_url = "`+srv.URL+`/v1"`) || !strings.Contains(codexText, `mode = "read-only"`) {
		t.Fatalf("codex:\n%s", codexText)
	}
	if !strings.Contains(codexText, `wire_api = "responses"`) {
		t.Fatalf("codex provider:\n%s", codexText)
	}
	if !strings.Contains(out, "Codex key    ready") {
		t.Fatalf("setup did not confirm the codex key:\n%s", out)
	}

	rcText := mustRead(t, rc)
	if strings.Count(rcText, "# >>> deeprouter-setup >>>") != 1 || !strings.Contains(rcText, "KEEP_ME=1") {
		t.Fatalf("rc:\n%s", rcText)
	}
	if !strings.Contains(rcText, "DEEPROUTER_API_KEY=") || !strings.Contains(rcText, "ANTHROPIC_BASE_URL=") {
		t.Fatalf("rc exports:\n%s", rcText)
	}

	out2, _, code := run(t, home, nil, setupArgs...)
	if code != 0 {
		t.Fatal(out2)
	}
	if mustRead(t, claude) != settings || mustRead(t, codex) != codexText || mustRead(t, rc) != rcText {
		t.Fatal("second setup was not idempotent")
	}
	if strings.Count(mustRead(t, rc), "# >>> deeprouter-setup >>>") != 1 {
		t.Fatal("duplicated shell block")
	}
	info, err := os.Stat(filepath.Join(home, ".deeprouter", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("profile mode %o", info.Mode().Perm())
	}

	out, errOut, code = run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 {
		t.Fatalf("test code %d\n%s\n%s", code, out, errOut)
	}
	assertNoKey(t, out+errOut)
	for _, needle := range []string{"200", "ttfb", "total", "Passed.", "/v1/models", "/v1/chat/completions", "/v1/messages"} {
		if !strings.Contains(out, needle) {
			t.Fatalf("missing %q in\n%s", needle, out)
		}
	}

	out, _, code = run(t, home, nil, "models", "--plain")
	if code != 0 {
		t.Fatal(out)
	}
	if !strings.Contains(out, "gpt-4.1") || !strings.Contains(out, "claude-sonnet-4-6") {
		t.Fatalf("models:\n%s", out)
	}

	out, errOut, code = run(t, home, nil, "profile", "add", "azure", "--label", "Azure", "--api-key", testgate.AzureKey, "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("add %d\n%s\n%s", code, out, errOut)
	}
	assertNoKey(t, out+errOut)

	out, errOut, code = run(t, home, nil, "switch", "--group", "azure", "--claude-model", "claude-opus-4-6", "--codex-model", "gpt-4.1")
	if code != 0 {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	assertNoKey(t, out+errOut)
	settings = mustRead(t, claude)
	if !strings.Contains(settings, testgate.AzureKey) || !strings.Contains(settings, "claude-opus-4-6") {
		t.Fatalf("switched settings:\n%s", settings)
	}
	if strings.Contains(settings, testgate.GoodKey) {
		t.Fatal("old key remains in settings")
	}
	codexText = mustRead(t, codex)
	if strings.Contains(codexText, testgate.GoodKey) || !strings.Contains(codexText, testgate.AzureKey) || !strings.Contains(codexText, `model = "gpt-4.1"`) {
		t.Fatalf("switched codex:\n%s", codexText)
	}
	if strings.Contains(codexText, "env_key") {
		t.Fatalf("switched codex still has env_key:\n%s", codexText)
	}
	if strings.Count(mustRead(t, rc), "# >>> deeprouter-setup >>>") != 1 {
		t.Fatal("switch duplicated the shell block")
	}
	if !strings.Contains(mustRead(t, rc), testgate.AzureKey) {
		t.Fatal("shell block did not pick up the new key")
	}

	out, _, code = run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 || !strings.Contains(out, "Passed.") {
		t.Fatalf("switched test %d\n%s", code, out)
	}
	assertNoKey(t, out)

	out, _, code = run(t, home, nil, "status")
	if code != 0 || !strings.Contains(out, "azure") || !strings.Contains(out, "wired") {
		t.Fatalf("status %d\n%s", code, out)
	}
	assertNoKey(t, out)

	out, _, code = run(t, home, nil, "groups")
	if code != 0 || !strings.Contains(out, "Officially") || !strings.Contains(out, "Vertex") {
		t.Fatalf("groups:\n%s", out)
	}

	out, errOut, code = run(t, home, nil, "restore")
	if code != 0 {
		t.Fatalf("restore %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, claude) != originalSettings || mustRead(t, codex) != originalCodex || mustRead(t, rc) != originalRC {
		t.Fatalf("restore mismatch\nsettings:\n%s\ncodex:\n%s\nrc:\n%s", mustRead(t, claude), mustRead(t, codex), mustRead(t, rc))
	}
	if mode(t, claude) != 0o644 {
		t.Fatalf("restored settings mode %o", mode(t, claude))
	}

	// Profiles remain, so a later setup can apply azure again. Uninstall --purge
	// restores (already original) and deletes the profile store.
	out, errOut, code = run(t, home, nil, "uninstall", "--purge")
	if code != 0 {
		t.Fatalf("uninstall %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".deeprouter")); !os.IsNotExist(err) {
		t.Fatalf("config dir remains: %v", err)
	}
	if mustRead(t, claude) != originalSettings {
		t.Fatal("uninstall changed restored settings")
	}
}

func TestProbeHints(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	cases := []struct {
		name  string
		key   string
		model string
		hint  string
	}{
		{"bad key", testgate.BadKey, "gpt-4.1", "401 Bad key"},
		{"quota", testgate.QuotaKey, "gpt-4.1", "403 Quota or permission"},
		{"missing model", testgate.GoodKey, "missing-model", "404 Model or path"},
		{"no channel", testgate.GoodKey, "down-model", "503 No channel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out, errOut, code := run(t, home, nil, "test",
				"--base-url", srv.URL,
				"--api-key", tc.key,
				"--claude-model", tc.model,
				"--codex-model", tc.model,
				"--timeout", "5s",
			)
			if code == 0 {
				t.Fatalf("expected failure\n%s", out)
			}
			if !strings.Contains(out, tc.hint) {
				t.Fatalf("missing %q\n%s\n%s", tc.hint, out, errOut)
			}
			if strings.Contains(out+errOut, tc.key) {
				t.Fatal("key leaked in output")
			}
		})
	}
}

func TestNoArgsPrintsHelpWhenNotATerminal(t *testing.T) {
	out, _, code := run(t, t.TempDir(), nil)
	if code != 0 || !strings.Contains(out, "Interactive setup") || !strings.Contains(out, "deeprouter-setup") {
		t.Fatalf("code %d\n%s", code, out)
	}
}

func TestModelsJSONParsesGatewayPayload(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "models", "--json", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--timeout", "5s")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	var ids []string
	if err := json.Unmarshal([]byte(out), &ids); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(ids) != 5 || ids[0] != "claude-fable-5" {
		t.Fatalf("ids %v", ids)
	}
}

func TestSetupAutoPicksModels(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--group", "officially", "--api-key", testgate.GoodKey, "--no-shell")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "Claude model  claude-sonnet-4-6") || !strings.Contains(errOut, "Codex model   gpt-5.4") {
		t.Fatalf("stderr:\n%s", errOut)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(settings, "claude-sonnet-4-6") {
		t.Fatalf("settings:\n%s", settings)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model = "gpt-5.4"`) {
		t.Fatalf("codex:\n%s", codexText)
	}
	out, errOut, code = run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 || !strings.Contains(out, "Passed.") {
		t.Fatalf("test %d\n%s\n%s", code, out, errOut)
	}
	_, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--group", "officially", "--api-key", testgate.GoodKey, "--no-shell", "--claude-model", "claude-fable-5", "--codex-model", "gpt-4.1")
	if code != 0 {
		t.Fatal(code)
	}
	if strings.Contains(errOut, "Fetching models") {
		t.Fatalf("explicit models were fetched:\n%s", errOut)
	}
	settings = mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	codexText = mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(settings, "claude-fable-5") || !strings.Contains(codexText, `model = "gpt-4.1"`) {
		t.Fatalf("explicit models were not kept\n%s\n%s", settings, codexText)
	}
}

func TestWizardAcceptsDefaults(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, "\n"+srv.URL+"\n\n\n")
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	assertNoKey(t, stdout+stderr)
	if !strings.Contains(stderr, "Using DEEPROUTER_API_KEY") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	for _, needle := range []string{"Passed.", "source ~/.zshrc", "Then run:  claude", "Or run:    codex", "Codex key    ready"} {
		if !strings.Contains(stdout, needle) {
			t.Fatalf("missing %q\n%s", needle, stdout)
		}
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(settings, "claude-sonnet-4-6") || !strings.Contains(settings, testgate.GoodKey) {
		t.Fatalf("settings:\n%s", settings)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model = "gpt-5.4"`) || strings.Contains(codexText, `model = "gpt-4.1"`) {
		t.Fatalf("codex:\n%s", codexText)
	}
	if !strings.Contains(codexText, `experimental_bearer_token = "`+testgate.GoodKey+`"`) || strings.Contains(codexText, "env_key") {
		t.Fatalf("codex auth:\n%s", codexText)
	}
	if mode(t, filepath.Join(home, ".codex", "config.toml")) != 0o600 {
		t.Fatalf("codex mode %o", mode(t, filepath.Join(home, ".codex", "config.toml")))
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".zshrc")), "DEEPROUTER_API_KEY=") {
		t.Fatal("shell block was not written")
	}
}

func TestWizardChoosesByNumber(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, "\n"+srv.URL+"\n1\n1\n")
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(settings, "claude-fable-5") || !strings.Contains(codexText, `model = "gpt-4.1"`) {
		t.Fatalf("settings:\n%s\ncodex:\n%s", settings, codexText)
	}
}

func TestStatusShowsCLIVersions(t *testing.T) {
	home := t.TempDir()
	stubCLIs(t, func(name string) (string, error) {
		switch name {
		case "claude":
			return "/usr/local/bin/claude", nil
		case "codex":
			return "", os.ErrNotExist
		default:
			return "", os.ErrNotExist
		}
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		if name == "claude" && len(args) == 1 && args[0] == "--version" {
			_, _ = io.WriteString(stdout, "1.2.3 (Claude Code)\n")
			return nil
		}
		return os.ErrNotExist
	})
	out, errOut, code := run(t, home, nil, "status", "--json")
	if code != 0 {
		t.Fatalf("status %d\n%s\n%s", code, out, errOut)
	}
	var payload struct {
		Profile string `json:"profile"`
		CLIs    struct {
			Claude struct {
				Installed bool   `json:"installed"`
				Path      string `json:"path"`
				Version   string `json:"version"`
			} `json:"claude"`
			Codex struct {
				Installed bool   `json:"installed"`
				Version   string `json:"version"`
			} `json:"codex"`
		} `json:"clis"`
	}
	if err := json.Unmarshal([]byte(out), &payload); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if payload.Profile != "" || !payload.CLIs.Claude.Installed || payload.CLIs.Claude.Version != "1.2.3 (Claude Code)" || payload.CLIs.Claude.Path != "/usr/local/bin/claude" {
		t.Fatalf("payload %+v", payload)
	}
	if payload.CLIs.Codex.Installed || payload.CLIs.Codex.Version != "" {
		t.Fatalf("codex %+v", payload.CLIs.Codex)
	}
	out, _, code = run(t, home, nil, "status")
	if code != 0 || !strings.Contains(out, "not installed") || !strings.Contains(out, "1.2.3") || !strings.Contains(out, "/usr/local/bin/claude") {
		t.Fatalf("status text %d\n%s", code, out)
	}
}

func TestSetupInstallCLIsFlag(t *testing.T) {
	home := t.TempDir()
	var npmArgs []string
	stubCLIs(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		if name == "npm" {
			npmArgs = append([]string{name}, args...)
			_, _ = io.WriteString(stdout, "added 2 packages\n")
			return nil
		}
		return os.ErrNotExist
	})
	args := []string{"setup", "--base-url", "http://127.0.0.1:9", "--api-key", testgate.GoodKey, "--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4", "--no-shell"}
	out, errOut, code := run(t, home, nil, args...)
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if npmArgs != nil {
		t.Fatalf("installed without the flag: %v", npmArgs)
	}
	if strings.Contains(out+errOut, testgate.GoodKey) {
		t.Fatal("setup printed the key")
	}
	out, errOut, code = run(t, home, nil, append(args, "--install-clis")...)
	if code != 0 {
		t.Fatalf("install %d\n%s\n%s", code, out, errOut)
	}
	if strings.Join(npmArgs, " ") != "npm install -g @anthropic-ai/claude-code @openai/codex" {
		t.Fatalf("npm args %v", npmArgs)
	}
	if !strings.Contains(out, "Running: npm install -g @anthropic-ai/claude-code @openai/codex") {
		t.Fatalf("stdout:\n%s", out)
	}
}

func TestSetupInstallCLIsPrintsDocsWithoutNpm(t *testing.T) {
	home := t.TempDir()
	stubCLIs(t, func(name string) (string, error) {
		return "", os.ErrNotExist
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		t.Fatalf("exec %s %v", name, args)
		return nil
	})
	out, errOut, code := run(t, home, nil, "setup", "--base-url", "http://127.0.0.1:9", "--api-key", testgate.GoodKey, "--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4", "--no-shell", "--install-clis")
	if code == 0 {
		t.Fatal("expected install to fail without npm")
	}
	text := out + errOut
	for _, needle := range []string{claudeDocs, codexDocs, "npm install -g @anthropic-ai/claude-code", "npm install -g @openai/codex"} {
		if !strings.Contains(text, needle) {
			t.Fatalf("missing %q\n%s", needle, text)
		}
	}
	if strings.Contains(text, testgate.GoodKey) {
		t.Fatal("printed the key")
	}
}

func TestWizardInstallsCLIsOnlyWhenAnsweredYes(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	var npmArgs []string
	stubCLIs(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		if name == "npm" {
			npmArgs = append([]string{name}, args...)
			return nil
		}
		return os.ErrNotExist
	})
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, "\n"+srv.URL+"\n\n\nn\n")
	if code != 0 {
		t.Fatalf("decline %d\n%s\n%s", code, stdout, stderr)
	}
	if npmArgs != nil {
		t.Fatalf("declined install still ran %v", npmArgs)
	}
	if !strings.Contains(stderr, "Install missing CLIs with npm? [y/N]") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	stdout, stderr, code = runWizard(t, home, testgate.GoodKey, "\n"+srv.URL+"\n\n\ny\n")
	if code != 0 {
		t.Fatalf("accept %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g @anthropic-ai/claude-code @openai/codex" {
		t.Fatalf("npm args %v\n%s", npmArgs, stdout)
	}
}

func TestProfileRemoveRefusesActive(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	_, _, code := run(t, home, nil, "setup", "--group", "openai", "--api-key", testgate.GoodKey, "--no-shell", "--claude-only", "--base-url", srv.URL)
	if code != 0 {
		t.Fatal(code)
	}
	_, errOut, code := run(t, home, nil, "profile", "remove", "openai")
	if code == 0 || !strings.Contains(errOut, "active") {
		t.Fatalf("code %d %s", code, errOut)
	}
}

func runWizard(t *testing.T, home, apiKey, answers string) (string, string, int) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("DEEPROUTER_API_KEY", apiKey)
	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(answers))
	if err != nil {
		t.Fatalf("wizard error: %v\n%s\n%s", err, stdout.String(), stderr.String())
	}
	return stdout.String(), stderr.String(), code
}

func run(t *testing.T, home string, stdin *bytes.Reader, args ...string) (string, string, int) {
	t.Helper()
	if home != "" {
		t.Setenv("HOME", home)
		t.Setenv("USERPROFILE", home)
		t.Setenv("SHELL", "/bin/zsh")
		t.Setenv("CODEX_HOME", "")
		t.Setenv("DEEPROUTER_API_KEY", "")
	}
	var stdout, stderr bytes.Buffer
	var in *bytes.Reader
	if stdin != nil {
		in = stdin
	} else {
		in = bytes.NewReader(nil)
	}
	code := mainIO(args, "0.1.0", &stdout, &stderr, in)
	return stdout.String(), stderr.String(), code
}

func assertNoKey(t *testing.T, text string) {
	t.Helper()
	for _, key := range []string{testgate.GoodKey, testgate.AzureKey, testgate.BadKey, testgate.QuotaKey} {
		if strings.Contains(text, key) {
			t.Fatalf("output contains API key %s\n%s", key, text)
		}
	}
}

func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path, data string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		t.Fatal(err)
	}
}

func mustRead(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func stubCLIs(t *testing.T, look func(string) (string, error), run func(context.Context, io.Writer, io.Writer, string, ...string) error) {
	t.Helper()
	prevLook, prevRun := cliLookPath, cliExec
	cliLookPath = look
	cliExec = run
	t.Cleanup(func() {
		cliLookPath = prevLook
		cliExec = prevRun
	})
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	return info.Mode().Perm()
}
