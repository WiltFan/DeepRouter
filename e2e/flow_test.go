package e2e

import (
	"crypto/sha256"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter-setup/internal/claudedesktop"
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
	cmd.Env = append(envWithout(os.Environ(), "DEEPROUTER_SETUP_VERSION"), "DEEPROUTER_NO_SETUP=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	bin := filepath.Join(prefix, "deeprouter")
	version := runBin(t, bin, nil, "version")
	wantVer := describedVersion(t, root)
	if strings.TrimSpace(version.stdout) != "deeprouter "+wantVer {
		t.Fatalf("version: %q, want deeprouter %s", version.stdout, wantVer)
	}
	if _, err := os.Stat(filepath.Join(prefix, "deeprouter-setup")); err != nil {
		t.Fatal(err)
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
		"LOCALAPPDATA=" + filepath.Join(home, "AppData", "Local"),
		"SHELL=/bin/zsh",
		"PATH=" + os.Getenv("PATH"),
		"TMPDIR=" + os.TempDir(),
	}
	desktopProfile := filepath.Join(claudedesktop.DirFor(home, runtime.GOOS, filepath.Join(home, "AppData", "Local")), claudedesktop.ProfileID+".json")
	setup := runBin(t, bin, env, "setup",
		"--base-url", srv.URL,
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
	if !strings.Contains(string(codexText), `experimental_bearer_token = "`+testgate.GoodKey+`"`) || !strings.Contains(string(codexText), "[model_providers.deeprouter]") {
		t.Fatalf("codex:\n%s", codexText)
	}
	if strings.Contains(string(codexText), "env_key") {
		t.Fatalf("codex still requires an env var:\n%s", codexText)
	}
	info, err := os.Stat(codex)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("codex mode %o", info.Mode().Perm())
	}
	desktopText, err := os.ReadFile(desktopProfile)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(desktopText), testgate.GoodKey) || strings.Contains(string(desktopText), "/v1") || !strings.Contains(setup.stdout, "Claude Desktop") {
		t.Fatalf("desktop:\n%s\nsetup:\n%s", desktopText, setup.stdout)
	}
	metaText, err := os.ReadFile(filepath.Join(filepath.Dir(desktopProfile), "_meta.json"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(metaText), testgate.GoodKey) {
		t.Fatal("desktop index holds the API key")
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
	sw := runBin(t, bin, env, "switch", "--profile", "azure", "--codex-model", "gpt-4.1", "--claude-model", "claude-sonnet-4-6")
	if sw.code != 0 {
		t.Fatalf("switch %d\n%s\n%s", sw.code, sw.stdout, sw.stderr)
	}
	settings, _ = os.ReadFile(claude)
	if !strings.Contains(string(settings), testgate.AzureKey) {
		t.Fatalf("switch did not apply the azure key:\n%s", settings)
	}
	desktopText, err = os.ReadFile(desktopProfile)
	if err != nil || !strings.Contains(string(desktopText), testgate.AzureKey) || strings.Contains(string(desktopText), testgate.GoodKey) {
		t.Fatalf("desktop after switch:\n%s", desktopText)
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
	if _, err := os.Stat(desktopProfile); !os.IsNotExist(err) {
		t.Fatalf("created desktop profile was not removed: %v", err)
	}
	rcText, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if string(rcText) != originalRC {
		t.Fatalf("rc after restore:\n%s", rcText)
	}
}

func TestInstallAddsPathAndSuggestsSetup(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX installer")
	}
	root := moduleRoot(t)
	home := t.TempDir()
	rc := filepath.Join(home, ".zshrc")
	if err := os.WriteFile(rc, []byte("# existing\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out := runInstall(t, root, home, nil)
	text := string(out)
	if !strings.Contains(text, "is not on PATH") || !strings.Contains(text, `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Fatalf("path hint missing:\n%s", text)
	}
	bin := filepath.Join(home, ".local", "bin", "deeprouter")
	if !strings.Contains(text, "Next, run:") || !strings.Contains(text, bin) {
		t.Fatalf("next step missing:\n%s", text)
	}
	body, err := os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "# existing") || strings.Count(string(body), "# deeprouter-setup: PATH") != 1 {
		t.Fatalf("rc:\n%s", body)
	}
	if !strings.Contains(string(body), `export PATH="$HOME/.local/bin:$PATH"`) {
		t.Fatalf("rc export:\n%s", body)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".local", "bin", "deeprouter-setup")); err != nil {
		t.Fatal(err)
	}

	again := runInstall(t, root, home, nil)
	if !strings.Contains(string(again), "PATH is already recorded") {
		t.Fatalf("second install:\n%s", again)
	}
	body, err = os.ReadFile(rc)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(body), "# deeprouter-setup: PATH") != 1 {
		t.Fatalf("duplicated path line:\n%s", body)
	}

	other := t.TempDir()
	otherRC := filepath.Join(other, ".zshrc")
	if err := os.WriteFile(otherRC, []byte("# keep\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	skipped := runInstall(t, root, other, []string{"--no-path"})
	if !strings.Contains(string(skipped), "is not on PATH") || !strings.Contains(string(skipped), "deeprouter-setup") {
		t.Fatalf("no-path output:\n%s", skipped)
	}
	kept, err := os.ReadFile(otherRC)
	if err != nil {
		t.Fatal(err)
	}
	if string(kept) != "# keep\n" {
		t.Fatalf("no-path edited rc:\n%s", kept)
	}
}

func runInstall(t *testing.T, root, home string, extra []string) []byte {
	t.Helper()
	args := append([]string{"install.sh", "--from-source"}, extra...)
	cmd := exec.Command("sh", args...)
	cmd.Dir = root
	env := []string{}
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "SHELL=") {
			continue
		}
		env = append(env, kv)
	}
	cmd.Env = append(env, "HOME="+home, "SHELL=/bin/zsh", "DEEPROUTER_NO_SETUP=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	return out
}

func TestInstallShRejectsWindowsShells(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX installer")
	}
	root := moduleRoot(t)
	dir := t.TempDir()
	script, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "install.sh"), script, 0o755); err != nil {
		t.Fatal(err)
	}
	uname := filepath.Join(dir, "uname")
	if err := os.WriteFile(uname, []byte("#!/bin/sh\nprintf '%s\\n' MINGW64_NT-10.0\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("sh", "install.sh")
	cmd.Dir = dir
	cmd.Env = append(envWithout(os.Environ(), "PATH"), "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "HOME="+dir, "DEEPROUTER_NO_SETUP=1")
	out, err := cmd.CombinedOutput()
	if err == nil {
		t.Fatalf("windows shell was accepted:\n%s", out)
	}
	text := string(out)
	if !strings.Contains(text, "install.ps1") || !strings.Contains(text, "irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex") || !strings.Contains(text, "powershell -ExecutionPolicy Bypass") {
		t.Fatalf("hint:\n%s", text)
	}
}

func TestInstallPS1Contract(t *testing.T) {
	root := moduleRoot(t)
	body, err := os.ReadFile(filepath.Join(root, "install.ps1"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, name := range []string{
		"deeprouter-windows-",
		"checksums.txt",
		`Programs\deeprouter`,
		"deeprouter-setup.cmd",
		"DEEPROUTER_NO_SETUP",
		"HKCU",
		"Get-FileHash",
		"irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex",
		`powershell -ExecutionPolicy Bypass -c "irm https://raw.githubusercontent.com/zfg5118/deeprouter-setup/main/install.ps1 | iex"`,
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("install.ps1 missing %s", name)
		}
	}
}

func TestReleaseWorkflowBuildsWindows(t *testing.T) {
	root := moduleRoot(t)
	body, err := os.ReadFile(filepath.Join(root, ".github", "workflows", "release.yml"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(body)
	for _, name := range []string{
		"windows/amd64",
		"windows/arm64",
		"deeprouter-windows-amd64.exe",
		"deeprouter-windows-arm64.exe",
		"checksums.txt",
	} {
		if !strings.Contains(text, name) {
			t.Fatalf("release workflow missing %s", name)
		}
	}
}

func TestInstallDownloadsLatestUnlessVersionPinned(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("install.sh is a POSIX installer")
	}
	root := moduleRoot(t)
	script, err := os.ReadFile(filepath.Join(root, "install.sh"))
	if err != nil {
		t.Fatal(err)
	}
	payload := []byte("not-a-real-binary\n")
	sum := sha256.Sum256(payload)

	assertURLs := func(t *testing.T, version string, wantLatest bool) {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "install.sh"), script, 0o755); err != nil {
			t.Fatal(err)
		}
		logPath := filepath.Join(dir, "curl.log")
		payloadPath := filepath.Join(dir, "payload")
		assetPath := filepath.Join(dir, "asset-name")
		if err := os.WriteFile(payloadPath, payload, 0o644); err != nil {
			t.Fatal(err)
		}
		shim := filepath.Join(dir, "curl")
		shimSrc := fmt.Sprintf(`#!/bin/sh
out=
url=
prev=
for arg in "$@"; do
  if [ "$prev" = "-o" ]; then
    out=$arg
    prev=
    continue
  fi
  case "$arg" in
    -o) prev=-o ;;
    -*) ;;
    *) url=$arg ;;
  esac
done
printf '%%s\n' "$url" >> %q
base=${url##*/}
if [ "$base" = "checksums.txt" ]; then
  asset=$(cat %q)
  printf '%%s  %%s\n' %q "$asset" > "$out"
else
  printf '%%s\n' "$base" > %q
  cp %q "$out"
fi
exit 0
`, logPath, assetPath, fmt.Sprintf("%x", sum), assetPath, payloadPath)
		if err := os.WriteFile(shim, []byte(shimSrc), 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("sh", "install.sh", "--prefix", filepath.Join(dir, "bin"), "--no-path")
		cmd.Dir = dir
		env := envWithout(os.Environ(), "DEEPROUTER_SETUP_VERSION")
		env = envWithout(env, "DEEPROUTER_SETUP_REPO")
		env = append(env, "HOME="+dir, "PATH="+dir+string(os.PathListSeparator)+os.Getenv("PATH"), "DEEPROUTER_NO_SETUP=1")
		if version != "" {
			env = append(env, "DEEPROUTER_SETUP_VERSION="+version)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("install: %v\n%s", err, out)
		}
		logged, err := os.ReadFile(logPath)
		if err != nil {
			t.Fatal(err)
		}
		text := string(logged)
		if wantLatest {
			if !strings.Contains(text, "https://github.com/zfg5118/deeprouter-setup/releases/latest/download/deeprouter-") ||
				!strings.Contains(text, "https://github.com/zfg5118/deeprouter-setup/releases/latest/download/checksums.txt") {
				t.Fatalf("latest urls:\n%s", text)
			}
			if strings.Contains(text, "/releases/download/v") {
				t.Fatalf("pinned a version:\n%s", text)
			}
			return
		}
		prefix := "https://github.com/zfg5118/deeprouter-setup/releases/download/v" + version + "/"
		if !strings.Contains(text, prefix+"deeprouter-") || !strings.Contains(text, prefix+"checksums.txt") {
			t.Fatalf("pinned urls:\n%s", text)
		}
		if strings.Contains(text, "/releases/latest/download/") {
			t.Fatalf("used latest:\n%s", text)
		}
	}

	assertURLs(t, "", true)
	assertURLs(t, "0.1.1", false)
}

func describedVersion(t *testing.T, root string) string {
	t.Helper()
	cmd := exec.Command("git", "describe", "--tags")
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		return "dev"
	}
	ver := strings.TrimSpace(string(out))
	if ver == "" {
		return "dev"
	}
	return ver
}

func envWithout(base []string, key string) []string {
	prefix := key + "="
	out := make([]string, 0, len(base))
	for _, kv := range base {
		if strings.HasPrefix(kv, prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
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
