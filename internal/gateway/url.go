package gateway

import (
	"fmt"
	"net/url"
	"strings"
)

const DefaultBaseURL = "https://deeprouter.top"

// Endpoints are the concrete URLs a gateway call uses.
// Anthropic clients append /v1/messages themselves, so AnthropicBase has no
// trailing /v1. OpenAI-compatible clients treat BaseURL as already including /v1.
type Endpoints struct {
	Input         string
	AnthropicBase string
	OpenAIBase    string
}

// ModelsURL is GET {openai}/models.
func (e Endpoints) ModelsURL() string { return e.OpenAIBase + "/models" }

// ChatURL is POST {openai}/chat/completions.
func (e Endpoints) ChatURL() string { return e.OpenAIBase + "/chat/completions" }

// MessagesURL is POST {anthropic}/v1/messages.
func (e Endpoints) MessagesURL() string { return e.AnthropicBase + "/v1/messages" }

// GeminiBase is the value of GOOGLE_GEMINI_BASE_URL.
// @google/genai joins baseUrl, the API version (v1beta unless
// GOOGLE_GENAI_API_VERSION is set), and a path such as
// models/{model}:generateContent. The base is therefore the origin, with no
// /v1 and no /v1beta suffix.
func (e Endpoints) GeminiBase() string {
	return strings.TrimSuffix(e.AnthropicBase, "/v1beta")
}

// GenerateContentURL is POST {origin}/v1beta/models/{model}:generateContent.
func (e Endpoints) GenerateContentURL(model string) string {
	return e.GeminiBase() + GenerateContentPath(model)
}

// GenerateContentPath is the path of a native Gemini generateContent call.
func GenerateContentPath(model string) string {
	return "/v1beta/models/" + url.PathEscape(model) + ":generateContent"
}

// ParseBase normalizes a gateway base URL.
// A trailing /v1 is kept for OpenAI and removed for Anthropic.
// A value with no scheme is treated as https.
func ParseBase(raw string) (Endpoints, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Endpoints{}, fmt.Errorf("base URL is empty")
	}
	if strings.Contains(raw, "?") || strings.Contains(raw, "#") {
		return Endpoints{}, fmt.Errorf("base URL must not include a query string or fragment")
	}
	input := raw
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return Endpoints{}, fmt.Errorf("invalid base URL: %w", err)
	}
	if u.Scheme != "https" && u.Scheme != "http" {
		return Endpoints{}, fmt.Errorf("base URL scheme must be http or https")
	}
	if u.Host == "" {
		return Endpoints{}, fmt.Errorf("base URL is missing a host")
	}
	if u.User != nil {
		return Endpoints{}, fmt.Errorf("base URL must not include a username or password")
	}
	path := strings.TrimSuffix(u.Path, "/")
	anthropicPath := path
	openaiPath := path
	if strings.HasSuffix(path, "/v1") {
		anthropicPath = strings.TrimSuffix(path, "/v1")
		openaiPath = path
	} else {
		if anthropicPath == "/" {
			anthropicPath = ""
		}
		openaiPath = anthropicPath + "/v1"
	}
	return Endpoints{
		Input:         input,
		AnthropicBase: join(u.Scheme, u.Host, anthropicPath),
		OpenAIBase:    join(u.Scheme, u.Host, openaiPath),
	}, nil
}

func join(scheme, host, path string) string {
	if path == "/" || path == "" {
		return scheme + "://" + host
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return scheme + "://" + host + path
}
