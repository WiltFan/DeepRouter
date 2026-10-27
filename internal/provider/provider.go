// Package provider lists the gateway presets deeprouter can configure.
package provider

import (
	"fmt"
	"net/url"
	"strings"

	"github.com/zfg5118/deeprouter/internal/gateway"
)

const (
	// CustomID is the config section written when the wizard asks for a base URL.
	CustomID = "custom"
	// EnvProvider selects a preset for setup and for the installers.
	// The API key variable stays DEEPROUTER_API_KEY.
	EnvProvider = "DEEPROUTER_PROVIDER"
)

// Preset is one hosted gateway.
type Preset struct {
	ID      string
	Name    string
	BaseURL string
}

func presets() []Preset {
	return []Preset{
		{ID: "deeprouter", Name: "DeepRouter", BaseURL: gateway.DefaultBaseURL},
		{ID: "wolfai", Name: "WolfAI", BaseURL: "https://wolfai.top"},
		{ID: "linkmesh", Name: "LinkMesh", BaseURL: "https://www.linkmesh.org"},
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
func ByID(id string) (Preset, bool) {
	id = strings.ToLower(strings.TrimSpace(id))
	for _, p := range presets() {
		if p.ID == id {
			return p, true
		}
	}
	return Preset{}, false
}

// ManagedIDs are the config section names this tool creates and later removes.
func ManagedIDs() []string {
	ids := make([]string, 0, 4)
	for _, p := range presets() {
		ids = append(ids, p.ID)
	}
	ids = append(ids, CustomID)
	return ids
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

// Parse accepts the --provider flag values: deeprouter, wolfai, or linkmesh.
func Parse(flag string) (Preset, error) {
	p, ok := ByID(flag)
	if !ok {
		return Preset{}, fmt.Errorf("unknown provider %q (use deeprouter, wolfai, or linkmesh)", strings.TrimSpace(flag))
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
