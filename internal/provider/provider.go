// Package provider lists the gateway presets deeprouter can configure.
package provider

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/gateway"
)

const (
	// CustomID is the config section written when the wizard asks for a base URL.
	CustomID = "custom"
	// EnvProvider selects a preset for setup and for the installers.
	// The API key variable stays DEEPROUTER_API_KEY.
	EnvProvider = "DEEPROUTER_PROVIDER"
)

// Preset is one hosted gateway or an official vendor API.
// Official presets do not use DeepRouter's model list. Their base URLs
// are the vendor origins the clients append to.
type Preset struct {
	ID       string
	Name     string
	BaseURL  string
	Official bool
}

func presets() []Preset {
	return []Preset{
		{ID: "deeprouter", Name: "DeepRouter", BaseURL: gateway.DefaultBaseURL},
		{ID: "wolfai", Name: "WolfAI", BaseURL: "https://wolfai.top"},
		{ID: "linkmesh", Name: "LinkMesh", BaseURL: "https://www.linkmesh.org"},
		// openai-api, not openai: Codex already uses [model_providers.openai]
		// for its built-in provider. This tool leaves that table alone.
		{ID: "openai-api", Name: "OpenAI", BaseURL: "https://api.openai.com", Official: true},
		{ID: "anthropic", Name: "Anthropic/Claude", BaseURL: "https://api.anthropic.com", Official: true},
		{ID: "gemini", Name: "Google Gemini", BaseURL: "https://generativelanguage.googleapis.com", Official: true},
		{ID: "xai", Name: "xAI Grok", BaseURL: "https://api.x.ai", Official: true},
	}
}

// Default is DeepRouter.
func Default() Preset {
	return presets()[0]
}

// All returns the presets in picker order.
func All() []Preset {
	return presets()
}

// ByID finds a preset. Matching ignores case and surrounding space.
// openai is the flag name for the openai-api section.
func ByID(id string) (Preset, bool) {
	id = aliasID(strings.ToLower(strings.TrimSpace(id)))
	for _, p := range presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

func aliasID(id string) string {
	if id == "openai" {
		return "openai-api"
	}
	return id
}

// ManagedIDs are the config section names this tool creates and later removes.
func ManagedIDs() []string {
	ids := make([]string, 0, len(presets())+1)
	for _, p := range presets() {
		ids = append(ids, p.ID)
	}
	ids = append(ids, CustomID)
	return ids
}

// IsOfficial reports whether id is a vendor API preset.
// Those presets skip the DeepRouter model list. The client keeps its default
// model unless the user passes a model flag.
func IsOfficial(id string) bool {
	p, ok := ByID(id)
	return ok && p.Official
}

// FlagList is the --provider value list, with "or" before the last id.
func FlagList() string {
	all := presets()
	ids := make([]string, len(all))
	for i, p := range all {
		ids[i] = p.FlagName()
	}
	if len(ids) < 2 {
		return strings.Join(ids, ", ")
	}
	return strings.Join(ids[:len(ids)-1], ", ") + ", or " + ids[len(ids)-1]
}

// FlagName is the --provider value. OpenAI's config section is openai-api,
// so the flag stays openai and does not take over Codex's built-in table.
func (p Preset) FlagName() string {
	if p.ID == "openai-api" {
		return "openai"
	}
	return p.ID
}

// IsManaged reports whether id is a section this tool owns.
func IsManaged(id string) bool {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, managed := range ManagedIDs() {
		if managed == id {
			return true
		}
	}
	return false
}

// Parse accepts the --provider flag values and DEEPROUTER_PROVIDER.
// custom is not a flag value; the wizard asks for that URL itself.
func Parse(flag string) (Preset, error) {
	p, ok := ByID(flag)
	if !ok {
		return Preset{}, fmt.Errorf("unknown provider %q (use %s)", strings.TrimSpace(flag), FlagList())
	}
	return p, nil
}

// NormalizeID maps an empty or unknown stored id to deeprouter.
// custom is kept so a wizard base URL stays on its own config section.
func NormalizeID(id string) string {
	id = strings.ToLower(strings.TrimSpace(id))
	if id == CustomID {
		return CustomID
	}
	if p, ok := ByID(id); ok {
		return p.ID
	}
	return Default().ID
}

// DisplayName is the label stored next to a provider id.
func DisplayName(id string) string {
	id = NormalizeID(id)
	if p, ok := ByID(id); ok {
		return p.Name
	}
	if id == CustomID {
		return "Custom"
	}
	return Default().Name
}

// Program is one app's route on a preset.
// Exposed is what a call with no API key showed: 401 means the route exists,
// 404 means this gateway does not serve it. LinkMesh is sub2api, not new-api.
type Program struct {
	App     apps.ID
	Exposed bool
	// Route is the path the app calls. It is named in the skip message.
	Route string
	// CodexWire is Codex wire_api. Empty for every other app.
	CodexWire string
}

// Programs is the per-app route table for a preset.
// custom has no table: the typed URL is not probed here.
// DeepRouter and WolfAI keep every app. LinkMesh's table matches routes that
// answered 401 without a key on https://www.linkmesh.org. POST
// /v1/models/{model}:generateContent answered 404, so Gemini uses /v1beta.
//
// Official presets expose a client only when that client's request matches
// the vendor API at the preset's base URL:
//
//   - OpenAI https://api.openai.com: Codex calls POST /v1/responses with
//     base_url https://api.openai.com/v1 and wire_api=responses. Current
//     Codex rejects wire_api=chat. Claude Code and Claude Desktop call
//     {origin}/v1/messages, which this host does not serve. Gemini CLI calls
//     {origin}/v1beta/models/{model}:generateContent, which it does not serve.
//     Grok Build writes POST /v1/chat/completions; that route exists, but the
//     client is xAI's and is offered on the xAI preset instead.
//   - Anthropic https://api.anthropic.com: Claude Code sets ANTHROPIC_BASE_URL
//     to the origin (no /v1) and POSTs /v1/messages. Claude Desktop's gateway
//     field is the same origin. Codex, Grok Build, and Gemini CLI do not
//     speak the Messages API.
//   - Google Gemini https://generativelanguage.googleapis.com: Gemini CLI
//     sets GOOGLE_GEMINI_BASE_URL to the origin and the SDK appends
//     /v1beta/models/{model}:generateContent with x-goog-api-key. The
//     OpenAI-compatible path is /v1beta/openai, which this writer does not
//     emit, so Codex and Grok Build are not offered. Claude is not offered.
//   - xAI https://api.x.ai: Codex uses POST /v1/responses (base_url
//     https://api.x.ai/v1, wire_api=responses). Grok Build uses POST
//     /v1/chat/completions at the same /v1 base. Claude and Gemini are not
//     offered.
func Programs(id string) []Program {
	switch NormalizeID(id) {
	case "linkmesh":
		return linkmeshPrograms()
	case "deeprouter", "wolfai":
		return newAPIPrograms()
	case "openai-api":
		return openaiPrograms()
	case "anthropic":
		return anthropicPrograms()
	case "gemini":
		return geminiPrograms()
	case "xai":
		return xaiPrograms()
	default:
		return nil
	}
}

func newAPIPrograms() []Program {
	return []Program{
		{App: apps.Codex, Exposed: true, Route: "POST /v1/responses", CodexWire: "responses"},
		{App: apps.ClaudeCode, Exposed: true, Route: "POST /v1/messages"},
		{App: apps.ClaudeDesktop, Exposed: true, Route: "POST /v1/messages"},
		{App: apps.Grok, Exposed: true, Route: "POST /v1/chat/completions"},
		{App: apps.Gemini, Exposed: true, Route: "POST /v1beta/models/{model}:generateContent"},
	}
}

func linkmeshPrograms() []Program {
	return []Program{
		{App: apps.Codex, Exposed: true, Route: "POST /v1/responses", CodexWire: "responses"},
		{App: apps.ClaudeCode, Exposed: true, Route: "POST /v1/messages"},
		{App: apps.ClaudeDesktop, Exposed: true, Route: "POST /v1/messages"},
		{App: apps.Grok, Exposed: true, Route: "POST /v1/chat/completions"},
		{App: apps.Gemini, Exposed: true, Route: "POST /v1beta/models/{model}:generateContent"},
	}
}

func openaiPrograms() []Program {
	return []Program{
		{App: apps.Codex, Exposed: true, Route: "POST /v1/responses", CodexWire: "responses"},
		{App: apps.ClaudeCode, Exposed: false, Route: "POST /v1/messages"},
		{App: apps.ClaudeDesktop, Exposed: false, Route: "POST /v1/messages"},
		{App: apps.Grok, Exposed: false, Route: "POST /v1/chat/completions"},
		{App: apps.Gemini, Exposed: false, Route: "POST /v1beta/models/{model}:generateContent"},
	}
}

func anthropicPrograms() []Program {
	return []Program{
		{App: apps.Codex, Exposed: false, Route: "POST /v1/responses"},
		{App: apps.ClaudeCode, Exposed: true, Route: "POST /v1/messages"},
		{App: apps.ClaudeDesktop, Exposed: true, Route: "POST /v1/messages"},
		{App: apps.Grok, Exposed: false, Route: "POST /v1/chat/completions"},
		{App: apps.Gemini, Exposed: false, Route: "POST /v1beta/models/{model}:generateContent"},
	}
}

func geminiPrograms() []Program {
	return []Program{
		{App: apps.Codex, Exposed: false, Route: "POST /v1/responses"},
		{App: apps.ClaudeCode, Exposed: false, Route: "POST /v1/messages"},
		{App: apps.ClaudeDesktop, Exposed: false, Route: "POST /v1/messages"},
		{App: apps.Grok, Exposed: false, Route: "POST /v1/chat/completions"},
		{App: apps.Gemini, Exposed: true, Route: "POST /v1beta/models/{model}:generateContent"},
	}
}

func xaiPrograms() []Program {
	return []Program{
		{App: apps.Codex, Exposed: true, Route: "POST /v1/responses", CodexWire: "responses"},
		{App: apps.ClaudeCode, Exposed: false, Route: "POST /v1/messages"},
		{App: apps.ClaudeDesktop, Exposed: false, Route: "POST /v1/messages"},
		{App: apps.Grok, Exposed: true, Route: "POST /v1/chat/completions"},
		{App: apps.Gemini, Exposed: false, Route: "POST /v1beta/models/{model}:generateContent"},
	}
}

// ForClients returns the presets to show after the user picks programs.
// DeepRouter, WolfAI, and LinkMesh support every client, so they are always
// included. An official preset is included only when every selected program
// is exposed on it. An empty selection returns every preset. Menu order is
// the preset order; the wizard adds the custom gateway after this list.
func ForClients(ids []apps.ID) []Preset {
	if len(ids) == 0 {
		return presets()
	}
	var out []Preset
	for _, p := range presets() {
		if !p.Official || supportsAll(p.ID, ids) {
			out = append(out, p)
		}
	}
	return out
}

func supportsAll(providerID string, ids []apps.ID) bool {
	offered := map[apps.ID]bool{}
	for _, id := range Offered(providerID) {
		offered[id] = true
	}
	for _, id := range ids {
		if !offered[id] {
			return false
		}
	}
	return true
}

// Offered is the apps a preset can configure, in catalog order.
// custom has no route table, so every writable app is offered.
func Offered(id string) []apps.ID {
	table := Programs(id)
	if table == nil {
		var ids []apps.ID
		for _, info := range apps.Catalog() {
			if info.Writable {
				ids = append(ids, info.ID)
			}
		}
		return ids
	}
	exposed := map[apps.ID]bool{}
	for _, prog := range table {
		if prog.Exposed {
			exposed[prog.App] = true
		}
	}
	var ids []apps.ID
	for _, info := range apps.Catalog() {
		if exposed[info.ID] {
			ids = append(ids, info.ID)
		}
	}
	return ids
}

// DefaultPrograms is what Enter and setup without --apps configure.
// It is the first offered app: Codex on the gateways, OpenAI, and xAI;
// Claude Code on Anthropic; Gemini CLI on Google Gemini.
func DefaultPrograms(id string) []apps.ID {
	offered := Offered(id)
	if len(offered) == 0 {
		return []apps.ID{apps.Codex}
	}
	return []apps.ID{offered[0]}
}

// ProgramFor returns the route row for one app.
// ok is false for custom and for any id that does not restrict apps.
func ProgramFor(id string, app apps.ID) (Program, bool) {
	for _, prog := range Programs(id) {
		if prog.App == app {
			return prog, true
		}
	}
	return Program{}, false
}

// CodexWire is the wire_api a preset writes for Codex when --codex-wire-api
// is omitted. force is false when the preset does not override a saved value.
// LinkMesh, OpenAI, and xAI force responses: that is the route Codex calls,
// and current Codex rejects wire_api=chat.
func CodexWire(id string) (wire string, force bool) {
	switch NormalizeID(id) {
	case "linkmesh", "openai-api", "xai":
	default:
		return "", false
	}
	prog, ok := ProgramFor(id, apps.Codex)
	if !ok || !prog.Exposed || prog.CodexWire == "" {
		return "", false
	}
	return prog.CodexWire, true
}

// UsesNewAPI reports whether id is a new-api gateway.
// DeepRouter and WolfAI are. LinkMesh is an independent gateway and does
// not serve new-api dashboard routes such as /api/status. custom is not
// treated as new-api. This tool does not call those routes for any provider.
func UsesNewAPI(id string) bool {
	switch NormalizeID(id) {
	case "deeprouter", "wolfai":
		return true
	default:
		return false
	}
}

// TokenHost is the site where a token for id is created.
// A preset uses its official host even when the configured base URL differs.
// custom uses the host of baseURL, including a port.
func TokenHost(id, baseURL string) string {
	id = NormalizeID(id)
	if p, ok := ByID(id); ok {
		if host := hostOf(p.BaseURL); host != "" {
			return host
		}
	}
	return hostOf(baseURL)
}

// PromptName is the site name in the token question.
// The Chinese name for a custom URL is 自定义网关.
func PromptName(id string, zh bool) string {
	id = NormalizeID(id)
	if p, ok := ByID(id); ok {
		return p.Name
	}
	if zh {
		return "自定义网关"
	}
	return "Custom"
}

// SameKeySource reports whether a key saved for savedID at savedBase belongs
// to the provider being configured. Presets match by id, so a preset keeps
// its key when only the base URL is overridden. custom also requires the
// same host. An empty saved id is deeprouter.
func SameKeySource(savedID, savedBase, id, baseURL string) bool {
	savedID = NormalizeID(savedID)
	id = NormalizeID(id)
	if savedID != id {
		return false
	}
	if id != CustomID {
		return true
	}
	savedHost := TokenHost(CustomID, savedBase)
	host := TokenHost(CustomID, baseURL)
	return host != "" && strings.EqualFold(savedHost, host)
}

func hostOf(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	if !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// KnownLabel reports whether label is empty or one of the preset display names.
// A name the user chose, such as Azure, is not known.
func KnownLabel(label string) bool {
	label = strings.TrimSpace(label)
	if label == "" || strings.EqualFold(label, "Custom") {
		return true
	}
	for _, p := range presets() {
		if strings.EqualFold(label, p.Name) || strings.EqualFold(label, p.ID) {
			return true
		}
	}
	return false
}
