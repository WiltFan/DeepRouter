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
	text := string(first)
	if strings.Index(text, `"ui"`) > strings.Index(text, `"mcpServers"`) || strings.Index(text, `"mcpServers"`) > strings.Index(text, `"model"`) || strings.Index(text, `"model"`) > strings.Index(text, `"security"`) {
		t.Fatalf("order:\n%s", text)
	}
	if strings.Index(text, `"name"`) > strings.Index(text, `"maxSessionTurns"`) {
		t.Fatalf("model order:\n%s", text)
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

func TestApplyPreservesOrderIndentAndArrays(t *testing.T) {
	single := []byte(`{"zebra":1,"alpha":{"items":[1,2]},"model":{"name":"old"}}`)
	out, err := Apply(single, Update{Model: "gemini-2.5-pro", SetModel: true})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	if strings.Index(text, `"zebra"`) > strings.Index(text, `"alpha"`) || strings.Index(text, `"alpha"`) > strings.Index(text, `"model"`) || strings.Index(text, `"model"`) > strings.Index(text, `"security"`) {
		t.Fatalf("order:\n%s", text)
	}
	if !strings.Contains(text, "[1,2]") {
		t.Fatalf("array expanded:\n%s", text)
	}
	again, err := Apply(out, Update{Model: "gemini-2.5-pro", SetModel: true})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != text {
		t.Fatalf("not idempotent\n%s\n%s", text, again)
	}

	four := []byte("{\n    \"zebra\": 1,\n    \"alpha\": {\n        \"items\": [1, 2]\n    }\n}\n")
	out, err = Apply(four, Update{})
	if err != nil {
		t.Fatal(err)
	}
	text = string(out)
	if strings.Contains(text, "\n  \"") || !strings.Contains(text, "\n    \"zebra\": 1,\n") || !strings.Contains(text, "[1, 2]") {
		t.Fatalf("four space:\n%s", text)
	}
	if strings.Index(text, `"zebra"`) > strings.Index(text, `"alpha"`) || strings.Index(text, `"alpha"`) > strings.Index(text, `"security"`) {
		t.Fatalf("four order:\n%s", text)
	}

	tabbed := []byte("{\n\t\"zebra\": 1,\n\t\"alpha\": [1, 2]\n}\n")
	out, err = Apply(tabbed, Update{})
	if err != nil {
		t.Fatal(err)
	}
	text = string(out)
	if strings.Contains(text, "\n ") || !strings.Contains(text, "\n\t\"zebra\": 1,\n") || !strings.Contains(text, "[1, 2]") {
		t.Fatalf("tab:\n%s", text)
	}
	if strings.Index(text, `"zebra"`) > strings.Index(text, `"alpha"`) || strings.Index(text, `"alpha"`) > strings.Index(text, `"security"`) {
		t.Fatalf("tab order:\n%s", text)
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
