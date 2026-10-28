// Package gemini edits Gemini CLI's user settings.json.
//
// The API key and gateway host are environment variables (GEMINI_API_KEY and
// GOOGLE_GEMINI_BASE_URL). This file only selects API-key auth and, when asked,
// the model, so Gemini CLI does not prompt for a Google login.
package gemini

import (
	"path/filepath"
	"strings"

	"github.com/zfg5118/deeprouter/internal/jsonedit"
)

// SelectedAuth is security.auth.selectedType for an API key.
const SelectedAuth = "gemini-api-key"

// Update is one settings edit. SetModel writes model.name. ClearModel removes
// model.name and leaves every other model field in place.
type Update struct {
	Model      string
	SetModel   bool
	ClearModel bool
}

// SettingsPath is ~/.gemini/settings.json, or $GEMINI_CLI_HOME/.gemini/settings.json.
func SettingsPath(home, cliHome string) string {
	parent := home
	if strings.TrimSpace(cliHome) != "" {
		parent = cliHome
	}
	return filepath.Join(parent, ".gemini", "settings.json")
}

// Apply merges auth and an optional model into a settings document.
// Other keys stay in their original order. Arrays stay as they were written.
// The result uses the file's indent: two spaces, four spaces, or a tab.
// An empty document becomes a new object.
func Apply(doc []byte, upd Update) ([]byte, error) {
	updates := []jsonedit.Nested{{
		Path:  []string{"security", "auth", "selectedType"},
		Value: SelectedAuth,
	}}
	switch {
	case upd.ClearModel:
		updates = append(updates, jsonedit.Nested{Path: []string{"model", "name"}, Delete: true})
	case upd.SetModel && upd.Model != "":
		updates = append(updates, jsonedit.Nested{
			Path:  []string{"model", "name"},
			Value: upd.Model,
		})
	}
	return jsonedit.SetNested(doc, updates)
}
