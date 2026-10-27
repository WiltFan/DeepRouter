// Package claudedesktop writes the local third-party configuration that
// Claude Desktop reads for Chat, Cowork, and the Code tab.
//
// The paths and keys come from Anthropic's Claude Desktop on 3P docs:
// https://claude.com/docs/third-party/claude-desktop/configuration-format
// https://claude.com/docs/third-party/claude-desktop/gateway
// https://claude.com/docs/third-party/claude-desktop/code
//
// A managed profile (MDM, Windows machine policy, or the root-owned Linux
// file) overrides this directory. This package does not write those.
package claudedesktop

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zfg5118/deeprouter/internal/provider"
)

const (
	// ProfileID is the configuration file this tool creates when the app
	// has no applied local configuration yet.
	ProfileID = "5f46c0de-0000-4000-8000-000000000046"
	// ProfileName is the label stored next to ProfileID.
	ProfileName = "DeepRouter"
	metaName    = "_meta.json"
)

// Dir is the local configLibrary for this OS.
// macOS: ~/Library/Application Support/Claude-3p/configLibrary
// Windows: %APPDATA%\Claude\configLibrary
// Linux: ~/.config/Claude-3p/configLibrary
//
// The Windows directory uses the same configLibrary layout as the macOS
// Claude-3p library (_meta.json plus one JSON file per configuration).
// %APPDATA% is the roaming profile, the Windows equivalent of Application Support.
func Dir(home string) string {
	return DirFor(home, runtime.GOOS, os.Getenv("APPDATA"))
}

// DirFor is Dir with an explicit OS and Windows roaming app-data directory.
// On Windows, appData is %APPDATA%. An empty value falls back to
// %USERPROFILE%\AppData\Roaming.
func DirFor(home, goos, appData string) string {
	var root string
	switch goos {
	case "darwin":
		root = filepath.Join(home, "Library", "Application Support", "Claude-3p")
	case "windows":
		base := appData
		if base == "" {
			base = filepath.Join(home, "AppData", "Roaming")
		}
		root = filepath.Join(base, "Claude")
	default:
		root = filepath.Join(home, ".config", "Claude-3p")
	}
	return filepath.Join(root, "configLibrary")
}

// Update is the gateway credential written into the applied configuration.
type Update struct {
	// BaseURL is the gateway origin without a trailing /v1. Claude Desktop's
	// in-app field uses that shape (https://llm-gateway.example.corp), and
	// Code sessions hand the value to Claude Code, which calls /v1/messages.
	BaseURL    string
	APIKey     string
	Model      string
	SetModel   bool
	ClearModel bool
	// Name is the label stored on the managed configuration entry.
	// Empty keeps ProfileName. A name the user chose is left in place.
	Name string
}

// Files are the two JSON documents in configLibrary.
type Files struct {
	ProfilePath string
	Profile     []byte
	MetaPath    string
	Meta        []byte
}

// Plan reads the library and returns the documents to write.
// An applied configuration is updated in place. When none is applied, Plan
// adds ProfileID and selects it. Other saved configurations are left listed.
func Plan(dir string, upd Update) (Files, error) {
	if dir == "" {
		return Files{}, fmt.Errorf("claude desktop config directory is empty")
	}
	if upd.BaseURL == "" || upd.APIKey == "" {
		return Files{}, fmt.Errorf("claude desktop gateway URL and API key are required")
	}
	metaPath := filepath.Join(dir, metaName)
	metaDoc, err := readObject(metaPath)
	if err != nil {
		return Files{}, err
	}
	id := appliedID(metaDoc)
	if id == "" {
		id = ProfileID
	}
	profilePath := filepath.Join(dir, id+".json")
	profile, err := readObject(profilePath)
	if err != nil {
		return Files{}, err
	}
	profile["inferenceProvider"] = "gateway"
	profile["inferenceGatewayBaseUrl"] = upd.BaseURL
	profile["inferenceGatewayApiKey"] = upd.APIKey
	profile["inferenceCredentialKind"] = "static"
	profile["inferenceGatewayAuthScheme"] = "bearer"
	setModel(profile, upd)

	if id == ProfileID {
		name := upd.Name
		if strings.TrimSpace(name) == "" {
			name = ProfileName
		}
		ensureEntry(metaDoc, ProfileID, name)
		metaDoc["appliedId"] = ProfileID
	}
	profileBytes, err := marshal(profile)
	if err != nil {
		return Files{}, err
	}
	metaBytes, err := marshal(metaDoc)
	if err != nil {
		return Files{}, err
	}
	return Files{
		ProfilePath: profilePath,
		Profile:     profileBytes,
		MetaPath:    metaPath,
		Meta:        metaBytes,
	}, nil
}

// ReadAPIKey returns the gateway API key of the applied configuration.
func ReadAPIKey(dir string) string {
	if dir == "" {
		return ""
	}
	meta, err := readObject(filepath.Join(dir, metaName))
	if err != nil {
		return ""
	}
	id := appliedID(meta)
	if id == "" {
		id = ProfileID
	}
	doc, err := readObject(filepath.Join(dir, id+".json"))
	if err != nil {
		return ""
	}
	key, _ := doc["inferenceGatewayApiKey"].(string)
	return strings.TrimSpace(key)
}

// Applied is the configuration file the app would load, and whether that
// file points inferenceGatewayBaseUrl at baseURL. A missing library is not
// wired. A broken JSON file is reported as not wired.
func Applied(dir, baseURL string) (path string, wired bool) {
	if dir == "" {
		return "", false
	}
	id := ""
	meta, err := readObject(filepath.Join(dir, metaName))
	if err == nil {
		id = appliedID(meta)
	}
	if id == "" {
		id = ProfileID
	}
	path = filepath.Join(dir, id+".json")
	doc, err := readObject(path)
	if err != nil {
		return path, false
	}
	url, _ := doc["inferenceGatewayBaseUrl"].(string)
	provider, _ := doc["inferenceProvider"].(string)
	want := strings.TrimRight(strings.TrimSpace(baseURL), "/")
	got := strings.TrimRight(strings.TrimSpace(url), "/")
	wired = provider == "gateway" && want != "" && got == want
	return path, wired
}

func setModel(profile map[string]any, upd Update) {
	if upd.ClearModel {
		delete(profile, "inferenceModels")
		profile["modelDiscoveryEnabled"] = true
		return
	}
	if upd.SetModel && upd.Model != "" {
		if list, ok := profile["inferenceModels"].([]any); ok && len(list) > 1 {
			return
		}
		profile["inferenceModels"] = []any{map[string]any{"name": upd.Model}}
		return
	}
	if _, ok := profile["inferenceModels"]; ok {
		return
	}
	if _, ok := profile["modelDiscoveryEnabled"]; ok {
		return
	}
	profile["modelDiscoveryEnabled"] = true
}

func appliedID(meta map[string]any) string {
	id, _ := meta["appliedId"].(string)
	id = strings.TrimSpace(id)
	if !safeID(id) {
		return ""
	}
	return id
}

func safeID(id string) bool {
	if id == "" || id == "." || id == ".." || id == "_meta" {
		return false
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '_', r == '-':
		default:
			return false
		}
	}
	return true
}

func ensureEntry(meta map[string]any, id, name string) {
	var entries []any
	if cur, ok := meta["entries"].([]any); ok {
		entries = cur
	}
	for _, item := range entries {
		obj, ok := item.(map[string]any)
		if !ok {
			continue
		}
		if obj["id"] == id {
			current, _ := obj["name"].(string)
			if strings.TrimSpace(current) == "" || provider.KnownLabel(current) {
				obj["name"] = name
			}
			meta["entries"] = entries
			return
		}
	}
	entries = append(entries, map[string]any{"id": id, "name": name})
	meta["entries"] = entries
}

func readObject(path string) (map[string]any, error) {
	b, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return map[string]any{}, nil
	}
	if err != nil {
		return nil, err
	}
	if len(strings.TrimSpace(string(b))) == 0 {
		return map[string]any{}, nil
	}
	var doc map[string]any
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, fmt.Errorf("%s is not a JSON object: %w", path, err)
	}
	if doc == nil {
		doc = map[string]any{}
	}
	return doc, nil
}

func marshal(doc map[string]any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(doc); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}
