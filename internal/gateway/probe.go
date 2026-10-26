package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const userAgent = "deeprouter"

// Check is one HTTP call in a connectivity run.
type Check struct {
	Name       string        `json:"name"`
	Method     string        `json:"method"`
	Path       string        `json:"path"`
	URL        string        `json:"url"`
	Status     int           `json:"status"`
	StatusText string        `json:"status_text"`
	TTFB       time.Duration `json:"-"`
	Total      time.Duration `json:"-"`
	TTFBMS     int64         `json:"ttfb_ms"`
	TotalMS    int64         `json:"total_ms"`
	OK         bool          `json:"ok"`
	Hint       string        `json:"hint,omitempty"`
	Detail     string        `json:"detail,omitempty"`
	Excerpt    string        `json:"excerpt,omitempty"`
	Err        string        `json:"error,omitempty"`
}

// Report is the full connectivity result.
type Report struct {
	BaseURL string   `json:"base_url"`
	Checks  []Check  `json:"checks"`
	OK      bool     `json:"ok"`
	Models  []string `json:"models,omitempty"`
}

// ProbeOptions selects what a connectivity run calls.
type ProbeOptions struct {
	Endpoints   Endpoints
	APIKey      string
	ClaudeModel string
	CodexModel  string
	Timeout     time.Duration
	Client      *http.Client
}

// Probe calls GET /v1/models, a one-token chat completion, and a one-token
// Anthropic messages request.
func Probe(ctx context.Context, opt ProbeOptions) Report {
	if opt.Timeout <= 0 {
		opt.Timeout = 30 * time.Second
	}
	client := opt.Client
	if client == nil {
		client = newClient(opt.Timeout)
	}
	report := Report{BaseURL: opt.Endpoints.AnthropicBase, OK: true}

	models, body := do(ctx, client, opt.APIKey, request{
		Name:   "models",
		Method: http.MethodGet,
		URL:    opt.Endpoints.ModelsURL(),
		Path:   "/v1/models",
		Auth:   authBearer,
	})
	var ids []string
	if models.OK {
		parsed, err := parseModelIDs(body)
		if err != nil {
			models.OK = false
			models.Hint = "The model list could not be parsed."
		} else {
			ids = parsed
			models.Detail = fmt.Sprintf("%d models", len(ids))
			models.Excerpt = ""
			report.Models = ids
		}
	}
	report.Checks = append(report.Checks, models)
	if !models.OK {
		report.OK = false
	}

	codexModel := opt.CodexModel
	claudeModel := opt.ClaudeModel
	if codexModel == "" {
		codexModel = PickDefault(ids, "gpt")
	}
	if codexModel == "" {
		codexModel = firstMatching(ids, "o1", "o3", "codex")
	}
	if claudeModel == "" {
		claudeModel = PickDefault(ids, "claude")
	}
	if codexModel == "" && len(ids) > 0 {
		codexModel = ids[0]
	}
	if claudeModel == "" && len(ids) > 0 {
		claudeModel = ids[0]
	}

	if codexModel == "" {
		report.Checks = append(report.Checks, skipped("chat", http.MethodPost, "/v1/chat/completions", "No Codex model id. Pass --codex-model, or run setup with a model."))
		report.OK = false
	} else {
		body := map[string]any{
			"model":      codexModel,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		}
		chat, _ := do(ctx, client, opt.APIKey, request{
			Name:   "chat",
			Method: http.MethodPost,
			URL:    opt.Endpoints.ChatURL(),
			Path:   "/v1/chat/completions",
			Auth:   authBearer,
			Body:   body,
			Detail: codexModel,
		})
		report.Checks = append(report.Checks, chat)
		if !chat.OK {
			report.OK = false
		}
	}

	if claudeModel == "" {
		report.Checks = append(report.Checks, skipped("messages", http.MethodPost, "/v1/messages", "No Claude model id. Pass --claude-model, or run setup with a model."))
		report.OK = false
	} else {
		body := map[string]any{
			"model":      claudeModel,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		}
		msg, _ := do(ctx, client, opt.APIKey, request{
			Name:   "messages",
			Method: http.MethodPost,
			URL:    opt.Endpoints.MessagesURL(),
			Path:   "/v1/messages",
			Auth:   authAnthropic,
			Body:   body,
			Detail: claudeModel,
		})
		report.Checks = append(report.Checks, msg)
		if !msg.OK {
			report.OK = false
		}
	}
	return report
}

// ListModels returns model ids from GET /v1/models.
func ListModels(ctx context.Context, opt ProbeOptions) ([]string, Check, error) {
	if opt.Timeout <= 0 {
		opt.Timeout = 30 * time.Second
	}
	client := opt.Client
	if client == nil {
		client = newClient(opt.Timeout)
	}
	check, body := do(ctx, client, opt.APIKey, request{
		Name:   "models",
		Method: http.MethodGet,
		URL:    opt.Endpoints.ModelsURL(),
		Path:   "/v1/models",
		Auth:   authBearer,
	})
	if !check.OK {
		return nil, check, fmt.Errorf("%s", checkFailure(check))
	}
	ids, err := parseModelIDs(body)
	if err != nil {
		check.OK = false
		check.Hint = "The model list could not be parsed."
		return nil, check, fmt.Errorf("%s", check.Hint)
	}
	if ids == nil {
		ids = []string{}
	}
	check.Detail = fmt.Sprintf("%d models", len(ids))
	check.Excerpt = ""
	return ids, check, nil
}

type authStyle int

const (
	authBearer authStyle = iota
	authAnthropic
)

type request struct {
	Name   string
	Method string
	URL    string
	Path   string
	Auth   authStyle
	Body   any
	Detail string
}

// modelsHandshakeTimeout is the TLS handshake limit for gateway calls made
// by test, including GET /v1/models. net/http's default transport stops at
// 10s, which timed out against a slow gateway.
const modelsHandshakeTimeout = 20 * time.Second

func newClient(timeout time.Duration) *http.Client {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	handshake := modelsHandshakeTimeout
	if timeout > handshake {
		handshake = timeout
	}
	var transport *http.Transport
	if base, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = base.Clone()
	} else {
		transport = &http.Transport{}
	}
	transport.TLSHandshakeTimeout = handshake
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func do(ctx context.Context, client *http.Client, apiKey string, req request) (Check, []byte) {
	check := Check{
		Name:   req.Name,
		Method: req.Method,
		Path:   req.Path,
		URL:    req.URL,
		Detail: req.Detail,
	}
	var body io.Reader
	if req.Body != nil {
		raw, err := json.Marshal(req.Body)
		if err != nil {
			check.Err = err.Error()
			check.Hint = "Could not encode the request."
			return finish(check), nil
		}
		body = bytes.NewReader(raw)
	}
	httpReq, err := http.NewRequestWithContext(ctx, req.Method, req.URL, body)
	if err != nil {
		check.Err = err.Error()
		check.Hint = "The request URL is not valid. Check the base URL."
		return finish(check), nil
	}
	httpReq.Header.Set("User-Agent", userAgent)
	httpReq.Header.Set("Accept", "application/json")
	if req.Body != nil {
		httpReq.Header.Set("Content-Type", "application/json")
	}
	switch req.Auth {
	case authAnthropic:
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
		httpReq.Header.Set("x-api-key", apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	default:
		// GET /v1/models and POST /v1/chat/completions use Bearer only.
		// The working curl is: curl $BASE/v1/models -H "Authorization: Bearer KEY"
		httpReq.Header.Set("Authorization", "Bearer "+apiKey)
	}

	start := time.Now()
	resp, err := client.Do(httpReq)
	ttfb := time.Since(start)
	if err != nil {
		check.TTFB = ttfb
		check.Total = time.Since(start)
		check.Err = err.Error()
		check.Hint = networkHint(err)
		return finish(check), nil
	}
	defer resp.Body.Close()
	check.Status = resp.StatusCode
	check.StatusText = strings.TrimSpace(strings.TrimPrefix(resp.Status, fmt.Sprint(resp.StatusCode)))
	if check.StatusText == "" {
		check.StatusText = http.StatusText(resp.StatusCode)
	}
	limited := io.LimitReader(resp.Body, 1<<20)
	raw, readErr := io.ReadAll(limited)
	total := time.Since(start)
	check.TTFB = ttfb
	check.Total = total
	// The excerpt is capped for display. Callers that need the payload,
	// including the model list, must use the returned body. Parsing the
	// excerpt drops every id once the JSON is longer than 180 bytes.
	excerpt := redact(string(raw), apiKey)
	excerpt = oneLine(excerpt, 180)
	check.Excerpt = excerpt
	if readErr != nil {
		check.Err = readErr.Error()
		check.Hint = "The response body could not be read."
		return finish(check), raw
	}
	check.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	check.Hint = Hint(resp.StatusCode, excerpt)
	if !check.OK && check.Excerpt != "" && check.Hint == "" {
		check.Hint = "The gateway returned an error."
	}
	return finish(check), raw
}

func finish(check Check) Check {
	check.TTFBMS = check.TTFB.Milliseconds()
	check.TotalMS = check.Total.Milliseconds()
	if check.Total < check.TTFB {
		check.Total = check.TTFB
		check.TotalMS = check.TTFBMS
	}
	return check
}

func skipped(name, method, path, hint string) Check {
	return Check{Name: name, Method: method, Path: path, Hint: hint, OK: false}
}

// Hint maps an HTTP status to a short explanation.
func Hint(status int, body string) string {
	switch status {
	case 0:
		return ""
	case http.StatusUnauthorized:
		return "401 Bad key. The API key was rejected. Check the key on this profile."
	case http.StatusForbidden:
		return "403 Quota or permission. The gateway refused this request. Check this key's access."
	case http.StatusNotFound:
		return "404 Model or path. That model or path was not found. Run `deeprouter models` and switch to an id the gateway lists."
	case http.StatusServiceUnavailable:
		return "503 No channel. No channel is available for this model right now. Try another model."
	case http.StatusTooManyRequests:
		return "429 Rate limit. The gateway asked us to slow down. Wait a moment and run the test again."
	default:
		if status >= 300 && status < 400 {
			return "The gateway redirected the request. Check the base URL, including http versus https and a trailing /v1."
		}
		if status >= 200 && status < 300 {
			return ""
		}
		if body != "" {
			return fmt.Sprintf("HTTP %d. The gateway returned an error.", status)
		}
		return fmt.Sprintf("HTTP %d.", status)
	}
}

func networkHint(err error) string {
	msg := err.Error()
	low := strings.ToLower(msg)
	switch {
	case strings.Contains(low, "timeout") || strings.Contains(low, "deadline"):
		return "The gateway did not respond before the timeout. Check the base URL, or raise --timeout."
	case strings.Contains(low, "tls") || strings.Contains(low, "certificate") || strings.Contains(low, "x509"):
		return "TLS handshake failed. Confirm the base URL and that the gateway certificate is valid."
	case strings.Contains(low, "connection refused"):
		return "The connection was refused. Check the host, port, and that the gateway is running."
	case strings.Contains(low, "no such host"):
		return "DNS lookup failed. Check the host in the base URL."
	default:
		return "Could not reach the gateway. Check the base URL and your network."
	}
}

func checkFailure(check Check) string {
	if check.Hint != "" {
		return check.Hint
	}
	if check.Err != "" {
		return check.Err
	}
	return fmt.Sprintf("HTTP %d", check.Status)
}

func parseModelIDs(body []byte) ([]string, error) {
	body = bytes.TrimSpace(body)
	if len(body) == 0 {
		return []string{}, nil
	}
	var wrap struct {
		Data   json.RawMessage `json:"data"`
		Models json.RawMessage `json:"models"`
	}
	if err := json.Unmarshal(body, &wrap); err != nil {
		ids, listErr := parseIDList(body)
		if listErr != nil {
			return nil, err
		}
		return ids, nil
	}
	var ids []string
	if len(bytes.TrimSpace(wrap.Data)) > 0 && string(bytes.TrimSpace(wrap.Data)) != "null" {
		parsed, err := parseIDList(wrap.Data)
		if err != nil {
			return nil, fmt.Errorf("model list data: %w", err)
		}
		ids = append(ids, parsed...)
	}
	if len(bytes.TrimSpace(wrap.Models)) > 0 && string(bytes.TrimSpace(wrap.Models)) != "null" {
		parsed, err := parseIDList(wrap.Models)
		if err != nil {
			return nil, fmt.Errorf("model list models: %w", err)
		}
		ids = append(ids, parsed...)
	}
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

func parseIDList(raw []byte) ([]string, error) {
	var list []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(list))
	for _, m := range list {
		if m.ID != "" {
			ids = append(ids, m.ID)
		}
	}
	return ids, nil
}

func firstMatching(ids []string, needles ...string) string {
	for _, id := range ids {
		low := strings.ToLower(id)
		for _, n := range needles {
			if strings.Contains(low, n) {
				return id
			}
		}
	}
	return ""
}

func redact(body, secret string) string {
	if secret != "" && len(secret) >= 4 {
		body = strings.ReplaceAll(body, secret, "****")
	}
	return body
}

func oneLine(s string, max int) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		return s[:max] + "…"
	}
	return s
}
