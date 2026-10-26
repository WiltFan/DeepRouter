// Package gemini edits Gemini CLI's user settings.json.
//
// The API key and gateway host are environment variables (GEMINI_API_KEY and
// GOOGLE_GEMINI_BASE_URL). This file only selects API-key auth and, when asked,
// the model, so Gemini CLI does not prompt for a Google login.
package gemini

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"path/filepath"
	"strings"
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
// Other keys are kept. An empty document becomes a new object.
func Apply(doc []byte, upd Update) ([]byte, error) {
	root := map[string]any{}
	trimmed := bytes.TrimSpace(bytes.TrimPrefix(doc, []byte{0xEF, 0xBB, 0xBF}))
	if len(trimmed) > 0 {
		dec := json.NewDecoder(bytes.NewReader(trimmed))
		dec.UseNumber()
		if err := dec.Decode(&root); err != nil {
			return nil, fmt.Errorf("gemini settings are not valid JSON: %w", err)
		}
		var extra any
		if err := dec.Decode(&extra); err != io.EOF {
			return nil, fmt.Errorf("gemini settings have trailing data after the JSON value")
		}
	}
	if err := setString(root, []string{"security", "auth", "selectedType"}, SelectedAuth); err != nil {
		return nil, err
	}
	if upd.ClearModel {
		if model, ok := root["model"].(map[string]any); ok {
			delete(model, "name")
		}
	} else if upd.SetModel && upd.Model != "" {
		if err := setString(root, []string{"model", "name"}, upd.Model); err != nil {
			return nil, err
		}
	}
	raw, err := json.MarshalIndent(root, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(raw, '\n'), nil
}

func setString(root map[string]any, path []string, value string) error {
	cur := root
	for i, key := range path[:len(path)-1] {
		next, ok := cur[key]
		if !ok || next == nil {
			child := map[string]any{}
			cur[key] = child
			cur = child
			continue
		}
		child, ok := next.(map[string]any)
		if !ok {
			return fmt.Errorf("gemini settings field %s is not an object", strings.Join(path[:i+1], "."))
		}
		cur = child
	}
	cur[path[len(path)-1]] = value
	return nil
}
