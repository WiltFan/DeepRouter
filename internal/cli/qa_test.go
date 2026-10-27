package cli

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/testgate"
)

func TestDevNullIsNotATerminal(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix tty detection")
	}
	f, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if isTerminal(f) {
		t.Fatal("/dev/null was treated as a terminal")
	}
}

func TestLocaleDefaultsToChinese(t *testing.T) {
	t.Setenv("LC_ALL", "")
	t.Setenv("LANG", "")
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
	if !promptLocaleZH() {
		t.Fatal("empty locale should be Chinese")
	}
	for _, loc := range []string{"C", "POSIX", "fr_FR.UTF-8", "zh_CN.UTF-8"} {
		t.Setenv("LC_ALL", loc)
		if !promptLocaleZH() {
			t.Fatalf("%s should be Chinese", loc)
		}
	}
	t.Setenv("LC_ALL", "en_GB.UTF-8")
	if promptLocaleZH() {
		t.Fatal("en_GB should be English")
	}
	if err := setUILang("en"); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	if promptLocaleZH() {
		t.Fatal("--lang en should win")
	}
	uiLang = ""
}

func TestCustomGatewayHintUsesFullURL(t *testing.T) {
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	zh := tokenPrompt("Codex / ChatGPT", "custom", "http://127.0.0.1:18080", "", "")
	if zh != "请输入该网关的令牌（http://127.0.0.1:18080），用于 Codex / ChatGPT：" {
		t.Fatalf("zh %q", zh)
	}
	if strings.Contains(zh, "创建") || strings.Contains(zh, "deeprouter.top") {
		t.Fatalf("custom hint %q", zh)
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	en := tokenPrompt("", "custom", "https://deeprouter.top/", "", "")
	if en != "Enter the token for https://deeprouter.top: " {
		t.Fatalf("en %q", en)
	}
}

func TestModelAnswerRejectsBareN(t *testing.T) {
	id, clear, err := chooseModel("n", nil, nil)
	if err == nil || id != "" || clear {
		t.Fatalf("empty list accepted n: %q %v %v", id, clear, err)
	}
	id, clear, err = chooseModel("n", []string{"gemini-2.5-pro"}, nil)
	if err == nil || clear || id != "" {
		t.Fatalf("listed menu accepted n: %q %v %v", id, clear, err)
	}
	id, clear, err = chooseModel("gemini-2.5-pro", []string{"gemini-2.5-pro"}, nil)
	if err != nil || clear || id != "gemini-2.5-pro" {
		t.Fatalf("exact id %q %v %v", id, clear, err)
	}
	id, clear, err = chooseModel("gpt-5.4", nil, nil)
	if err != nil || clear || id != "gpt-5.4" {
		t.Fatalf("plausible id %q %v %v", id, clear, err)
	}
}

func TestSetupRejectsBadKeyWithoutWriting(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.BadKey)
	if code == 0 {
		t.Fatalf("bad key exited 0\n%s\n%s", out, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(err) {
		t.Fatalf("bad key wrote codex: %v", err)
	}
	if _, err := os.Stat(filepath.Join(home, ".claude", "settings.json")); !os.IsNotExist(err) {
		t.Fatalf("bad key wrote claude: %v", err)
	}
	if !strings.Contains(errOut, "401") {
		t.Fatalf("stderr:\n%s", errOut)
	}
}

func TestSetupDefaultIsCodexOnly(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey)
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".codex", "config.toml")); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".zshrc"),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s exists: %v", path, err)
		}
	}
	if strings.Contains(out, "Claude Code") || strings.Contains(out, shellFollowupNeedle()) {
		t.Fatalf("output:\n%s", out)
	}
}

func TestReportSkippedDoesNotSayPassed(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	if _, _, code := run(t, home, nil, "setup",
		"--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--apps", "gemini", "--gemini-model", "default"); code != 0 {
		t.Fatal(code)
	}
	// Replace the saved model list behavior by pointing test at a gateway
	// whose catalog has no gemini id. The profile still probes Gemini.
	bare := startNoGeminiGate(t)
	out, errOut, code := run(t, home, nil, "test", "--base-url", bare, "--api-key", testgate.GoodKey, "--timeout", "5s")
	if code != 0 {
		t.Fatalf("test %d\n%s\n%s", code, out, errOut)
	}
	if strings.Contains(out, "Passed.") {
		t.Fatalf("skipped gemini was reported as Passed:\n%s", out)
	}
	if !strings.Contains(out, "skipped") {
		t.Fatalf("summary:\n%s", out)
	}
}

func TestEmptyKeyCanBeEnteredAgain(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	// Platform 4 is the custom gateway so the retry stays on the local server.
	stdout, stderr, code := runWizard(t, home, "", "4\n\n"+srv.URL+"\n\n"+testgate.GoodKey+"\n\n\nn\n")
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "令牌不能为空，请重新输入。") || !strings.Contains(stderr, "请输入该网关的令牌（"+srv.URL+"）") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), testgate.GoodKey) {
		t.Fatal("key was not saved after the retry")
	}
}

func TestFailedProbeRollsBackTheWrite(t *testing.T) {
	home := t.TempDir()
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key != testgate.GoodKey && r.URL.Path != "/v1/models" {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"}]}`)
			return
		}
		posts++
		if posts >= 2 {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	withTerminal(t)
	var stdout, stderr bytes.Buffer
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString("4\n\n"+srv.URL+"\n"+testgate.GoodKey+"\n\n\n2\n"))
	if err != nil || code != 1 {
		t.Fatalf("wizard %d %v\n%s\n%s", code, err, stdout.String(), stderr.String())
	}
	text := stdout.String() + stderr.String()
	if !strings.Contains(text, "Rolled back the files just written.") || !strings.Contains(text, "503") {
		t.Fatalf("output:\n%s", text)
	}
	if _, statErr := os.Stat(filepath.Join(home, ".codex", "config.toml")); !os.IsNotExist(statErr) {
		t.Fatalf("rollback left codex config: %v", statErr)
	}
}

func TestEnterSelectsProbedRecommendation(t *testing.T) {
	home := t.TempDir()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key != testgate.GoodKey && r.URL.Path != "/v1/models" {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"recommended_model":"gpt-5.5","data":[{"id":"gpt-5.5","recommended":true},{"id":"gpt-5.4"},{"id":"gpt-4o"}]}`)
			return
		}
		body, _ := io.ReadAll(r.Body)
		if strings.Contains(string(body), `"gpt-5.5"`) {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	withTerminal(t)
	stdout, stderr, code := runWizard(t, home, "", "4\n\n"+srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n")
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if clientDefaultModelZH != "不指定模型（使用客户端默认）" {
		t.Fatalf("label %q", clientDefaultModelZH)
	}
	if !strings.Contains(stderr, "  1) "+clientDefaultModelEN) {
		t.Fatalf("option 1:\n%s", stderr)
	}
	if !strings.Contains(stderr, "  2) gpt-5.4") {
		t.Fatalf("verified model should follow the client default:\n%s", stderr)
	}
	if strings.Contains(stderr, "  1) gpt-5.5") || strings.Contains(stderr, "  2) gpt-5.5") || strings.Contains(stderr, "  3) gpt-5.5") {
		t.Fatalf("failed model is still a choice:\n%s", stderr)
	}
	cfg := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(cfg, "model = ") || strings.Contains(cfg, "gpt-5.5") || strings.Contains(cfg, "gpt-5.4") {
		t.Fatalf("config wrote a probed model:\n%s", cfg)
	}
	if !strings.Contains(stdout, "gpt-5.4") {
		t.Fatalf("connectivity did not probe a working model:\n%s", stdout)
	}
}

func startNoGeminiGate(t *testing.T) string {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" {
			key = r.Header.Get("x-goog-api-key")
		}
		if key != testgate.GoodKey {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"}]}`)
			return
		}
		http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
	}))
	t.Cleanup(srv.Close)
	return srv.URL
}
