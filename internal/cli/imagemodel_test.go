package cli

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/imageconfig"
	"github.com/zfg5118/deeprouter/internal/textutil"
)

// catalogCall is one GET /v1/models the model step asked for.
type catalogCall struct{ base, key string }

// withCatalog replaces imageCatalog for one test and records each call.
// fn sees the address and key the user chose; nothing goes on the network.
func withCatalog(t *testing.T, fn func(base, key string) ([]gateway.Model, gateway.Check, error)) *[]catalogCall {
	t.Helper()
	prev := imageCatalog
	var mu sync.Mutex
	calls := &[]catalogCall{}
	imageCatalog = func(base, key string) ([]gateway.Model, gateway.Check, error) {
		mu.Lock()
		*calls = append(*calls, catalogCall{base, key})
		mu.Unlock()
		return fn(base, key)
	}
	t.Cleanup(func() { imageCatalog = prev })
	return calls
}

// listOf is a catalog the gateway would return for these ids.
func listOf(t *testing.T, body string) func(base, key string) ([]gateway.Model, gateway.Check, error) {
	t.Helper()
	models, err := gateway.ParseModels([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	return func(string, string) ([]gateway.Model, gateway.Check, error) {
		return models, gateway.Check{OK: true}, nil
	}
}

// mixedCatalog has chat, video, Gemini-native, and Images models.
const mixedCatalog = `{"data":[
	{"id":"gpt-5.4"},{"id":"grok-4"},{"id":"gpt-image-2"},{"id":"sora-2"},{"id":"veo-3"},
	{"id":"gemini-3-pro-image-preview"},{"id":"nano-banana-pro"},{"id":"grok-3-image"},
	{"id":"gpt-image-2.6-flare"},{"id":"grok-2-image"},{"id":"mj_imagine"},{"id":"my-image-model"},
	{"id":"vendor-chat","type":"chat"}]}`

// sectionAfter is the text from the model step on.
func sectionAfter(stderr string) string {
	if i := strings.Index(stderr, "The model list comes from"); i >= 0 {
		return stderr[i:]
	}
	return ""
}

// claims are positive statements the UI must never print about a save.
var claims = []string{"token is valid", "key is valid", "is reachable", "connected to", "verified model", "model is supported",
	"you have permission", "has permission", "was tested", "has access", "可以使用", "已验证", "连接成功", "有权限使用"}

func assertNoClaims(t *testing.T, text string) {
	t.Helper()
	low := strings.ToLower(text)
	for _, c := range claims {
		if strings.Contains(low, c) {
			t.Fatalf("UI claims %q:\n%s", c, text)
		}
	}
}

// TestImageModelListGPTImageFamilyAndDocumentedIDs is the v0.3.19 picker
// catalog plus documented Images ids: gpt-image-* and dall-e-3 are kept by
// id, and grok-3-image stays even when tagged with another protocol.
func TestImageModelListGPTImageFamilyAndDocumentedIDs(t *testing.T) {
	home := t.TempDir()
	withCatalog(t, listOf(t, `{"data":[
		{"id":"gpt-image-2.5-flare","supported_endpoint_types":["image-generation"]},{"id":"gpt-image-2.5-sunburst"},
		{"id":"gpt-image-2.6-flare"},{"id":"dall-e-3"},{"id":"grok-3-image","supported_endpoint_types":["gemini"]},
		{"id":"grok-2-image"},{"id":"flux-kontext-dev"},{"id":"gemini-2.5-flash-image","supported_endpoint_types":["image-generation"]}]}`))
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n\n\n"))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	menu := sectionAfter(stderr)
	want := "  1) gpt-image-2.6-flare (default)\n  2) gpt-image-2.5-flare\n  3) gpt-image-2.5-sunburst\n  4) dall-e-3\n  5) grok-3-image\n  6) Enter a model ID"
	if !strings.Contains(menu, want) {
		t.Fatalf("missing %q\n%s", want, menu)
	}
	for _, not := range []string{"grok-2-image", "flux-kontext-dev", "gemini"} {
		if strings.Contains(menu, not) {
			t.Fatalf("menu lists %q\n%s", not, menu)
		}
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "gpt-image-2.6-flare" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageModelDefaultIsSameAsGateway(t *testing.T) {
	if imageconfig.DefaultModel != gateway.FlareImageModel {
		t.Fatalf("%s != %s", imageconfig.DefaultModel, gateway.FlareImageModel)
	}
}

func TestImageModelListShowsOnlyImagesModelsFlareFirst(t *testing.T) {
	home := t.TempDir()
	calls := withCatalog(t, listOf(t, mixedCatalog))
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n\n\n"))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if len(*calls) != 1 || (*calls)[0] != (catalogCall{"https://deeprouter.top", imgKeyA}) {
		t.Fatalf("calls %+v", *calls)
	}
	menu := sectionAfter(stderr)
	for _, want := range []string{
		"Load the image models this token can see? [Y/n] ",
		"Image models visible to this token on deeprouter.top. Visible does not mean your account or channel may use a model, and none was test-generated:",
		"  1) gpt-image-2.6-flare (default)\n  2) gpt-image-2\n  3) grok-3-image\n  4) Enter a model ID (typed by you; not checked)\nModel [1]: ",
	} {
		if !strings.Contains(menu, want) {
			t.Fatalf("missing %q\n%s", want, menu)
		}
	}
	for _, not := range []string{"gpt-5.4", "grok-4", "sora", "veo", "gemini", "nano-banana", "grok-2-image", "mj_", "my-image-model", "vendor-chat", "Keep current"} {
		if strings.Contains(menu, not) {
			t.Fatalf("menu lists %q\n%s", not, menu)
		}
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "gpt-image-2.6-flare" {
		t.Fatalf("config %+v", cfg)
	}
	for _, want := range []string{"Model   gpt-image-2.6-flare (on this token's model list; not test-generated)",
		"Only GET /v1/models was sent, to deeprouter.top, to list models. Nothing was generated or billed."} {
		if !strings.Contains(stdout, want) {
			t.Fatalf("missing %q\n%s", want, stdout)
		}
	}
	assertNoClaims(t, stdout+menu)
	assertNoSecret(t, stdout+stderr, imgKeyA)
}

func TestImageModelWithoutFlareDefaultsToFirstVisible(t *testing.T) {
	home := t.TempDir()
	withCatalog(t, listOf(t, `{"data":[{"id":"gpt-5.4"},{"id":"doubao-seedream-5-0-260128"},{"id":"grok-3-image"},{"id":"veo-3"}]}`))
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "2\n"+imgKeyA+"\ny\n\n"))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	menu := sectionAfter(stderr)
	if !strings.Contains(menu, "  1) doubao-seedream-5-0-260128 (default)\n  2) grok-3-image\n  3) Enter a model ID") {
		t.Fatalf("menu\n%s", menu)
	}
	if strings.Contains(stdout+menu, "flare") {
		t.Fatalf("flare added though the token cannot see it\n%s\n%s", menu, stdout)
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "doubao-seedream-5-0-260128" || cfg.BaseURL != "https://wolfai.top" {
		t.Fatalf("config %+v", cfg)
	}

	// A number picks another listed model.
	_, _, code = runMenu(t, home, imageAnswers(false, "2\n\ny\n2\n"))
	if cfg := loadImageCfg(t, home); code != 0 || cfg.Model != "grok-3-image" {
		t.Fatalf("code %d config %+v", code, cfg)
	}

	// Re-entering: the saved model is marked and is the Enter default.
	_, stderr, code = runMenu(t, home, imageAnswers(false, "2\n\ny\n\n"))
	if code != 0 || !strings.Contains(stderr, "  2) grok-3-image (current) (default)\n") || !strings.Contains(stderr, "Model [2]: ") {
		t.Fatalf("code %d\n%s", code, sectionAfter(stderr))
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "grok-3-image" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageModelSavedButNotListed(t *testing.T) {
	home := t.TempDir()
	withCatalog(t, listOf(t, `{"data":[{"id":"gpt-image-1"}]}`))
	if _, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\nn\n2\nmy-house-model\n")); code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	// The saved model is offered, unverified, but Enter takes the list.
	stdout, stderr, code := runMenu(t, home, imageAnswers(false, "1\n\n\n2\n"))
	menu := sectionAfter(stderr)
	if code != 0 || !strings.Contains(menu, "  1) gpt-image-1 (default)\n  2) Keep current: my-house-model (not on this list; unverified)\n  3) Enter a model ID") {
		t.Fatalf("code %d\n%s", code, menu)
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "my-house-model" || !strings.Contains(stdout, "Model   my-house-model (unverified)") {
		t.Fatalf("keep current: config %+v\n%s", cfg, stdout)
	}
	_, _, code = runMenu(t, home, imageAnswers(false, "1\n\n\n\n"))
	if cfg := loadImageCfg(t, home); code != 0 || cfg.Model != "gpt-image-1" {
		t.Fatalf("Enter: code %d config %+v", code, cfg)
	}
}

// failingCatalogs are list outcomes that give no token-visible image model.
func failingCatalogs(t *testing.T) map[string]func(base, key string) ([]gateway.Model, gateway.Check, error) {
	fail := func(check gateway.Check) func(string, string) ([]gateway.Model, gateway.Check, error) {
		return func(string, string) ([]gateway.Model, gateway.Check, error) {
			return nil, check, errors.New(check.Hint)
		}
	}
	return map[string]func(base, key string) ([]gateway.Model, gateway.Check, error){
		"401":         fail(gateway.Check{Status: 401, Hint: "HTTP 401: the gateway did not accept this key."}),
		"timeout":     tlsCatalog(t, func(w http.ResponseWriter, r *http.Request) { <-r.Context().Done() }, 200*time.Millisecond),
		"refused":     fail(gateway.Check{Hint: "Could not connect to the gateway."}),
		"parse error": fail(gateway.Check{Hint: "The model list could not be parsed."}),
		"empty list":  listOf(t, `{"data":[]}`),
		"no image":    listOf(t, `{"data":[{"id":"gpt-5.4"},{"id":"sora-2"},{"id":"gemini-3-pro-image-preview"}]}`),
	}
}

func TestImageModelListFailureStillSaves(t *testing.T) {
	for name, fn := range failingCatalogs(t) {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			withCatalog(t, fn)
			// First time: Enter saves the Skill default as an unverified value.
			stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n\n\n"))
			if code != 0 {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			menu := sectionAfter(stderr)
			for _, want := range []string{
				"There is no token-visible model list. You can still save.",
				"  1) gpt-image-2.6-flare: the Skill default (unverified) (default)\n  2) Enter a model ID (typed by you; not checked)\nModel [1]: ",
			} {
				if !strings.Contains(menu, want) {
					t.Fatalf("missing %q\n%s", want, menu)
				}
			}
			if strings.Contains(menu, "Image models visible to this token") || strings.Contains(menu, "gpt-image-2\n") {
				t.Fatalf("faked a token-visible list\n%s", menu)
			}
			cfg := loadImageCfg(t, home)
			if cfg.Model != imageconfig.DefaultModel || cfg.APIKey != imgKeyA {
				t.Fatalf("config %+v", cfg)
			}
			if !strings.Contains(stdout, "Model   gpt-image-2.6-flare (unverified)") || strings.Contains(stdout, "on this token's model list") {
				t.Fatalf("stdout\n%s", stdout)
			}
			assertNoClaims(t, stdout+menu)

			// A saved model is kept, marked unverified, and is the default.
			if _, _, code := runMenu(t, home, imageAnswers(false, "1\n\nn\n2\ngrok-3-image\n")); code != 0 {
				t.Fatal(code)
			}
			_, stderr, code = runMenu(t, home, imageAnswers(false, "1\n\n\n\n"))
			menu = sectionAfter(stderr)
			if code != 0 || !strings.Contains(menu, "  1) Keep current: grok-3-image (unverified) (default)\n  2) gpt-image-2.6-flare: the Skill default (unverified)\n  3) Enter a model ID") {
				t.Fatalf("code %d\n%s", code, menu)
			}
			if cfg := loadImageCfg(t, home); cfg.Model != "grok-3-image" {
				t.Fatalf("config %+v", cfg)
			}
			assertNoSecret(t, stdout+stderr, imgKeyA)
		})
	}
}

func TestImageModelSkipMakesNoRequest(t *testing.T) {
	home := t.TempDir()
	calls := withCatalog(t, listOf(t, mixedCatalog))
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\nn\n\n"))
	if code != 0 || len(*calls) != 0 {
		t.Fatalf("code %d calls %v\n%s", code, *calls, stderr)
	}
	if !strings.Contains(stderr, "Skipped the model list.") || !strings.Contains(stdout, "No API call was made and nothing was billed. The address, token, and model were not checked.") {
		t.Fatalf("%s\n%s", stderr, stdout)
	}
	if cfg := loadImageCfg(t, home); cfg.Model != imageconfig.DefaultModel {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageModelManualIDAlwaysOffered(t *testing.T) {
	cases := map[string]struct {
		fn     func(t *testing.T) func(base, key string) ([]gateway.Model, gateway.Check, error)
		load   string
		manual string
	}{
		"listed": {func(t *testing.T) func(string, string) ([]gateway.Model, gateway.Check, error) {
			return listOf(t, mixedCatalog)
		}, "\n", "4"},
		"failed": {func(t *testing.T) func(string, string) ([]gateway.Model, gateway.Check, error) {
			return failingCatalogs(t)["401"]
		}, "\n", "2"},
		"empty": {func(t *testing.T) func(string, string) ([]gateway.Model, gateway.Check, error) {
			return listOf(t, `{"data":[]}`)
		}, "\n", "2"},
		"skipped": {func(t *testing.T) func(string, string) ([]gateway.Model, gateway.Check, error) {
			return listOf(t, mixedCatalog)
		}, "n\n", "2"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			withCatalog(t, c.fn(t))
			bad := "\n" + "a b\n" + "../x\n" + "https://evil.example/m\n" + "yes\n"
			stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n"+c.load+c.manual+"\n"+bad+"nano-banana-pro\n"))
			if code != 0 {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			for _, want := range []string{"model id is empty", "must not contain spaces", "must not look like a path or URL", "model id is not valid"} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("missing %q\n%s", want, stderr)
				}
			}
			if n := strings.Count(stderr, "Model ID: "); n != 6 {
				t.Fatalf("asked %d times\n%s", n, stderr)
			}
			if cfg := loadImageCfg(t, home); cfg.Model != "nano-banana-pro" {
				t.Fatalf("config %+v", cfg)
			}
			if !strings.Contains(stdout, "Model   nano-banana-pro (entered by you; not checked)") || strings.Contains(stdout, "on this token's model list") {
				t.Fatalf("stdout\n%s", stdout)
			}
			assertNoClaims(t, stdout+sectionAfter(stderr))
		})
	}
}

func TestImageModelRefreshesForEachKeyAndAddress(t *testing.T) {
	home := t.TempDir()
	lists := map[catalogCall]string{
		{"https://deeprouter.top", imgKeyA}:  `{"data":[{"id":"gpt-image-2"}]}`,
		{"https://deeprouter.top", imgKeyB}:  `{"data":[{"id":"grok-3-image"}]}`,
		{"https://img.example.com", imgKeyB}: `{"data":[{"id":"gpt-image-1.5"}]}`,
	}
	calls := withCatalog(t, func(base, key string) ([]gateway.Model, gateway.Check, error) {
		body, ok := lists[catalogCall{base, key}]
		if !ok {
			return nil, gateway.Check{Status: 401, Hint: "HTTP 401."}, errors.New("401")
		}
		models, _ := gateway.ParseModels([]byte(body))
		return models, gateway.Check{OK: true}, nil
	})
	run := func(rest string) string {
		t.Helper()
		_, stderr, code := runMenu(t, home, rest)
		if code != 0 {
			t.Fatalf("code %d\n%s", code, stderr)
		}
		return sectionAfter(stderr)
	}
	m1 := run(imageAnswers(true, "1\n"+imgKeyA+"\n\n\n"))
	m2 := run(imageAnswers(false, "1\n"+imgKeyB+"\n\n\n"))
	m3 := run(imageAnswers(false, "4\nhttps://img.example.com\n"+imgKeyB+"\n\n\n"))
	if !strings.Contains(m1, "1) gpt-image-2 (default)") || strings.Contains(m1, "grok-3-image") {
		t.Fatalf("run 1\n%s", m1)
	}
	if !strings.Contains(m2, "1) grok-3-image (default)") || strings.Contains(m2, "1) gpt-image-2") {
		t.Fatalf("run 2 reused the old list\n%s", m2)
	}
	if !strings.Contains(m3, "1) gpt-image-1.5 (default)") || strings.Contains(m3, "1) grok-3-image") || !strings.Contains(m3, "Keep current: grok-3-image") {
		t.Fatalf("run 3 reused the old list\n%s", m3)
	}
	want := []catalogCall{{"https://deeprouter.top", imgKeyA}, {"https://deeprouter.top", imgKeyB}, {"https://img.example.com", imgKeyB}}
	if len(*calls) != len(want) {
		t.Fatalf("calls %v", *calls)
	}
	for i := range want {
		if (*calls)[i] != want[i] {
			t.Fatalf("call %d went to %s with key %s", i, (*calls)[i].base, textutil.Mask((*calls)[i].key))
		}
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "gpt-image-1.5" || cfg.BaseURL != "https://img.example.com" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageModelCancelWritesNothing(t *testing.T) {
	withCatalog(t, listOf(t, mixedCatalog))
	for name, rest := range map[string]string{
		"at load question": "1\n" + imgKeyA + "\n",
		"at model menu":    "1\n" + imgKeyA + "\n\n",
		"at model id":      "1\n" + imgKeyA + "\n\n4\n",
		"after bad id":     "1\n" + imgKeyA + "\nn\n2\na b\n",
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			stdout, stderr, code := runMenu(t, home, imageAnswers(true, rest))
			if code != 2 || !strings.Contains(stderr, "Cancelled. The image address and token were not saved.") {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			if _, err := os.Lstat(imageCfgPath(home)); !os.IsNotExist(err) {
				t.Fatalf("wrote the image config: %v", err)
			}
			if strings.Contains(stdout, "Saved") {
				t.Fatalf("faked success\n%s", stdout)
			}
		})
	}
	// A saved file stays byte for byte.
	home := t.TempDir()
	if _, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n\n2\n")); code != 0 {
		t.Fatalf("%d\n%s", code, stderr)
	}
	before, _ := os.ReadFile(imageCfgPath(home))
	if _, _, code := runMenu(t, home, imageAnswers(false, "2\n"+imgKeyB+"\n\n4\n")); code != 2 {
		t.Fatal(code)
	}
	if after, _ := os.ReadFile(imageCfgPath(home)); string(after) != string(before) {
		t.Fatal("cancel changed the file")
	}
}

func TestImageModelChinese(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	withCatalog(t, listOf(t, mixedCatalog))
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n\n\n"))
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	for _, want := range []string{"获取令牌可见的绘画模型？[Y/n] ", "令牌可见的绘画模型（deeprouter.top）。可见不代表账号或渠道有权限使用，也没有试生成：",
		"1) gpt-image-2.6-flare（默认）", "4) 手动输入模型ID（不检查是否可用）", "模型 [1]: "} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("missing %q\n%s", want, stderr)
		}
	}
	if !strings.Contains(stdout, "（在令牌可见列表中，未试生成）") {
		t.Fatalf("stdout\n%s", stdout)
	}
	withCatalog(t, listOf(t, `{"data":[]}`))
	_, stderr, _ = runMenu(t, home, imageAnswers(false, "1\n\n\n2\n\n"))
	for _, want := range []string{"没有令牌可见的模型列表，仍然可以保存。", "保留当前：gpt-image-2.6-flare（未验证）", "模型ID不能为空"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("missing %q\n%s", want, stderr)
		}
	}
}

// tlsCatalog serves GET /v1/models from a local TLS server through the real
// gateway.ListCatalog, so the request the CLI builds is checked: the path,
// the method, and the Authorization header.
func tlsCatalog(t *testing.T, h http.HandlerFunc, timeout time.Duration) func(base, key string) ([]gateway.Model, gateway.Check, error) {
	t.Helper()
	srv := httptest.NewTLSServer(h)
	t.Cleanup(srv.Close)
	return func(base, key string) ([]gateway.Model, gateway.Check, error) {
		// The user's address is replaced by the test server's; the path
		// under it is kept.
		ep, err := gateway.ParseBase(srv.URL + strings.TrimPrefix(base, "https://"+imageconfig.Host(base)))
		if err != nil {
			return nil, gateway.Check{}, err
		}
		ctx, cancel := context.WithTimeout(context.Background(), timeout)
		defer cancel()
		return gateway.ListCatalog(ctx, gateway.ProbeOptions{Endpoints: ep, APIKey: key, Timeout: timeout, Client: srv.Client()})
	}
}

func TestImageModelListRequestIsOnlyGetModels(t *testing.T) {
	home := t.TempDir()
	var mu sync.Mutex
	var seen []string
	withCatalog(t, tlsCatalog(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		seen = append(seen, r.Method+" "+r.URL.Path+" auth="+map[bool]string{true: "A", false: "other"}[r.Header.Get("Authorization") == "Bearer "+imgKeyA])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(mixedCatalog))
	}, 5*time.Second))
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "4\nhttps://img.example.com/api\n"+imgKeyA+"\n\n2\n"))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if len(seen) != 1 || seen[0] != "GET /api/v1/models auth=A" {
		t.Fatalf("requests %v", seen)
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "gpt-image-2" || cfg.BaseURL != "https://img.example.com/api" {
		t.Fatalf("config %+v", cfg)
	}
	assertNoSecret(t, stdout+stderr, imgKeyA)
}
