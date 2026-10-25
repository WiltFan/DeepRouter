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
