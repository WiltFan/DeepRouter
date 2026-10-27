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

func TestPickAnotherModelAfterFailedProbeCanLeaveItUnset(t *testing.T) {
	home := t.TempDir()
	posts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"},{"id":"gpt-4.1"}]}`)
			return
		}
		posts++
		body, _ := io.ReadAll(r.Body)
		if posts >= 2 && strings.Contains(string(body), `"gpt-5.4"`) {
			http.Error(w, `{"error":{"message":"no available channel"}}`, http.StatusServiceUnavailable)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	withTerminal(t)
	// Pick gpt-5.4, which stops answering after the write; then choose
	// "pick another model" and press Enter for the client default.
	stdout, stderr, code := runWizard(t, home, "", "4\n\n"+srv.URL+"\n"+testgate.GoodKey+"\n2\n\n1\n\nn\n")
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "Codex / ChatGPT: no model set; the client uses its own default.") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if !strings.Contains(stderr, "  2) gpt-4.1  (answered a test request)") {
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
}

func TestSetupSummaryLabelsAndColumns(t *testing.T) {
	srv := testgate.New()
	defer srv.Close()

	home := t.TempDir()
	withTerminal(t)
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	stdout, stderr, code := runWizard(t, home, "", "4\n1,2\n"+srv.URL+"\n"+testgate.GoodKey+"\n\n\n\n\nn\n")
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"配置        default（自定义）",
		"\n  Claude Code      ~/.claude/settings.json   模型：",
		"\n  Codex / ChatGPT  ~/.codex/config.toml   模型：",
		"配置     default（自定义）",
		"然后运行：claude\n",
		"或者运行：codex\n",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("missing %q in:\n%s", want, stdout)
		}
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
		"\n  Claude Code      ~/.claude/settings.json   model client default\n",
		"\n  Codex / ChatGPT  ~/.codex/config.toml   model client default\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
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
