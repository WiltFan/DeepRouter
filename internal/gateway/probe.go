package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
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
	// Skipped is a check that was not run, with Hint explaining why.
	// It does not fail the report.
	Skipped bool   `json:"skipped,omitempty"`
	Hint    string `json:"hint,omitempty"`
	Detail  string `json:"detail,omitempty"`
	Excerpt string `json:"excerpt,omitempty"`
	Err     string `json:"error,omitempty"`
	// Message is the gateway's own error text from a failed response.
	Message string `json:"message,omitempty"`
	// Probed is set when no model is configured for this program and Detail
	// is an id picked only to send this request.
	Probed bool `json:"probed,omitempty"`
	// KeyRejected is set from a 4xx body that says the key itself is invalid.
	// Gemini and xAI use HTTP 400 for that. It is not part of the JSON report.
	KeyRejected bool `json:"-"`
}

// Rejected reports that the server refused the key itself.
// HTTP 401 and 403 always count. Another 4xx counts when the body says the
// key is invalid: Gemini API_KEY_INVALID or INVALID_ARGUMENT with a key
// error, xAI "incorrect API key", OpenAI invalid_api_key, and Anthropic
// authentication_error or "invalid x-api-key". A 5xx response does not
// count, and neither does a 4xx about something else, such as a missing model.
func (c Check) Rejected() bool {
	if c.Status == http.StatusUnauthorized || c.Status == http.StatusForbidden {
		return true
	}
	if c.Status < 400 || c.Status >= 500 {
		return false
	}
	if c.KeyRejected {
		return true
	}
	return marksInvalidKey(c.Message) || marksInvalidKey(c.Excerpt) || marksInvalidKey(c.Hint)
}

// marksInvalidKey reports provider text that means the key itself was refused.
// INVALID_ARGUMENT alone is not enough: it also covers a bad model id.
func marksInvalidKey(body string) bool {
	low := strings.ToLower(body)
	if low == "" {
		return false
	}
	for _, needle := range []string{
		"api_key_invalid",
		"invalid_api_key",
		"incorrect api key",
		"invalid api key",
		"api key not valid",
		"api key is invalid",
		"api key invalid",
		"api key expired",
		"invalid x-api-key",
		"authentication_error",
	} {
		if strings.Contains(low, needle) {
			return true
		}
	}
	if strings.Contains(low, "invalid_argument") && (strings.Contains(low, "api key") || strings.Contains(low, "api_key")) {
		return true
	}
	return false
}

// Report is the full connectivity result.
type Report struct {
	BaseURL string   `json:"base_url"`
	Checks  []Check  `json:"checks"`
	OK      bool     `json:"ok"`
	Models  []string `json:"models,omitempty"`
}

// Fallback probe ids are used only when the gateway model list is empty or
// could not be loaded. A non-empty catalog never uses these.
const (
	fallbackChatModel   = "gpt-5.4"
	fallbackClaudeModel = "claude-sonnet-4-6"
	fallbackGeminiModel = "gemini-2.5-pro"
	fallbackGrokModel   = "grok-4"
)

// ProbeOptions selects what a connectivity run calls.
//
// With Scoped false, the run calls GET /v1/models, POST /v1/chat/completions,
// and POST /v1/messages, plus generateContent when ProbeGemini is set.
// With Scoped true, only the marked endpoints run:
// Codex is ProbeModels and ProbeChat, Claude Code is ProbeClaude,
// Claude Desktop is ProbeDesktop, Grok Build is ProbeGrok, and Gemini CLI
// is ProbeGemini.
type ProbeOptions struct {
	Endpoints   Endpoints
	APIKey      string
	ClaudeModel string
	CodexModel  string
	// DesktopModel is the Claude Desktop id. Empty picks one from the list.
	DesktopModel string
	// GrokModel is the Grok Build id. Empty picks one from the list.
	GrokModel string
	// GeminiModel is the chosen Gemini id. Empty picks one from the model list.
	GeminiModel string
	// Per-program keys override APIKey for that program's request.
	CodexKey   string
	ClaudeKey  string
	DesktopKey string
	GrokKey    string
	// GeminiKey is sent as x-goog-api-key. Empty uses APIKey.
	GeminiKey string
	// Scoped limits the run to the Probe* endpoints below.
	Scoped bool
	// ProbeModels adds GET /v1/models (Codex / ChatGPT).
	ProbeModels bool
	// ProbeChat adds POST /v1/chat/completions for the Codex model.
	ProbeChat bool
	// ProbeGrok adds POST /v1/chat/completions for the Grok model.
	ProbeGrok bool
	// ProbeClaude adds POST /v1/messages for the Claude Code model.
	ProbeClaude bool
	// ProbeDesktop adds POST /v1/messages for the Claude Desktop model.
	ProbeDesktop bool
	// ProbeGemini adds POST /v1beta/models/{model}:generateContent.
	ProbeGemini bool
	// CodexWire is responses or chat. responses probes POST /v1/responses
	// for the Codex model, including reasoning.effort, matching a real Codex
	// request. Empty and chat keep POST /v1/chat/completions.
	CodexWire string
	Timeout   time.Duration
	Client    *http.Client
}

// Probe calls the connectivity endpoints selected by opt.
// A chosen model id is the only model posted for that client. An empty id
// is not saved: candidates are tried newest first and the check stops at
// the first one that answers, so a later model is never requested. Each
// client's check has its own timeout, so one hung model does not use up
// the time left for the next client. A list with no model of the right
// family skips that check instead of failing it. A hardcoded id is used
// only when the list is empty or unavailable.
//
// Requests stay on GET /v1/models, POST /v1/chat/completions,
// POST /v1/messages, and Gemini generateContent. new-api dashboard routes
// such as GET /api/status are not called. LinkMesh is not a new-api gateway
// and does not serve them.
func Probe(ctx context.Context, opt ProbeOptions) Report {
	if opt.Timeout <= 0 {
		opt.Timeout = 30 * time.Second
	}
	client := opt.Client
	if client == nil {
		client = newClient(opt.Timeout)
	}
	report := Report{BaseURL: opt.Endpoints.AnthropicBase, OK: true}
	plan := opt.plan()
	chatSpecs, messageSpecs, geminiSpecs := opt.specs(plan)

	var catalog []Model
	listOK := false
	if plan.models || specsNeedCatalog(chatSpecs, messageSpecs, geminiSpecs) {
		modelsKey := opt.APIKey
		if opt.Scoped {
			modelsKey = firstKey(opt.CodexKey, opt.APIKey)
		}
		models, body := do(ctx, client, modelsKey, request{
			Name:   "models",
			Method: http.MethodGet,
			URL:    opt.Endpoints.ModelsURL(),
			Path:   "/v1/models",
			Auth:   authBearer,
		})
		if models.OK {
			parsed, err := ParseModels(body)
			if err != nil {
				models.OK = false
				models.Hint = "The model list could not be parsed."
			} else {
				catalog = parsed
				listOK = true
				ids := ModelIDs(parsed)
				models.Detail = fmt.Sprintf("%d models", len(ids))
				models.Excerpt = ""
				report.Models = ids
			}
		}
		if plan.models {
			report.Checks = append(report.Checks, models)
			if !models.OK {
				report.OK = false
			}
		}
	}

	runModelChecks(ctx, client, opt, &report, catalog, listOK, chatSpecs)
	runModelChecks(ctx, client, opt, &report, catalog, listOK, messageSpecs)
	runModelChecks(ctx, client, opt, &report, catalog, listOK, geminiSpecs)
	return report
}

type probePlan struct {
	models  bool
	chat    bool
	grok    bool
	claude  bool
	desktop bool
	gemini  bool
}

func (opt ProbeOptions) plan() probePlan {
	if !opt.Scoped {
		return probePlan{models: true, chat: true, claude: true, gemini: opt.ProbeGemini}
	}
	return probePlan{
		models:  opt.ProbeModels,
		chat:    opt.ProbeChat,
		grok:    opt.ProbeGrok,
		claude:  opt.ProbeClaude,
		desktop: opt.ProbeDesktop,
		gemini:  opt.ProbeGemini,
	}
}

type modelSpec struct {
	name     string
	family   string
	chosen   string
	fallback string
	key      string
	auth     authStyle
	kind     string
}

func (opt ProbeOptions) specs(plan probePlan) (chat, messages, gemini []modelSpec) {
	if !opt.Scoped {
		chat = []modelSpec{chatSpec("chat", "gpt", opt.CodexModel, fallbackChatModel, opt.APIKey)}
		messages = []modelSpec{messageSpec("messages", opt.ClaudeModel, opt.APIKey)}
		if plan.gemini {
			gemini = []modelSpec{geminiSpec(opt.GeminiModel, firstKey(opt.GeminiKey, opt.APIKey))}
		}
		return chat, messages, gemini
	}
	if plan.chat {
		chat = append(chat, chatSpec("chat", "gpt", opt.CodexModel, fallbackChatModel, firstKey(opt.CodexKey, opt.APIKey)))
	}
	if plan.grok {
		chat = append(chat, chatSpec("grok", "grok", opt.GrokModel, fallbackGrokModel, firstKey(opt.GrokKey, opt.APIKey)))
	}
	if plan.claude {
		messages = append(messages, messageSpec("messages", opt.ClaudeModel, firstKey(opt.ClaudeKey, opt.APIKey)))
	}
	if plan.desktop {
		messages = append(messages, messageSpec("messages", opt.DesktopModel, firstKey(opt.DesktopKey, opt.APIKey)))
	}
	if plan.gemini {
		gemini = append(gemini, geminiSpec(opt.GeminiModel, firstKey(opt.GeminiKey, opt.APIKey)))
	}
	return chat, messages, gemini
}

func chatSpec(name, family, chosen, fallback, key string) modelSpec {
	return modelSpec{name: name, family: family, chosen: chosen, fallback: fallback, key: key, auth: authBearer, kind: "chat"}
}

func messageSpec(name, chosen, key string) modelSpec {
	return modelSpec{name: name, family: "claude", chosen: chosen, fallback: fallbackClaudeModel, key: key, auth: authAnthropic, kind: "messages"}
}

func geminiSpec(chosen, key string) modelSpec {
	return modelSpec{name: "generateContent", family: "gemini", chosen: chosen, fallback: fallbackGeminiModel, key: key, auth: authGoogle, kind: "gemini"}
}

func specsNeedCatalog(groups ...[]modelSpec) bool {
	for _, group := range groups {
		for _, spec := range group {
			if strings.TrimSpace(spec.chosen) == "" {
				return true
			}
		}
	}
	return false
}

func firstKey(specific, shared string) string {
	if strings.TrimSpace(specific) != "" {
		return specific
	}
	return shared
}

func runModelChecks(ctx context.Context, client *http.Client, opt ProbeOptions, report *Report, catalog []Model, listOK bool, specs []modelSpec) {
	seenModel := map[string]bool{}
	seenSkip := map[string]bool{}
	for _, spec := range specs {
		model, skip := selectProbeModel(spec.chosen, catalog, listOK, spec.family, spec.fallback)
		if skip {
			note := skipHint(spec.family)
			if seenSkip[note] {
				continue
			}
			seenSkip[note] = true
			report.Checks = append(report.Checks, skipped(spec.name, http.MethodPost, spec.path(""), note))
			continue
		}
		// An empty model is not saved. Try candidates newest first and stop
		// at the first one that answers; later ids are not requested.
		// Claude Code and Claude Desktop share that check. A chosen id is
		// the only model posted for that client.
		probed := strings.TrimSpace(spec.chosen) == ""
		dedupe := spec.kind + "\x00" + model
		if probed {
			dedupe = spec.kind + "\x00" + spec.family
		}
		if seenModel[dedupe] {
			continue
		}
		seenModel[dedupe] = true
		spec = applyCodexWire(opt, spec)
		checkCtx, cancel := independentContext(ctx, opt.Timeout)
		var check Check
		if probed && listOK && len(catalog) > 0 {
			check = firstAnswering(checkCtx, client, opt, spec, catalog)
		} else {
			check = postModel(checkCtx, client, opt, spec, model)
		}
		cancel()
		check.Probed = probed
		report.Checks = append(report.Checks, check)
		if !check.OK {
			report.OK = false
		}
	}
}

// firstAnswering posts candidates one at a time, newest first, and returns
// the first success. A refused key does not stop the walk, because a 403
// can belong to one model. Nothing after the success is requested. The
// context is this client's own timer.
func firstAnswering(ctx context.Context, client *http.Client, opt ProbeOptions, spec modelSpec, catalog []Model) Check {
	order := cappedCandidates(catalog, spec.family)
	if len(order) == 0 {
		return Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model answered a test request."}
	}
	var last Check
	var rejected Check
	sawRejected := false
	for _, id := range order {
		if ctx.Err() != nil {
			break
		}
		check := postModel(ctx, client, opt, spec, id)
		if check.OK {
			return check
		}
		if check.Rejected() && !sawRejected {
			rejected = check
			sawRejected = true
		}
		last = check
	}
	if sawRejected {
		return rejected
	}
	if last.Name == "" {
		return Check{Name: spec.name, Method: http.MethodPost, Path: spec.path(""), Hint: "No model answered a test request."}
	}
	return last
}

// independentContext is a timeout that does not inherit parent's deadline.
// One client's check can run until that deadline without cancelling the
// next client's check. An explicit cancel of parent still stops this one.
func independentContext(parent context.Context, timeout time.Duration) (context.Context, context.CancelFunc) {
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	if parent == nil {
		return ctx, cancel
	}
	stop := context.AfterFunc(parent, func() {
		if errors.Is(parent.Err(), context.DeadlineExceeded) {
			return
		}
		cancel()
	})
	return ctx, func() {
		stop()
		cancel()
	}
}

func postModel(ctx context.Context, client *http.Client, opt ProbeOptions, spec modelSpec, model string) Check {
	path := spec.path(model)
	req := request{
		Name:   spec.name,
		Method: http.MethodPost,
		Path:   path,
		Auth:   spec.auth,
		Detail: model,
	}
	switch spec.kind {
	case "messages":
		req.URL = opt.Endpoints.MessagesURL()
		req.Body = map[string]any{
			"model":      model,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		}
	case "gemini":
		req.URL = opt.Endpoints.GenerateContentURL(model)
		req.Body = map[string]any{
			"contents": []map[string]any{
				{"role": "user", "parts": []map[string]string{{"text": "ping"}}},
			},
			"generationConfig": map[string]any{"maxOutputTokens": 1},
		}
	case "responses":
		req.URL = opt.Endpoints.ResponsesURL()
		req.Path = "/v1/responses"
		req.Body = map[string]any{
			"model":             model,
			"input":             "ping",
			"max_output_tokens": 16,
			"reasoning":         map[string]any{"effort": "medium"},
		}
	default:
		req.URL = opt.Endpoints.ChatURL()
		req.Body = map[string]any{
			"model":      model,
			"max_tokens": 1,
			"messages":   []map[string]string{{"role": "user", "content": "ping"}},
		}
	}
	check, _ := do(ctx, client, spec.key, req)
	return check
}

func (s modelSpec) path(model string) string {
	switch s.kind {
	case "gemini":
		if model == "" {
			return "/v1beta/models/{model}:generateContent"
		}
		return GenerateContentPath(model)
	case "messages":
		return "/v1/messages"
	case "responses":
		return "/v1/responses"
	default:
		return "/v1/chat/completions"
	}
}

func applyCodexWire(opt ProbeOptions, spec modelSpec) modelSpec {
	if spec.name == "chat" && spec.family == "gpt" && strings.EqualFold(strings.TrimSpace(opt.CodexWire), "responses") {
		spec.kind = "responses"
	}
	return spec
}

// selectProbeModel returns the id to send. skip is set when the catalog is
// non-empty but has no chat model of that family. A chosen id is kept.
func selectProbeModel(chosen string, catalog []Model, listOK bool, family, fallback string) (model string, skip bool) {
	chosen = strings.TrimSpace(chosen)
	if chosen != "" {
		return chosen, false
	}
	if !listOK || len(catalog) == 0 {
		return fallback, false
	}
	if id := PickDefaultModels(catalog, family); id != "" {
		return id, false
	}
	return "", true
}

func skipHint(family string) string {
	return fmt.Sprintf("Skipped. No %s chat model in this gateway's model list.", family)
}

// ListModels returns model ids from GET /v1/models.
func ListModels(ctx context.Context, opt ProbeOptions) ([]string, Check, error) {
	models, check, err := ListCatalog(ctx, opt)
	if err != nil {
		return nil, check, err
	}
	return ModelIDs(models), check, nil
}

// ListCatalog returns gateway model records from GET /v1/models.
// The records keep type and capability metadata used by FilterChat.
func ListCatalog(ctx context.Context, opt ProbeOptions) ([]Model, Check, error) {
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
	models, err := ParseModels(body)
	if err != nil {
		check.OK = false
		check.Hint = "The model list could not be parsed."
		return nil, check, fmt.Errorf("%s", check.Hint)
	}
	if models == nil {
		models = []Model{}
	}
	check.Detail = fmt.Sprintf("%d models", len(models))
	check.Excerpt = ""
	return models, check, nil
}

// CheckOfficial asks one official API whether apiKey is accepted.
// providerID is openai, openai-api, anthropic, gemini, or xai. baseURL is
// the configured origin; the request uses that host with the vendor's
// models path. OpenAI and xAI send Authorization: Bearer. Gemini sends
// x-goog-api-key. Anthropic sends x-api-key and anthropic-version, and
// does not send Authorization. client may be nil. A refused key is
// Check.Rejected, including HTTP 401, 403, and a 4xx body that says the
// key is invalid. Status 0 means the host did not answer. The returned
// check does not include the raw key.
func CheckOfficial(ctx context.Context, providerID, baseURL, apiKey string, client *http.Client) (Check, error) {
	ep, err := ParseBase(baseURL)
	if err != nil {
		return Check{Hint: err.Error()}, err
	}
	id := strings.ToLower(strings.TrimSpace(providerID))
	if id == "openai" {
		id = "openai-api"
	}
	var req request
	switch id {
	case "openai-api", "xai":
		req = request{
			Name:   "models",
			Method: http.MethodGet,
			URL:    ep.ModelsURL(),
			Path:   "/v1/models",
			Auth:   authBearer,
		}
	case "gemini":
		req = request{
			Name:   "models",
			Method: http.MethodGet,
			URL:    ep.GeminiBase() + "/v1beta/models",
			Path:   "/v1beta/models",
			Auth:   authGoogle,
		}
	case "anthropic":
		req = request{
			Name:   "models",
			Method: http.MethodGet,
			URL:    ep.AnthropicBase + "/v1/models",
			Path:   "/v1/models",
			Auth:   authAnthropicKey,
		}
	default:
		return Check{}, fmt.Errorf("not an official API: %s", providerID)
	}
	if client == nil {
		client = newClient(20 * time.Second)
	}
	check, _ := do(ctx, client, apiKey, req)
	return check, nil
}

type authStyle int

const (
	authBearer authStyle = iota
	authAnthropic
	authGoogle
	// authAnthropicKey is the official api.anthropic.com style: x-api-key and
	// anthropic-version, with no Authorization header.
	authAnthropicKey
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

// do sends one connectivity request. The path is an OpenAI, Anthropic, or
// Gemini route. new-api dashboard routes such as /api/status are not used;
// LinkMesh does not serve them.
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
	case authAnthropicKey:
		httpReq.Header.Set("x-api-key", apiKey)
		httpReq.Header.Set("anthropic-version", "2023-06-01")
	case authGoogle:
		httpReq.Header.Set("x-goog-api-key", apiKey)
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
	// Classify the key from the full body. The excerpt below is capped at
	// 180 bytes, and Gemini puts API_KEY_INVALID in details past that.
	if resp.StatusCode >= 400 && resp.StatusCode < 500 && marksInvalidKey(string(raw)) {
		check.KeyRejected = true
	}
	// The excerpt is capped for display. Callers that need the payload,
	// including the model list, must use the returned body. Parsing the
	// excerpt drops every id once the JSON is longer than 180 bytes.
	excerpt := redact(string(raw), apiKey)
	excerpt = scrubSecrets(excerpt)
	excerpt = oneLine(excerpt, 180)
	check.Excerpt = excerpt
	if readErr != nil {
		check.Err = readErr.Error()
		check.Hint = "The response body could not be read."
		return finish(check), raw
	}
	check.OK = resp.StatusCode >= 200 && resp.StatusCode < 300
	check.Hint = Hint(resp.StatusCode, excerpt)
	if !check.OK {
		check.Message = gatewayErrorMessage(excerpt)
	}
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
	return Check{Name: name, Method: method, Path: path, Hint: hint, OK: true, Skipped: true}
}

// Hint maps an HTTP status to a short explanation.
// A 4xx response includes the gateway's error message when the body has one.
func Hint(status int, body string) string {
	switch status {
	case 0:
		return ""
	case http.StatusUnauthorized:
		return appendGatewayMessage("401 Bad key. The API key was rejected. Check the key on this profile.", body)
	case http.StatusForbidden:
		return appendGatewayMessage("403 Quota or permission. The gateway refused this request. Check this key's access.", body)
	case http.StatusNotFound:
		return appendGatewayMessage("404 Model or path. That model or path was not found. Run `deeprouter models` and switch to an id the gateway lists.", body)
	case http.StatusServiceUnavailable:
		return appendGatewayMessage("503 No channel. No channel is available for this model right now. Try another model.", body)
	case http.StatusTooManyRequests:
		return appendGatewayMessage("429 Rate limit. The gateway asked us to slow down. Wait a moment and run the test again.", body)
	default:
		if status >= 300 && status < 400 {
			return "The gateway redirected the request. Check the base URL, including http versus https and a trailing /v1."
		}
		if status >= 200 && status < 300 {
			return ""
		}
		if status >= 400 && status < 500 {
			msg := gatewayErrorMessage(body)
			if msg != "" {
				return fmt.Sprintf("HTTP %d. %s", status, msg)
			}
			if strings.TrimSpace(body) != "" {
				return fmt.Sprintf("HTTP %d. The gateway returned an error.", status)
			}
			return fmt.Sprintf("HTTP %d.", status)
		}
		if strings.TrimSpace(body) != "" {
			return fmt.Sprintf("HTTP %d. The gateway returned an error.", status)
		}
		return fmt.Sprintf("HTTP %d.", status)
	}
}

func appendGatewayMessage(hint, body string) string {
	msg := gatewayErrorMessage(body)
	if msg == "" || strings.Contains(hint, msg) {
		return hint
	}
	return hint + " " + msg
}

// gatewayErrorMessage returns a short, secret-free error from a response body.
func gatewayErrorMessage(body string) string {
	body = strings.TrimSpace(body)
	if body == "" {
		return ""
	}
	if msg := jsonErrorMessage(body); msg != "" {
		return oneLine(scrubSecrets(msg), 160)
	}
	return oneLine(scrubSecrets(body), 160)
}

func jsonErrorMessage(body string) string {
	var payload any
	if err := json.Unmarshal([]byte(body), &payload); err != nil {
		return ""
	}
	return digErrorMessage(payload, 0)
}

func digErrorMessage(v any, depth int) string {
	if depth > 4 || v == nil {
		return ""
	}
	switch t := v.(type) {
	case string:
		return strings.TrimSpace(t)
	case map[string]any:
		for _, key := range []string{"message", "msg", "detail", "error_description"} {
			if s, ok := t[key].(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
		}
		if errVal, ok := t["error"]; ok {
			if s, ok := errVal.(string); ok && strings.TrimSpace(s) != "" {
				return strings.TrimSpace(s)
			}
			if nested := digErrorMessage(errVal, depth+1); nested != "" {
				return nested
			}
		}
	}
	return ""
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
	models, err := ParseModels(body)
	if err != nil {
		return nil, err
	}
	ids := ModelIDs(models)
	if ids == nil {
		ids = []string{}
	}
	return ids, nil
}

func redact(body, secret string) string {
	if secret != "" && len(secret) >= 4 {
		body = strings.ReplaceAll(body, secret, "****")
	}
	return body
}

var secretRE = regexp.MustCompile(`(?i)(?:sk-[A-Za-z0-9_\-]{6,}|Bearer\s+\S+)`)

func scrubSecrets(s string) string {
	return secretRE.ReplaceAllString(s, "****")
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
