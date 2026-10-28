package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestModelsHandshakeTimeout(t *testing.T) {
	client := newClient(30 * time.Second)
	transport, ok := client.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", client.Transport)
	}
	if transport.TLSHandshakeTimeout != 30*time.Second {
		t.Fatalf("default test timeout handshake %s", transport.TLSHandshakeTimeout)
	}
	short := newClient(5 * time.Second)
	shortTransport, ok := short.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", short.Transport)
	}
	if short.Timeout != 5*time.Second || shortTransport.TLSHandshakeTimeout != 20*time.Second {
		t.Fatalf("timeout %s handshake %s", short.Timeout, shortTransport.TLSHandshakeTimeout)
	}
	if http.DefaultTransport.(*http.Transport).TLSHandshakeTimeout != 10*time.Second {
		t.Fatal("expected the net/http default handshake timeout to stay 10s")
	}
}

func TestHint(t *testing.T) {
	cases := map[int]string{
		401: "401 Bad key",
		403: "403 Quota or permission",
		404: "404 Model or path",
		503: "503 No channel",
	}
	for status, needle := range cases {
		got := Hint(status, "")
		if !strings.Contains(got, needle) {
			t.Fatalf("%d: %s", status, got)
		}
	}
	if !strings.Contains(Hint(503, ""), "No channel is available") {
		t.Fatal(Hint(503, ""))
	}
}

func TestProbeSuccessAndLatency(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(20 * time.Millisecond)
		if got := r.Header.Get("Authorization"); got != "Bearer good-key" {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"},{"id":"claude-sonnet-4-6"}]}`)
		case "/v1/chat/completions":
			assertModel(t, r, "gpt-4.1")
			_, _ = io.WriteString(w, `{"id":"chatcmpl_test","choices":[{"message":{"content":"pong"}}]}`)
		case "/v1/messages":
			if r.Header.Get("x-api-key") != "good-key" || r.Header.Get("anthropic-version") == "" {
				t.Errorf("anthropic headers: %v", r.Header)
			}
			assertModel(t, r, "claude-sonnet-4-6")
			_, _ = io.WriteString(w, `{"id":"msg_test","type":"message","content":[{"type":"text","text":"pong"}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		ClaudeModel: "claude-sonnet-4-6",
		CodexModel:  "gpt-4.1",
		Timeout:     5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if len(report.Checks) != 3 {
		t.Fatalf("checks %d", len(report.Checks))
	}
	for _, c := range report.Checks {
		if c.Status != 200 {
			t.Fatalf("%s status %d", c.Name, c.Status)
		}
		if c.TTFB <= 0 || c.Total < c.TTFB {
			t.Fatalf("%s ttfb %s total %s", c.Name, c.TTFB, c.Total)
		}
		if c.TTFBMS < 0 || c.TotalMS < c.TTFBMS {
			t.Fatalf("%s ms ttfb %d total %d", c.Name, c.TTFBMS, c.TotalMS)
		}
	}
	if len(report.Models) != 2 {
		t.Fatalf("models %+v", report.Models)
	}
}

func TestProbeStatusHints(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		var payload struct {
			Model string `json:"model"`
		}
		if r.Body != nil && r.Method == http.MethodPost {
			_ = json.NewDecoder(r.Body).Decode(&payload)
		}
		switch {
		case key == "bad-key":
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
		case key == "quota-key":
			http.Error(w, `{"error":{"message":"quota exceeded"}}`, http.StatusForbidden)
		case payload.Model == "missing-model":
			http.Error(w, `{"error":{"message":"model not found"}}`, http.StatusNotFound)
		case payload.Model == "down-model":
			http.Error(w, `{"error":{"message":"no channel"}}`, http.StatusServiceUnavailable)
		case r.URL.Path == "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1"},{"id":"claude-sonnet-4-6"}]}`)
		default:
			w.WriteHeader(http.StatusOK)
			_, _ = io.WriteString(w, `{"ok":true}`)
		}
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}

	cases := []struct {
		name  string
		key   string
		model string
		want  string
	}{
		{"unauthorized", "bad-key", "gpt-4.1", "401 Bad key"},
		{"forbidden", "quota-key", "gpt-4.1", "403 Quota or permission"},
		{"missing", "good-key", "missing-model", "404 Model or path"},
		{"down", "good-key", "down-model", "503 No channel"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := Probe(context.Background(), ProbeOptions{
				Endpoints:   ep,
				APIKey:      tc.key,
				CodexModel:  tc.model,
				ClaudeModel: tc.model,
				Timeout:     5 * time.Second,
			})
			if report.OK {
				t.Fatal("expected failure")
			}
			var hit bool
			for _, c := range report.Checks {
				if strings.Contains(c.Hint, tc.want) {
					hit = true
				}
				if strings.Contains(c.Excerpt, tc.key) || strings.Contains(c.Hint, tc.key) {
					t.Fatalf("leaked key in %+v", c)
				}
			}
			if !hit {
				t.Fatalf("hint %q not found in %+v", tc.want, report.Checks)
			}
		})
	}
}

// deepRouterModelsBody is the shape returned by https://deeprouter.top/v1/models.
// The first object is the payload observed against that gateway. The list is
// longer than the 180-byte display excerpt, which used to be parsed instead of
// the body and produced zero model ids.
const deepRouterModelsBody = `{"data":[{"id":"claude-fable-5","object":"model","owned_by":"claude","supported_endpoint_types":["anthropic","openai"]},{"id":"claude-sonnet-4-6","object":"model","owned_by":"claude","supported_endpoint_types":["anthropic","openai"]},{"id":"claude-opus-4-6","object":"model","owned_by":"claude","supported_endpoint_types":["anthropic","openai"]},{"id":"gpt-4.1","object":"model","owned_by":"openai","supported_endpoint_types":["openai"]},{"id":"gpt-5.4","object":"model","owned_by":"openai","supported_endpoint_types":["openai"]}]}`

func TestListModelsDeepRouterPayload(t *testing.T) {
	if len(deepRouterModelsBody) <= 180 {
		t.Fatalf("fixture is %d bytes; the excerpt cap is 180", len(deepRouterModelsBody))
	}
	truncated := oneLine(deepRouterModelsBody, 180)
	if _, err := parseModelIDs([]byte(truncated)); err == nil {
		t.Fatal("truncated excerpt parsed; the fixture no longer reproduces the bug")
	}
	ids, err := parseModelIDs([]byte(deepRouterModelsBody))
	if err != nil {
		t.Fatal(err)
	}
	want := []string{"claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-6", "gpt-4.1", "gpt-5.4"}
	if strings.Join(ids, ",") != strings.Join(want, ",") {
		t.Fatalf("ids %v", ids)
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Errorf("authorization %q", got)
		}
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			if got := r.Header.Get("x-api-key"); got != "" {
				t.Errorf("models request sent x-api-key %q", got)
			}
			if got := r.Header.Get("anthropic-version"); got != "" {
				t.Errorf("models request sent anthropic-version %q", got)
			}
			_, _ = io.WriteString(w, deepRouterModelsBody)
			return
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()

	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	got, check, err := ListModels(context.Background(), ProbeOptions{
		Endpoints: ep,
		APIKey:    "test-key",
		Timeout:   5 * time.Second,
	})
	if err != nil {
		t.Fatal(err)
	}
	if check.Detail != "5 models" {
		t.Fatalf("detail %q excerpt %q", check.Detail, check.Excerpt)
	}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("list %v", got)
	}

	report := Probe(context.Background(), ProbeOptions{
		Endpoints: ep,
		APIKey:    "test-key",
		Timeout:   5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	var chatModel, claudeModel string
	for _, c := range report.Checks {
		switch c.Name {
		case "models":
			if c.Detail != "5 models" {
				t.Fatalf("models detail %q", c.Detail)
			}
		case "chat":
			chatModel = c.Detail
		case "messages":
			claudeModel = c.Detail
		}
	}
	if chatModel != "gpt-5.4" || claudeModel != "claude-fable-5" {
		t.Fatalf("picked chat %q claude %q", chatModel, claudeModel)
	}
}

func TestPickDefault(t *testing.T) {
	ids := []string{"claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-6", "gpt-4.1", "gpt-5.4"}
	if got := PickDefault(ids, "claude"); got != "claude-fable-5" {
		t.Fatalf("claude %s", got)
	}
	if got := PickDefault(ids, "gpt"); got != "gpt-5.4" {
		t.Fatalf("gpt %s", got)
	}
	if got := PickDefault([]string{"Claude-Fable-5"}, "claude"); got != "Claude-Fable-5" {
		t.Fatalf("only fable %s", got)
	}
	if got := PickDefault([]string{"gpt-4.1"}, "gpt"); got != "" {
		t.Fatalf("only gpt-4.1 %s", got)
	}
	if got := PickDefault([]string{"other"}, "claude"); got != "" {
		t.Fatalf("missing %s", got)
	}
	filtered := FilterIDs(ids, "gpt")
	if strings.Join(filtered, ",") != "gpt-4.1,gpt-5.4" {
		t.Fatalf("filter %v", filtered)
	}
}

func TestProbeGenerateContent(t *testing.T) {
	type hit struct {
		key, auth string
		contents  bool
	}
	var mu sync.Mutex
	hits := map[string]hit{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":generateContent") {
			var body struct {
				Contents []any `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("body: %v", err)
			}
			mu.Lock()
			hits[r.URL.Path] = hit{
				key:      r.Header.Get("x-goog-api-key"),
				auth:     r.Header.Get("Authorization"),
				contents: len(body.Contents) > 0,
			}
			mu.Unlock()
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`)
			return
		}
		if r.Header.Get("Authorization") != "Bearer good-key" {
			http.Error(w, `{"error":{"message":"invalid api key"}}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"},{"id":"claude-sonnet-4-6"},{"id":"gemini-2.5-pro"},{"id":"gemini-2.0-flash"}]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		GeminiKey:   "gem-key",
		ProbeGemini: true,
		Timeout:     5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if len(report.Checks) != 4 {
		t.Fatalf("checks %d", len(report.Checks))
	}
	pro := hits["/v1beta/models/gemini-2.5-pro:generateContent"]
	if pro.key != "gem-key" || pro.auth != "" || !pro.contents {
		t.Fatalf("gemini-2.5-pro hit %+v all %v", pro, hits)
	}
	if report.Checks[3].Name != "generateContent" || report.Checks[3].Detail != "gemini-2.5-pro" {
		t.Fatalf("check %+v", report.Checks[3])
	}

	hits = map[string]hit{}
	report = Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		GeminiModel: "gemini-2.0-flash",
		ProbeGemini: true,
		Timeout:     5 * time.Second,
	})
	chosen := hits["/v1beta/models/gemini-2.0-flash:generateContent"]
	if !report.OK || chosen.key != "good-key" || len(hits) != 1 {
		t.Fatalf("chosen hits %v ok %v %+v", hits, report.OK, report.Checks)
	}

	var sawGenerate bool
	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":generateContent") {
			sawGenerate = true
		}
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"}]}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer bare.Close()
	ep, err = ParseBase(bare.URL)
	if err != nil {
		t.Fatal(err)
	}
	report = Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		ProbeGemini: true,
		Timeout:     5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("skipped checks failed the report: %+v", report.Checks)
	}
	if sawGenerate {
		t.Fatal("generateContent ran with no gemini model")
	}
	var skippedGemini, skippedClaude bool
	var chatModel string
	for _, c := range report.Checks {
		switch c.Name {
		case "generateContent":
			skippedGemini = c.Skipped && strings.Contains(c.Hint, "No gemini chat model")
		case "messages":
			skippedClaude = c.Skipped && strings.Contains(c.Hint, "No claude chat model")
		case "chat":
			chatModel = c.Detail
		}
	}
	if !skippedGemini || !skippedClaude || chatModel != "gpt-5.4" {
		t.Fatalf("gemini %v claude %v chat %q checks %+v", skippedGemini, skippedClaude, chatModel, report.Checks)
	}
}

func TestProbeDefaultSkipsNonChat(t *testing.T) {
	const body = `{"data":[
		{"id":"dall-e-3"},
		{"id":"sora-2"},
		{"id":"veo-3"},
		{"id":"imagen-3"},
		{"id":"text-embedding-3-small"},
		{"id":"tts-1"},
		{"id":"whisper-1"},
		{"id":"gpt-4o-audio-preview"},
		{"id":"gpt-4o-realtime-preview"},
		{"id":"omni-moderation-latest"},
		{"id":"grok-2-flare"},
		{"id":"grok-sunburst"},
		{"id":"gemini-2.5-flash-image"},
		{"id":"nano-banana"},
		{"id":"video-gen-1"},
		{"id":"gpt-4o-mini-transcribe"},
		{"id":"gpt-5.4","type":"image"},
		{"id":"gpt-image-keep","type":"chat"},
		{"id":"claude-sonnet-4-6"},
		{"id":"gemini-2.5-pro","type":"image"},
		{"id":"gemini-2.0-flash"},
		{"id":"grok-4"}
	]}`
	var chatModel, claudeModel, geminiPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.URL.Path == "/v1/models":
			_, _ = io.WriteString(w, body)
		case r.URL.Path == "/v1/chat/completions":
			chatModel = modelFrom(t, r)
			_, _ = io.WriteString(w, `{"ok":true}`)
		case r.URL.Path == "/v1/messages":
			claudeModel = modelFrom(t, r)
			_, _ = io.WriteString(w, `{"ok":true}`)
		case strings.HasSuffix(r.URL.Path, ":generateContent"):
			geminiPath = r.URL.Path
			_, _ = io.WriteString(w, `{"candidates":[{"content":{"parts":[{"text":"pong"}]}}]}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		ProbeGemini: true,
		Timeout:     5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if chatModel != "gpt-5.4" || claudeModel != "claude-sonnet-4-6" || geminiPath != "/v1beta/models/gemini-2.0-flash:generateContent" {
		t.Fatalf("chat %q claude %q gemini %q", chatModel, claudeModel, geminiPath)
	}
	if len(report.Models) != 22 || report.Models[0] != "dall-e-3" {
		t.Fatalf("models %v", report.Models)
	}

	chatModel, claudeModel = "", ""
	fallback := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"dall-e-3"},{"id":"gpt-image-1"},{"id":"sora-2"}]}`)
			return
		}
		if r.URL.Path == "/v1/chat/completions" {
			chatModel = modelFrom(t, r)
		}
		if r.URL.Path == "/v1/messages" {
			claudeModel = modelFrom(t, r)
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer fallback.Close()
	ep, err = ParseBase(fallback.URL)
	if err != nil {
		t.Fatal(err)
	}
	report = Probe(context.Background(), ProbeOptions{
		Endpoints: ep,
		APIKey:    "good-key",
		Timeout:   5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if chatModel != "" || claudeModel != "" {
		t.Fatalf("fallback chat %q claude %q", chatModel, claudeModel)
	}
	var skippedClaude, skippedGPT bool
	for _, c := range report.Checks {
		if c.Name == "messages" && c.Skipped && strings.Contains(c.Hint, "No claude chat model") {
			skippedClaude = true
		}
		if c.Name == "chat" && c.Skipped && strings.Contains(c.Hint, "No gpt chat model") {
			skippedGPT = true
		}
	}
	if !skippedClaude || !skippedGPT {
		t.Fatalf("family was not skipped: %+v", report.Checks)
	}
}

func TestProbeUsesChosenModelAndSkipsOtherFamilies(t *testing.T) {
	var paths []string
	var models []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.Method+" "+r.URL.Path)
		if r.Method == http.MethodPost {
			models = append(models, modelFrom(t, r))
		}
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-sonnet-4-6"},{"id":"gpt-5.4"},{"id":"grok-4"},{"id":"gemini-2.5-pro"}]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		CodexModel:  "gpt-4.1",
		Scoped:      true,
		ProbeModels: true,
		ProbeChat:   true,
		Timeout:     5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if strings.Join(paths, ",") != "GET /v1/models,POST /v1/chat/completions" {
		t.Fatalf("paths %v", paths)
	}
	if strings.Join(models, ",") != "gpt-4.1" {
		t.Fatalf("models %v", models)
	}
}

func TestProbePicksFamilyOrSkips(t *testing.T) {
	var mu sync.Mutex
	got := map[string]bool{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-opus-4-6"},{"id":"claude-sonnet-4-6"},{"id":"grok-4"},{"id":"dall-e-3"}]}`)
			return
		}
		if r.Method == http.MethodPost {
			id := modelFrom(t, r)
			mu.Lock()
			got[r.URL.Path+" "+id] = true
			mu.Unlock()
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints:    ep,
		APIKey:       "good-key",
		Scoped:       true,
		ProbeChat:    true,
		ProbeGrok:    true,
		ProbeClaude:  true,
		ProbeDesktop: true,
		ProbeGemini:  true,
		Timeout:      5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if !got["/v1/chat/completions grok-4"] || !got["/v1/messages claude-sonnet-4-6"] {
		t.Fatalf("calls %v", got)
	}
	var skippedGPT, skippedGemini bool
	var messageChecks int
	var messageModel string
	for _, c := range report.Checks {
		if c.Name == "chat" && c.Skipped && strings.Contains(c.Hint, "No gpt chat model") {
			skippedGPT = true
		}
		if c.Name == "generateContent" && c.Skipped && strings.Contains(c.Hint, "No gemini chat model") {
			skippedGemini = true
		}
		if c.Name == "messages" && !c.Skipped {
			messageChecks++
			messageModel = c.Detail
		}
		if c.Path == "/v1/models" {
			t.Fatalf("models check was visible: %+v", report.Checks)
		}
	}
	if !skippedGPT || !skippedGemini || messageChecks != 1 || messageModel != "claude-sonnet-4-6" {
		t.Fatalf("gpt %v gemini %v messages %d model %q %+v", skippedGPT, skippedGemini, messageChecks, messageModel, report.Checks)
	}
}

func TestProbeHardcodedFallbackWhenListUnavailable(t *testing.T) {
	var got []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			http.Error(w, `{"error":{"message":"unavailable"}}`, http.StatusInternalServerError)
			return
		}
		if r.Method == http.MethodPost {
			got = append(got, r.URL.Path+" "+modelFrom(t, r))
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints: ep,
		APIKey:    "good-key",
		Timeout:   5 * time.Second,
	})
	if report.OK {
		t.Fatal("models failure should fail the report")
	}
	if strings.Join(got, ",") != "/v1/chat/completions gpt-5.4,/v1/messages claude-sonnet-4-6" {
		t.Fatalf("calls %v", got)
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[]}`)
			return
		}
		if r.Method == http.MethodPost {
			if strings.Contains(r.URL.Path, "generateContent") {
				got = append(got, r.URL.Path)
			} else {
				got = append(got, r.URL.Path+" "+modelFrom(t, r))
			}
		}
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	defer empty.Close()
	ep, err = ParseBase(empty.URL)
	if err != nil {
		t.Fatal(err)
	}
	got = nil
	report = Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		ProbeGemini: true,
		Timeout:     5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	if strings.Join(got, ",") != "/v1/chat/completions gpt-5.4,/v1/messages claude-sonnet-4-6,/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("empty-list calls %v", got)
	}
}

func TestProbeClientErrorIncludesBodyWithoutSecrets(t *testing.T) {
	const key = "sk-live-secret-value"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5.4"},{"id":"claude-sonnet-4-6"}]}`)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusBadRequest)
		fmt.Fprintf(w, `{"error":{"message":"model not found","key":"%s"}}`, key)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	report := Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      key,
		CodexModel:  "gpt-5",
		ClaudeModel: "claude-sonnet-4-6",
		Timeout:     5 * time.Second,
	})
	if report.OK {
		t.Fatal("expected failure")
	}
	var hint string
	for _, c := range report.Checks {
		if c.Name == "chat" {
			hint = c.Hint
			if c.Rejected() {
				t.Fatal("model not found is not a rejected key")
			}
			if strings.Contains(c.Excerpt, key) || strings.Contains(c.Hint, key) || strings.Contains(c.Excerpt, "sk-live") {
				t.Fatalf("secret leaked: %+v", c)
			}
		}
	}
	if !strings.Contains(hint, "HTTP 400.") || !strings.Contains(hint, "model not found") {
		t.Fatalf("hint %q", hint)
	}
	if got := Hint(400, `{"error":{"message":"`+strings.Repeat("x", 400)+`"}}`); !strings.Contains(got, "…") {
		t.Fatalf("not truncated: %s", got)
	}
}

func TestProbeDoesNotCallNewAPIStatus(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		if r.URL.Path == "/api/status" || strings.HasPrefix(r.URL.Path, "/api/") {
			t.Errorf("new-api path %s", r.URL.Path)
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/v1/models":
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-5"},{"id":"claude-sonnet-4-6"},{"id":"gemini-2.5-pro"},{"id":"grok-4"}]}`)
		default:
			_, _ = io.WriteString(w, `{"id":"ok","choices":[{"message":{"content":"pong"}}],"type":"message","content":[{"type":"text","text":"pong"}]}`)
		}
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	opt := ProbeOptions{
		Endpoints:    ep,
		APIKey:       "good-key",
		Scoped:       true,
		ProbeModels:  true,
		ProbeChat:    true,
		ProbeGrok:    true,
		ProbeClaude:  true,
		ProbeDesktop: true,
		ProbeGemini:  true,
		Client:       srv.Client(),
		Timeout:      5 * time.Second,
	}
	report := Probe(ctx, opt)
	if !report.OK {
		t.Fatalf("scoped probe: %+v", report.Checks)
	}
	opt.Scoped = false
	opt.ProbeGemini = true
	report = Probe(ctx, opt)
	if !report.OK {
		t.Fatalf("unscoped probe: %+v", report.Checks)
	}
	if len(paths) == 0 {
		t.Fatal("probe made no requests")
	}
	for _, path := range paths {
		if path == "/api/status" || strings.HasPrefix(path, "/api/") {
			t.Fatalf("recorded %s", path)
		}
	}
	for _, check := range report.Checks {
		if check.Path == "/api/status" || strings.HasPrefix(check.Path, "/api/") {
			t.Fatalf("check path %s", check.Path)
		}
	}
}

func TestCodexResponsesProbeSendsReasoningEffort(t *testing.T) {
	var mu sync.Mutex
	bodies := map[string]map[string]any{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/models" {
			_, _ = io.WriteString(w, `{"data":[{"id":"gpt-4.1-mini"},{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`)
			return
		}
		raw, _ := io.ReadAll(r.Body)
		var payload map[string]any
		if err := json.Unmarshal(raw, &payload); err != nil {
			t.Errorf("body: %v", err)
			http.Error(w, `{"error":{"message":"bad json"}}`, http.StatusBadRequest)
			return
		}
		model, _ := payload["model"].(string)
		mu.Lock()
		bodies[r.URL.Path+" "+model] = payload
		mu.Unlock()
		if model == "gpt-4.1-mini" || model == "gpt-5.4-mini" {
			http.Error(w, `{"error":{"message":"Unsupported parameter: reasoning.effort"}}`, http.StatusBadRequest)
			return
		}
		_, _ = io.WriteString(w, `{"id":"ok"}`)
	}))
	defer srv.Close()
	ep, err := ParseBase(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	models, err := ParseModels([]byte(`{"data":[{"id":"gpt-4.1-mini"},{"id":"gpt-5.4"},{"id":"gpt-5.4-mini"}]}`))
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	result := VerifiedModels(ctx, ProbeOptions{
		Endpoints: ep,
		APIKey:    "sk-test",
		CodexWire: "responses",
		Timeout:   5 * time.Second,
	}, models, "gpt", "responses")
	if strings.Join(result.Passed, ",") != "gpt-5.4" {
		t.Fatalf("passed %v skipped %+v", result.Passed, result.Skipped)
	}
	var sawMini bool
	for _, skip := range result.Skipped {
		if skip.ID == "gpt-4.1-mini" {
			t.Fatalf("gpt-4.1-mini is below the floor and should not be probed: %+v", result.Skipped)
		}
		if skip.ID == "gpt-5.4-mini" && skip.Check.Status == http.StatusBadRequest && !skip.Check.OK && !skip.Check.Rejected() {
			sawMini = strings.Contains(skip.Check.Message, "reasoning.effort") || strings.Contains(skip.Check.Excerpt, "reasoning.effort")
		}
	}
	if !sawMini {
		t.Fatalf("skipped %+v", result.Skipped)
	}
	for _, id := range []string{"gpt-5.4", "gpt-5.4-mini"} {
		body := bodies["/v1/responses "+id]
		reasoning, _ := body["reasoning"].(map[string]any)
		if reasoning["effort"] != "medium" {
			t.Fatalf("%s reasoning %#v body %#v", id, body["reasoning"], body)
		}
	}

	one := ProbeID(ctx, ProbeOptions{Endpoints: ep, APIKey: "sk-test", Timeout: 5 * time.Second}, "gpt", "responses", "gpt-4.1-mini")
	if one.OK || one.Status != http.StatusBadRequest || one.Detail != "gpt-4.1-mini" {
		t.Fatalf("explicit probe %+v", one)
	}
	body := bodies["/v1/responses gpt-4.1-mini"]
	reasoning, _ := body["reasoning"].(map[string]any)
	if reasoning["effort"] != "medium" {
		t.Fatalf("explicit reasoning %#v", body["reasoning"])
	}

	report := Probe(ctx, ProbeOptions{
		Endpoints:  ep,
		APIKey:     "sk-test",
		CodexModel: "gpt-5.4",
		Timeout:    5 * time.Second,
	})
	if !report.OK {
		t.Fatalf("%+v", report.Checks)
	}
	chat := bodies["/v1/chat/completions gpt-5.4"]
	if _, ok := chat["reasoning"]; ok {
		t.Fatalf("chat body included reasoning: %#v", chat)
	}
}

func modelFrom(t *testing.T, r *http.Request) string {
	t.Helper()
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	return payload.Model
}

func assertModel(t *testing.T, r *http.Request, want string) {
	t.Helper()
	var payload struct {
		Model string `json:"model"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		t.Fatal(err)
	}
	if payload.Model != want {
		t.Fatalf("model %s", payload.Model)
	}
}
