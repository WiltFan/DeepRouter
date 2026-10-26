package gateway

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
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
	if chatModel != "gpt-5.4" || claudeModel != "claude-sonnet-4-6" {
		t.Fatalf("picked chat %q claude %q", chatModel, claudeModel)
	}
}

func TestPickDefault(t *testing.T) {
	ids := []string{"claude-fable-5", "claude-sonnet-4-6", "claude-opus-4-6", "gpt-4.1", "gpt-5.4"}
	if got := PickDefault(ids, "claude"); got != "claude-sonnet-4-6" {
		t.Fatalf("claude %s", got)
	}
	if got := PickDefault(ids, "gpt"); got != "gpt-5.4" {
		t.Fatalf("gpt %s", got)
	}
	if got := PickDefault([]string{"Claude-Fable-5"}, "claude"); got != "Claude-Fable-5" {
		t.Fatalf("only fable %s", got)
	}
	if got := PickDefault([]string{"gpt-4.1"}, "gpt"); got != "gpt-4.1" {
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
	var gotPath, gotKey, gotAuth string
	var sawContents bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, ":generateContent") {
			gotPath = r.URL.Path
			gotKey = r.Header.Get("x-goog-api-key")
			gotAuth = r.Header.Get("Authorization")
			var body struct {
				Contents []any `json:"contents"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("body: %v", err)
			}
			sawContents = len(body.Contents) > 0
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
	if gotPath != "/v1beta/models/gemini-2.5-pro:generateContent" || gotKey != "gem-key" || gotAuth != "" || !sawContents {
		t.Fatalf("path %q key %q auth %q contents %v", gotPath, gotKey, gotAuth, sawContents)
	}
	if report.Checks[3].Name != "generateContent" || report.Checks[3].Detail != "gemini-2.5-pro" {
		t.Fatalf("check %+v", report.Checks[3])
	}

	report = Probe(context.Background(), ProbeOptions{
		Endpoints:   ep,
		APIKey:      "good-key",
		GeminiModel: "gemini-2.0-flash",
		ProbeGemini: true,
		Timeout:     5 * time.Second,
	})
	if !report.OK || gotPath != "/v1beta/models/gemini-2.0-flash:generateContent" || gotKey != "good-key" {
		t.Fatalf("chosen model path %q key %q ok %v %+v", gotPath, gotKey, report.OK, report.Checks)
	}

	bare := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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
	if report.OK {
		t.Fatal("expected a skipped gemini check to fail the report")
	}
	var skipped bool
	for _, c := range report.Checks {
		if c.Name == "generateContent" && strings.Contains(c.Hint, "No Gemini model id") {
			skipped = true
		}
	}
	if !skipped {
		t.Fatalf("%+v", report.Checks)
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
	if chatModel != "gpt-image-keep" || claudeModel != "claude-sonnet-4-6" || geminiPath != "/v1beta/models/gemini-2.0-flash:generateContent" {
		t.Fatalf("chat %q claude %q gemini %q", chatModel, claudeModel, geminiPath)
	}
	if len(report.Models) != 22 || report.Models[0] != "dall-e-3" {
		t.Fatalf("models %v", report.Models)
	}

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
	if chatModel != "gpt-image-1" || claudeModel != "dall-e-3" {
		t.Fatalf("fallback chat %q claude %q", chatModel, claudeModel)
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
