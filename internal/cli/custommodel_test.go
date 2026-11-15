package cli

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zfg5118/deeprouter/internal/testgate"
)

// customGate is testgate.Handler, except that a POST for one model id on
// one route answers the way mode says. Every POST is recorded.
type customGate struct {
	model string
	route string
	mode  string // "ok", "fail", "timeout", "error200", "empty200"

	mu    sync.Mutex
	posts []string // "path model"
}

func (g *customGate) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		testgate.Handler().ServeHTTP(w, r)
		return
	}
	raw, _ := io.ReadAll(r.Body)
	var payload struct {
		Model string `json:"model"`
	}
	_ = json.Unmarshal(raw, &payload)
	g.mu.Lock()
	g.posts = append(g.posts, r.URL.Path+" "+payload.Model)
	g.mu.Unlock()
	if payload.Model == g.model && r.URL.Path == g.route {
		switch g.mode {
		case "fail":
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = io.WriteString(w, `{"error":{"message":"no available channel"}}`)
			return
		case "timeout":
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		case "error200":
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"error":{"message":"upstream model unavailable"}}`)
			return
		case "empty200":
			w.WriteHeader(http.StatusOK)
			return
		}
	}
	r.Body = io.NopCloser(strings.NewReader(string(raw)))
	testgate.Handler().ServeHTTP(w, r)
}

func (g *customGate) postsFor(model string) []string {
	g.mu.Lock()
	defer g.mu.Unlock()
	var out []string
	for _, p := range g.posts {
		if strings.HasSuffix(p, " "+model) {
			out = append(out, p)
		}
	}
	return out
}

func startCustomGate(t *testing.T, g *customGate) string {
	t.Helper()
	srv := httptest.NewServer(g)
	t.Cleanup(srv.Close)
	return srv.URL
}

// noClients hides installed client programs so a test never runs them.
func noClients(t *testing.T) {
	t.Helper()
	stubCLIs(t, func(string) (string, error) { return "", os.ErrNotExist },
		func(context.Context, io.Writer, io.Writer, string, ...string) error { return os.ErrNotExist })
}

func shortModelProbe(t *testing.T) {
	t.Helper()
	prev := modelProbeTimeout
	modelProbeTimeout = 300 * time.Millisecond
	t.Cleanup(func() { modelProbeTimeout = prev })
}

const (
	existingCodex  = "# hand-written\nmodel = \"keep-me\"\n[model_providers.mine]\nbase_url = \"https://example.invalid/v1\"\n"
	existingClaude = "{\n  \"model\": \"keep-me\",\n  \"env\": {\"KEEP\": \"1\"}\n}\n"
)

// seedConfigs writes existing client configs that must survive a refusal.
func seedConfigs(t *testing.T, home string) (codexPath, claudePath string) {
	t.Helper()
	codexPath = filepath.Join(home, ".codex", "config.toml")
	claudePath = filepath.Join(home, ".claude", "settings.json")
	for path, body := range map[string]string{codexPath: existingCodex, claudePath: existingClaude} {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return codexPath, claudePath
}

func assertBytes(t *testing.T, path, want string) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if string(got) != want {
		t.Fatalf("%s changed:\n%s", path, got)
	}
}

// assertNotPassed fails if id is reported as passing. Other listed models
// may still carry their own "(answered a test request)" label.
func assertNotPassed(t *testing.T, text, id string) {
	t.Helper()
	if strings.Contains(text, id+" answered a test request") || strings.Contains(text, id+" 已通过测试") {
		t.Fatalf("claimed a pass:\n%s", text)
	}
}

func TestCodexChatFlagKeepsExistingConfigBytes(t *testing.T) {
	noClients(t)
	g := &customGate{}
	url := startCustomGate(t, g)
	home := t.TempDir()
	codexPath, claudePath := seedConfigs(t, home)
	stdout, stderr, code := run(t, home, nil, "setup", "--base-url", url, "--api-key", testgate.GoodKey, "--no-shell",
		"--apps", "codex", "--codex-model", "gemini-4-carbon", "--codex-wire-api", "chat")
	if code == 0 {
		t.Fatalf("chat accepted\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, `wire_api = "chat"`) || !strings.Contains(stderr, "Nothing was written.") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	assertBytes(t, codexPath, existingCodex)
	assertBytes(t, claudePath, existingClaude)
	assertNotPassed(t, stderr, "gemini-4-carbon")
	if posts := g.postsFor("gemini-4-carbon"); len(posts) != 0 {
		t.Fatalf("probed before refusing chat: %v", posts)
	}
}

func TestCodexCustomModelSavedOnlyAfterResponsesPass(t *testing.T) {
	noClients(t)
	g := &customGate{model: "gemini-4-carbon", route: "/v1/responses", mode: "ok"}
	url := startCustomGate(t, g)
	home := t.TempDir()
	codexPath, _ := seedConfigs(t, home)
	stdout, stderr, code := run(t, home, nil, "setup", "--base-url", url, "--api-key", testgate.GoodKey, "--no-shell",
		"--apps", "codex", "--codex-model", "gemini-4-carbon")
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "gemini-4-carbon answered a test request on POST /v1/responses.") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	text := mustRead(t, codexPath)
	if !strings.Contains(text, `model = "gemini-4-carbon"`) || !strings.Contains(text, `wire_api = "responses"`) || strings.Contains(text, `wire_api = "chat"`) {
		t.Fatalf("config.toml:\n%s", text)
	}
	if posts := g.postsFor("gemini-4-carbon"); len(posts) == 0 || posts[0] != "/v1/responses gemini-4-carbon" {
		t.Fatalf("posts: %v", posts)
	}
	for _, p := range g.postsFor("gemini-4-carbon") {
		if !strings.HasPrefix(p, "/v1/responses ") {
			t.Fatalf("tested on another route: %v", g.postsFor("gemini-4-carbon"))
		}
	}
}

// Every way the Responses test can fail leaves the existing config bytes.
func TestCodexCustomModelFailureKeepsExistingConfigBytes(t *testing.T) {
	for _, mode := range []string{"fail", "timeout", "error200", "empty200"} {
		t.Run(mode, func(t *testing.T) {
			noClients(t)
			shortModelProbe(t)
			g := &customGate{model: "gemini-4-carbon", route: "/v1/responses", mode: mode}
			url := startCustomGate(t, g)
			home := t.TempDir()
			codexPath, claudePath := seedConfigs(t, home)
			for _, extra := range [][]string{nil, {"--skip-verify"}} {
				args := append([]string{"setup", "--base-url", url, "--api-key", testgate.GoodKey, "--no-shell",
					"--apps", "codex", "--codex-model", "gemini-4-carbon"}, extra...)
				stdout, stderr, code := run(t, home, nil, args...)
				if code == 0 {
					t.Fatalf("%v saved\n%s\n%s", extra, stdout, stderr)
				}
				if !strings.Contains(stderr, "Nothing was written.") {
					t.Fatalf("stderr:\n%s", stderr)
				}
				assertNotPassed(t, stdout+stderr, "gemini-4-carbon")
				assertBytes(t, codexPath, existingCodex)
				assertBytes(t, claudePath, existingClaude)
			}
			if _, err := os.Stat(filepath.Join(home, ".deeprouter", "config.json")); !os.IsNotExist(err) {
				t.Fatalf("profile written: %v", err)
			}
		})
	}
}

func TestClaudeCustomModelTestedOnlyOnMessages(t *testing.T) {
	const id = "kimi-k3-turbo" // no "claude" in the name
	noClients(t)
	g := &customGate{model: id, route: "/v1/messages", mode: "ok"}
	url := startCustomGate(t, g)
	home := t.TempDir()
	_, claudePath := seedConfigs(t, home)
	stdout, stderr, code := run(t, home, nil, "setup", "--base-url", url, "--api-key", testgate.GoodKey, "--no-shell",
		"--apps", "claude-code", "--claude-model", id)
	if code != 0 {
		t.Fatalf("setup %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, id+" answered a test request on POST /v1/messages.") {
		t.Fatalf("stderr:\n%s", stderr)
	}
	if text := mustRead(t, claudePath); !strings.Contains(text, id) {
		t.Fatalf("settings.json:\n%s", text)
	}
	posts := g.postsFor(id)
	if len(posts) == 0 {
		t.Fatal("never tested")
	}
	for _, p := range posts {
		if p != "/v1/messages "+id {
			t.Fatalf("tested off /v1/messages: %v", posts)
		}
	}
}

// A Claude id that answers /v1/responses but not /v1/messages is not saved.
func TestClaudeCustomModelMessagesFailureKeepsBytes(t *testing.T) {
	const id = "kimi-k3-turbo"
	for _, mode := range []string{"fail", "error200", "empty200"} {
		t.Run(mode, func(t *testing.T) {
			noClients(t)
			g := &customGate{model: id, route: "/v1/messages", mode: mode}
			url := startCustomGate(t, g)
			home := t.TempDir()
			codexPath, claudePath := seedConfigs(t, home)
			stdout, stderr, code := run(t, home, nil, "setup", "--base-url", url, "--api-key", testgate.GoodKey, "--no-shell",
				"--apps", "claude-code", "--claude-model", id)
			if code == 0 {
				t.Fatalf("saved\n%s\n%s", stdout, stderr)
			}
			assertNotPassed(t, stdout+stderr, id)
			assertBytes(t, claudePath, existingClaude)
			assertBytes(t, codexPath, existingCodex)
		})
	}
}

func TestInvalidCustomModelIDsWriteNothing(t *testing.T) {
	noClients(t)
	g := &customGate{}
	url := startCustomGate(t, g)
	for _, flag := range []string{"codex-model", "claude-model"} {
		for _, id := range []string{"n", "yes", "bad\nid", "bad\x01id", "two words"} {
			home := t.TempDir()
			codexPath, claudePath := seedConfigs(t, home)
			apps := "codex"
			if flag == "claude-model" {
				apps = "claude-code"
			}
			stdout, stderr, code := run(t, home, nil, "setup", "--base-url", url, "--api-key", testgate.GoodKey, "--no-shell",
				"--apps", apps, "--"+flag, id)
			if code == 0 {
				t.Fatalf("--%s %q accepted\n%s\n%s", flag, id, stdout, stderr)
			}
			if !strings.Contains(stderr, "is not a valid model id") {
				t.Fatalf("--%s %q stderr:\n%s", flag, id, stderr)
			}
			assertNotPassed(t, stdout+stderr, id)
			assertBytes(t, codexPath, existingCodex)
			assertBytes(t, claudePath, existingClaude)
		}
	}
	if len(g.posts) != 0 {
		t.Fatalf("invalid ids were probed: %v", g.posts)
	}
}

// The wizard tests a typed id on Responses; a failure is not saved and the
// pass line appears only after a real pass.
func TestWizardCustomCodexModel(t *testing.T) {
	cases := []struct {
		mode string
		zh   bool
	}{{"ok", false}, {"ok", true}, {"fail", false}, {"timeout", false}, {"error200", true}, {"empty200", false}}
	for _, tc := range cases {
		t.Run(tc.mode, func(t *testing.T) {
			noClients(t)
			shortModelProbe(t)
			withTerminal(t)
			if tc.zh {
				t.Setenv("LC_ALL", "zh_CN.UTF-8")
				t.Setenv("LANG", "zh_CN.UTF-8")
			}
			g := &customGate{model: "gemini-4-carbon", route: "/v1/responses", mode: tc.mode}
			url := startCustomGate(t, g)
			home := t.TempDir()
			isolateHome(t, home)
			codexPath, _ := seedConfigs(t, home)
			stdout, stderr, code := runWizard(t, home, "", wizardCustom("1", url+"\n"+testgate.GoodKey+"\ngemini-4-carbon\n"))
			passLine := "gemini-4-carbon answered a test request on POST /v1/responses."
			if tc.zh {
				passLine = "gemini-4-carbon 已通过测试（POST /v1/responses）。"
			}
			if tc.mode == "ok" {
				if code != 0 || !strings.Contains(stderr, passLine) {
					t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
				}
				if text := mustRead(t, codexPath); !strings.Contains(text, `model = "gemini-4-carbon"`) {
					t.Fatalf("config.toml:\n%s", text)
				}
				return
			}
			if code == 0 {
				t.Fatalf("wizard saved a failed id\n%s\n%s", stdout, stderr)
			}
			assertNotPassed(t, stdout+stderr, "gemini-4-carbon")
			assertBytes(t, codexPath, existingCodex)
			for _, p := range g.postsFor("gemini-4-carbon") {
				if !strings.HasPrefix(p, "/v1/responses ") {
					t.Fatalf("tested off /v1/responses: %v", g.postsFor("gemini-4-carbon"))
				}
			}
		})
	}
}

// The wizard does not ask for a Codex protocol: after the platform comes
// the gateway, and there is no Chat choice on screen.
func TestWizardHasNoCodexProtocolMenu(t *testing.T) {
	noClients(t)
	withTerminal(t)
	srv := testgate.New()
	defer srv.Close()
	home := t.TempDir()
	stdout, stderr, code := runWizard(t, home, "", wizardCustom("1", srv.URL+"\n"+testgate.GoodKey+"\n\n"))
	if code != 0 {
		t.Fatalf("wizard %d\n%s\n%s", code, stdout, stderr)
	}
	for _, bad := range []string{"Codex protocol", "Chat Completions", "接口协议"} {
		if strings.Contains(stderr, bad) {
			t.Fatalf("menu shown (%q):\n%s", bad, stderr)
		}
	}
	if text := mustRead(t, filepath.Join(home, ".codex", "config.toml")); !strings.Contains(text, `wire_api = "responses"`) {
		t.Fatalf("config.toml:\n%s", text)
	}
}
