package gemini

import (
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
)

func TestApplyMergesAuthAndModel(t *testing.T) {
	in := []byte("{\n  \"ui\": {\"theme\": \"dark\"},\n  \"mcpServers\": {\"local\": {\"command\": \"echo\"}},\n  \"model\": {\"name\": \"old-model\", \"maxSessionTurns\": 10}\n}\n")
	upd := Update{Model: "gemini-2.5-pro", SetModel: true}
	first, err := Apply(in, upd)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Apply(first, upd)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("not idempotent:\n%s\n%s", first, second)
	}
	var doc map[string]any
	if err := json.Unmarshal(first, &doc); err != nil {
		t.Fatal(err)
	}
	ui := doc["ui"].(map[string]any)
	if ui["theme"] != "dark" {
		t.Fatalf("ui %v", doc["ui"])
	}
	mcp := doc["mcpServers"].(map[string]any)
	local := mcp["local"].(map[string]any)
	if local["command"] != "echo" {
		t.Fatalf("mcp %v", doc["mcpServers"])
	}
	model := doc["model"].(map[string]any)
	if model["name"] != "gemini-2.5-pro" || model["maxSessionTurns"] != float64(10) {
		t.Fatalf("model %v", model)
	}
	security := doc["security"].(map[string]any)
	auth := security["auth"].(map[string]any)
	if auth["selectedType"] != SelectedAuth {
		t.Fatalf("auth %v", auth)
	}

	cleared, err := Apply(first, Update{ClearModel: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(cleared, &doc); err != nil {
		t.Fatal(err)
	}
	model = doc["model"].(map[string]any)
	if _, ok := model["name"]; ok {
		t.Fatalf("name still set: %v", model)
	}
	if model["maxSessionTurns"] == nil {
		t.Fatalf("cleared other model fields: %v", model)
	}
	if strings.Contains(string(cleared), "old-model") || strings.Contains(string(cleared), "gemini-2.5-pro") {
		t.Fatalf("model id remained:\n%s", cleared)
	}
}

func TestApplyEmptyDocument(t *testing.T) {
	out, err := Apply(nil, Update{})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"selectedType": "gemini-api-key"`) || strings.Contains(string(out), `"model"`) {
		t.Fatalf("fresh:\n%s", out)
	}
}

func TestApplyRejectsNonObjectField(t *testing.T) {
	_, err := Apply([]byte(`{"security":"nope"}`), Update{})
	if err == nil || !strings.Contains(err.Error(), "not an object") {
		t.Fatal(err)
	}
}

func TestSettingsPath(t *testing.T) {
	if got := SettingsPath("/home/u", ""); got != filepath.Join("/home/u", ".gemini", "settings.json") {
		t.Fatal(got)
	}
	if got := SettingsPath("/home/u", "/opt/gemini"); got != filepath.Join("/opt/gemini", ".gemini", "settings.json") {
		t.Fatal(got)
	}
}
