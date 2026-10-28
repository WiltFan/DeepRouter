package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/claudedesktop"
	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/provider"
	"github.com/zfg5118/deeprouter/internal/testgate"
	"github.com/zfg5118/deeprouter/internal/textutil"
	"github.com/zfg5118/deeprouter/internal/userenv"
)

func TestMain(m *testing.M) {
	// Wizard tests answer a single y/n. A live npmjs.org dial or a China
	// locale would insert another prompt and consume that answer.
	npmRegistryUnreachable = func() bool { return false }
	npmGlobalPrefix = func() (string, error) { return "", fmt.Errorf("npm prefix stub") }
	resetNpmPrefixCache()
	_ = os.Setenv("TZ", "UTC")
	_ = os.Setenv("LANG", "en_US.UTF-8")
	_ = os.Setenv("LC_ALL", "en_US.UTF-8")
	_ = os.Setenv("LC_CTYPE", "en_US.UTF-8")
	_ = os.Setenv("DEEPROUTER_PROVIDER", "")
	os.Exit(m.Run())
}

func TestPromptModelOmitsNonChat(t *testing.T) {
	catalog, err := gateway.ParseModels([]byte(`{"data":[
		{"id":"grok-2-image"},
		{"id":"dall-e-3"},
		{"id":"grok-flare"},
		{"id":"grok-sunburst"},
		{"id":"sora-2"},
		{"id":"nano-banana"},
		{"id":"gpt-4o","type":"image"},
		{"id":"grok-4"},
		{"id":"grok-vision-image","type":"chat"}
	]}`))
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	id, clear, err := promptModel(bufio.NewReader(strings.NewReader("1\n")), &stderr, "Grok Build", "grok", catalog, false, "", "", "")
	if err != nil || !clear || id != "" {
		t.Fatalf("enter id %q clear %v err %v\n%s", id, clear, err, stderr.String())
	}
	stderr.Reset()
	id, clear, err = promptModel(bufio.NewReader(strings.NewReader("2\n")), &stderr, "Grok Build", "grok", catalog, false, "", "", "")
	if err != nil || clear || id != "grok-4" {
		t.Fatalf("id %q clear %v err %v\n%s", id, clear, err, stderr.String())
	}
	text := stderr.String()
	if !strings.Contains(text, "  1) "+clientDefaultModelEN) || !strings.Contains(text, "  2) grok-4") {
		t.Fatalf("menu:\n%s", text)
	}
	for _, absent := range []string{"grok-2-image", "dall-e-3", "grok-flare", "grok-sunburst", "sora-2", "nano-banana", "gpt-4o"} {
		if strings.Contains(text, absent) {
			t.Fatalf("listed %s\n%s", absent, text)
		}
	}
	if !strings.Contains(text, "grok-4") || !strings.Contains(text, "grok-vision-image") {
		t.Fatalf("menu:\n%s", text)
	}

	fallback, err := gateway.ParseModels([]byte(`{"data":[{"id":"grok-2-image"},{"id":"grok-flare"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	stderr.Reset()
	id, clear, err = promptModel(bufio.NewReader(strings.NewReader("2\n")), &stderr, "Grok Build", "grok", fallback, false, "", "", "")
	if err != nil || clear || id != "grok-2-image" {
		t.Fatalf("fallback id %q clear %v err %v\n%s", id, clear, err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "grok-flare") {
		t.Fatalf("fallback menu:\n%s", stderr.String())
	}
}

// wizardCustom is the wizard's new order: programs first, then the custom
// gateway, which is always the last platform for those programs.
func wizardCustom(programs, rest string) string {
	return wizardPlatform(programs, provider.CustomID, rest)
}

func wizardPlatform(programs, platformID, rest string) string {
	line := "\n"
	if programs != "" {
		line = programs + "\n"
	}
	return line + platformChoice(programs, platformID) + "\n" + rest
}

func platformChoice(programs, platformID string) string {
	ids, err := apps.ParseIndexed(programs, apps.Catalog(), nil)
	if err != nil {
		panic(err)
	}
	list := provider.ForClients(ids)
	if platformID == provider.CustomID {
		return strconv.Itoa(len(list) + 1)
	}
	want := platformID
	if p, ok := provider.ByID(platformID); ok {
		want = p.ID
	}
	for i, p := range list {
		if p.ID == want {
			return strconv.Itoa(i + 1)
		}
	}
	panic("platform " + platformID + " is not offered for " + programs)
}

func TestWizardReusesEnteredKeyAndCanCancel(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")

	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(wizardCustom("1", srv.URL+"\n"+testgate.GoodKey+"\n\nn\n")))
	if err != nil || code != 0 {
		t.Fatalf("write %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	text := stderr.String()
	if strings.Contains(text, "Continue?") || strings.Contains(text, "继续写入") {
		t.Fatalf("confirm prompt returned:\n%s", text)
	}
	if !strings.Contains(text, "These files will be modified:") || !strings.Contains(text, "config.toml") || !strings.Contains(text, "deeprouter restore") {
		t.Fatalf("summary:\n%s", text)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex", "config.toml")); statErr != nil {
		t.Fatalf("setup did not write codex config: %v", statErr)
	}

	home = t.TempDir()
	isolateHome(t, home)
	stdout.Reset()
	stderr.Reset()
	code, err = cmdWizard(&stdout, &stderr, bytes.NewBufferString(wizardCustom("1,2", srv.URL+"\n"+testgate.GoodKey+"\n\n\n\n\n")))
	if err != nil || code != 0 {
		t.Fatalf("reuse %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	text = stderr.String()
	if !strings.Contains(text, "Claude Code token (Enter to reuse") || !strings.Contains(text, "Reused (") {
		t.Fatalf("reuse prompt:\n%s", text)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), testgate.GoodKey) {
		t.Fatal("codex key missing")
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".claude", "settings.json")), testgate.GoodKey) {
		t.Fatal("claude did not reuse the key")
	}
}

func TestEnterSelectsCodexOnlyEvenWhenOthersAreInstalled(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	stubCLIs(t, func(name string) (string, error) {
		switch name {
		case "codex", "claude", "npm":
			return "/usr/bin/" + name, nil
		default:
			return "", os.ErrNotExist
		}
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		return nil
	})
	withTerminal(t)
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\n\n")))
	if err != nil || code != 0 {
		t.Fatalf("wizard %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	text := stderr.String()
	if !strings.Contains(text, "Example: 1,2") || !strings.Contains(text, "Programs [1]: ") || !strings.Contains(text, "Press Enter for Codex / ChatGPT.") {
		t.Fatalf("prompt:\n%s", text)
	}
	if strings.Contains(text, "Programs [1,2]") || strings.Contains(text, "already installed") {
		t.Fatalf("installed default leaked into the prompt:\n%s", text)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex", "config.toml")); statErr != nil {
		t.Fatal(statErr)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(statErr) {
		t.Fatalf("Enter wrote Claude Code: %v", statErr)
	}
}

func TestWizardPromptsInChineseWhenLocaleIsZH(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	stubCLIs(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		return nil
	})
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "选择要配置的程序") || !strings.Contains(stderr, "多选示例：1,2") || !strings.Contains(stderr, "程序 [1]: ") || !strings.Contains(stderr, "不指定模型（使用客户端默认）") || !strings.Contains(stderr, "用 npm 安装缺少的命令行工具？[Y/n]") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "已安装的程序") {
		t.Fatalf("installed default leaked into the Chinese prompt:\n%s", stderr)
	}
	if strings.Contains(stderr, "Choose which programs") {
		t.Fatalf("english intro:\n%s", stderr)
	}
	if !strings.Contains(stdout, "然后运行：") || !strings.Contains(stdout, "通过。") {
		t.Fatalf("stdout:\n%s", stdout)
	}
	if strings.Contains(stdout, "Passed.") || strings.Contains(stdout, "Then run:") {
		t.Fatalf("stdout stayed English:\n%s", stdout)
	}
}

func TestChinaNPMEnterDefaultsToYes(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	t.Setenv("TZ", "Asia/Shanghai")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "en_US.UTF-8")
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
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\n\n\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "does not change your saved npm config") || !strings.Contains(stderr, "Install missing CLIs with npm? [Y/n]") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g --registry https://registry.npmmirror.com @openai/codex" {
		t.Fatalf("npm args %v", npmArgs)
	}
}

func TestSetupHelpNamesChatGPTDesktop(t *testing.T) {
	out, errOut, code := run(t, "", nil, "setup", "--help")
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	text := out + errOut
	if strings.Contains(text, "ChatGPT desktop app") || strings.Contains(text, "Codex desktop app") || !strings.Contains(text, "Codex / ChatGPT") {
		t.Fatalf("help:\n%s", text)
	}
	if !strings.Contains(text, "--codex-model") || !strings.Contains(text, "--codex-only") || !strings.Contains(text, "--codex-key") {
		t.Fatalf("codex flags:\n%s", text)
	}
}

func TestHelpAndVersion(t *testing.T) {
	out, _, code := run(t, "", nil, "help")
	if code != 0 || !strings.Contains(out, "  setup") || !strings.Contains(out, "restore") || !strings.Contains(out, "Enter keeps a token") {
		t.Fatalf("help code %d\n%s", code, out)
	}
	out, _, code = run(t, "", nil, "version")
	if code != 0 || !strings.Contains(out, "deeprouter 0.1.0") {
		t.Fatalf("version %d %s", code, out)
	}
	_, errOut, code := run(t, "", nil, "nope")
	if code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("unknown %d %s", code, errOut)
	}
}

func TestSetupRequiresKey(t *testing.T) {
	home := t.TempDir()
	_, errOut, code := run(t, home, nil, "setup")
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
		"--profile", "default",
		"--api-key", testgate.GoodKey,
		"--apps", "claude-code,claude-desktop,codex",
		"--claude-model", "claude-sonnet-4-6",
		"--codex-model", "gpt-4.1",
	}
	out, errOut, code := run(t, home, nil, setupArgs...)
	if code != 0 {
		t.Fatalf("setup code %d\n%s\n%s", code, out, errOut)
	}
	assertNoKey(t, out+errOut)
	if !strings.Contains(out, "default") || !strings.Contains(out, "Claude Desktop") || strings.Contains(out, shellFollowupNeedle()) {
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
	assertPerm(t, claude, 0o600)

	codexText := mustRead(t, codex)
	if !strings.Contains(codexText, `experimental_bearer_token = "`+testgate.GoodKey+`"`) {
		t.Fatalf("bearer token missing:\n%s", codexText)
	}
	if strings.Contains(codexText, "env_key") {
		t.Fatalf("deeprouter still requires an env var:\n%s", codexText)
	}
	assertPerm(t, codex, 0o600)
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

	rcText := readShellRecord(t, home)
	if strings.Contains(rcText, "DEEPROUTER_API_KEY=") {
		t.Fatalf("setup wrote the shell key:\n%s", rcText)
	}
	if !useUserEnv() && rcText != originalRC {
		t.Fatalf("setup edited the shell:\n%s", rcText)
	}

	out2, _, code := run(t, home, nil, setupArgs...)
	if code != 0 {
		t.Fatal(out2)
	}
	if mustRead(t, claude) != settings || mustRead(t, codex) != codexText || readShellRecord(t, home) != rcText {
		t.Fatal("second setup was not idempotent")
	}
	info, err := os.Stat(filepath.Join(home, ".deeprouter", "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !permMatches(info.Mode().Perm(), 0o600) {
		t.Fatalf("profile mode %o", info.Mode().Perm())
	}

	out, errOut, code = run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 {
		t.Fatalf("test code %d\n%s\n%s", code, out, errOut)
	}
	assertNoKey(t, out+errOut)
	for _, needle := range []string{"200", "ttfb", "total", "Passed.", "/v1/models", "/v1/responses", "/v1/messages"} {
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

	out, errOut, code = run(t, home, nil, "switch", "--profile", "azure", "--claude-model", "claude-opus-4-6", "--codex-model", "gpt-4.1")
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
	switchedShell := readShellRecord(t, home)
	if !useUserEnv() && switchedShell != originalRC {
		t.Fatalf("switch edited the shell:\n%s", switchedShell)
	}
	if strings.Contains(switchedShell, testgate.AzureKey) {
		t.Fatal("switch wrote the shell key")
	}

	out, _, code = run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 || !strings.Contains(out, "Passed.") {
		t.Fatalf("switched test %d\n%s", code, out)
	}
	assertNoKey(t, out)

	out, _, code = run(t, home, nil, "status")
	if code != 0 || !strings.Contains(out, "azure") || !strings.Contains(out, "wired") || !strings.Contains(out, "Claude Desktop") {
		t.Fatalf("status %d\n%s", code, out)
	}
	assertNoKey(t, out)

	_, errOut, code = run(t, home, nil, "groups")
	if code != 2 || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("groups %d\n%s", code, errOut)
	}

	out, errOut, code = run(t, home, nil, "restore")
	if code != 0 {
		t.Fatalf("restore %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, claude) != originalSettings || mustRead(t, codex) != originalCodex || mustRead(t, rc) != originalRC {
		t.Fatalf("restore mismatch\nsettings:\n%s\ncodex:\n%s\nrc:\n%s", mustRead(t, claude), mustRead(t, codex), mustRead(t, rc))
	}
	if useUserEnv() {
		assertUserEnvCleared(t)
	}
	if !permMatches(mode(t, claude), 0o644) {
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

func TestUserEnvSetupRestoresPreviousValues(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEEPROUTER_USER_ENV", "1")
	st := userenv.MemFor(home)
	if err := st.Set("ANTHROPIC_MODEL", "kept-by-user"); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(home, ".zshrc")
	mustWrite(t, rc, "export KEEP_ME=1\n", 0o644)
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, rc) != "export KEEP_ME=1\n" {
		t.Fatalf("startup file changed:\n%s", mustRead(t, rc))
	}
	if strings.Contains(out, `HKCU\Environment`) || strings.Contains(out, "Open a new terminal to pick up the user environment variables.") {
		t.Fatalf("default setup wrote the user environment:\n%s", out)
	}
	if _, ok, err := st.Get("DEEPROUTER_API_KEY"); err != nil || ok {
		t.Fatalf("setup stored DEEPROUTER_API_KEY (ok %v err %v)", ok, err)
	}
	if _, ok, err := st.Get("ANTHROPIC_AUTH_TOKEN"); err != nil || ok {
		t.Fatalf("setup stored ANTHROPIC_AUTH_TOKEN (ok %v err %v)", ok, err)
	}
	out, errOut, code = run(t, home, nil, "restore")
	if code != 0 {
		t.Fatalf("restore %d\n%s\n%s", code, out, errOut)
	}
	if _, ok, err := st.Get("ANTHROPIC_AUTH_TOKEN"); err != nil || ok {
		t.Fatalf("restore left the token (ok %v err %v)", ok, err)
	}
	if _, ok, err := st.Get("DEEPROUTER_API_KEY"); err != nil || ok {
		t.Fatalf("restore left the shared key (ok %v err %v)", ok, err)
	}
	model, ok, err := st.Get("ANTHROPIC_MODEL")
	if err != nil || !ok || model != "kept-by-user" {
		t.Fatalf("model %q ok %v err %v", model, ok, err)
	}
	if mustRead(t, rc) != "export KEEP_ME=1\n" {
		t.Fatal("restore edited the startup file")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("created settings remained: %v", err)
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

func TestNoArgsNonTTYWithoutAKeyDoesNotPrintHelp(t *testing.T) {
	out, errOut, code := run(t, t.TempDir(), bytes.NewReader([]byte("https://evil.example\n")))
	if code != 2 || !strings.Contains(errOut, "Run deeprouter in a terminal.") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(out, "Interactive setup") || strings.Contains(errOut, "Programs [") || strings.Contains(errOut, "evil.example") {
		t.Fatalf("non-tty printed help, the picker, or consumed the pipe\n%s\n%s", out, errOut)
	}
	if !strings.Contains(errOut, "DEEPROUTER_API_KEY") || !strings.Contains(errOut, "--api-key-stdin") || strings.Contains(errOut, "--api-key <") {
		t.Fatalf("non-tty hint:\n%s", errOut)
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
	if len(ids) != 7 || ids[0] != "claude-fable-5" || ids[5] != "grok-4" || ids[6] != "gemini-2.5-pro" {
		t.Fatalf("ids %v", ids)
	}
}

func TestSetupAutoPicksModels(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "claude-code,codex", "--no-shell")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "Checking the token at "+srv.URL) {
		t.Fatalf("stderr:\n%s", errOut)
	}
	if !strings.Contains(out, "settings.json   model client default") || !strings.Contains(out, "config.toml   model client default") {
		t.Fatalf("stdout:\n%s", out)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	if strings.Contains(settings, "claude-sonnet-4-6") || strings.Contains(settings, "ANTHROPIC_MODEL") {
		t.Fatalf("settings wrote a model:\n%s", settings)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(codexText, "model = ") {
		t.Fatalf("codex wrote a model:\n%s", codexText)
	}
	out, errOut, code = run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 || !strings.Contains(out, "Passed.") {
		t.Fatalf("test %d\n%s\n%s", code, out, errOut)
	}
	_, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "claude-code,codex", "--no-shell", "--claude-model", "claude-fable-5", "--codex-model", "gpt-4.1")
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

func TestNoArgsNonTTYRunsCodexWizard(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", testgate.GoodKey)
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	var stdout, stderr bytes.Buffer
	code := mainIO(nil, "0.1.0", &stdout, &stderr, bytes.NewBufferString(srv.URL+"\n\n\nn\nn\n"))
	if code != 2 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	errText := stderr.String()
	if !strings.Contains(errText, "请在终端中运行 deeprouter。") || !strings.Contains(errText, "deeprouter setup") {
		t.Fatalf("stderr:\n%s", errText)
	}
	if !strings.Contains(errText, "DEEPROUTER_API_KEY") || !strings.Contains(errText, "--api-key-stdin") || strings.Contains(errText, "--api-key <") {
		t.Fatalf("non-tty hint:\n%s", errText)
	}
	if strings.Contains(errText, srv.URL) || strings.Contains(stdout.String(), "通过") {
		t.Fatalf("non-tty consumed the pipe\n%s\n%s", stdout.String(), errText)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("non-tty wrote codex config: %v", err)
	}
}

func TestWizardAcceptsDefaults(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	assertNoKey(t, stdout+stderr)
	if !strings.Contains(stderr, "Programs [1]:") || !strings.Contains(stderr, "Enter the gateway token (") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	for _, needle := range []string{"Passed.", "Then run:  codex", "Codex key    ready", "POST /v1/responses"} {
		if !strings.Contains(stdout, needle) {
			t.Fatalf("missing %q\n%s", needle, stdout)
		}
	}
	if strings.Contains(stdout, shellFollowupNeedle()) {
		t.Fatalf("wizard wrote a shell follow-up:\n%s", stdout)
	}
	if strings.Contains(stdout, "Then run:  claude") || strings.Contains(stdout, "Or run:    codex") {
		t.Fatalf("stdout configured Claude:\n%s", stdout)
	}
	if !strings.Contains(stdout, "GET /v1/models") || strings.Contains(stdout, "POST /v1/messages") {
		t.Fatalf("codex-only probe:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("claude settings: %v", err)
	}
	if _, err := os.Stat(claudedesktop.Dir(home)); !os.IsNotExist(err) {
		t.Fatalf("desktop library: %v", err)
	}
	if !strings.Contains(stdout, "model client default") {
		t.Fatalf("stdout:\n%s", stdout)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(codexText, "model = ") {
		t.Fatalf("enter wrote a model:\n%s", codexText)
	}
	if !strings.Contains(codexText, `experimental_bearer_token = "`+testgate.GoodKey+`"`) || strings.Contains(codexText, "env_key") {
		t.Fatalf("codex auth:\n%s", codexText)
	}
	assertPerm(t, filepath.Join(home, ".codex", "config.toml"), 0o600)
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) && !useUserEnv() {
		t.Fatalf("wizard wrote the shell: %v", err)
	}
}

func TestWizardPromptsToKeepOrReplaceKey(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)

	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, ttyReplaceAnswers(srv.URL, testgate.GoodKey))
	if code != 0 {
		t.Fatalf("keep %d\n%s\n%s", code, stdout, stderr)
	}
	assertNoKey(t, stdout+stderr)
	if offeredKey(stderr, testgate.GoodKey) || strings.Contains(stderr, "Using DEEPROUTER_API_KEY") {
		t.Fatalf("custom gateway reused another provider's key:\n%s", stderr)
	}
	if strings.Contains(stderr, "for Claude Code") || strings.Contains(stderr, "for Claude Desktop") || !strings.Contains(stderr, "for Codex / ChatGPT: ") {
		t.Fatalf("per-program prompts:\n%s", stderr)
	}
	assertProgramKeys(t, home, testgate.GoodKey, testgate.AzureKey)
	assertShellUntouched(t, home)

	stdout, stderr, code = runWizard(t, home, testgate.GoodKey, ttyReplaceAnswers(srv.URL, testgate.AzureKey))
	if code != 0 {
		t.Fatalf("replace %d\n%s\n%s", code, stdout, stderr)
	}
	assertNoKey(t, stdout+stderr)
	if !strings.Contains(stderr, tokenPrompt("Codex / ChatGPT", "custom", srv.URL, testgate.GoodKey, "")) {
		t.Fatalf("stderr:\n%s", stderr)
	}
	assertProgramKeys(t, home, testgate.AzureKey, testgate.GoodKey)

	t.Setenv("DEEPROUTER_API_KEY", "")
	var outBuf, errBuf bytes.Buffer
	code, err := cmdWizard(&outBuf, &errBuf, bytes.NewBufferString(ttyDefaultAnswers(srv.URL)))
	if err != nil || code != 0 {
		t.Fatalf("saved keep %d %v\n%s\n%s", code, err, outBuf.String(), errBuf.String())
	}
	text := outBuf.String() + errBuf.String()
	assertNoKey(t, text)
	if !strings.Contains(errBuf.String(), tokenPrompt("Codex / ChatGPT", "custom", srv.URL, testgate.AzureKey, "")) {
		t.Fatalf("stderr:\n%s", errBuf.String())
	}
	assertProgramKeys(t, home, testgate.AzureKey, testgate.GoodKey)
}

func TestWizardPromptsForKeySavedInShell(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	_, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "claude-code,claude-desktop,codex", "--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4")
	if code != 0 {
		t.Fatal(errOut)
	}
	if err := os.RemoveAll(filepath.Join(home, ".deeprouter")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".claude", "settings.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatal(err)
	}
	withTerminal(t)
	t.Setenv("DEEPROUTER_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(ttyReplaceAnswers(srv.URL, testgate.AzureKey)))
	if err != nil || code != 0 {
		t.Fatalf("wizard %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	if offeredKey(stderr.String(), testgate.GoodKey) {
		t.Fatalf("custom gateway reused the shell key:\n%s", stderr.String())
	}
	assertNoKey(t, stdout.String()+stderr.String())
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	saved := mustRead(t, filepath.Join(home, ".deeprouter", "config.json"))
	if !strings.Contains(codexText, testgate.AzureKey) || strings.Contains(codexText, testgate.GoodKey) {
		t.Fatalf("codex:\n%s", codexText)
	}
	if !strings.Contains(saved, testgate.AzureKey) || strings.Contains(saved, testgate.GoodKey) {
		t.Fatalf("config:\n%s", saved)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("wizard recreated claude settings: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) && !useUserEnv() {
		t.Fatalf("setup or the wizard wrote a shell file: %v", err)
	}
	desktop := filepath.Join(claudedesktop.Dir(home), claudedesktop.ProfileID+".json")
	if !strings.Contains(mustRead(t, desktop), testgate.GoodKey) || strings.Contains(mustRead(t, desktop), testgate.AzureKey) {
		t.Fatal("interactive wizard changed the desktop profile written by setup")
	}

	stdinIsTerminal = func(io.Reader) bool { return false }
	var out2, err2 bytes.Buffer
	code, err = cmdWizard(&out2, &err2, bytes.NewBufferString(srv.URL+"\n"))
	if err != nil || code != 2 || !strings.Contains(err2.String(), "Run deeprouter in a terminal.") {
		t.Fatalf("non-tty %d %v\n%s\n%s", code, err, out2.String(), err2.String())
	}
	codexText = mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, testgate.AzureKey) || strings.Contains(codexText, `model_provider = "deeprouter"`) {
		t.Fatalf("non-tty changed codex:\n%s", codexText)
	}
}

func TestWizardNonTTYUsesSavedKey(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	_, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--codex-model", "gpt-5.4")
	if code != 0 {
		t.Fatal(errOut)
	}
	t.Setenv("DEEPROUTER_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(srv.URL+"\n"))
	if err != nil || code != 2 || !strings.Contains(stderr.String(), "Run deeprouter in a terminal.") {
		t.Fatalf("wizard %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "Programs [") {
		t.Fatalf("non-interactive wizard prompted:\n%s", stderr.String())
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, testgate.GoodKey) {
		t.Fatalf("codex:\n%s", codexText)
	}
}

func TestWizardTTYRejectsEmptyKey(t *testing.T) {
	home := t.TempDir()
	withTerminal(t)
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString("\n\n\n"))
	if err != nil || code != 2 {
		t.Fatalf("code %d err %v\n%s", code, err, stderr.String())
	}
	text := stderr.String()
	prompt := "Enter the DeepRouter token (create one at deeprouter.top) for Codex / ChatGPT: "
	if strings.Count(text, prompt) != 2 || !strings.Contains(text, "The token is empty. Enter it again.") || !strings.HasSuffix(text, "Setup cancelled. Nothing was written.\n") {
		t.Fatalf("stderr:\n%s", text)
	}
}

func TestSetupReplacesKeyWithoutDuplicatingShell(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	args := func(key string) []string {
		return []string{"setup", "--base-url", srv.URL, "--api-key", key, "--codex-model", "gpt-5.4"}
	}
	if _, errOut, code := run(t, home, nil, args(testgate.GoodKey)...); code != 0 {
		t.Fatal(errOut)
	}
	if _, errOut, code := run(t, home, nil, args(testgate.AzureKey)...); code != 0 {
		t.Fatal(errOut)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, testgate.AzureKey) || strings.Contains(codexText, testgate.GoodKey) {
		t.Fatalf("codex:\n%s", codexText)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) && !useUserEnv() {
		t.Fatalf("setup wrote the shell: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("default setup wrote claude: %v", err)
	}
}

func TestSetupUsesEnvWithoutPromptOnTTY(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", testgate.GoodKey)
	var stdout, stderr bytes.Buffer
	code := mainIO([]string{
		"setup", "--base-url", srv.URL,
		"--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4",
	}, "0.1.0", &stdout, &stderr, bytes.NewReader(nil))
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	if strings.Contains(stderr.String(), "API key") || strings.Contains(stderr.String(), "Enter the ") || strings.Contains(stderr.String(), "令牌") {
		t.Fatalf("setup prompted:\n%s", stderr.String())
	}
	assertNoKey(t, stdout.String()+stderr.String())
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), testgate.GoodKey) {
		t.Fatal("env key was not saved")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("default setup wrote claude: %v", err)
	}
}

func TestSetupPromptsOnTTYWhenNoKey(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	out, errOut, code := run(t, home, bytes.NewReader([]byte(testgate.GoodKey+"\n")),
		"setup", "--base-url", srv.URL,
		"--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4", "--no-shell")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "Enter the DeepRouter token (create one at deeprouter.top): ") || strings.Contains(errOut, "deeprouter.top) [") {
		t.Fatalf("prompt:\n%s", errOut)
	}
	assertNoKey(t, out+errOut)
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), testgate.GoodKey) {
		t.Fatal("prompted key was not saved")
	}
}

func TestPromptSecretLineMasksInput(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	called := false
	prevTerm := stdinIsTerminal
	prevRead := readMasked
	stdinIsTerminal = func(io.Reader) bool { return true }
	readMasked = func(f *os.File, echo io.Writer) (string, error) {
		called = true
		if f != r {
			t.Errorf("masked read used the wrong file")
		}
		for range testgate.AzureKey {
			fmt.Fprint(echo, "*")
		}
		fmt.Fprintln(echo)
		return testgate.AzureKey, nil
	}
	t.Cleanup(func() {
		stdinIsTerminal = prevTerm
		readMasked = prevRead
	})
	var stderr bytes.Buffer
	got, err := promptSecretLine(r, &stderr, bufio.NewReader(r), keyPrompt(testgate.GoodKey))
	if err != nil {
		t.Fatal(err)
	}
	if !called {
		t.Fatal("masked read was not used")
	}
	if got != testgate.AzureKey {
		t.Fatalf("got %q", got)
	}
	text := stderr.String()
	if !strings.Contains(text, keyPrompt(testgate.GoodKey)) {
		t.Fatalf("prompt %q", text)
	}
	if !strings.Contains(text, strings.Repeat("*", len(testgate.AzureKey))) {
		t.Fatalf("stars missing:\n%s", text)
	}
	if !strings.Contains(text, "Entered ("+textutil.MaskConfirm(testgate.AzureKey)+")") {
		t.Fatalf("confirmation %q", text)
	}
	if strings.Contains(text, testgate.AzureKey) || strings.Contains(text, testgate.GoodKey) {
		t.Fatalf("secret written to the prompt:\n%s", text)
	}
	_ = w
}

func TestWizardChoosesByNumber(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n3\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("claude settings: %v", err)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model = "gpt-4.1"`) {
		t.Fatalf("codex:\n%s", codexText)
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
	args := []string{"setup", "--base-url", "http://127.0.0.1:9", "--api-key", testgate.GoodKey, "--apps", "claude-code,codex", "--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4", "--no-shell"}
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
	out, errOut, code := run(t, home, nil, "setup", "--base-url", "http://127.0.0.1:9", "--api-key", testgate.GoodKey, "--apps", "claude-code,codex", "--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4", "--no-shell", "--install-clis")
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
	withTerminal(t)
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("decline %d\n%s\n%s", code, stdout, stderr)
	}
	if npmArgs != nil {
		t.Fatalf("declined install still ran %v", npmArgs)
	}
	if !strings.Contains(stderr, "Install missing CLIs with npm? [y/N]") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "codex is not installed yet. Its configuration is written anyway and works after you install it. The ChatGPT desktop app uses the same config, so N is fine if you only use the desktop app.") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "claude is not installed") {
		t.Fatalf("unselected claude CLI was reported:\n%s", stderr)
	}
	stdout, stderr, code = runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\ny\n"))
	if code != 0 {
		t.Fatalf("accept %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g @openai/codex" {
		t.Fatalf("npm args %v\n%s", npmArgs, stdout)
	}
}

func TestWizardMissingNpmWarnsWithoutAsking(t *testing.T) {
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	// Enter selects Codex. The custom platform keeps the probe on the test
	// server. The trailing Y used to answer the npm question and exit 1
	// after a successful write and connectivity test.
	answers := wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\nY\n")
	stubMissing := func(t *testing.T) {
		t.Helper()
		stubCLIs(t, func(string) (string, error) {
			return "", os.ErrNotExist
		}, func(context.Context, io.Writer, io.Writer, string, ...string) error {
			t.Fatal("npm was executed")
			return nil
		})
	}
	assertOnce := func(t *testing.T, stdout, stderr, phrase, nextMark string) {
		t.Helper()
		text := stdout + stderr
		if strings.Count(text, phrase) != 1 {
			t.Fatalf("%q count %d\nstdout:\n%s\nstderr:\n%s", phrase, strings.Count(text, phrase), stdout, stderr)
		}
		if strings.Contains(stdout, phrase) {
			t.Fatalf("closing steps repeated %q\n%s", phrase, stdout)
		}
		if !strings.Contains(stdout, nextMark) {
			t.Fatalf("stdout:\n%s", stdout)
		}
	}

	t.Run("english", func(t *testing.T) {
		home := t.TempDir()
		stubMissing(t)
		stdout, stderr, code := runWizard(t, home, testgate.GoodKey, answers)
		if code != 0 {
			t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "Passed.") {
			t.Fatalf("probe:\n%s", stdout)
		}
		cfg := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
		if !strings.Contains(cfg, srv.URL) {
			t.Fatalf("config:\n%s", cfg)
		}
		text := stdout + stderr
		for _, absent := range []string{"Install missing CLIs with npm?", "npmmirror", "registry.npmmirror.com"} {
			if strings.Contains(text, absent) {
				t.Fatalf("asked %q\nstdout:\n%s\nstderr:\n%s", absent, stdout, stderr)
			}
		}
		if !strings.Contains(stderr, "npm is not installed. Install Node.js, which includes npm: https://nodejs.org/") {
			t.Fatalf("stderr:\n%s", stderr)
		}
		assertOnce(t, stdout, stderr, "is not installed yet", "\nNext:\n")
		if !strings.Contains(stdout, "Then run:  codex") {
			t.Fatalf("stdout:\n%s", stdout)
		}
	})

	t.Run("chinese", func(t *testing.T) {
		home := t.TempDir()
		stubMissing(t)
		t.Setenv("LC_ALL", "zh_CN.UTF-8")
		t.Setenv("LANG", "zh_CN.UTF-8")
		t.Setenv("TZ", "Asia/Shanghai")
		stdout, stderr, code := runWizard(t, home, testgate.GoodKey, answers)
		if code != 0 {
			t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stdout, "通过。") {
			t.Fatalf("probe:\n%s", stdout)
		}
		text := stdout + stderr
		for _, absent := range []string{"用 npm 安装", "npmmirror", "registry.npmmirror.com", "Install missing CLIs with npm?"} {
			if strings.Contains(text, absent) {
				t.Fatalf("asked %q\nstdout:\n%s\nstderr:\n%s", absent, stdout, stderr)
			}
		}
		if !strings.Contains(stderr, "没有安装 npm。请先安装 Node.js（自带 npm）：https://nodejs.org/") {
			t.Fatalf("stderr:\n%s", stderr)
		}
		assertOnce(t, stdout, stderr, "尚未安装", "\n接下来：")
		if !strings.Contains(stdout, "然后运行：codex") {
			t.Fatalf("stdout:\n%s", stdout)
		}
	})
}

func TestChinaLocale(t *testing.T) {
	cases := []struct {
		name                     string
		tz, lang, lcAll, lcCtype string
		want                     bool
	}{
		{name: "shanghai", tz: "Asia/Shanghai", lang: "C", lcAll: "C", lcCtype: "C", want: true},
		{name: "chongqing", tz: "Asia/Chongqing", lang: "C", lcAll: "C", lcCtype: "C", want: true},
		{name: "harbin", tz: "Asia/Harbin", lang: "C", lcAll: "C", lcCtype: "C", want: true},
		{name: "urumqi", tz: "Asia/Urumqi", lang: "C", lcAll: "C", lcCtype: "C", want: true},
		{name: "kashgar", tz: "Asia/Kashgar", lang: "C", lcAll: "C", lcCtype: "C", want: true},
		{name: "prc", tz: "PRC", lang: "C", lcAll: "C", lcCtype: "C", want: true},
		{name: "lang", tz: "UTC", lang: "zh_CN.UTF-8", lcAll: "C", lcCtype: "C", want: true},
		{name: "lc-all", tz: "UTC", lang: "C", lcAll: "zh-CN.UTF-8", lcCtype: "C", want: true},
		{name: "lc-ctype", tz: "UTC", lang: "C", lcAll: "C", lcCtype: "zh_CN", want: true},
		{name: "english", tz: "UTC", lang: "en_US.UTF-8", lcAll: "C", lcCtype: "C", want: false},
		{name: "bare-cst", tz: "CST", lang: "en_US.UTF-8", lcAll: "C", lcCtype: "C", want: false},
		{name: "chicago", tz: "America/Chicago", lang: "en_US.UTF-8", lcAll: "C", lcCtype: "C", want: false},
		{name: "tokyo", tz: "Asia/Tokyo", lang: "ja_JP.UTF-8", lcAll: "C", lcCtype: "C", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv("TZ", tc.tz)
			t.Setenv("LANG", tc.lang)
			t.Setenv("LC_ALL", tc.lcAll)
			t.Setenv("LC_CTYPE", tc.lcCtype)
			if chinaLocale() != tc.want {
				t.Fatalf("chinaLocale() = %v, want %v", chinaLocale(), tc.want)
			}
		})
	}
}

func TestWizardOffersNPMMirrorWithoutChangingConfig(t *testing.T) {
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
	restoreProbe := npmRegistryUnreachable
	t.Cleanup(func() { npmRegistryUnreachable = restoreProbe })

	run := func(t *testing.T, answers string) (string, string, int) {
		t.Helper()
		npmArgs = nil
		return runWizard(t, home, testgate.GoodKey, wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n"+answers))
	}
	wantHint := "npm config set registry https://registry.npmmirror.com"
	wantAsk := "Use https://registry.npmmirror.com for this install only? [Y/n]"
	wantAskNo := "Use https://registry.npmmirror.com for this install only? [y/N]"

	withTerminal(t)
	t.Setenv("TZ", "Asia/Shanghai")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	stdout, stderr, code := run(t, "y\ny\n")
	if code != 0 {
		t.Fatalf("mirror install %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g --registry https://registry.npmmirror.com @openai/codex" {
		t.Fatalf("npm args %v\n%s", npmArgs, stdout)
	}
	if !strings.Contains(stderr, wantHint) || !strings.Contains(stderr, wantAsk) {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(strings.Join(npmArgs, " "), "config") {
		t.Fatalf("changed npm config: %v", npmArgs)
	}

	stdout, stderr, code = run(t, "y\nn\n")
	if code != 0 {
		t.Fatalf("default registry %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g @openai/codex" {
		t.Fatalf("npm args %v\n%s", npmArgs, stdout)
	}

	stdout, stderr, code = run(t, "n\n")
	if code != 0 {
		t.Fatalf("decline install %d\n%s\n%s", code, stdout, stderr)
	}
	if npmArgs != nil {
		t.Fatalf("declined install still ran %v", npmArgs)
	}

	t.Setenv("TZ", "UTC")
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "en_US.UTF-8")
	npmRegistryUnreachable = func() bool { return true }
	stdout, stderr, code = run(t, "y\ny\n")
	if code != 0 {
		t.Fatalf("unreachable registry %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g --registry https://registry.npmmirror.com @openai/codex" {
		t.Fatalf("npm args %v\n%s", npmArgs, stdout)
	}
	if !strings.Contains(stderr, wantAskNo) {
		t.Fatalf("stderr:\n%s", stderr)
	}

	npmRegistryUnreachable = func() bool { return false }
	t.Setenv("TZ", "CST")
	stdout, stderr, code = run(t, "y\n")
	if code != 0 {
		t.Fatalf("cst %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, wantAsk) {
		t.Fatalf("bare CST offered the npm mirror:\n%s", stderr)
	}
	if strings.Join(npmArgs, " ") != "npm install -g @openai/codex" {
		t.Fatalf("npm args %v\n%s", npmArgs, stdout)
	}
}

func TestSetupInstallCLIsIgnoresNPMMirror(t *testing.T) {
	home := t.TempDir()
	t.Setenv("TZ", "Asia/Shanghai")
	t.Setenv("LANG", "zh_CN.UTF-8")
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
	out, errOut, code := run(t, home, nil, "setup", "--base-url", "http://127.0.0.1:9", "--api-key", testgate.GoodKey, "--apps", "claude-code,codex", "--claude-model", "claude-sonnet-4-6", "--codex-model", "gpt-5.4", "--no-shell", "--install-clis")
	if code != 0 {
		t.Fatalf("install %d\n%s\n%s", code, out, errOut)
	}
	if strings.Join(npmArgs, " ") != "npm install -g @anthropic-ai/claude-code @openai/codex" {
		t.Fatalf("npm args %v", npmArgs)
	}
	if strings.Contains(errOut, "registry.npmmirror.com") || strings.Contains(out, "registry.npmmirror.com") {
		t.Fatalf("non-interactive install offered a registry\n%s\n%s", out, errOut)
	}
}

func TestProfileRemoveRefusesActive(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	_, _, code := run(t, home, nil, "setup", "--profile", "openai", "--api-key", testgate.GoodKey, "--no-shell", "--claude-only", "--base-url", srv.URL)
	if code != 0 {
		t.Fatal(code)
	}
	_, errOut, code := run(t, home, nil, "profile", "remove", "openai")
	if code == 0 || !strings.Contains(errOut, "active") {
		t.Fatalf("code %d %s", code, errOut)
	}
}

func withTerminal(t *testing.T) {
	t.Helper()
	prev := stdinIsTerminal
	stdinIsTerminal = func(io.Reader) bool { return true }
	t.Cleanup(func() { stdinIsTerminal = prev })
}

func TestSetupClaudeOnlyWritesDesktop(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--claude-only", "--claude-model", "claude-sonnet-4-6", "--no-shell")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Claude Desktop") || !strings.Contains(out, "Quit and reopen") {
		t.Fatalf("setup output:\n%s", out)
	}
	profile := filepath.Join(claudedesktop.Dir(home), claudedesktop.ProfileID+".json")
	text := mustRead(t, profile)
	if !strings.Contains(text, testgate.GoodKey) || strings.Contains(text, "/v1") {
		t.Fatalf("profile:\n%s", text)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("codex config exists: %v", err)
	}
	status, errOut, code := run(t, home, nil, "status", "--json")
	if code != 0 {
		t.Fatalf("status %d\n%s", code, errOut)
	}
	var payload struct {
		DesktopPath  string `json:"desktop_path"`
		DesktopWired bool   `json:"desktop_wired"`
	}
	if err := json.Unmarshal([]byte(status), &payload); err != nil {
		t.Fatal(err)
	}
	if !payload.DesktopWired || payload.DesktopPath != profile {
		t.Fatalf("status %+v\n%s", payload, status)
	}
	assertNoKey(t, out+status)
}

func TestSetupCodexOnlySkipsClaudeDesktop(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--codex-only", "--codex-model", "gpt-5.4", "--no-shell")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Stat(claudedesktop.Dir(home)); !os.IsNotExist(err) {
		t.Fatalf("desktop library: %v", err)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), `experimental_bearer_token = "`+testgate.GoodKey+`"`) {
		t.Fatal("codex bearer token missing")
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("claude settings exist: %v", err)
	}
}

func TestSetupAppsWritesOnlyTheSelection(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL,
		"--apps", "claude-code,codex",
		"--claude-code-key", testgate.GoodKey,
		"--codex-key", testgate.AzureKey,
		"--claude-model", "claude-sonnet-4-6",
		"--codex-model", "gpt-5.4")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(settings, testgate.GoodKey) || strings.Contains(settings, testgate.AzureKey) {
		t.Fatalf("claude key:\n%s", settings)
	}
	if !strings.Contains(codexText, testgate.AzureKey) || strings.Contains(codexText, testgate.GoodKey) {
		t.Fatalf("codex key:\n%s", codexText)
	}
	if _, err := os.Stat(claudedesktop.Dir(home)); !os.IsNotExist(err) {
		t.Fatalf("desktop library: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".grok", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("grok config: %v", err)
	}
	assertShellUntouched(t, home)
	assertNoKey(t, out+errOut)

	out, errOut, code = run(t, home, nil, "setup", "--apps", "gemini", "--api-key", testgate.GoodKey, "--gemini-model", "gemini-2.5-pro", "--base-url", srv.URL)
	if code != 0 {
		t.Fatalf("gemini %d\n%s\n%s", code, out, errOut)
	}
	geminiSettings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
	if !strings.Contains(geminiSettings, `"selectedType": "gemini-api-key"`) || !strings.Contains(geminiSettings, `"name": "gemini-2.5-pro"`) {
		t.Fatalf("gemini settings:\n%s", geminiSettings)
	}
	envText := managedShell(t, home)
	if !strings.Contains(envText, "GEMINI_API_KEY=") || !strings.Contains(envText, testgate.GoodKey) || !strings.Contains(envText, "GOOGLE_GEMINI_BASE_URL=") || !strings.Contains(envText, srv.URL) || strings.Contains(envText, srv.URL+"/v1") {
		t.Fatalf("gemini env:\n%s", envText)
	}
	if strings.Contains(geminiSettings, testgate.GoodKey) {
		t.Fatal("gemini settings stored the API key")
	}
	_, errOut, code = run(t, home, nil, "setup", "--apps", "grok", "--claude-only", "--api-key", testgate.GoodKey)
	if code != 2 || !strings.Contains(errOut, "--apps") {
		t.Fatalf("conflict %d\n%s", code, errOut)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	codexPath := filepath.Join(home, ".codex", "config.toml")
	beforeSettings := mustRead(t, settingsPath)
	beforeCodex := mustRead(t, codexPath)
	out, errOut, code = run(t, home, nil, "setup",
		"--base-url", srv.URL,
		"--apps", "claude-desktop",
		"--claude-desktop-key", testgate.AzureKey,
		"--claude-desktop-model", "claude-opus-4-6")
	if code != 0 {
		t.Fatalf("desktop %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, settingsPath) != beforeSettings || mustRead(t, codexPath) != beforeCodex {
		t.Fatal("desktop setup rewrote another program")
	}
	desktop := mustRead(t, filepath.Join(claudedesktop.Dir(home), claudedesktop.ProfileID+".json"))
	if !strings.Contains(desktop, testgate.AzureKey) || !strings.Contains(desktop, "claude-opus-4-6") || strings.Contains(desktop, testgate.GoodKey) || strings.Contains(desktop, "/v1") {
		t.Fatalf("desktop:\n%s", desktop)
	}
}

func TestSetupAppsGrokAndSwitchLeavesOtherFiles(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--apps", "claude-code", "--claude-model", "claude-sonnet-4-6")
	if code != 0 {
		t.Fatalf("claude %d\n%s\n%s", code, out, errOut)
	}
	settingsPath := filepath.Join(home, ".claude", "settings.json")
	settings := strings.Replace(mustRead(t, settingsPath), "claude-sonnet-4-6", "user-picked-model", 1)
	mustWrite(t, settingsPath, settings, 0o600)

	out, errOut, code = run(t, home, nil, "setup",
		"--base-url", srv.URL, "--profile", "azure", "--api-key", testgate.AzureKey,
		"--apps", "grok", "--grok-model", "grok-4")
	if code != 0 {
		t.Fatalf("grok %d\n%s\n%s", code, out, errOut)
	}
	grokPath := filepath.Join(home, ".grok", "config.toml")
	grokText := mustRead(t, grokPath)
	for _, needle := range []string{
		`default = "deeprouter"`,
		`[model.deeprouter]`,
		`model = "grok-4"`,
		`base_url = "` + srv.URL + `/v1"`,
		`api_key = "` + testgate.AzureKey + `"`,
		`api_backend = "chat_completions"`,
	} {
		if !strings.Contains(grokText, needle) {
			t.Fatalf("missing %q\n%s", needle, grokText)
		}
	}
	if strings.Contains(grokText, "env_key") || strings.Contains(grokText, testgate.GoodKey) {
		t.Fatalf("grok:\n%s", grokText)
	}
	assertPerm(t, grokPath, 0o600)
	if mustRead(t, settingsPath) != settings {
		t.Fatal("grok setup rewrote Claude Code settings")
	}
	if !strings.Contains(out, "Grok Build") || !strings.Contains(out, "Start a new Grok Build session") {
		t.Fatalf("setup output:\n%s", out)
	}

	again := grokText
	out, errOut, code = run(t, home, nil, "setup",
		"--base-url", srv.URL, "--profile", "azure", "--api-key", testgate.AzureKey,
		"--apps", "grok", "--grok-model", "grok-4")
	if code != 0 {
		t.Fatalf("repeat %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, grokPath) != again {
		t.Fatal("repeating grok setup changed config.toml")
	}
	if !strings.Contains(out, "Already up to date.") {
		t.Fatalf("stdout:\n%s", out)
	}

	out, errOut, code = run(t, home, nil, "switch", "--profile", "default")
	if code != 0 {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, settingsPath) != settings {
		t.Fatal("switch rewrote Claude Code settings")
	}
	switched := mustRead(t, grokPath)
	if !strings.Contains(switched, testgate.GoodKey) || strings.Contains(switched, testgate.AzureKey) {
		t.Fatalf("grok after switch:\n%s", switched)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("codex was created: %v", err)
	}
	status, errOut, code := run(t, home, nil, "status", "--json")
	if code != 0 {
		t.Fatalf("status %d\n%s", code, errOut)
	}
	if !strings.Contains(status, `"grok_wired": true`) || strings.Contains(status, testgate.GoodKey) {
		t.Fatalf("status:\n%s", status)
	}

	out, errOut, code = run(t, home, nil, "switch", "--grok-model", "grok-4-fast")
	if code != 0 {
		t.Fatalf("model switch %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, grokPath), `model = "grok-4-fast"`) {
		t.Fatalf("grok model:\n%s", mustRead(t, grokPath))
	}
	if mustRead(t, settingsPath) != settings {
		t.Fatal("model switch rewrote Claude Code settings")
	}
}

func TestWizardRePromptsWhenNoProgramIsSelected(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, "9\n"+wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "program 9 is out of range") || strings.Count(stderr, "Programs [1]:") < 2 {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("claude settings: %v", err)
	}
}

func TestSetupGeminiKeyModelAndRestore(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	settings := filepath.Join(home, ".gemini", "settings.json")
	if err := os.MkdirAll(filepath.Dir(settings), 0o700); err != nil {
		t.Fatal(err)
	}
	original := "{\n  \"ui\": {\"theme\": \"dark\"}\n}\n"
	mustWrite(t, settings, original, 0o644)
	stubCLIs(t, func(name string) (string, error) {
		return "", os.ErrNotExist
	}, func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
		return os.ErrNotExist
	})
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL,
		"--apps", "gemini",
		"--api-key", testgate.GoodKey,
		"--gemini-key", testgate.AzureKey,
		"--gemini-model", "gemini-2.5-pro")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Gemini CLI is not installed") || !strings.Contains(out, "npm install -g @google/gemini-cli") {
		t.Fatalf("stdout:\n%s", out)
	}
	if strings.Contains(errOut, "is not installed") {
		t.Fatalf("install notice printed twice:\n%s", errOut)
	}
	text := mustRead(t, settings)
	if !strings.Contains(text, `"theme": "dark"`) || !strings.Contains(text, `"selectedType": "gemini-api-key"`) || !strings.Contains(text, `"name": "gemini-2.5-pro"`) {
		t.Fatalf("settings:\n%s", text)
	}
	if strings.Contains(text, testgate.AzureKey) || strings.Contains(text, testgate.GoodKey) {
		t.Fatal("settings stored a key")
	}
	envText := managedShell(t, home)
	if !strings.Contains(envText, "GEMINI_API_KEY=") || !strings.Contains(envText, testgate.AzureKey) || strings.Contains(envText, testgate.GoodKey) {
		t.Fatalf("env:\n%s", envText)
	}
	if !strings.Contains(envText, "GOOGLE_GEMINI_BASE_URL=") || !strings.Contains(envText, srv.URL) || strings.Contains(envText, srv.URL+"/v1") {
		t.Fatalf("base url:\n%s", envText)
	}
	if !strings.Contains(out, "Gemini CLI") || !strings.Contains(out, "gemini-2.5-pro") {
		t.Fatalf("stdout:\n%s", out)
	}

	out, errOut, code = run(t, home, nil, "setup",
		"--base-url", srv.URL,
		"--apps", "gemini",
		"--api-key", testgate.AzureKey,
		"--gemini-model", "default")
	if code != 0 {
		t.Fatalf("clear %d\n%s\n%s", code, out, errOut)
	}
	text = mustRead(t, settings)
	if strings.Contains(text, "gemini-2.5-pro") || strings.Contains(text, `"name"`) {
		t.Fatalf("model remained:\n%s", text)
	}
	if !strings.Contains(text, `"theme": "dark"`) || !strings.Contains(text, `"selectedType": "gemini-api-key"`) {
		t.Fatalf("cleared settings:\n%s", text)
	}
	if !strings.Contains(out, "client default") {
		t.Fatalf("stdout:\n%s", out)
	}

	out, errOut, code = run(t, home, nil, "switch", "--gemini-model", "gemini-2.5-flash")
	if code != 0 {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, settings), "gemini-2.5-flash") {
		t.Fatalf("switched settings:\n%s", mustRead(t, settings))
	}

	out, errOut, code = run(t, home, nil, "restore")
	if code != 0 {
		t.Fatalf("restore %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, settings) != original {
		t.Fatalf("restored settings:\n%s", mustRead(t, settings))
	}
	if useUserEnv() {
		assertUserEnvCleared(t)
	}
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		if strings.Contains(mustRead(t, filepath.Join(home, ".zshrc")), "GEMINI_API_KEY") {
			t.Fatalf("restore left gemini env:\n%s", mustRead(t, filepath.Join(home, ".zshrc")))
		}
	}
}

func TestGeminiUserEnvRestoresPreviousKey(t *testing.T) {
	home := t.TempDir()
	t.Setenv("DEEPROUTER_USER_ENV", "1")
	st := userenv.MemFor(home)
	if err := st.Set("GEMINI_API_KEY", "sk-old-gemini-key"); err != nil {
		t.Fatal(err)
	}
	rc := filepath.Join(home, ".zshrc")
	mustWrite(t, rc, "export KEEP_ME=1\n", 0o644)
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL,
		"--apps", "gemini",
		"--api-key", testgate.GoodKey,
		"--gemini-model", "default")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, rc) != "export KEEP_ME=1\n" {
		t.Fatalf("startup file changed:\n%s", mustRead(t, rc))
	}
	if got, ok, err := st.Get("GEMINI_API_KEY"); err != nil || !ok || got != testgate.GoodKey {
		t.Fatalf("key %q ok %v err %v", got, ok, err)
	}
	if got, ok, err := st.Get("GOOGLE_GEMINI_BASE_URL"); err != nil || !ok || got != srv.URL {
		t.Fatalf("base %q ok %v err %v", got, ok, err)
	}
	settings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
	if strings.Contains(settings, `"name"`) || !strings.Contains(settings, `"selectedType": "gemini-api-key"`) {
		t.Fatalf("settings:\n%s", settings)
	}
	out, errOut, code = run(t, home, nil, "uninstall")
	if code != 0 {
		t.Fatalf("uninstall %d\n%s\n%s", code, out, errOut)
	}
	if got, ok, err := st.Get("GEMINI_API_KEY"); err != nil || !ok || got != "sk-old-gemini-key" {
		t.Fatalf("restored key %q ok %v err %v", got, ok, err)
	}
	if _, ok, err := st.Get("GOOGLE_GEMINI_BASE_URL"); err != nil || ok {
		t.Fatalf("base remained (ok %v err %v)", ok, err)
	}
	if _, err := os.Stat(filepath.Join(home, ".gemini", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("settings file remained: %v", err)
	}
}

func TestWizardTTYSelectsGrokOnly(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	answers := wizardCustom("4", srv.URL+"\n"+testgate.GoodKey+"\n2\n\n")
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, answers)
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "for Grok Build: ") || offeredKey(stderr, testgate.GoodKey) {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "for Claude Code:") || strings.Contains(stderr, "for Codex / ChatGPT:") {
		t.Fatalf("unselected programs were prompted:\n%s", stderr)
	}
	grokText := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(grokText, testgate.GoodKey) || !strings.Contains(grokText, `model = "grok-4"`) || !strings.Contains(grokText, `api_backend = "chat_completions"`) {
		t.Fatalf("grok:\n%s", grokText)
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "config.toml"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s exists: %v", path, err)
		}
	}
	assertShellUntouched(t, home)
	if _, err := os.Stat(claudedesktop.Dir(home)); !os.IsNotExist(err) {
		t.Fatalf("desktop library: %v", err)
	}
	if !strings.Contains(stdout, "Then run:  grok") || strings.Contains(stdout, "Then run:  claude") {
		t.Fatalf("next steps:\n%s", stdout)
	}

	before := grokText
	t.Setenv("DEEPROUTER_API_KEY", "")
	var outBuf, errBuf bytes.Buffer
	code, err := cmdWizard(&outBuf, &errBuf, bytes.NewBufferString(answers))
	if err != nil || code != 0 {
		t.Fatalf("keep %d %v\n%s\n%s", code, err, outBuf.String(), errBuf.String())
	}
	if mustRead(t, filepath.Join(home, ".grok", "config.toml")) != before {
		t.Fatal("keeping the grok key changed config.toml")
	}

	stdout, stderr, code = runWizard(t, home, testgate.GoodKey, wizardCustom("5,4", srv.URL+"\n"+testgate.GoodKey+"\n2\n"+testgate.GoodKey+"\n2\n\n"))
	if code != 0 {
		t.Fatalf("mixed %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "for Gemini CLI") || !strings.Contains(stderr, "for Grok Build") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	geminiSettings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
	if !strings.Contains(geminiSettings, `"selectedType": "gemini-api-key"`) || !strings.Contains(geminiSettings, "gemini-2.5-pro") {
		t.Fatalf("gemini settings:\n%s", geminiSettings)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".grok", "config.toml")), `model = "grok-4"`) {
		t.Fatal("mixed selection dropped grok")
	}
}

func TestGatewayPromptWording(t *testing.T) {
	const want = "Gateway URL (Enter for https://deeprouter.top, or any compatible endpoint):"
	if gatewayPrompt != want {
		t.Fatalf("prompt %q", gatewayPrompt)
	}
	if !strings.Contains(gatewayPromptZH, "网关地址") || !strings.Contains(gatewayPromptZH, "https://deeprouter.top") {
		t.Fatalf("chinese prompt %q", gatewayPromptZH)
	}
	_, errOut, code := run(t, "", nil, "test", "--help")
	if code != 0 || !strings.Contains(errOut, "generateContent") || !strings.Contains(errOut, "x-goog-api-key") {
		t.Fatalf("test help %d\n%s", code, errOut)
	}
}

func TestPromptPlatformEnglishChineseAndEnvDefault(t *testing.T) {
	codex := []apps.ID{apps.Codex}
	var stderr bytes.Buffer
	in := bufio.NewReader(strings.NewReader("2\n"))
	id, custom, err := promptPlatform(in, &stderr, false, codex)
	if err != nil || custom || id != "wolfai" {
		t.Fatalf("id %s custom %v err %v\n%s", id, custom, err, stderr.String())
	}
	text := stderr.String()
	if !strings.Contains(text, "Platform [1]:") || !strings.Contains(text, "https://deeprouter.top") || !strings.Contains(text, "https://wolfai.top") || !strings.Contains(text, "https://www.linkmesh.org") || !strings.Contains(text, "Custom base URL") {
		t.Fatalf("english menu:\n%s", text)
	}
	for _, want := range []string{
		"https://api.openai.com",
		"https://api.x.ai",
		"6) Custom base URL",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("english menu missing %q\n%s", want, text)
		}
	}
	for _, absent := range []string{
		"https://api.anthropic.com",
		"https://generativelanguage.googleapis.com",
		"Official API, needs a network outside China",
		"官方直连，需海外网络",
	} {
		if strings.Contains(text, absent) {
			t.Fatalf("codex menu listed %q\n%s", absent, text)
		}
	}
	if strings.Index(text, "https://api.x.ai") > strings.Index(text, "Custom base URL") {
		t.Fatalf("custom gateway should be last:\n%s", text)
	}
	if strings.Contains(text, "平台") {
		t.Fatalf("english menu used chinese:\n%s", text)
	}

	stderr.Reset()
	in = bufio.NewReader(strings.NewReader("4\n"))
	id, custom, err = promptPlatform(in, &stderr, false, []apps.ID{apps.ClaudeCode})
	if err != nil || custom || id != "anthropic" {
		t.Fatalf("claude platform %s custom %v err %v\n%s", id, custom, err, stderr.String())
	}
	claudeMenu := stderr.String()
	for _, want := range []string{
		"https://deeprouter.top",
		"https://wolfai.top",
		"https://www.linkmesh.org",
		"https://api.anthropic.com",
		"5) Custom base URL",
	} {
		if !strings.Contains(claudeMenu, want) {
			t.Fatalf("claude menu missing %q\n%s", want, claudeMenu)
		}
	}
	if strings.Contains(claudeMenu, "https://api.openai.com") || strings.Contains(claudeMenu, "https://api.x.ai") || strings.Contains(claudeMenu, "https://generativelanguage.googleapis.com") {
		t.Fatalf("claude menu listed another official API:\n%s", claudeMenu)
	}

	stderr.Reset()
	in = bufio.NewReader(strings.NewReader("\n"))
	id, custom, err = promptPlatform(in, &stderr, false, []apps.ID{apps.Codex, apps.ClaudeCode})
	if err != nil || custom || id != "deeprouter" {
		t.Fatalf("mixed %s custom %v err %v\n%s", id, custom, err, stderr.String())
	}
	if strings.Contains(stderr.String(), "https://api.openai.com") || strings.Contains(stderr.String(), "https://api.anthropic.com") || !strings.Contains(stderr.String(), "4) Custom base URL") {
		t.Fatalf("mixed menu:\n%s", stderr.String())
	}

	t.Setenv("LC_ALL", "zh_TW.UTF-8")
	t.Setenv("LANG", "C")
	if !promptLocaleZH() {
		t.Fatal("zh_TW should select chinese prompts")
	}
	stderr.Reset()
	in = bufio.NewReader(strings.NewReader("\n"))
	id, custom, err = promptPlatform(in, &stderr, true, codex)
	if err != nil || custom || id != "deeprouter" {
		t.Fatalf("id %s custom %v err %v", id, custom, err)
	}
	if !strings.Contains(stderr.String(), "平台 [1]:") || !strings.Contains(stderr.String(), "自定义网关地址") || !strings.Contains(stderr.String(), "（默认）") {
		t.Fatalf("chinese menu:\n%s", stderr.String())
	}
	t.Setenv("LC_ALL", "C")
	t.Setenv("LANG", "en_US.UTF-8")
	if !promptLocaleZH() {
		t.Fatal("LC_ALL=C is not English, so the UI stays Chinese")
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	if promptLocaleZH() {
		t.Fatal("LC_ALL=en_US should select English even when LANG is Chinese")
	}
	t.Setenv("LC_ALL", "")
	if !promptLocaleZH() {
		t.Fatal("empty LC_ALL should fall through to LANG")
	}

	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("DEEPROUTER_PROVIDER", "linkmesh")
	stderr.Reset()
	in = bufio.NewReader(strings.NewReader("\n"))
	id, custom, err = promptPlatform(in, &stderr, false, codex)
	if err != nil || custom || id != "linkmesh" {
		t.Fatalf("env default %s custom %v err %v\n%s", id, custom, err, stderr.String())
	}
	if !strings.Contains(stderr.String(), "Platform [3]:") {
		t.Fatalf("default index:\n%s", stderr.String())
	}
	t.Setenv("DEEPROUTER_PROVIDER", "openai")
	stderr.Reset()
	in = bufio.NewReader(strings.NewReader("\n"))
	id, custom, err = promptPlatform(in, &stderr, false, codex)
	if err != nil || custom || id != "openai-api" || !strings.Contains(stderr.String(), "Platform [4]:") {
		t.Fatalf("openai default %s custom %v err %v\n%s", id, custom, err, stderr.String())
	}
	stderr.Reset()
	in = bufio.NewReader(strings.NewReader("\n"))
	id, custom, err = promptPlatform(in, &stderr, false, []apps.ID{apps.ClaudeCode})
	if err != nil || custom || id != "deeprouter" || !strings.Contains(stderr.String(), "Platform [1]:") {
		t.Fatalf("openai env is not valid for Claude, got %s custom %v err %v\n%s", id, custom, err, stderr.String())
	}
	t.Setenv("DEEPROUTER_PROVIDER", "nope")
	if _, _, err := promptPlatform(bufio.NewReader(strings.NewReader("\n")), &stderr, false, codex); err == nil {
		t.Fatal("invalid DEEPROUTER_PROVIDER was accepted")
	}
}

func TestTokenPromptNamesSite(t *testing.T) {
	en := tokenPrompt("Codex / ChatGPT", "wolfai", "http://127.0.0.1:9", "", "")
	if en != "Enter the WolfAI token (create one at wolfai.top) for Codex / ChatGPT: " {
		t.Fatalf("english %q", en)
	}
	if strings.Contains(en, "127.0.0.1") {
		t.Fatalf("preset prompt used the override host: %s", en)
	}
	masked := tokenPrompt("", "deeprouter", "http://127.0.0.1:9", testgate.GoodKey, "")
	if masked != "Enter the DeepRouter token (create one at deeprouter.top) ["+textutil.Mask(testgate.GoodKey)+"]: " {
		t.Fatalf("masked %q", masked)
	}
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	zh := tokenPrompt("", "wolfai", "", "", "")
	if zh != "请输入 WolfAI 的令牌（在 wolfai.top 创建）：" {
		t.Fatalf("chinese %q", zh)
	}
	zhProg := tokenPrompt("Codex / ChatGPT", "wolfai", "", testgate.GoodKey, "")
	if !strings.HasPrefix(zhProg, "请输入 WolfAI 的令牌（在 wolfai.top 创建），用于 Codex / ChatGPT [") || !strings.HasSuffix(zhProg, "]：") {
		t.Fatalf("chinese program %q", zhProg)
	}
	custom := tokenPrompt("", "custom", "https://example.com:8443/v1", "", "")
	if custom != "请输入该网关的令牌（https://example.com:8443/v1）：" {
		t.Fatalf("custom chinese %q", custom)
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	customEN := tokenPrompt("Grok Build", "custom", "https://example.com:8443/v1", "", "")
	if customEN != "Enter the gateway token (https://example.com:8443/v1) for Grok Build: " {
		t.Fatalf("custom english %q", customEN)
	}
	link := tokenPrompt("", "linkmesh", "http://127.0.0.1:9", "", "")
	if link != "Enter the LinkMesh token (create one at www.linkmesh.org): " {
		t.Fatalf("linkmesh %q", link)
	}
}

func TestSummaryAndPromptsMaskOnlyTheTail(t *testing.T) {
	want := "sk-****alue"
	if textutil.Mask(testgate.GoodKey) != want || strings.Contains(textutil.Mask(testgate.GoodKey), "sk-g") {
		t.Fatalf("mask %s", textutil.Mask(testgate.GoodKey))
	}
	bracket := tokenPrompt("Codex / ChatGPT", "deeprouter", "https://deeprouter.top", testgate.GoodKey, "")
	reuse := tokenPrompt("Claude Code", "deeprouter", "https://deeprouter.top", "", testgate.GoodKey)
	hint := keyPrompt(testgate.GoodKey)
	for _, prompt := range []string{bracket, reuse, hint} {
		if !strings.Contains(prompt, want) || strings.Contains(prompt, "sk-g") || strings.Contains(prompt, testgate.GoodKey) {
			t.Fatalf("prompt %q", prompt)
		}
	}
	if !strings.Contains(hint, "DEEPROUTER") && !strings.Contains(hint, "DeepRouter") {
		t.Fatalf("env hint %q", hint)
	}

	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "codex", "--codex-model", "gpt-5.4", "--no-shell")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "Key") || !strings.Contains(out, want) || strings.Contains(out, "sk-g") || strings.Contains(out, testgate.GoodKey) {
		t.Fatalf("summary:\n%s", out)
	}
	status, errOut, code := run(t, home, nil, "status")
	if code != 0 || !strings.Contains(status, want) || strings.Contains(status, "sk-g") || strings.Contains(status, testgate.GoodKey) {
		t.Fatalf("status %d\n%s\n%s", code, status, errOut)
	}

	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	home = t.TempDir()
	out, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "codex", "--codex-model", "gpt-5.4", "--no-shell")
	if code != 0 {
		t.Fatalf("zh setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "令牌") || !strings.Contains(out, want) || strings.Contains(out, "sk-g") || strings.Contains(out, testgate.GoodKey) {
		t.Fatalf("zh summary:\n%s", out)
	}
}

func TestProviderKeyIsNotShared(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "codex", "--codex-model", "gpt-5.4")
	if code != 0 {
		t.Fatal(out, errOut)
	}
	t.Setenv("DEEPROUTER_API_KEY", testgate.GoodKey)
	var wolfOut, wolfErr bytes.Buffer
	code = mainIO([]string{
		"setup", "--provider", "wolfai", "--base-url", srv.URL,
		"--apps", "codex", "--codex-model", "gpt-5.4",
	}, "0.1.0", &wolfOut, &wolfErr, bytes.NewReader(nil))
	if code == 0 || !strings.Contains(wolfErr.String(), "API key required") {
		t.Fatalf("wolfai reused a key %d\n%s", code, wolfErr.String())
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(codexText, "wolfai") || !strings.Contains(codexText, testgate.GoodKey) {
		t.Fatalf("codex changed:\n%s", codexText)
	}

	withTerminal(t)
	var stdout, stderr bytes.Buffer
	code = mainIO([]string{
		"setup", "--provider", "linkmesh", "--base-url", srv.URL,
		"--apps", "codex", "--codex-model", "gpt-5.4",
	}, "0.1.0", &stdout, &stderr, bytes.NewBufferString(testgate.AzureKey+"\n"))
	if code != 0 {
		t.Fatalf("linkmesh %d\n%s\n%s", code, stdout.String(), stderr.String())
	}
	prompt, _, _ := strings.Cut(stderr.String(), "\n")
	if !strings.Contains(prompt, "Enter the LinkMesh token (create one at www.linkmesh.org): ") {
		t.Fatalf("prompt:\n%s", stderr.String())
	}
	if offeredKey(stderr.String(), testgate.GoodKey) || strings.Contains(prompt, "127.0.0.1") {
		t.Fatalf("prompt reused deeprouter or the override host:\n%s", stderr.String())
	}
	codexText = mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model_provider = "linkmesh"`) || !strings.Contains(codexText, testgate.AzureKey) || strings.Contains(codexText, "[model_providers.deeprouter]") {
		t.Fatalf("codex:\n%s", codexText)
	}
}

func TestWizardCustomHostDoesNotReuseKey(t *testing.T) {
	home := t.TempDir()
	a := testgate.New()
	defer a.Close()
	b := testgate.New()
	defer b.Close()
	withTerminal(t)
	if _, _, code := runWizard(t, home, testgate.GoodKey, ttyReplaceAnswers(a.URL, testgate.GoodKey)); code != 0 {
		t.Fatal(code)
	}
	t.Setenv("DEEPROUTER_API_KEY", "")
	var stdout, stderr bytes.Buffer
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(ttyDefaultAnswers(b.URL)))
	if err != nil || code != 2 || !strings.Contains(stderr.String(), "The token is empty. Enter it again.") || !strings.Contains(stderr.String(), "Setup cancelled. Nothing was written.") {
		t.Fatalf("code %d err %v\n%s", code, err, stderr.String())
	}
	if offeredKey(stderr.String(), testgate.GoodKey) {
		t.Fatalf("reused key from another host:\n%s", stderr.String())
	}
	stdout.Reset()
	stderr.Reset()
	code, err = cmdWizard(&stdout, &stderr, bytes.NewBufferString(ttyDefaultAnswers(a.URL)))
	if err != nil || code != 0 {
		t.Fatalf("same host %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	if !offeredKey(stderr.String(), testgate.GoodKey) {
		t.Fatalf("same host did not offer the saved key:\n%s", stderr.String())
	}
}

func TestLinkMeshWritesSub2APIConfig(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	_, errOut, code := run(t, home, nil, "setup",
		"--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--apps", "codex", "--codex-model", "gpt-5.4", "--codex-wire-api", "chat")
	if code != 0 {
		t.Fatal(errOut)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), `wire_api = "chat"`) {
		t.Fatal(mustRead(t, filepath.Join(home, ".codex", "config.toml")))
	}
	out, errOut, code := run(t, home, nil, "setup",
		"--provider", "linkmesh",
		"--base-url", srv.URL,
		"--api-key", testgate.AzureKey,
		"--apps", "claude-code,claude-desktop,codex,grok,gemini",
		"--claude-model", "claude-sonnet-4-6",
		"--codex-model", "gpt-5.4",
		"--grok-model", "grok-4",
		"--gemini-model", "gemini-2.5-pro")
	if code != 0 {
		t.Fatalf("linkmesh %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "is skipped") || strings.Contains(errOut, "已跳过") {
		t.Fatalf("supported apps were skipped:\n%s", errOut)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `wire_api = "responses"`) || !strings.Contains(codexText, `base_url = "`+srv.URL+`/v1"`) || strings.Contains(codexText, `wire_api = "chat"`) || !strings.Contains(codexText, `model_provider = "linkmesh"`) {
		t.Fatalf("codex:\n%s", codexText)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(settings, `"ANTHROPIC_BASE_URL": "`+srv.URL+`"`) || strings.Contains(settings, srv.URL+"/v1") {
		t.Fatalf("claude:\n%s", settings)
	}
	desktop := mustRead(t, filepath.Join(claudedesktop.Dir(home), claudedesktop.ProfileID+".json"))
	if !strings.Contains(desktop, `"inferenceGatewayBaseUrl": "`+srv.URL+`"`) {
		t.Fatalf("desktop:\n%s", desktop)
	}
	grokText := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(grokText, `base_url = "`+srv.URL+`/v1"`) || !strings.Contains(grokText, `api_backend = "chat_completions"`) {
		t.Fatalf("grok:\n%s", grokText)
	}
	envText := managedShell(t, home)
	if !strings.Contains(envText, srv.URL) || strings.Contains(envText, srv.URL+"/v1") {
		t.Fatalf("gemini base:\n%s", envText)
	}
}

func TestLimitProgramsSkipsMissingRoute(t *testing.T) {
	prev := programExposed
	programExposed = func(id string, app apps.ID) (provider.Program, bool) {
		if id == "linkmesh" && app == apps.ClaudeCode {
			return provider.Program{App: app, Exposed: false, Route: "POST /v1/messages"}, true
		}
		return provider.ProgramFor(id, app)
	}
	t.Cleanup(func() { programExposed = prev })

	var buf bytes.Buffer
	kept, err := limitPrograms("linkmesh", []apps.ID{apps.Codex, apps.ClaudeCode, apps.Gemini}, &buf, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(kept) != 2 || kept[0] != apps.Codex || kept[1] != apps.Gemini {
		t.Fatalf("kept %v", kept)
	}
	if buf.String() != "LinkMesh does not expose POST /v1/messages, so Claude Code is skipped.\n" {
		t.Fatalf("message %q", buf.String())
	}
	buf.Reset()
	if _, err := limitPrograms("linkmesh", []apps.ID{apps.ClaudeCode}, &buf, true); err == nil || !strings.Contains(err.Error(), "no programs left") {
		t.Fatal(err)
	}
	if buf.String() != "LinkMesh 不提供 POST /v1/messages，已跳过 Claude Code。\n" {
		t.Fatalf("chinese %q", buf.String())
	}

	// Restore the live table. Every LinkMesh app stays.
	programExposed = prev
	buf.Reset()
	kept, err = limitPrograms("linkmesh", []apps.ID{apps.Codex, apps.ClaudeCode, apps.ClaudeDesktop, apps.Grok, apps.Gemini}, &buf, false)
	if err != nil || len(kept) != 5 || buf.Len() != 0 {
		t.Fatalf("live kept %v err %v\n%s", kept, err, buf.String())
	}
}

func TestWizardCustomProviderSection(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, ttyReplaceAnswers(srv.URL, testgate.GoodKey))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "Platform [1]:") || !strings.Contains(stderr, gatewayPrompt) {
		t.Fatalf("stderr:\n%s", stderr)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model_provider = "custom"`) || !strings.Contains(codexText, "[model_providers.custom]") || strings.Contains(codexText, "[model_providers.deeprouter]") {
		t.Fatalf("codex:\n%s", codexText)
	}
	if !strings.Contains(codexText, srv.URL) {
		t.Fatalf("custom url missing:\n%s", codexText)
	}
	saved := mustRead(t, filepath.Join(home, ".deeprouter", "config.json"))
	if !strings.Contains(saved, `"provider": "custom"`) {
		t.Fatalf("profile:\n%s", saved)
	}
}

func TestSetupProviderWritesPresetAndRemovesPrevious(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup",
		"--provider", "wolfai",
		"--base-url", srv.URL,
		"--api-key", testgate.GoodKey,
		"--apps", "codex,grok",
		"--codex-model", "gpt-5.4",
		"--grok-model", "grok-4")
	if code != 0 {
		t.Fatalf("wolfai %d\n%s\n%s", code, out, errOut)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model_provider = "wolfai"`) || !strings.Contains(codexText, "[model_providers.wolfai]") || strings.Contains(codexText, "deeprouter") {
		t.Fatalf("codex:\n%s", codexText)
	}
	if !strings.Contains(codexText, srv.URL) || strings.Contains(codexText, "https://wolfai.top") {
		t.Fatalf("url override:\n%s", codexText)
	}
	grokText := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(grokText, "[model.wolfai]") || !strings.Contains(grokText, `default = "wolfai"`) || strings.Contains(grokText, "[model.deeprouter]") {
		t.Fatalf("grok:\n%s", grokText)
	}
	saved := mustRead(t, filepath.Join(home, ".deeprouter", "config.json"))
	if !strings.Contains(saved, `"provider": "wolfai"`) || !strings.Contains(saved, `"label": "WolfAI"`) {
		t.Fatalf("profile:\n%s", saved)
	}
	status, _, _ := run(t, home, nil, "status", "--json")
	if !strings.Contains(status, `"grok_wired": true`) || !strings.Contains(status, `"codex_wired": true`) {
		t.Fatalf("status:\n%s", status)
	}

	out, errOut, code = run(t, home, nil, "setup",
		"--provider", "linkmesh",
		"--base-url", srv.URL,
		"--api-key", testgate.GoodKey,
		"--apps", "codex,grok",
		"--codex-model", "gpt-5.4",
		"--grok-model", "grok-4")
	if code != 0 {
		t.Fatalf("linkmesh %d\n%s\n%s", code, out, errOut)
	}
	codexText = mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(codexText, "wolfai") || !strings.Contains(codexText, `model_provider = "linkmesh"`) || !strings.Contains(codexText, "[model_providers.linkmesh]") {
		t.Fatalf("codex after switch:\n%s", codexText)
	}
	grokText = mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if strings.Contains(grokText, "wolfai") || !strings.Contains(grokText, "[model.linkmesh]") {
		t.Fatalf("grok after switch:\n%s", grokText)
	}

	t.Setenv("DEEPROUTER_PROVIDER", "wolfai")
	out, errOut, code = run(t, home, nil, "setup",
		"--base-url", srv.URL,
		"--api-key", testgate.GoodKey,
		"--apps", "codex",
		"--codex-model", "gpt-4o")
	if code != 0 {
		t.Fatalf("env %d\n%s\n%s", code, out, errOut)
	}
	codexText = mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model_provider = "wolfai"`) || strings.Contains(codexText, "linkmesh") || strings.Contains(codexText, "https://wolfai.top") || !strings.Contains(codexText, srv.URL) {
		t.Fatalf("env provider with base-url override:\n%s", codexText)
	}
	if strings.Contains(codexText, "disable_response_storage") {
		t.Fatalf("removed key:\n%s", codexText)
	}

	_, errOut, code = run(t, home, nil, "setup", "--provider", "custom", "--api-key", testgate.GoodKey)
	if code == 0 || !strings.Contains(errOut, "unknown provider") {
		t.Fatalf("custom flag code %d\n%s", code, errOut)
	}

	t.Setenv("DEEPROUTER_PROVIDER", "")
	id, base, explicit, err := resolveGateway("wolfai", true, true, "http://127.0.0.1:9", "", "")
	if err != nil || id != "wolfai" || base != "http://127.0.0.1:9" || !explicit {
		t.Fatalf("resolve %s %s %v %v", id, base, explicit, err)
	}
	id, base, explicit, err = resolveGateway("", false, true, srv.URL, "", "https://deeprouter.top")
	if err != nil || id != "deeprouter" || base != srv.URL || explicit {
		t.Fatalf("base only %s %s %v %v", id, base, explicit, err)
	}
}

func TestNoticeFindsExecutableOutsidePATH(t *testing.T) {
	dir := t.TempDir()
	name := "gemini"
	path := filepath.Join(dir, name)
	if runtime.GOOS == "windows" {
		path += ".cmd"
	}
	mustWrite(t, path, "", 0o755)
	prevLook := cliLookPath
	prevDirs := programExtraDirs
	cliLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	programExtraDirs = func() []string { return []string{dir} }
	t.Cleanup(func() {
		cliLookPath = prevLook
		programExtraDirs = prevDirs
	})
	if !programPresent(apps.Gemini, name) {
		t.Fatal("executable in an extra bin dir was treated as missing")
	}
	var buf bytes.Buffer
	noticePrograms(&buf, []apps.ID{apps.Gemini})
	if buf.Len() != 0 {
		t.Fatalf("notice:\n%s", buf.String())
	}
}

func TestNoticeListsInstallCommandsWhenMissing(t *testing.T) {
	stubCLIs(t, func(string) (string, error) {
		return "", os.ErrNotExist
	}, func(context.Context, io.Writer, io.Writer, string, ...string) error {
		return os.ErrNotExist
	})
	var buf bytes.Buffer
	noticePrograms(&buf, []apps.ID{apps.Gemini, apps.ClaudeCode, apps.Codex, apps.Grok, apps.ClaudeDesktop})
	text := buf.String()
	for _, id := range []apps.ID{apps.Gemini, apps.ClaudeCode, apps.Codex, apps.Grok, apps.ClaudeDesktop} {
		info, _ := apps.ByID(id)
		if !strings.Contains(text, info.Title+" is not installed yet.") {
			t.Fatalf("missing notice for %s\n%s", info.Title, text)
		}
		_, how, _ := apps.InstallHelp(id)
		for _, line := range how {
			if !strings.Contains(text, line) {
				t.Fatalf("missing %q\n%s", line, text)
			}
		}
	}
}

func TestWizardGeminiOnlySummaryNoticeAndProbe(t *testing.T) {
	home := t.TempDir()
	mustMkdir(t, filepath.Join(home, ".deeprouter"))
	mustWrite(t, filepath.Join(home, ".deeprouter", "config.json"), "{\n  \"version\": 1,\n  \"active_profile\": \"openai\",\n  \"profiles\": [\n    {\"name\": \"openai\", \"label\": \"OpenAI\", \"base_url\": \"https://deeprouter.top\", \"api_key\": \""+testgate.GoodKey+"\", \"wire_api\": \"responses\"}\n  ]\n}\n", 0o600)
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stubCLIs(t, func(string) (string, error) {
		return "", os.ErrNotExist
	}, func(context.Context, io.Writer, io.Writer, string, ...string) error {
		return os.ErrNotExist
	})
	stdout, stderr, code := runWizard(t, home, testgate.GoodKey, wizardCustom("5", srv.URL+"\n"+testgate.GoodKey+"\n\n\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	noticeAt := strings.Index(stderr, "Gemini CLI is not installed yet.")
	promptAt := strings.Index(stderr, gatewayPrompt)
	if noticeAt < 0 || promptAt < 0 || noticeAt > promptAt {
		t.Fatalf("notice should precede configuration\n%s", stderr)
	}
	_, how, _ := apps.InstallHelp(apps.Gemini)
	for _, line := range how {
		if !strings.Contains(stderr, line) {
			t.Fatalf("install line %q\nstderr:\n%s", line, stderr)
		}
		if strings.Contains(stdout, line) {
			t.Fatalf("install line %q repeated under Next\nstdout:\n%s", line, stdout)
		}
	}
	if strings.Count(stdout+stderr, "is not installed yet") != 1 || strings.Contains(stdout, "is not installed yet") {
		t.Fatalf("notice should appear once, before configuration\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "Install missing CLIs with npm?") || !strings.Contains(stderr, "https://nodejs.org/") {
		t.Fatalf("missing npm should warn, not ask\nstdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "Configured Gemini CLI.") || strings.Contains(stdout, "Configured openai (OpenAI)") {
		t.Fatalf("summary:\n%s", stdout)
	}
	if !strings.Contains(stdout, "/v1beta/models/gemini-2.5-pro:generateContent") || !strings.Contains(stdout, "Passed.") || !strings.Contains(stdout, "Then run:  gemini") {
		t.Fatalf("probe:\n%s", stdout)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("claude settings: %v", err)
	}
	settings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
	if !strings.Contains(settings, `"selectedType": "gemini-api-key"`) || strings.Contains(settings, `"name"`) {
		t.Fatalf("settings:\n%s", settings)
	}

	out, errOut, code := run(t, home, nil, "test", "--timeout", "5s")
	if code != 0 || !strings.Contains(out, "/v1beta/models/gemini-2.5-pro:generateContent") {
		t.Fatalf("test %d\n%s\n%s", code, out, errOut)
	}
}

func TestWizardSelectionScopesProbeAndNotices(t *testing.T) {
	withTerminal(t)
	srv := testgate.New()
	defer srv.Close()
	_, grokHow, _ := apps.InstallHelp(apps.Grok)
	if len(grokHow) == 0 || grokHow[0] == "" {
		t.Fatal("grok install hint is empty")
	}
	grokHint := grokHow[0]

	cases := []struct {
		name      string
		selection string
		models    []string
		npm       string
		wantOut   []string
		forbidOut []string
		wantErr   []string
		forbidErr []string
		wantNPM   string
	}{
		{
			name:      "codex",
			selection: "1",
			models:    []string{""},
			npm:       "n",
			wantOut: []string{
				"Configured Codex / ChatGPT.",
				"GET /v1/models",
				"POST /v1/responses",
				"gpt-5.4",
				"Passed.",
				"Then run:  codex",
				"Quit and reopen Codex / ChatGPT.",
			},
			forbidOut: []string{
				"POST /v1/messages",
				"generateContent",
				"Claude Code",
				"Claude Desktop",
				"Grok Build",
				"Gemini CLI",
				"Then run:  claude",
				"Then run:  grok",
				"Then run:  gemini",
			},
			wantErr: []string{
				"codex is not installed yet. Its configuration is written anyway and works after you install it. The ChatGPT desktop app uses the same config, so N is fine if you only use the desktop app.",
				"Install missing CLIs with npm? [y/N]",
			},
			forbidErr: []string{
				"claude is not installed",
				"grok is not installed",
				"gemini is not installed",
			},
		},
		{
			name:      "codex chosen model",
			selection: "1",
			models:    []string{"3"},
			npm:       "n",
			wantOut: []string{
				"Configured Codex / ChatGPT.",
				"GET /v1/models",
				"POST /v1/responses",
				"gpt-4.1",
				"Passed.",
			},
			forbidOut: []string{
				"gpt-5.4",
				"POST /v1/messages",
				"generateContent",
				"Claude Code",
				"Grok Build",
				"Gemini CLI",
			},
			wantErr: []string{
				"codex is not installed yet. Its configuration is written anyway and works after you install it. The ChatGPT desktop app uses the same config, so N is fine if you only use the desktop app.",
			},
			forbidErr: []string{"claude is not installed", "grok is not installed", "gemini is not installed"},
		},
		{
			name:      "claude code",
			selection: "2",
			models:    []string{""},
			npm:       "y",
			wantNPM:   "npm install -g @anthropic-ai/claude-code",
			wantOut: []string{
				"Configured Claude Code.",
				"POST /v1/messages",
				"claude-sonnet-4-6",
				"Passed.",
				"Then run:  claude",
			},
			forbidOut: []string{
				"GET /v1/models",
				"POST /v1/chat/completions",
				"generateContent",
				"Codex / ChatGPT",
				"Claude Desktop",
				"Grok Build",
				"Gemini CLI",
				"Quit and reopen",
				"Then run:  codex",
			},
			wantErr: []string{
				"claude is not installed yet.",
				"Install missing CLIs with npm? [y/N]",
			},
			forbidErr: []string{
				"codex is not installed",
				"grok is not installed",
				"gemini is not installed",
			},
		},
		{
			name:      "claude desktop",
			selection: "3",
			models:    []string{""},
			wantOut: []string{
				"Configured Claude Desktop.",
				"POST /v1/messages",
				"claude-sonnet-4-6",
				"Passed.",
				"Quit and reopen Claude Desktop.",
			},
			forbidOut: []string{
				"GET /v1/models",
				"POST /v1/chat/completions",
				"generateContent",
				"Codex / ChatGPT",
				"Claude Code",
				"Grok Build",
				"Gemini CLI",
				"Then run:",
				"Quit and reopen Codex",
			},
			forbidErr: []string{
				"claude is not installed",
				"codex is not installed",
				"grok is not installed",
				"gemini is not installed",
				"Install missing CLIs with npm?",
			},
		},
		{
			name:      "grok",
			selection: "4",
			models:    []string{""},
			wantOut: []string{
				"Configured Grok Build.",
				"POST /v1/chat/completions",
				"grok-4",
				"Passed.",
				"Then run:  grok",
			},
			forbidOut: []string{
				"GET /v1/models",
				"POST /v1/messages",
				"generateContent",
				"Codex / ChatGPT",
				"Claude",
				"Gemini CLI",
				"Quit and reopen",
			},
			wantErr:   []string{"grok is not installed yet.", grokHint},
			forbidErr: []string{"codex is not installed", "claude is not installed", "gemini is not installed", "Install missing CLIs with npm?"},
		},
		{
			name:      "gemini",
			selection: "5",
			models:    []string{""},
			npm:       "y",
			wantNPM:   "npm install -g @google/gemini-cli",
			wantOut: []string{
				"Configured Gemini CLI.",
				"/v1beta/models/gemini-2.5-pro:generateContent",
				"Passed.",
				"Then run:  gemini",
			},
			forbidOut: []string{
				"GET /v1/models",
				"POST /v1/chat/completions",
				"POST /v1/messages",
				"Codex / ChatGPT",
				"Claude",
				"Grok Build",
				"Quit and reopen",
			},
			wantErr: []string{
				"gemini is not installed yet.",
				"Install missing CLIs with npm? [y/N]",
			},
			forbidErr: []string{"codex is not installed", "claude is not installed", "grok is not installed"},
		},
		{
			name:      "codex and claude",
			selection: "1,2",
			models:    []string{"", ""},
			npm:       "y",
			wantNPM:   "npm install -g @anthropic-ai/claude-code @openai/codex",
			wantOut: []string{
				"Configured Claude Code, Codex / ChatGPT.",
				"GET /v1/models",
				"POST /v1/responses",
				"POST /v1/messages",
				"gpt-5.4",
				"claude-sonnet-4-6",
				"Passed.",
				"Then run:  claude",
				"Or run:    codex",
				"Quit and reopen Codex / ChatGPT.",
			},
			forbidOut: []string{
				"generateContent",
				"Claude Desktop",
				"Grok Build",
				"Gemini CLI",
				"Then run:  grok",
				"Then run:  gemini",
				"Quit and reopen Claude Desktop",
			},
			wantErr: []string{
				"codex is not installed yet. Its configuration is written anyway and works after you install it. The ChatGPT desktop app uses the same config, so N is fine if you only use the desktop app.",
				"claude is not installed yet.",
				"Install missing CLIs with npm? [y/N]",
			},
			forbidErr: []string{"grok is not installed", "gemini is not installed"},
		},
		{
			name:      "grok and gemini",
			selection: "4,5",
			models:    []string{"", ""},
			npm:       "y",
			wantNPM:   "npm install -g @google/gemini-cli",
			wantOut: []string{
				"Configured Grok Build, Gemini CLI.",
				"POST /v1/chat/completions",
				"grok-4",
				"/v1beta/models/gemini-2.5-pro:generateContent",
				"Passed.",
				"Then run:  grok",
				"Then run:  gemini",
			},
			forbidOut: []string{
				"GET /v1/models",
				"POST /v1/messages",
				"Codex / ChatGPT",
				"Claude",
				"Quit and reopen",
			},
			wantErr: []string{
				"grok is not installed yet.",
				grokHint,
				"gemini is not installed yet.",
				"Install missing CLIs with npm? [y/N]",
			},
			forbidErr: []string{"codex is not installed", "claude is not installed"},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
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
					return nil
				}
				return os.ErrNotExist
			})
			var answers strings.Builder
			answers.WriteString(tc.selection)
			answers.WriteByte('\n')
			answers.WriteString(platformChoice(tc.selection, provider.CustomID))
			answers.WriteByte('\n')
			answers.WriteString(srv.URL)
			answers.WriteByte('\n')
			for _, model := range tc.models {
				answers.WriteString(testgate.GoodKey)
				answers.WriteByte('\n')
				answers.WriteString(model)
				answers.WriteByte('\n')
			}
			if tc.npm != "" {
				answers.WriteString(tc.npm)
				answers.WriteByte('\n')
			}
			stdout, stderr, code := runWizard(t, home, testgate.GoodKey, answers.String())
			if code != 0 {
				t.Fatalf("wizard %d\nstdout:\n%s\nstderr:\n%s", code, stdout, stderr)
			}
			for _, needle := range tc.wantOut {
				if !strings.Contains(stdout, needle) {
					t.Fatalf("stdout missing %q\n%s", needle, stdout)
				}
			}
			for _, needle := range tc.forbidOut {
				if strings.Contains(stdout, needle) {
					t.Fatalf("stdout has %q\n%s", needle, stdout)
				}
			}
			for _, needle := range tc.wantErr {
				if !strings.Contains(stderr, needle) {
					t.Fatalf("stderr missing %q\n%s", needle, stderr)
				}
			}
			for _, needle := range tc.forbidErr {
				if strings.Contains(stderr, needle) {
					t.Fatalf("stderr has %q\n%s", needle, stderr)
				}
			}
			if tc.wantNPM == "" {
				if npmArgs != nil {
					t.Fatalf("npm ran: %v", npmArgs)
				}
			} else if strings.Join(npmArgs, " ") != tc.wantNPM {
				t.Fatalf("npm %q, want %q", strings.Join(npmArgs, " "), tc.wantNPM)
			}
		})
	}
}

func ttyDefaultAnswers(url string) string {
	// Enter selects Codex / ChatGPT, then the custom gateway, then an empty
	// key. End of input at the key prompt cancels.
	return wizardCustom("", url+"\n\n\n\n")
}

func ttyReplaceAnswers(url, key string) string {
	return wizardCustom("", url+"\n"+key+"\n\n\n")
}

func assertProgramKeys(t *testing.T, home, want, absent string) {
	t.Helper()
	for _, path := range []string{
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".deeprouter", "config.json"),
	} {
		text := mustRead(t, path)
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing %s\n%s", path, want, text)
		}
		if absent != "" && strings.Contains(text, absent) {
			t.Fatalf("%s still has %s\n%s", path, absent, text)
		}
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		claudedesktop.Dir(home),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s exists: %v", path, err)
		}
	}
}

func assertKeyFiles(t *testing.T, home, want, absent string) {
	t.Helper()
	desktop := claudedesktop.Dir(home)
	profile := filepath.Join(desktop, claudedesktop.ProfileID+".json")
	meta := filepath.Join(desktop, "_meta.json")
	paths := []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".codex", "config.toml"),
		filepath.Join(home, ".deeprouter", "config.json"),
		profile,
	}
	if !useUserEnv() {
		paths = append(paths, filepath.Join(home, ".zshrc"))
	}
	for _, path := range paths {
		text := mustRead(t, path)
		if !strings.Contains(text, want) {
			t.Fatalf("%s missing %s\n%s", path, want, text)
		}
		if absent != "" && strings.Contains(text, absent) {
			t.Fatalf("%s still has %s\n%s", path, absent, text)
		}
	}
	rc := managedShell(t, home)
	if !strings.Contains(rc, want) || (absent != "" && strings.Contains(rc, absent)) {
		t.Fatalf("shell environment key mismatch:\n%s", rc)
	}
	if useUserEnv() {
		if strings.Count(rc, "DEEPROUTER_API_KEY=") != 1 || strings.Count(rc, "ANTHROPIC_AUTH_TOKEN=") != 1 {
			t.Fatalf("user env:\n%s", rc)
		}
	} else if strings.Count(rc, "# >>> deeprouter-setup >>>") != 1 || strings.Count(rc, "DEEPROUTER_API_KEY=") != 1 || strings.Count(rc, "ANTHROPIC_AUTH_TOKEN=") != 1 {
		t.Fatalf("shell block:\n%s", rc)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	if strings.Count(settings, "ANTHROPIC_AUTH_TOKEN") != 1 {
		t.Fatalf("settings:\n%s", settings)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Count(codexText, "experimental_bearer_token") != 1 {
		t.Fatalf("codex:\n%s", codexText)
	}
	metaText := mustRead(t, meta)
	if strings.Contains(metaText, want) || strings.Count(metaText, `"appliedId"`) != 1 {
		t.Fatalf("meta:\n%s", metaText)
	}
	if absent != "" && strings.Contains(metaText, absent) {
		t.Fatalf("meta still has %s\n%s", absent, metaText)
	}
	if strings.Contains(mustRead(t, profile), "/v1") {
		t.Fatalf("desktop base URL includes /v1:\n%s", mustRead(t, profile))
	}
}

func isolateHome(t *testing.T, home string) {
	t.Helper()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	// Claude Desktop on Windows reads %APPDATA%. Pin both app-data
	// directories inside the temp home so a test never writes the real profile.
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("LOCALAPPDATA", filepath.Join(home, "AppData", "Local"))
	t.Setenv("SHELL", "/bin/zsh")
	t.Setenv("CODEX_HOME", "")
	t.Setenv("GROK_HOME", "")
	t.Setenv("GEMINI_CLI_HOME", "")
	if useUserEnv() {
		prev := userenv.SetCurrent(userenv.MemFor(home))
		t.Cleanup(func() { userenv.SetCurrent(prev) })
	}
}

func shellFollowupNeedle() string {
	if useUserEnv() {
		return "Open a new terminal to pick up the user environment variables."
	}
	return "source ~/.zshrc"
}

// readShellRecord returns the shell startup text, or the Windows user-env
// record. A missing record is empty: setup does not create one unless it
// writes a shell variable.
func readShellRecord(t *testing.T, home string) string {
	t.Helper()
	if useUserEnv() {
		rec, err := userenv.Load(userenv.RecordPath(filepath.Join(home, ".deeprouter")))
		if os.IsNotExist(err) {
			return ""
		}
		if err != nil {
			t.Fatal(err)
		}
		return rec.Text()
	}
	return managedShell(t, home)
}

func managedShell(t *testing.T, home string) string {
	t.Helper()
	if useUserEnv() {
		rec, err := userenv.Load(userenv.RecordPath(filepath.Join(home, ".deeprouter")))
		if err != nil {
			t.Fatal(err)
		}
		return rec.Text()
	}
	return mustRead(t, filepath.Join(home, ".zshrc"))
}

// shellKeyText is the DeepRouter token a new shell would see.
// The Windows record lives in ~/.deeprouter, so removing that directory
// deletes the file while the value remains in the user-environment store.
func shellKeyText(t *testing.T, home string) string {
	t.Helper()
	if !useUserEnv() {
		return managedShell(t, home)
	}
	path := userenv.RecordPath(filepath.Join(home, ".deeprouter"))
	rec, err := userenv.Load(path)
	if err == nil {
		return rec.Text()
	}
	if !os.IsNotExist(err) {
		t.Fatal(err)
	}
	st := userenv.Active()
	if st == nil {
		t.Fatal("user environment store is not bound")
	}
	var b strings.Builder
	for _, key := range userenv.ManagedKeys {
		v, ok, getErr := st.Get(key)
		if getErr != nil {
			t.Fatal(getErr)
		}
		if !ok {
			continue
		}
		b.WriteString(key)
		b.WriteByte('=')
		b.WriteString(v)
		b.WriteByte('\n')
	}
	if b.Len() == 0 {
		t.Fatal("user environment has no managed keys")
	}
	return b.String()
}

func assertShellUntouched(t *testing.T, home string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(home, ".zshrc")); !os.IsNotExist(err) {
		t.Fatalf("shell startup file exists: %v", err)
	}
	if !useUserEnv() {
		return
	}
	path := userenv.RecordPath(filepath.Join(home, ".deeprouter"))
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("user environment record exists: %v", err)
	}
	assertUserEnvCleared(t)
}

func assertUserEnvCleared(t *testing.T) {
	t.Helper()
	st := userenv.Active()
	if st == nil {
		t.Fatal("user environment store is not bound")
	}
	for _, key := range userenv.ManagedKeys {
		if _, ok, err := st.Get(key); err != nil || ok {
			t.Fatalf("user environment still has %s (ok %v err %v)", key, ok, err)
		}
	}
}

func TestOfficialPresetsSkipTheModelList(t *testing.T) {
	home := t.TempDir()
	out, errOut, code := run(t, home, nil, "setup", "--provider", "openai", "--api-key", testgate.GoodKey)
	if code != 0 {
		t.Fatalf("openai %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(errOut, "Checking the token") || strings.Contains(errOut, "Fetching models") || strings.Contains(errOut, "Continue?") {
		t.Fatalf("probed or confirmed:\n%s", errOut)
	}
	if !strings.Contains(errOut, "These files will be modified:") || !strings.Contains(errOut, "deeprouter restore") || !strings.Contains(errOut, "Official API: no model list was fetched.") {
		t.Fatalf("notice:\n%s", errOut)
	}
	codexText := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `model_provider = "openai-api"`) || !strings.Contains(codexText, `base_url = "https://api.openai.com/v1"`) || !strings.Contains(codexText, `wire_api = "responses"`) || strings.Contains(codexText, "[model_providers.openai]") || strings.Contains(codexText, "model = ") {
		t.Fatalf("codex:\n%s", codexText)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("openai wrote claude: %v", err)
	}

	_, errOut, code = run(t, home, nil, "setup", "--provider", "openai", "--api-key", testgate.GoodKey, "--apps", "claude-code")
	if code == 0 || !strings.Contains(errOut, "no programs left") {
		t.Fatalf("claude on openai %d\n%s", code, errOut)
	}

	_, errOut, code = run(t, home, nil, "setup", "--provider", "openai", "--api-key", testgate.GoodKey, "--codex-model", "gpt-5.4")
	if code != 0 {
		t.Fatalf("explicit model %d\n%s", code, errOut)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), `model = "gpt-5.4"`) {
		t.Fatal(mustRead(t, filepath.Join(home, ".codex", "config.toml")))
	}

	home = t.TempDir()
	_, errOut, code = run(t, home, nil, "setup", "--provider", "anthropic", "--api-key", testgate.GoodKey)
	if code != 0 {
		t.Fatalf("anthropic %d\n%s", code, errOut)
	}
	settings := mustRead(t, filepath.Join(home, ".claude", "settings.json"))
	if !strings.Contains(settings, `"ANTHROPIC_BASE_URL": "https://api.anthropic.com"`) || strings.Contains(settings, "api.anthropic.com/v1") || strings.Contains(settings, "ANTHROPIC_MODEL") {
		t.Fatalf("claude:\n%s", settings)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("anthropic wrote codex: %v", err)
	}

	home = t.TempDir()
	_, errOut, code = run(t, home, nil, "setup", "--provider", "gemini", "--api-key", testgate.GoodKey)
	if code != 0 {
		t.Fatalf("gemini %d\n%s", code, errOut)
	}
	envText := managedShell(t, home)
	if !strings.Contains(envText, "GOOGLE_GEMINI_BASE_URL=") || !strings.Contains(envText, "https://generativelanguage.googleapis.com") || strings.Contains(envText, "generativelanguage.googleapis.com/v1") {
		t.Fatalf("gemini env:\n%s", envText)
	}
	gemSettings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
	if strings.Contains(gemSettings, `"name"`) {
		t.Fatalf("gemini model was set:\n%s", gemSettings)
	}

	home = t.TempDir()
	_, errOut, code = run(t, home, nil, "setup", "--provider", "xai", "--api-key", testgate.GoodKey, "--apps", "codex,grok")
	if code != 0 {
		t.Fatalf("xai %d\n%s", code, errOut)
	}
	codexText = mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if !strings.Contains(codexText, `base_url = "https://api.x.ai/v1"`) || !strings.Contains(codexText, `wire_api = "responses"`) || !strings.Contains(codexText, `model_provider = "xai"`) {
		t.Fatalf("xai codex:\n%s", codexText)
	}
	grokText := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(grokText, `base_url = "https://api.x.ai/v1"`) || !strings.Contains(grokText, `api_backend = "chat_completions"`) || !strings.Contains(grokText, "[model.xai]") {
		t.Fatalf("xai grok:\n%s", grokText)
	}

	t.Setenv("DEEPROUTER_PROVIDER", "openai")
	home = t.TempDir()
	_, errOut, code = run(t, home, nil, "setup", "--api-key", testgate.GoodKey)
	if code != 0 || !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), "https://api.openai.com/v1") {
		t.Fatalf("env provider %d\n%s", code, errOut)
	}
	t.Setenv("DEEPROUTER_PROVIDER", "")

	withTerminal(t)
	home = t.TempDir()
	stdout, stderr, code := runWizard(t, home, "", wizardPlatform("", "openai", testgate.GoodKey+"\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "https://api.anthropic.com") || strings.Contains(stderr, "https://generativelanguage.googleapis.com") || strings.Contains(stderr, "Official API, needs a network outside China") || strings.Contains(stderr, "Fetching models") || strings.Contains(stderr, "Continue?") {
		t.Fatalf("menu or probe:\n%s", stderr)
	}
	if !strings.Contains(stderr, "1) Codex / ChatGPT") || !strings.Contains(stderr, "https://deeprouter.top") || !strings.Contains(stderr, "https://wolfai.top") || !strings.Contains(stderr, "https://www.linkmesh.org") || !strings.Contains(stderr, "https://api.openai.com") || !strings.Contains(stderr, "no model set") || !strings.Contains(stderr, "deeprouter restore") {
		t.Fatalf("wizard stderr:\n%s", stderr)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), "https://api.openai.com/v1") {
		t.Fatal(mustRead(t, filepath.Join(home, ".codex", "config.toml")))
	}

	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	home = t.TempDir()
	stdout, stderr, code = runWizard(t, home, "", wizardPlatform("2", "anthropic", testgate.GoodKey+"\nn\n"))
	if code != 0 {
		t.Fatalf("zh anthropic %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "将修改这些文件：") || !strings.Contains(stderr, "deeprouter restore") || !strings.Contains(stderr, "不指定模型，使用客户端默认") || !strings.Contains(stderr, "https://api.anthropic.com") || !strings.Contains(stderr, "https://deeprouter.top") || !strings.Contains(stderr, "https://wolfai.top") || !strings.Contains(stderr, "https://www.linkmesh.org") {
		t.Fatalf("zh stderr:\n%s", stderr)
	}
	if strings.Contains(stderr, "官方直连，需海外网络") || strings.Contains(stderr, "https://api.openai.com") || strings.Contains(stderr, "继续写入") || strings.Contains(stderr, "正在从") {
		t.Fatalf("zh still asked:\n%s", stderr)
	}
	if !strings.Contains(stderr, "用于 Claude Code") {
		t.Fatalf("default program:\n%s", stderr)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".claude", "settings.json")), "https://api.anthropic.com") {
		t.Fatal(mustRead(t, filepath.Join(home, ".claude", "settings.json")))
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
}

func runWizard(t *testing.T, home, apiKey, answers string) (string, string, int) {
	t.Helper()
	isolateHome(t, home)
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
		isolateHome(t, home)
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

// offeredKey reports whether text shows secret as a saved key the user can
// keep or reuse. The confirmation after typing uses the same visible tail,
// so the brackets and the reuse sentence are what count.
func offeredKey(text, secret string) bool {
	mask := textutil.Mask(secret)
	return strings.Contains(text, "["+mask+"]") ||
		strings.Contains(text, "reuse "+mask) ||
		strings.Contains(text, "沿用 "+mask)
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
	prevDirs := programExtraDirs
	cliLookPath = look
	cliExec = run
	programExtraDirs = func() []string { return nil }
	t.Cleanup(func() {
		cliLookPath = prevLook
		cliExec = prevRun
		programExtraDirs = prevDirs
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

func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	got := mode(t, path)
	if !permMatches(got, want) {
		t.Fatalf("%s mode %o, want %o", path, got, want)
	}
}

// permMatches compares Unix modes. Windows only stores a read-only bit, so a
// writable file is reported as 0666 and a directory as 0777.
func permMatches(got, want os.FileMode) bool {
	if runtime.GOOS != "windows" {
		return got == want
	}
	return got&0o200 != 0
}
