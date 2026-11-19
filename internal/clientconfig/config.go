package clientconfig

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/jsonedit"
)

const (
	PiModels   = "models.json"
	PiAuth     = "auth.json"
	PiSettings = "settings.json"
)

func OpenCodePath(home, xdg, override, explicit string) (string, error) {
	if explicit != "" {
		absolute, err := filepath.Abs(explicit)
		if err != nil {
			return "", err
		}
		if err := CheckOpenCodeUserDir(filepath.Dir(absolute)); err != nil {
			return "", err
		}
		return absolute, nil
	}
	dir, err := OpenCodeDir(home, xdg, override)
	if err != nil {
		return "", err
	}
	for _, name := range []string{"opencode.jsonc", "opencode.json"} {
		candidate := filepath.Join(dir, name)
		if _, err := os.Lstat(candidate); err == nil {
			return filepath.Abs(candidate)
		} else if !os.IsNotExist(err) {
			return "", fmt.Errorf("cannot inspect OpenCode user configuration")
		}
	}
	return filepath.Abs(filepath.Join(dir, "opencode.json"))
}

func OpenCodeDir(home, xdg, override string) (string, error) {
	dir := override
	if dir == "" {
		if xdg == "" {
			xdg = filepath.Join(home, ".config")
		}
		dir = filepath.Join(xdg, "opencode")
	}
	absolute, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	if err := CheckOpenCodeUserDir(absolute); err != nil {
		return "", err
	}
	return absolute, nil
}

func CheckOpenCodeUserDir(dir string) error {
	managed := []string{"/etc/opencode", "/Library/Application Support/opencode"}
	if filepath.VolumeName(dir) != "" {
		managed = nil
		if data := os.Getenv("ProgramData"); data != "" && filepath.IsAbs(data) {
			managed = append(managed, filepath.Join(data, "opencode"))
		}
	}
	resolved := resolveExistingParent(dir)
	for _, path := range managed {
		if rel, err := filepath.Rel(resolveExistingParent(path), resolved); err == nil && filepath.IsLocal(rel) {
			return fmt.Errorf("refusing to write in a machine-wide OpenCode configuration directory")
		}
	}
	return nil
}

func resolveExistingParent(path string) string {
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		return resolved
	}
	parent := filepath.Dir(path)
	if parent == path {
		return path
	}
	return filepath.Join(resolveExistingParent(parent), filepath.Base(path))
}

func PiDir(home, override string) (string, error) {
	if override == "" {
		override = filepath.Join(home, ".pi", "agent")
	} else if override == "~" {
		override = home
	} else if strings.HasPrefix(override, "~/") || (os.PathSeparator == '\\' && strings.HasPrefix(override, `~\`)) {
		override = filepath.Join(home, override[2:])
	}
	return filepath.Abs(override)
}

type Connection struct {
	ID, Name, BaseURL, Key, Model, Protocol string
}

func OpenCode(path string, doc []byte, conn Connection) ([]byte, error) {
	root, err := object(doc)
	if err != nil || strings.EqualFold(filepath.Ext(path), ".jsonc") {
		return nil, fmt.Errorf("OpenCode requires a strict JSON config for automatic edits (JSONC is left untouched)")
	}
	if strings.Contains(conn.Model, "#") {
		return nil, fmt.Errorf("OpenCode model IDs cannot contain #")
	}
	legacy, err := object(root["provider"])
	if err != nil {
		return nil, fmt.Errorf("invalid OpenCode provider configuration")
	}
	native, err := object(root["providers"])
	if err != nil {
		return nil, fmt.Errorf("invalid OpenCode providers configuration")
	}
	protocol, err := ResolveProtocol(conn.ID, conn.Protocol)
	if err != nil {
		return nil, err
	}
	base, err := connectionBase(conn.BaseURL, protocol, false)
	if err != nil {
		return nil, err
	}
	nativeAdapter, legacyAdapter := "openai-compatible", "openai-compatible"
	switch protocol {
	case gateway.OpenAIResponses:
		nativeAdapter, legacyAdapter = "openai-compatible/responses", "openai"
	case gateway.AnthropicMessages:
		nativeAdapter, legacyAdapter = "anthropic-compatible", "anthropic"
	case gateway.GoogleGenerativeAI:
		nativeAdapter, legacyAdapter = "google", "google"
	}
	section, pkg, settings, adapter := "providers", "package", "settings", "@opencode/ai/providers/"+nativeAdapter
	if (legacy[conn.ID] != nil || root["providers"] == nil && root["provider"] != nil) && native[conn.ID] == nil {
		section, pkg, settings, adapter = "provider", "npm", "options", "@ai-sdk/"+legacyAdapter
	}
	model := ModelID(protocol, conn.Model)
	return edit(doc,
		field(adapter, section, conn.ID, pkg),
		field(conn.Name, section, conn.ID, "name"),
		field(base, section, conn.ID, settings, "baseURL"),
		field(conn.Key, section, conn.ID, settings, "apiKey"),
		field(model, section, conn.ID, "models", model, "name"),
		field(conn.ID+"/"+model, "model"),
	)
}

func Pi(doc []byte, name string, conn Connection) ([]byte, error) {
	root, err := object(doc)
	if err != nil {
		return nil, fmt.Errorf("invalid Pi %s", name)
	}
	protocol, err := ResolveProtocol(conn.ID, conn.Protocol)
	if err != nil {
		return nil, err
	}
	model := ModelID(protocol, conn.Model)
	switch name {
	case PiAuth:
		key := strings.ReplaceAll(conn.Key, "$", "$$")
		if strings.HasPrefix(key, "!") {
			key = "$" + key
		}
		credential, _ := json.Marshal(struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}{Type: "api_key", Key: key})
		return edit(doc, jsonedit.Nested{Path: []string{conn.ID}, Raw: credential})
	case PiSettings:
		return edit(doc, field(conn.ID, "defaultProvider"), field(model, "defaultModel"))
	case PiModels:
		providers, err := object(root["providers"])
		if err != nil {
			return nil, fmt.Errorf("invalid Pi providers configuration")
		}
		current, err := object(providers[conn.ID])
		if err != nil {
			return nil, fmt.Errorf("invalid Pi provider configuration")
		}
		base, err := connectionBase(conn.BaseURL, protocol, true)
		if err != nil {
			return nil, err
		}
		updates := []jsonedit.Nested{
			field(base, "providers", conn.ID, "baseUrl"),
			field(protocol, "providers", conn.ID, "api"),
		}
		models, err := piModels(current["models"], model, protocol, base)
		if err != nil {
			return nil, err
		}
		if models != nil {
			updates = append(updates, jsonedit.Nested{Path: []string{"providers", conn.ID, "models"}, Raw: models})
		}
		return edit(doc, updates...)
	default:
		return nil, fmt.Errorf("unknown Pi configuration file")
	}
}

func piModels(raw json.RawMessage, model, protocol, base string) (json.RawMessage, error) {
	var models []json.RawMessage
	if raw != nil && (json.Unmarshal(raw, &models) != nil || models == nil) {
		return nil, fmt.Errorf("Pi provider models must be an array")
	}
	seen := map[string]bool{}
	updated := false
	for index, entry := range models {
		var existing struct{ ID, API, BaseURL string }
		if json.Unmarshal(entry, &existing) != nil || existing.ID == "" || seen[existing.ID] {
			return nil, fmt.Errorf("Pi provider models must have unique IDs")
		}
		seen[existing.ID] = true
		if existing.ID == model {
			var fields []jsonedit.Nested
			if existing.API != "" && existing.API != protocol {
				fields = append(fields, field(protocol, "api"))
			}
			if existing.BaseURL != "" && existing.BaseURL != base {
				fields = append(fields, field(base, "baseUrl"))
			}
			if len(fields) > 0 {
				next, err := edit(entry, fields...)
				if err != nil {
					return nil, err
				}
				models[index] = next
				updated = true
			}
		}
	}
	if seen[model] {
		if updated {
			return json.Marshal(models)
		}
		return nil, nil
	}
	entry, _ := json.Marshal(struct {
		ID string `json:"id"`
	}{ID: model})
	return json.Marshal(append(models, entry))
}

func object(raw []byte) (map[string]json.RawMessage, error) {
	raw = bytes.TrimSpace(bytes.TrimPrefix(raw, []byte{0xef, 0xbb, 0xbf}))
	if len(raw) == 0 {
		return nil, nil
	}
	var result map[string]json.RawMessage
	if json.Unmarshal(raw, &result) != nil || result == nil {
		return nil, fmt.Errorf("expected a JSON object")
	}
	return result, nil
}

func field(value string, path ...string) jsonedit.Nested {
	return jsonedit.Nested{Path: path, Value: value}
}

func edit(doc []byte, updates ...jsonedit.Nested) ([]byte, error) {
	next, err := jsonedit.SetNestedStrict(doc, updates)
	if err != nil {
		return nil, fmt.Errorf("cannot safely merge client JSON configuration")
	}
	return next, nil
}
