package cli

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zfg5118/deeprouter/internal/claudedesktop"
	"github.com/zfg5118/deeprouter/internal/gateway"
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

func TestUnknownLangEnvFollowsTheLocale(t *testing.T) {
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
	t.Setenv("LANG", "")
	for _, tc := range []struct{ env, locale string }{
		{"zh_TW", "en_US.UTF-8"},
		{"ja", ""},
	} {
		uiLang = ""
		t.Setenv("DEEPROUTER_LANG", tc.env)
		t.Setenv("LC_ALL", tc.locale)
		var stdout, stderr bytes.Buffer
		if code := mainIO([]string{"version"}, "1.2.3", &stdout, &stderr, strings.NewReader("")); code != 0 || stdout.String() != "deeprouter 1.2.3\n" || stderr.Len() != 0 {
			t.Fatalf("DEEPROUTER_LANG=%s: exit %d, stdout %q, stderr %q", tc.env, code, stdout.String(), stderr.String())
		}
		if got, want := promptLocaleZH(), tc.locale == ""; got != want {
			t.Errorf("DEEPROUTER_LANG=%s with LC_ALL=%q: Chinese is %v", tc.env, tc.locale, got)
		}
	}
	uiLang = ""
	t.Setenv("DEEPROUTER_LANG", "")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	var stderr bytes.Buffer
	if code := mainIO([]string{"--lang", "ja", "version"}, "1.2.3", io.Discard, &stderr, strings.NewReader("")); code != 2 || stderr.String() != "deeprouter：语言只能是 zh 或 en\n" {
		t.Fatalf("--lang ja: exit %d, stderr %q", code, stderr.String())
	}
}

func TestSkipReasonLeavesOutTheAdvice(t *testing.T) {
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
	msg := "分组 default 下模型 gpt-5.5 无可用渠道"
	noChannel := gateway.Check{Status: 503, Message: msg, Hint: gateway.Hint(503, `{"error":{"message":"`+msg+`"}}`)}
	plain := gateway.Check{Status: 500, Hint: gateway.Hint(500, "oops")}
	for _, tc := range []struct {
		locale string
		check  gateway.Check
		want   string
	}{
		{"zh_CN.UTF-8", noChannel, "503 没有可用渠道。网关返回：" + msg},
		{"en_US.UTF-8", noChannel, "503 No channel. Gateway said: " + msg},
		{"zh_CN.UTF-8", plain, "HTTP 500。"},
		{"en_US.UTF-8", plain, "HTTP 500."},
	} {
		t.Setenv("LC_ALL", tc.locale)
		if got := skipReason(tc.check); got != tc.want {
			t.Errorf("%s %d: got %q, want %q", tc.locale, tc.check.Status, got, tc.want)
		}
	}
}

func TestOtherStatusHintSaysErrorOnce(t *testing.T) {
	uiLang = "zh"
	t.Cleanup(func() { uiLang = "" })
	body := `{"error":{"message":"unknown model"}}`
	for _, tc := range []struct {
		check gateway.Check
		want  string
	}{
		{gateway.Check{Status: 400, Hint: gateway.Hint(400, body), Message: "unknown model"}, "HTTP 400。网关返回：unknown model"},
		{gateway.Check{Status: 400, Hint: gateway.Hint(400, "")}, "HTTP 400。"},
	} {
		if got := localHint(tc.check); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
	msg := "分组 default 下模型 gpt-5.4 无可用渠道"
	noChannel := gateway.Check{Status: 503, Message: msg, Hint: gateway.Hint(503, `{"error":{"message":"`+msg+`"}}`)}
	if got, want := localHint(noChannel), "503 没有可用渠道：这个模型现在不可用，请换一个模型。网关返回："+msg; got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}

func TestHintMarksTheGatewayMessage(t *testing.T) {
	t.Cleanup(func() { uiLang = "" })
	msg := "分组 default 下模型 gpt-5.5 无可用渠道"
	noChannel := gateway.Check{Status: 503, Message: msg, Hint: gateway.Hint(503, `{"error":{"message":"`+msg+`"}}`)}
	badKey := gateway.Check{Status: 401, Message: "Invalid token", Hint: gateway.Hint(401, `{"error":{"message":"Invalid token"}}`)}
	serverError := gateway.Check{Status: 500, Message: "oops", Hint: gateway.Hint(500, "oops")}
	refused := gateway.Check{Hint: "The connection was refused. Check the host, port, and that the gateway is running."}
	for _, tc := range []struct {
		lang  string
		check gateway.Check
		want  string
	}{
		{"en", noChannel, "503 No channel. No channel is available for this model right now. Try another model. Gateway said: " + msg},
		{"en", badKey, "401 Bad key. The API key was rejected. Check the key on this profile. Gateway said: Invalid token"},
		{"zh", badKey, "401 令牌无效：网关拒绝了这个令牌，请检查令牌。网关返回：Invalid token"},
		{"en", serverError, "HTTP 500. Gateway said: oops"},
		{"zh", serverError, "HTTP 500。网关返回：oops"},
		{"en", gateway.Check{Status: 500, Hint: gateway.Hint(500, "")}, "HTTP 500."},
		{"en", refused, refused.Hint},
		{"zh", refused, "连接被拒绝。请检查主机、端口，以及网关是否在运行。"},
	} {
		uiLang = tc.lang
		if got := localHint(tc.check); got != tc.want {
			t.Errorf("%s %d: got %q, want %q", tc.lang, tc.check.Status, got, tc.want)
		}
	}
}

func TestCheckLineKeepsTheTimesApart(t *testing.T) {
	uiLang = "en"
	t.Cleanup(func() { uiLang = "" })
	for _, tc := range []struct {
		check gateway.Check
		want  string
	}{
		{gateway.Check{Status: 200, StatusText: "OK"}, "200 OK               ttfb 0ms   total 0ms"},
		{gateway.Check{Status: 503, StatusText: "Service Unavailable"}, "503 Service Unavailable   ttfb 0ms   total 0ms"},
		{gateway.Check{}, "failed               ttfb 0ms   total 0ms"},
	} {
		if got := describeCheck(tc.check); got != tc.want {
			t.Errorf("got %q, want %q", got, tc.want)
		}
	}
}

func TestInterruptSaysWhatHappenedToTheWrite(t *testing.T) {
	uiLang = "en"
	t.Cleanup(func() { uiLang = "" })
	write := func(g *interruptGuard) {
		if err := g.write(writeSnapshot{}, func() error { return nil }); err != nil {
			t.Fatal(err)
		}
	}
	for _, tc := range []struct {
		name string
		step func(*interruptGuard)
		want string
	}{
		{"before the write", func(*interruptGuard) {}, "Setup cancelled. Nothing was written."},
		{"before the test passed", write, "Interrupted. Rolled back the files just written."},
		{"after the test passed", func(g *interruptGuard) { write(g); g.settle() }, "Interrupted. The files above stay as written."},
		{"after a rollback", func(g *interruptGuard) { write(g); g.rollback() }, "Interrupted."},
	} {
		var stderr bytes.Buffer
		g := &interruptGuard{e: env{stderr: &stderr}}
		tc.step(g)
		if code := g.interrupted(); code != 130 {
			t.Fatalf("%s: exit %d", tc.name, code)
		}
		if got := stderr.String(); got != "\n"+tc.want+"\n" {
			t.Errorf("%s: %q", tc.name, got)
		}
	}
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
	if en != "Enter the gateway token (https://deeprouter.top): " {
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
	if !strings.Contains(out, ":generateContent\n  Skipped. No gemini chat model") || strings.Contains(out, "\n  skipped\n") {
		t.Fatalf("a skipped check should say so once:\n%s", out)
	}
}

func TestEmptyKeyCanBeEnteredAgain(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	// The last platform option is the custom gateway so the retry stays on the local server.
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("", srv.URL+"\n\n"+testgate.GoodKey+"\n\nn\n"))
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
	code, err := cmdWizard(&stdout, &stderr, bytes.NewBufferString(wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\n2\n")))
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

func TestPickAnotherModelAfterFailedProbeCanLeaveItUnset(t *testing.T) {
	home := t.TempDir()
	var mu sync.Mutex
	seen := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`)
			return
		}
		var payload struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		id := payload.Model
		mu.Lock()
		seen[id]++
		n := seen[id]
		mu.Unlock()
		if id == "gpt-5.4" && n >= 2 {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	withTerminal(t)
	// Pick gpt-5.4, which stops answering after the write; then choose
	// "pick another model" and press Enter for the client default.
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n2\n1\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "Codex / ChatGPT: no model set; the client uses its own default.") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "  2) gpt-5.4-mini  (answered a test request)") || strings.Contains(stderr, "gpt-4.1") {
		t.Fatalf("the second picker should offer the model that still answers:\n%s", stderr)
	}
	cfg := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
	if strings.Contains(cfg, "model = ") {
		t.Fatalf("config still sets a model:\n%s", cfg)
	}
	if !strings.Contains(stdout, "1 check failed.") || !strings.Contains(stdout, "\nPassed.\n") {
		t.Fatalf("the first test should fail and the second pass:\n%s", stdout)
	}
}

func TestWizardConfirmsTheCodexKeyBeforeTheCLIQuestion(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stubCLIs(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	}, func(context.Context, io.Writer, io.Writer, string, ...string) error { return nil })
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")
	// One buffer for stdout and stderr, as in a terminal.
	var out bytes.Buffer
	code, err := cmdWizard(&out, &out, bytes.NewBufferString(wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n")))
	if err != nil || code != 0 {
		t.Fatalf("wizard %d %v\n%s", code, err, out.String())
	}
	text := out.String()
	passed := strings.Index(text, "\nPassed.\n")
	key := strings.Index(text, "\nCodex key    ready in ")
	cli := strings.Index(text, "\n\ncodex is not installed yet.")
	next := strings.Index(text, "\nNext:\n")
	if passed < 0 || key < passed || cli < key || next < cli {
		t.Errorf("want the test result, the Codex key line, a blank line and the CLI question, then Next:\n%s", text)
	}
	assertSummaryOnce(t, text, "Gateway", srv.URL, 10, 9)
	assertSummaryOnce(t, text, "Key", "sk-", 10, 9)
	assertSummaryOnce(t, text, "Profile", "default", 10, 9)
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
			_, _ = io.WriteString(w, `{"recommended_model":"gpt-5.5","data":[{"id":"gpt-5.5","recommended":true},{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`)
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
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if clientDefaultModelZH != "不指定模型（使用客户端默认）" {
		t.Fatalf("label %q", clientDefaultModelZH)
	}
	if !strings.Contains(stderr, "  1) "+clientDefaultModelEN) {
		t.Fatalf("option 1:\n%s", stderr)
	}
	if !strings.Contains(stderr, "  2) gpt-5.4  (answered a test request)") || !strings.Contains(stderr, "  3) gpt-5.4-mini  (answered a test request)") || strings.Contains(stderr, "gpt-4o") {
		t.Fatalf("verified models should follow the client default:\n%s", stderr)
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

func TestSwitchRefusesModelThatDoesNotAnswer(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	if out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey); code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	cfg := filepath.Join(home, ".codex", "config.toml")
	before := mustRead(t, cfg)
	out, errOut, code := run(t, home, nil, "switch", "--codex-model", "gpt-5.5")
	if code != 1 {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "model gpt-5.5 did not answer a test request: 503") || !strings.HasSuffix(errOut, "Nothing was written.\n") {
		t.Fatalf("stderr:\n%s", errOut)
	}
	if mustRead(t, cfg) != before {
		t.Fatal("switch wrote a model that does not answer")
	}
	if out, errOut, code = run(t, home, nil, "switch", "--codex-model", "gpt-4.1"); code != 0 {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, cfg), `model = "gpt-4.1"`) {
		t.Fatalf("codex:\n%s", mustRead(t, cfg))
	}
}

func TestProfileUseChecksTheTokenFirst(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	if out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey); code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	cfg := filepath.Join(home, ".codex", "config.toml")
	before := mustRead(t, cfg)
	if out, errOut, code := run(t, home, nil, "profile", "add", "spent", "--base-url", srv.URL, "--api-key", testgate.QuotaKey); code != 0 {
		t.Fatalf("add %d\n%s\n%s", code, out, errOut)
	}
	out, errOut, code := run(t, home, nil, "profile", "use", "spent")
	if code != 1 || !strings.Contains(errOut, "(403)") || !strings.Contains(errOut, "Nothing was written.") {
		t.Fatalf("use %d\n%s\n%s", code, out, errOut)
	}
	if mustRead(t, cfg) != before {
		t.Fatal("profile use wrote a refused token")
	}
	status, _, _ := run(t, home, nil, "status", "--json")
	if !strings.Contains(status, `"profile": "default"`) {
		t.Fatalf("active profile changed:\n%s", status)
	}
}

func TestProfileAddUseOnAFreshHomeWritesCodexOnly(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	out, errOut, code := run(t, home, nil, "profile", "add", "work", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--use")
	if code != 0 {
		t.Fatalf("add --use %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), testgate.GoodKey) {
		t.Fatal("codex was not written")
	}
	for _, path := range []string{
		filepath.Join(home, ".claude", "settings.json"),
		filepath.Join(home, ".zshrc"),
		claudedesktop.Dir(home),
	} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("%s exists: %v", path, err)
		}
	}
	if strings.Contains(out, "Claude Code") {
		t.Fatalf("stdout:\n%s", out)
	}
}

func TestSetupTokenPromptEndOfInputCancels(t *testing.T) {
	home := t.TempDir()
	withTerminal(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	out, errOut, code := run(t, home, bytes.NewReader([]byte("\n")), "setup")
	if code != 2 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(errOut, "令牌不能为空，请重新输入。") || !strings.HasSuffix(errOut, "已取消配置，没有写入任何文件。\n") || strings.Contains(errOut, "EOF") {
		t.Fatalf("stderr:\n%s", errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".deeprouter")); !os.IsNotExist(err) {
		t.Fatalf("cancelled setup wrote state: %v", err)
	}
}

func TestChineseErrorsAndFlagErrors(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"bogus"}, "deeprouter：未知命令 \"bogus\"\n"},
		{[]string{"setup", "--nope"}, "未知参数：-nope\n用法：deeprouter setup [参数]"},
		{[]string{"test", "--timeout", "soon"}, "参数 -timeout 的值 \"soon\" 无效："},
		{[]string{"setup", "--apps", "nope"}, "deeprouter：未知程序 \"nope\"\n"},
		{[]string{"setup", "--base-url", "ftp://x"}, "deeprouter：网关地址必须以 http:// 或 https:// 开头\n"},
		{[]string{"setup"}, "deeprouter：需要令牌。请传入 --api-key、设置 DEEPROUTER_API_KEY，或使用 --api-key-stdin\n"},
		{[]string{"profile", "use"}, "deeprouter：用法：deeprouter profile use <名称>\n"},
		{[]string{"switch", "--codex-model", "gpt-4.1"}, "deeprouter：还没有当前配置。请先运行 deeprouter setup"},
		{[]string{"switch", "--profile", "ghost"}, "deeprouter：没有名为 \"ghost\" 的配置。创建方法：deeprouter profile add ghost"},
		{[]string{"setup", "--codex-wire-api", "grpc"}, "deeprouter：--codex-wire-api 只能是 responses 或 chat\n"},
	}
	for _, tc := range cases {
		_, errOut, code := run(t, home, nil, tc.args...)
		if code == 0 || !strings.Contains(errOut, tc.want) {
			t.Errorf("%v: code %d\n%s", tc.args, code, errOut)
		}
	}
	_, errOut, code := run(t, home, nil, "bogus")
	if code != 2 || !strings.HasPrefix(errOut, "deeprouter：未知命令 \"bogus\"\n\n") || strings.Count(errOut, "未知命令") != 1 || !strings.Contains(errOut, "用法：") {
		t.Errorf("unknown command should be the first line, once, before the help: code %d\n%s", code, errOut)
	}
}

func TestSetupSummaryLabelsAndColumns(t *testing.T) {
	srv := testgate.New()
	defer srv.Close()

	home := t.TempDir()
	withTerminal(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("1,2", srv.URL+"\n"+testgate.GoodKey+"\n\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	claudePath := filepath.FromSlash("~/.claude/settings.json")
	codexPath := filepath.FromSlash("~/.codex/config.toml")
	for _, want := range []string{
		"配置        default（自定义）",
		"\n  Claude Code      " + claudePath + "   模型：",
		"\n  Codex / ChatGPT  " + codexPath + "   模型：",
		"然后运行：claude\n",
		"或者运行：codex\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "配置     default（自定义）") {
		t.Errorf("summary header printed twice:\n%s", stdout)
	}
	if strings.Contains(stdout, "Custom") {
		t.Errorf("English label in Chinese output:\n%s", stdout)
	}

	t.Setenv("LC_ALL", "en_US.UTF-8")
	out, errOut, code := run(t, t.TempDir(), nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "codex,claude-code")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{
		"\n  Claude Code      " + claudePath + "   model client default\n",
		"\n  Codex / ChatGPT  " + codexPath + "   model client default\n\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
	if key, check := strings.Index(out, "Codex key    ready in "), strings.Index(out, "\nThen check the gateway:"); key < 0 || check < key {
		t.Errorf("the Codex key line should come before the gateway check hint:\n%s", out)
	}

	out, errOut, code = run(t, t.TempDir(), nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "codex,gemini")
	if code != 0 {
		t.Fatalf("setup gemini %d\n%s\n%s", code, out, errOut)
	}
	shellRow := "\n  Shell "
	if useUserEnv() {
		shellRow = "\n  User environment "
	}
	if gem, sh := strings.Index(out, "\n  Gemini CLI "), strings.Index(out, shellRow); gem < 0 || sh < gem {
		t.Errorf("the shell row should follow the Gemini CLI row:\n%s", out)
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

func TestWizardRepromptsABadGatewayURL(t *testing.T) {
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	for _, tc := range []struct {
		lang string
		want string
	}{
		{"en_US.UTF-8", "The gateway URL is not valid. Enter it again."},
		{"zh_CN.UTF-8", "网关地址无效，请重新输入。"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.lang)
			t.Setenv("LANG", tc.lang)
			stdout, stderr, code := runWizard(t, t.TempDir(), "", wizardCustom("", "not a url\n"+srv.URL+"\n"+testgate.GoodKey+"\n\nn\n"))
			if code != 0 {
				t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, tc.want) || strings.Contains(stderr, "invalid character") || strings.Contains(stderr, "parse \"") {
				t.Fatalf("stderr:\n%s", stderr)
			}
		})
	}
}

func TestModelMenuListsOnlyVerifiedModels(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"},{"id":"gpt-6.1-terra"},{"id":"gpt-5.4-mini"}]}`)
			return
		}
		body, _ := io.ReadAll(io.LimitReader(r.Body, 1<<20))
		if strings.Contains(string(body), "gpt-6.1-terra") {
			http.Error(w, `{"error":{"message":"model forbidden"}}`, http.StatusForbidden)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	withTerminal(t)
	stdout, stderr, code := runWizard(t, t.TempDir(), "", wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, ") gpt-6.1-terra") || !strings.Contains(stderr, "Skipped gpt-6.1-terra") {
		t.Fatalf("menu:\n%s", stderr)
	}
	if !strings.Contains(stderr, "  1) "+clientDefaultModelEN) || !strings.Contains(stderr, "  2) gpt-5.4  (answered a test request)") || !strings.Contains(stderr, "  3) gpt-5.4-mini  (answered a test request)") || strings.Contains(stderr, "gpt-4o") {
		t.Fatalf("menu:\n%s", stderr)
	}
}

func TestSetupLeavesExistingCodexModel(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("model = \"gpt-old\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := run(t, "", nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey)
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, cfg), "model = \"gpt-old\"") {
		t.Fatalf("model removed:\n%s", mustRead(t, cfg))
	}
	if !strings.Contains(out, "gpt-old") || strings.Contains(out, "config.toml   model client default") {
		t.Fatalf("summary:\n%s", out)
	}
	if out, errOut, code = run(t, "", nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--codex-model", "default"); code != 0 {
		t.Fatalf("default %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, cfg), "model = \"gpt-old\"") {
		t.Fatalf("default deleted the model:\n%s", mustRead(t, cfg))
	}
	if out, errOut, code = run(t, "", nil, "switch", "--codex-model", "default"); code != 0 {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(mustRead(t, cfg), "model = \"gpt-old\"") {
		t.Fatalf("switch default deleted the model:\n%s", mustRead(t, cfg))
	}
	if out, errOut, code = run(t, "", nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--codex-model", "gpt-5.4"); code != 0 {
		t.Fatalf("replace %d\n%s\n%s", code, out, errOut)
	}
	got := mustRead(t, cfg)
	if !strings.Contains(got, "model = \"gpt-5.4\"") || strings.Contains(got, "gpt-old") {
		t.Fatalf("replace:\n%s", got)
	}
}

func TestWizardEnterKeepsExistingCodexModel(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	isolateHome(t, home)
	cfg := filepath.Join(home, ".codex", "config.toml")
	if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfg, []byte("model = \"gpt-old\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(mustRead(t, cfg), "model = \"gpt-old\"") {
		t.Fatalf("wizard removed the model:\n%s", mustRead(t, cfg))
	}
	if !strings.Contains(stdout, "gpt-old") {
		t.Fatalf("summary hid the kept model:\n%s", stdout)
	}
}

func TestCLIRunLineExplainsNpmBinOffPath(t *testing.T) {
	root := t.TempDir()
	dir := root
	full := filepath.Join(root, "codex.cmd")
	if runtime.GOOS != "windows" {
		dir = filepath.Join(root, "bin")
		full = filepath.Join(dir, "codex")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	prevPrefix, prevLook := npmGlobalPrefix, cliLookPath
	npmGlobalPrefix = func() (string, error) { return root, nil }
	resetNpmPrefixCache()
	cliLookPath = func(string) (string, error) { return "", os.ErrNotExist }
	t.Cleanup(func() {
		npmGlobalPrefix = prevPrefix
		cliLookPath = prevLook
		resetNpmPrefixCache()
	})
	line := cliRunLine(false, "codex")
	if runtime.GOOS == "windows" {
		if !strings.Contains(line, "set PATH="+root) || !strings.Contains(line, full) {
			t.Fatalf("line:\n%s", line)
		}
	} else if !strings.Contains(line, full) || !strings.Contains(line, `export PATH="`+dir+`:$PATH"`) {
		t.Fatalf("line:\n%s", line)
	}
	if strings.Contains(cliRunLine(false, "claude"), "PATH=") {
		t.Fatal("a missing binary should stay a plain run line")
	}
	if got := inspectCLI("codex", "@openai/codex", ""); got.Path != full {
		t.Fatalf("off-PATH codex path %q", got.Path)
	}
	win := pathHint("windows", "Then run:  ", "然后运行：", "codex", `C:\npm`, `C:\npm\codex.cmd`)
	if !strings.Contains(win, `set PATH=C:\npm;%PATH%`) || !strings.Contains(win, `C:\npm\codex.cmd`) {
		t.Fatal(win)
	}
}

func TestReopenHintMentionsShellOnlyWhenChanged(t *testing.T) {
	if got := reopenHint(true, false, false); got != "Quit and reopen Codex / ChatGPT." {
		t.Fatal(got)
	}
	if got := reopenHint(true, true, true); !strings.Contains(got, "They do not read the shell startup file.") {
		t.Fatal(got)
	}
	if got := reopenHint(false, true, false); strings.Contains(got, "shell") {
		t.Fatal(got)
	}
}

func TestRestoreListUsesLocalTimeAndHelpDocumentsFlags(t *testing.T) {
	if got := formatBackupTime("2026-10-28T02:13:07Z", time.FixedZone("CST", 8*3600)); got != "2026-10-28 10:13:07 +0800" {
		t.Fatalf("local %q", got)
	}
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	if out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey); code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	if out, errOut, code := run(t, "", nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--codex-model", "gpt-5.4"); code != 0 {
		t.Fatalf("second %d\n%s\n%s", code, out, errOut)
	}
	out, errOut, code := run(t, "", nil, "restore", "--list")
	if code != 0 {
		t.Fatalf("list %d\n%s\n%s", code, out, errOut)
	}
	if !strings.Contains(out, "before the first edit") || !regexp.MustCompile(`\d{8}T\d+\.\d+Z  \d{4}-\d{2}-\d{2} \d{2}:\d{2}:\d{2} \+0000`).MatchString(out) {
		t.Fatalf("list:\n%s", out)
	}
	_, errOut, code = run(t, "", nil, "restore", "--help")
	if code != 0 || !strings.Contains(errOut, "List backups") || !strings.Contains(errOut, "local time") || !strings.Contains(errOut, "original is the copy") {
		t.Fatalf("help:\n%s", errOut)
	}
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	_, errOut, code = run(t, "", nil, "restore", "--help")
	if code != 0 || !strings.Contains(errOut, "列出备份") || !strings.Contains(errOut, "本地时间") || !strings.Contains(errOut, "备份 id 来自 --list") {
		t.Fatalf("zh help:\n%s", errOut)
	}
}

func assertSummaryOnce(t *testing.T, text, label, value string, setupWidth, reportWidth int) {
	t.Helper()
	setup := pad(label, setupWidth) + value
	report := pad(label, reportWidth) + value
	if strings.Count(text, setup) != 1 {
		t.Fatalf("%q count %d\n%s", setup, strings.Count(text, setup), text)
	}
	if strings.Contains(text, report) {
		t.Fatalf("report header %q is still present\n%s", report, text)
	}
}

func TestWizardPrintsTheSummaryHeaderOnce(t *testing.T) {
	srv := testgate.New()
	defer srv.Close()
	withTerminal(t)
	stubCLIs(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	}, func(context.Context, io.Writer, io.Writer, string, ...string) error { return nil })
	for _, tc := range []struct {
		lang    string
		width   int
		gateway string
		key     string
		profile string
	}{
		{"en_US.UTF-8", 10, "Gateway", "Key", "Profile"},
		{"zh_CN.UTF-8", 12, "网关", "令牌", "配置"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.lang)
			t.Setenv("LANG", tc.lang)
			home := t.TempDir()
			isolateHome(t, home)
			t.Setenv("DEEPROUTER_API_KEY", "")
			var out bytes.Buffer
			code, err := cmdWizard(&out, &out, bytes.NewBufferString(wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n")))
			if err != nil || code != 0 {
				t.Fatalf("wizard %d %v\n%s", code, err, out.String())
			}
			text := out.String()
			assertSummaryOnce(t, text, tc.gateway, srv.URL, tc.width, 9)
			assertSummaryOnce(t, text, tc.key, "sk-", tc.width, 9)
			assertSummaryOnce(t, text, tc.profile, "default", tc.width, 9)
		})
	}
}

func TestChooseModelRetestsAFailedIDWhenNothingPassed(t *testing.T) {
	id, clear, err := chooseModel("gemini-3.8-flash", nil, map[string]bool{
		"gemini-2.5-pro":   true,
		"gemini-3.8-flash": true,
	})
	if err != nil || clear || id != "gemini-3.8-flash" {
		t.Fatalf("id %q clear %v err %v", id, clear, err)
	}
	_, _, err = chooseModel("gemini-2.5-pro", []string{"gemini-3.8-flash"}, map[string]bool{"gemini-2.5-pro": true})
	if !errors.Is(err, errModelFailed) {
		t.Fatalf("listed failure: %v", err)
	}
	_, _, err = chooseModel("n", nil, nil)
	if err == nil || errors.Is(err, errModelFailed) {
		t.Fatalf("n should not be a model id: %v", err)
	}
}

func TestPickModelExplainsWhenEveryCandidateFailed(t *testing.T) {
	models, err := gateway.ParseModels([]byte(`{"data":[{"id":"gemini-2.5-pro"},{"id":"gemini-2.0-flash"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	probe := familyProbe{ran: true, skipped: []gateway.Skip{
		{ID: "gemini-2.5-pro", Check: gateway.Check{Status: http.StatusServiceUnavailable, Hint: "503 No channel"}},
		{ID: "gemini-2.0-flash", Check: gateway.Check{Status: 0, Hint: "timed out"}},
	}}
	cases := []struct {
		zh   bool
		kept string
		want string
	}{
		{false, "", "No Gemini CLI model answered. Timed-out and failed models were skipped. Press Enter to leave the model unset, or type a model id (it will be tested)."},
		{false, "gemini-2.5-pro", "No Gemini CLI model answered. Timed-out and failed models were skipped. Press Enter to keep the existing model gemini-2.5-pro, or type a model id (it will be tested)."},
		{true, "", "Gemini CLI 的候选模型都没有通过。超时和失败的模型已跳过。直接回车不指定模型，或手动输入模型 id（会再测一次）。"},
		{true, "gemini-2.5-pro", "Gemini CLI 的候选模型都没有通过。超时和失败的模型已跳过。直接回车保留已有模型 gemini-2.5-pro，或手动输入模型 id（会再测一次）。"},
	}
	for _, tc := range cases {
		var stderr bytes.Buffer
		got, clear, err := pickModel(bufio.NewReader(strings.NewReader("gemini-3.8-flash\n")), &stderr, "Gemini CLI", "gemini", models, tc.zh, "", "", "", probe, tc.kept)
		if err != nil || clear || got != "gemini-3.8-flash" {
			t.Fatalf("zh %v kept %q id %q clear %v err %v\n%s", tc.zh, tc.kept, got, clear, err, stderr.String())
		}
		if !strings.Contains(stderr.String(), tc.want) {
			t.Fatalf("missing %q\n%s", tc.want, stderr.String())
		}
	}
}

func TestTypedModelIsAcceptedWhenEveryCandidateFails(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gemini-2.0-flash"},{"id":"gemini-2.5-pro"},{"id":"gemini-1.5-flash"}]}`)
			return
		}
		if !strings.Contains(r.URL.Path, "gemini-3.8-flash") {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`)
	}))
	defer srv.Close()
	withTerminal(t)
	for _, tc := range []struct {
		lang, want string
	}{
		{"en_US.UTF-8", "No Gemini CLI model answered. Timed-out and failed models were skipped. Press Enter to leave the model unset, or type a model id (it will be tested)."},
		{"zh_CN.UTF-8", "Gemini CLI 的候选模型都没有通过。超时和失败的模型已跳过。直接回车不指定模型，或手动输入模型 id（会再测一次）。"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.lang)
			t.Setenv("LANG", tc.lang)
			stdout, stderr, code := runWizard(t, t.TempDir(), "", wizardCustom("5", srv.URL+"\n"+testgate.GoodKey+"\ngemini-3.8-flash\nn\n"))
			if code != 0 {
				t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
			}
			if !strings.Contains(stderr, tc.want) {
				t.Fatalf("prompt:\n%s", stderr)
			}
			if strings.Contains(stderr, "  2) gemini-2.5-pro") || strings.Contains(stderr, "  2) gemini-2.0-flash") {
				t.Fatalf("failed models stayed in the menu:\n%s", stderr)
			}
			settings := mustRead(t, filepath.Join(os.Getenv("HOME"), ".gemini", "settings.json"))
			if !strings.Contains(settings, "gemini-3.8-flash") {
				t.Fatalf("settings:\n%s", settings)
			}
		})
	}
}

func TestGrokChooseRequiresAConcreteModel(t *testing.T) {
	id, clear, err := chooseListed("", []string{"grok-4.6", "grok-4"}, nil, true)
	if err != nil || clear || id != "grok-4.6" {
		t.Fatalf("enter %q clear %v err %v", id, clear, err)
	}
	id, clear, err = chooseListed("1", []string{"grok-4.6", "grok-4"}, nil, true)
	if err != nil || clear || id != "grok-4.6" {
		t.Fatalf("1 %q clear %v err %v", id, clear, err)
	}
	id, clear, err = chooseListed("2", []string{"grok-4.6", "grok-4"}, nil, true)
	if err != nil || clear || id != "grok-4" {
		t.Fatalf("2 %q clear %v err %v", id, clear, err)
	}
	if _, _, err = chooseListed("default", []string{"grok-4.6"}, nil, true); !errors.Is(err, errModelRequired) {
		t.Fatalf("default err %v", err)
	}
	if _, _, err = chooseListed("", nil, nil, true); !errors.Is(err, errModelRequired) {
		t.Fatalf("empty list err %v", err)
	}
	id, clear, err = chooseModel("", []string{"gemini-2.5-pro"}, nil)
	if err != nil || !clear || id != "" {
		t.Fatalf("gemini enter id %q clear %v err %v", id, clear, err)
	}
	id, clear, err = chooseModel("1", []string{"gpt-5.4"}, nil)
	if err != nil || !clear || id != "" {
		t.Fatalf("codex option 1 id %q clear %v err %v", id, clear, err)
	}
}

func TestGrokPickModelHasNoClientDefault(t *testing.T) {
	models, err := gateway.ParseModels([]byte(`{"data":[{"id":"grok-4"},{"id":"grok-4.6"},{"id":"grok-3"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	probe := familyProbe{ran: true, passed: []string{"grok-4.6", "grok-4"}, skipped: []gateway.Skip{
		{ID: "grok-3", Check: gateway.Check{Status: http.StatusServiceUnavailable, Hint: "503 No channel"}},
	}}
	for _, tc := range []struct {
		zh           bool
		want, absent string
	}{
		{false, "Grok Build model (Enter selects the newest model that answered, grok-4.6):", clientDefaultModelEN},
		{true, "Grok Build 模型（直接回车选择检测通过的最新模型 grok-4.6）：", clientDefaultModelZH},
	} {
		var stderr bytes.Buffer
		got, clear, err := pickModel(bufio.NewReader(strings.NewReader("\n")), &stderr, "Grok Build", "grok", models, tc.zh, "", "", "", probe, "")
		if err != nil || clear || got != "grok-4.6" {
			t.Fatalf("zh %v id %q clear %v err %v\n%s", tc.zh, got, clear, err, stderr.String())
		}
		text := stderr.String()
		if !strings.Contains(text, tc.want) || !strings.Contains(text, "  1) grok-4.6") || !strings.Contains(text, "  2) grok-4") || strings.Contains(text, tc.absent) || strings.Contains(text, "  3) grok-3") {
			t.Fatalf("menu:\n%s", text)
		}
	}
	failed := familyProbe{ran: true, check: gateway.Check{Status: http.StatusServiceUnavailable, Hint: "503 No channel"}}
	for _, tc := range []struct {
		zh                bool
		lang, want, retry string
	}{
		{false, "en_US.UTF-8", "No Grok Build model answered. Timed-out and failed models were skipped. Type a model id (it will be tested).", "Grok Build needs a specific model"},
		{true, "zh_CN.UTF-8", "Grok Build 的候选模型都没有通过。超时和失败的模型已跳过。请输入模型 id（会再测一次）。", "Grok Build 必须选择一个具体模型"},
	} {
		t.Setenv("LC_ALL", tc.lang)
		t.Setenv("LANG", tc.lang)
		var stderr bytes.Buffer
		got, clear, err := pickModel(bufio.NewReader(strings.NewReader("\ngrok-4.6\n")), &stderr, "Grok Build", "grok", models, tc.zh, "", "", "", failed, "")
		if err != nil || clear || got != "grok-4.6" {
			t.Fatalf("fail zh %v id %q clear %v err %v\n%s", tc.zh, got, clear, err, stderr.String())
		}
		text := stderr.String()
		if !strings.Contains(text, tc.want) || !strings.Contains(text, tc.retry) || strings.Contains(text, clientDefaultModelEN) || strings.Contains(text, clientDefaultModelZH) || strings.Contains(text, "  1) ") {
			t.Fatalf("all-fail menu:\n%s", text)
		}
	}
}

func catalogGateway(catalog string, allow func(model string) bool) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" && r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, catalog)
			return
		}
		model := ""
		if r.Body != nil {
			raw, _ := io.ReadAll(r.Body)
			var payload struct {
				Model string `json:"model"`
			}
			_ = json.Unmarshal(raw, &payload)
			model = payload.Model
		}
		if strings.Contains(r.URL.Path, "/models/") {
			path := r.URL.Path
			if i := strings.Index(path, "/models/"); i >= 0 {
				model = strings.TrimSuffix(path[i+len("/models/"):], ":generateContent")
			}
		}
		if allow != nil && !allow(model) {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(r.URL.Path, "generateContent"):
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`)
		case strings.HasSuffix(r.URL.Path, "/responses"):
			_, _ = io.WriteString(w, `{"id":"resp","output":[{"type":"message","content":[{"type":"output_text","text":"pong"}]}]}`)
		default:
			_, _ = io.WriteString(w, `{"id":"chat","choices":[{"message":{"role":"assistant","content":"pong"}}]}`)
		}
	}))
}

func TestSetupGrokPicksNewestAndOthersStayUnset(t *testing.T) {
	const catalog = `{"data":[{"id":"grok-3"},{"id":"grok-4"},{"id":"grok-4.6"},{"id":"gemini-2.5-pro"},{"id":"gpt-5.4"}]}`
	srv := catalogGateway(catalog, func(model string) bool { return model != "grok-3" })
	defer srv.Close()
	stubCLIs(t, func(name string) (string, error) {
		if name == "npm" {
			return "/usr/bin/npm", nil
		}
		return "", os.ErrNotExist
	}, func(context.Context, io.Writer, io.Writer, string, ...string) error { return nil })
	withTerminal(t)

	for _, lang := range []string{"en_US.UTF-8", "zh_CN.UTF-8"} {
		t.Run("setup-"+lang, func(t *testing.T) {
			t.Setenv("LC_ALL", lang)
			t.Setenv("LANG", lang)
			home := t.TempDir()
			out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "grok")
			if code != 0 {
				t.Fatalf("grok %d\n%s\n%s", code, out, errOut)
			}
			text := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
			if !strings.Contains(text, "model = \"grok-4.6\"") || strings.Contains(text, "grok-3") {
				t.Fatalf("grok:\n%s", text)
			}
			if lang == "en_US.UTF-8" {
				if !strings.Contains(errOut, "Grok Build will use grok-4.6, the newest model that answered.") {
					t.Fatalf("stderr:\n%s", errOut)
				}
			} else if !strings.Contains(errOut, "Grok Build 将使用 grok-4.6，这是检测通过的最新模型。") {
				t.Fatalf("stderr:\n%s", errOut)
			}

			home = t.TempDir()
			out, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "gemini")
			if code != 0 {
				t.Fatalf("gemini %d\n%s\n%s", code, out, errOut)
			}
			settings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
			if strings.Contains(settings, `"name"`) {
				t.Fatalf("gemini model was set:\n%s", settings)
			}

			home = t.TempDir()
			out, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "codex")
			if code != 0 {
				t.Fatalf("codex %d\n%s\n%s", code, out, errOut)
			}
			codex := mustRead(t, filepath.Join(home, ".codex", "config.toml"))
			if strings.Contains(codex, `model = "`) {
				t.Fatalf("codex model was set:\n%s", codex)
			}
		})
	}

	t.Run("wizard", func(t *testing.T) {
		t.Setenv("LC_ALL", "en_US.UTF-8")
		t.Setenv("LANG", "en_US.UTF-8")
		home := t.TempDir()
		stdout, stderr, code := runWizard(t, home, "", wizardCustom("4", srv.URL+"\n"+testgate.GoodKey+"\n\n"))
		if code != 0 {
			t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
		}
		if strings.Contains(stderr, clientDefaultModelEN) || !strings.Contains(stderr, "  1) grok-4.6") || !strings.Contains(stderr, "  2) grok-4") || strings.Contains(stderr, "  3) grok-3") {
			t.Fatalf("menu:\n%s", stderr)
		}
		if !strings.Contains(stderr, "Skipped grok-3") {
			t.Fatalf("skip:\n%s", stderr)
		}
		text := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
		if !strings.Contains(text, `model = "grok-4.6"`) {
			t.Fatalf("grok:\n%s", text)
		}

		home = t.TempDir()
		stdout, stderr, code = runWizard(t, home, "", wizardCustom("5", srv.URL+"\n"+testgate.GoodKey+"\n\nn\n"))
		if code != 0 {
			t.Fatalf("gemini wizard %d\n%s\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "  1) "+clientDefaultModelEN) {
			t.Fatalf("gemini menu:\n%s", stderr)
		}
		settings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
		if strings.Contains(settings, `"name"`) {
			t.Fatalf("gemini model was set:\n%s", settings)
		}

		home = t.TempDir()
		stdout, stderr, code = runWizard(t, home, "", wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\nn\n"))
		if code != 0 {
			t.Fatalf("codex wizard %d\n%s\n%s", code, stdout, stderr)
		}
		if !strings.Contains(stderr, "  1) "+clientDefaultModelEN) {
			t.Fatalf("codex menu:\n%s", stderr)
		}
	})

	t.Run("zh-wizard", func(t *testing.T) {
		t.Setenv("LC_ALL", "zh_CN.UTF-8")
		t.Setenv("LANG", "zh_CN.UTF-8")
		home := t.TempDir()
		stdout, stderr, code := runWizard(t, home, "", wizardCustom("4", srv.URL+"\n"+testgate.GoodKey+"\n\n"))
		if code != 0 {
			t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
		}
		if strings.Contains(stderr, clientDefaultModelZH) || !strings.Contains(stderr, "直接回车选择检测通过的最新模型 grok-4.6") || !strings.Contains(stderr, "  1) grok-4.6") {
			t.Fatalf("menu:\n%s", stderr)
		}
		text := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
		if !strings.Contains(text, `model = "grok-4.6"`) {
			t.Fatalf("grok:\n%s", text)
		}
	})
}

func TestSetupGrokErrorsWhenNoModelAnswers(t *testing.T) {
	const catalog = `{"data":[{"id":"grok-4.6"},{"id":"grok-4"},{"id":"gemini-2.5-pro"}]}`
	srv := catalogGateway(catalog, func(model string) bool {
		return model == "grok-manual" || strings.HasPrefix(model, "gemini")
	})
	defer srv.Close()
	for _, tc := range []struct {
		lang, want string
	}{
		{"en_US.UTF-8", "No Grok Build model answered a test request. Timed-out and failed models were skipped."},
		{"zh_CN.UTF-8", "没有 Grok Build 模型通过测试请求。超时和失败的模型已跳过。"},
	} {
		t.Run(tc.lang, func(t *testing.T) {
			t.Setenv("LC_ALL", tc.lang)
			t.Setenv("LANG", tc.lang)
			home := t.TempDir()
			out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "grok")
			if code != 1 || !strings.Contains(errOut, tc.want) || !strings.Contains(errOut, uiText(tc.lang)) {
				t.Fatalf("grok %d\n%s\n%s", code, out, errOut)
			}
			if _, err := os.Stat(filepath.Join(home, ".grok")); !os.IsNotExist(err) {
				t.Fatalf("wrote grok config: %v", err)
			}
			if _, err := os.Stat(filepath.Join(home, ".deeprouter")); !os.IsNotExist(err) {
				t.Fatalf("wrote profile: %v", err)
			}

			home = t.TempDir()
			out, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "gemini")
			if code != 0 {
				t.Fatalf("gemini %d\n%s\n%s", code, out, errOut)
			}
			settings := mustRead(t, filepath.Join(home, ".gemini", "settings.json"))
			if strings.Contains(settings, `"name"`) {
				t.Fatalf("gemini model was set:\n%s", settings)
			}
		})
	}

	withTerminal(t)
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")
	home := t.TempDir()
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("4", srv.URL+"\n"+testgate.GoodKey+"\n\ngrok-manual\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "No Grok Build model answered. Timed-out and failed models were skipped. Type a model id (it will be tested).") || strings.Contains(stderr, clientDefaultModelEN) {
		t.Fatalf("prompt:\n%s", stderr)
	}
	text := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(text, `model = "grok-manual"`) {
		t.Fatalf("grok:\n%s", text)
	}

	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	home = t.TempDir()
	stdout, stderr, code = runWizard(t, home, "", wizardCustom("4", srv.URL+"\n"+testgate.GoodKey+"\n\ngrok-manual\n"))
	if code != 0 {
		t.Fatalf("zh wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "Grok Build 的候选模型都没有通过。超时和失败的模型已跳过。请输入模型 id（会再测一次）。") || strings.Contains(stderr, clientDefaultModelZH) {
		t.Fatalf("zh prompt:\n%s", stderr)
	}
	text = mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(text, `model = "grok-manual"`) {
		t.Fatalf("zh grok:\n%s", text)
	}
}

func TestGrokModelDefaultIsRejected(t *testing.T) {
	srv := testgate.New()
	defer srv.Close()
	home := t.TempDir()
	out, errOut, code := run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "grok", "--grok-model", "default")
	if code != 2 || !strings.Contains(errOut, `"default" is not accepted`) {
		t.Fatalf("default %d\n%s\n%s", code, out, errOut)
	}
	if _, err := os.Stat(filepath.Join(home, ".grok")); !os.IsNotExist(err) {
		t.Fatalf("default wrote grok: %v", err)
	}

	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	t.Setenv("LANG", "zh_CN.UTF-8")
	home = t.TempDir()
	_, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "grok", "--grok-model", "default")
	if code != 2 || !strings.Contains(errOut, "不能传 default") {
		t.Fatalf("zh default %d\n%s", code, errOut)
	}
	t.Setenv("LC_ALL", "en_US.UTF-8")
	t.Setenv("LANG", "en_US.UTF-8")

	home = t.TempDir()
	out, errOut, code = run(t, home, nil, "setup", "--base-url", srv.URL, "--api-key", testgate.GoodKey, "--apps", "grok", "--grok-model", "grok-4")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, out, errOut)
	}
	out, errOut, code = run(t, home, nil, "switch", "--grok-model", "default")
	if code != 2 || !strings.Contains(errOut, `"default" is not accepted`) {
		t.Fatalf("switch %d\n%s\n%s", code, out, errOut)
	}
	text := mustRead(t, filepath.Join(home, ".grok", "config.toml"))
	if !strings.Contains(text, `model = "grok-4"`) {
		t.Fatalf("switch cleared the model:\n%s", text)
	}
}
