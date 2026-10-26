// Package testgate is a local OpenAI- and Anthropic-compatible gateway for tests.
package testgate

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"time"
)

const (
	// GoodKey is accepted.
	GoodKey = "sk-good-key-value"
	// AzureKey is a second accepted key, used to prove profile switching.
	AzureKey = "sk-azure-key-9999"
	// BadKey is rejected with 401.
	BadKey = "sk-bad-key-value"
	// QuotaKey is rejected with 403.
	QuotaKey = "sk-quota-key-value"
)

// New starts a gateway that understands /v1/models, /v1/chat/completions,
// and /v1/messages.
func New() *httptest.Server {
	return httptest.NewServer(Handler())
}

// Handler is the gateway mux.
func Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(15 * time.Millisecond)
		key := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if key == "" {
			key = r.Header.Get("x-api-key")
		}
		var payload struct {
			Model string `json:"model"`
		}
		if r.Body != nil && r.Method == http.MethodPost {
			raw, _ := io.ReadAll(r.Body)
			_ = json.Unmarshal(raw, &payload)
		}
		switch {
		case key == BadKey:
			writeErr(w, http.StatusUnauthorized, "invalid api key")
		case key == QuotaKey:
			writeErr(w, http.StatusForbidden, "quota exceeded")
		case payload.Model == "missing-model":
			writeErr(w, http.StatusNotFound, "model not found")
		case payload.Model == "down-model":
			writeErr(w, http.StatusServiceUnavailable, "no channel")
		case key != GoodKey && key != AzureKey:
			writeErr(w, http.StatusUnauthorized, "invalid api key")
		case r.URL.Path == "/v1/models" && r.Method == http.MethodGet:
			w.Header().Set("Content-Type", "application/json")
			// Same shape as https://deeprouter.top/v1/models, and longer than the
			// 180-byte excerpt that used to be parsed in place of the body.
			_, _ = io.WriteString(w, `{"data":[{"id":"claude-fable-5","object":"model","owned_by":"claude","supported_endpoint_types":["anthropic","openai"]},{"id":"claude-sonnet-4-6","object":"model","owned_by":"claude","supported_endpoint_types":["anthropic","openai"]},{"id":"claude-opus-4-6","object":"model","owned_by":"claude","supported_endpoint_types":["anthropic","openai"]},{"id":"gpt-4.1","object":"model","owned_by":"openai","supported_endpoint_types":["openai"]},{"id":"gpt-5.4","object":"model","owned_by":"openai","supported_endpoint_types":["openai"]},{"id":"grok-4","object":"model","owned_by":"xai","supported_endpoint_types":["openai"]}]}`)
		case r.URL.Path == "/v1/chat/completions" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"chatcmpl_test","choices":[{"message":{"role":"assistant","content":"pong"}}]}`)
		case r.URL.Path == "/v1/messages" && r.Method == http.MethodPost:
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"id":"msg_test","type":"message","content":[{"type":"text","text":"pong"}]}`)
		default:
			writeErr(w, http.StatusNotFound, "unknown path")
		}
	})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{
		"error": map[string]string{"message": msg},
	})
}
