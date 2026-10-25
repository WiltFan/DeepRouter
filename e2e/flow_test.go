package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter-setup/internal/testgate"
)

func TestInstallSetupTestSwitchRestore(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX installer")
	}
	root := moduleRoot(t)
	prefix := t.TempDir()
	cmd := exec.Command("sh", "install.sh", "--prefix", prefix, "--from-source")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	bin := filepath.Join(prefix, "deeprouter-setup")
	version := runBin(t, bin, nil, "version")
	if !strings.Contains(version.stdout, "deeprouter-setup 0.1.0") {
		t.Fatalf("version: %s", version.stdout)
	}

	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	claude := filepath.Join(home, ".claude", "settings.json")
	codex := filepath.Join(home, ".codex", "config.toml")
	rc := filepath.Join(home, ".zshrc")
	if err := os.MkdirAll(filepath.Dir(claude), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Dir(codex), 0o700); err != nil {
		t.Fatal(err)
	}
	originalRC := "export KEEP_ME=1\n"
	if err := os.WriteFile(rc, []byte(originalRC), 0o644); err != nil {
		t.Fatal(err)
	}

	env := []string{
		"HOME=" + home,
		"USERPROFILE=" + home,
		"SHELL=/bin/zsh",
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}
	setup := runBin(t, bin, env, "setup",
		"--base-url", srv.URL,
		"--group", "officially",
		"--api-key", testgate.GoodKey,
		"--claude-model", "claude-sonnet-4-6",
		"--codex-model", "gpt-4.1",
	)
	if setup.code != 0 {
		t.Fatalf("setup %d\n%s\n%s", setup.code, setup.stdout, setup.stderr)
	}
	if strings.Contains(setup.stdout+setup.stderr, testgate.GoodKey) {
		t.Fatal("setup printed the API key")
	}
	settings, err := os.ReadFile(claude)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(settings), testgate.GoodKey) || !strings.Contains(string(settings), "ANTHROPIC_BASE_URL") {
		t.Fatalf("settings:\n%s", settings)
	}
	codexText, err := os.ReadFile(codex)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(codexText), testgate.GoodKey) || !strings.Contains(string(codexText), "[model_providers.deeprouter]") {
		t.Fatalf("codex:\n%s", codexText)
	}

	testCmd := runBin(t, bin, env, "test", "--timeout", "5s")
	if testCmd.code != 0 || !strings.Contains(testCmd.stdout, "Passed.") || !strings.Contains(testCmd.stdout, "ttfb") {
		t.Fatalf("test %d\n%s\n%s", testCmd.code, testCmd.stdout, testCmd.stderr)
	}

	models := runBin(t, bin, env, "models", "--plain")
	if models.code != 0 || !strings.Contains(models.stdout, "gpt-4.1") {
		t.Fatalf("models %d\n%s", models.code, models.stdout)
	}

	add := runBin(t, bin, env, "profile", "add", "azure", "--label", "Azure", "--api-key", testgate.AzureKey, "--base-url", srv.URL)
	if add.code != 0 {
		t.Fatalf("add %d\n%s\n%s", add.code, add.stdout, add.stderr)
	}
	sw := runBin(t, bin, env, "switch", "--group", "azure", "--codex-model", "gpt-4.1", "--claude-model", "claude-sonnet-4-6")
	if sw.code != 0 {
		t.Fatalf("switch %d\n%s\n%s", sw.code, sw.stdout, sw.stderr)
	}
	settings, _ = os.ReadFile(claude)
	if !strings.Contains(string(settings), testgate.AzureKey) {
		t.Fatalf("switch did not apply the azure key:\n%s", settings)
	}

	restore := runBin(t, bin, env, "restore")
	if restore.code != 0 {
		t.Fatalf("restore %d\n%s\n%s", restore.code, restore.stdout, restore.stderr)
	}
	if _, err := os.Stat(claude); !os.IsNotExist(err) {
		t.Fatalf("created settings file was not removed: %v", err)
	}
	if _, err := os.Stat(codex); !os.IsNotExist(err) {
		t.Fatalf("created codex file was not removed: %v", err)
	}
	rcText, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(rcText) != originalRC {
		t.Fatalf("rc after restore:\n%s", rcText)
	}
}

func TestNoForbiddenFraming(t *testing.T) {
	root := moduleRoot(t)
	// Built without the contiguous phrases so this file stays clean.
	banned := []string{
		"\u9006\u5411",
		"\u53f7\u6c60",
		"reverse-" + "engineered",
		"reverse " + "engineered",
		"account " + "pool",
	}
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		base := info.Name()
		if info.IsDir() {
			switch base {
			case ".git", "dist", "agent-tools":
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(base, ".gif") || strings.HasSuffix(base, ".png") {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		low := strings.ToLower(string(body))
		for _, word := range banned {
			if strings.Contains(low, strings.ToLower(word)) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s contains %q", rel, word)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

type result struct {
	stdout string
	stderr string
	code   int
}

func runBin(t *testing.T, bin string, env []string, args ...string) result {
	t.Helper()
	cmd := exec.Command(bin, args...)
	if env != nil {
		cmd.Env = env
	}
	var stdout, stderr strings.Builder
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			t.Fatal(err)
		}
	}
	return result{stdout: stdout.String(), stderr: stderr.String(), code: code}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found")
		}
		dir = parent
	}
}
