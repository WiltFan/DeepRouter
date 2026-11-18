package clientconfig

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProviderProtocolSchemas(t *testing.T) {
	for preset, base := range map[string]string{
		"deeprouter": "https://deeprouter.top",
		"wolfai":     "https://wolfai.top",
		"linkmesh":   "https://www.linkmesh.org",
		"custom":     "https://example.invalid/relay",
	} {
		for _, protocol := range []struct{ api, native, legacy, openSuffix, piSuffix string }{
			{"openai-completions", "openai-compatible", "openai-compatible", "/v1", "/v1"},
			{"openai-responses", "openai-compatible/responses", "openai", "/v1", "/v1"},
			{"anthropic-messages", "anthropic-compatible", "anthropic", "/v1", ""},
			{"google-generative-ai", "google", "google", "/v1beta", "/v1beta"},
		} {
			t.Run(preset+"/"+protocol.api, func(t *testing.T) {
				conn := Connection{ID: preset, BaseURL: base + "/v1/", Model: "vendor/model", Key: "test-key", Protocol: protocol.api}
				model := conn.Model
				if protocol.api == "google-generative-ai" {
					conn.BaseURL, conn.Model, model = base+"/v1beta/", "models/gemini-test", "gemini-test"
				}
				for _, schema := range []struct {
					doc, section, key, data, adapter string
				}{
					{"{}", "providers", "package", "settings", "@opencode/ai/providers/" + protocol.native},
					{`{"provider":{"` + preset + `":{}}}`, "provider", "npm", "options", "@ai-sdk/" + protocol.legacy},
				} {
					out, err := OpenCode("opencode.json", []byte(schema.doc), conn)
					if err != nil {
						t.Fatal(err)
					}
					var root map[string]json.RawMessage
					var providers map[string]map[string]json.RawMessage
					var config map[string]string
					var models map[string]map[string]string
					var selected, adapter string
					if json.Unmarshal(out, &root) != nil || json.Unmarshal(root[schema.section], &providers) != nil || len(providers) != 1 ||
						json.Unmarshal(providers[preset][schema.key], &adapter) != nil || adapter != schema.adapter ||
						json.Unmarshal(providers[preset][schema.data], &config) != nil || config["baseURL"] != base+protocol.openSuffix || config["apiKey"] != conn.Key ||
						json.Unmarshal(providers[preset]["models"], &models) != nil || models[model]["name"] != model ||
						json.Unmarshal(root["model"], &selected) != nil || selected != preset+"/"+model {
						t.Fatal("wrong OpenCode schema fields")
					}
					repeat, err := OpenCode("opencode.json", out, conn)
					if err != nil || !bytes.Equal(out, repeat) {
						t.Fatal("OpenCode repeat setup changed config")
					}
				}
				out, err := Pi(nil, PiModels, conn)
				var doc struct {
					Providers map[string]struct {
						API, BaseURL string
						Models       []struct{ ID string }
					}
				}
				if err != nil || json.Unmarshal(out, &doc) != nil || len(doc.Providers) != 1 {
					t.Fatalf("Pi provider schema: %v", err)
				}
				pi := doc.Providers[preset]
				if pi.API != protocol.api || pi.BaseURL != base+protocol.piSuffix || len(pi.Models) != 1 || pi.Models[0].ID != model || bytes.Contains(out, []byte(conn.Key)) {
					t.Fatal("wrong Pi schema fields")
				}
				repeat, err := Pi(out, PiModels, conn)
				if err != nil || !bytes.Equal(out, repeat) {
					t.Fatal("Pi repeat setup changed config")
				}
			})
		}
	}
}

func TestPiUpdatesSelectedModelOverrides(t *testing.T) {
	conn := Connection{ID: "custom", BaseURL: "https://example.invalid/v1", Model: "vendor/model", Protocol: "anthropic-messages"}
	doc := []byte(`{"providers":{"custom":{"models":[{"id":"vendor/model","api":"openai-responses","baseUrl":"https://old.invalid/v1","name":"Mine"},{"id":"other","api":"openai-responses"}]}}}`)
	out, err := Pi(doc, PiModels, conn)
	if err != nil || !bytes.Contains(out, []byte(`{"id":"vendor/model","api":"anthropic-messages","baseUrl":"https://example.invalid","name":"Mine"}`)) ||
		!bytes.Contains(out, []byte(`{"id":"other","api":"openai-responses"}`)) {
		t.Fatalf("Pi model overrides were not merged: %v", err)
	}
}

func TestUserPaths(t *testing.T) {
	home := t.TempDir()
	xdg := filepath.Join(home, "xdg")
	override := filepath.Join(home, "override")
	for _, example := range []struct{ xdg, override, explicit, want string }{
		{want: filepath.Join(home, ".config", "opencode", "opencode.json")},
		{xdg: xdg, want: filepath.Join(xdg, "opencode", "opencode.json")},
		{xdg: xdg, override: override, want: filepath.Join(override, "opencode.json")},
		{xdg: xdg, override: override, explicit: filepath.Join(home, "chosen.json"), want: filepath.Join(home, "chosen.json")},
	} {
		got, err := OpenCodePath(home, example.xdg, example.override, example.explicit)
		if err != nil || got != example.want {
			t.Fatalf("OpenCodePath = %q, %v, want %q", got, err, example.want)
		}
	}
	if err := os.Mkdir(override, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"opencode.json", "opencode.jsonc"} {
		if err := os.WriteFile(filepath.Join(override, name), []byte("{}"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if got, err := OpenCodePath(home, xdg, override, ""); err != nil || got != filepath.Join(override, "opencode.jsonc") {
		t.Fatalf("JSONC precedence: %q, %v", got, err)
	}
	if got, err := PiDir(home, ""); err != nil || got != filepath.Join(home, ".pi", "agent") {
		t.Fatalf("Pi default: %q, %v", got, err)
	}
	if got, err := PiDir(home, "~/override"); err != nil || got != override {
		t.Fatalf("Pi override: %q, %v", got, err)
	}
}

func TestOpenCodeNativeMerge(t *testing.T) {
	doc := []byte(`{"theme":"dark","provider":{"custom":{"npm":"legacy"}},"providers":{"custom":{"settings":{"extra":true},"models":{"old":{"name":"Old"}}}},"model":"other/default"}`)
	conn := Connection{ID: "custom", Name: "Custom", BaseURL: "https://example.invalid/v1", Key: "test-key", Model: "vendor/model"}
	next, err := OpenCode("opencode.json", doc, conn)
	if err != nil {
		t.Fatal(err)
	}
	last := -1
	for _, want := range []string{`"theme": "dark"`, `"npm": "legacy"`, `"extra": true`, `"baseURL": "https://example.invalid/v1"`, `"old":`, `"vendor/model":`, `"package": "@opencode/ai/providers/openai-compatible"`, `"model": "custom/vendor/model"`} {
		index := strings.Index(string(next), want)
		if index <= last {
			t.Fatalf("missing or out of order: %s", want)
		}
		last = index
	}
	repeated, err := OpenCode("opencode.json", next, conn)
	if err != nil || string(repeated) != string(next) {
		t.Fatalf("repeat changed config: %v", err)
	}
	if _, err := OpenCode("opencode.json", []byte("{ // comment\n}"), conn); err == nil || !strings.Contains(err.Error(), "JSONC") {
		t.Fatalf("JSONC refusal: %v", err)
	}
}

func TestPiModelsAndCredentialMerge(t *testing.T) {
	conn := Connection{ID: "custom", BaseURL: "https://example.invalid/v1", Key: "!test$key", Model: "vendor/model"}
	doc := []byte(`{"providers":{"other":{"api":"anthropic-messages"},"custom":{"models":[{"id":"old","name":"Old"}]}}}`)
	models, err := Pi(doc, PiModels, conn)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"other":`, `"old","name":"Old"`, `"id":"vendor/model"`, `"api": "openai-completions"`, `"baseUrl": "https://example.invalid/v1"`} {
		if !strings.Contains(string(models), want) {
			t.Errorf("missing %s", want)
		}
	}
	repeated, err := Pi(models, PiModels, conn)
	if err != nil || string(repeated) != string(models) {
		t.Fatalf("repeat changed models: %v", err)
	}
	credential, err := Pi([]byte(`{"other":{"type":"api_key","key":"old"},"custom":{"type":"oauth","access":"old"}}`), PiAuth, conn)
	if err != nil || !strings.Contains(string(credential), `"key": "$!test$$key"`) || !strings.Contains(string(credential), `"other":`) || strings.Contains(string(credential), `"access"`) {
		t.Fatalf("credential merge failed: %v", err)
	}
}
