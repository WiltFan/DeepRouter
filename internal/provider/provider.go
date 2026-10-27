// Package provider lists the gateway presets deeprouter can configure.
package provider

import (
	"fmt"
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
