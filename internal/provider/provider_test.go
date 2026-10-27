package provider

import (
	"testing"

	"github.com/zfg5118/deeprouter/internal/gateway"
)

func TestPresets(t *testing.T) {
	if Default().ID != "deeprouter" || Default().BaseURL != "https://deeprouter.top" {
		t.Fatalf("default %+v", Default())
	}
	all := All()
	if len(all) != 3 {
		t.Fatalf("%d presets", len(all))
	}
	wolf, ok := ByID(" WolfAI ")
	if !ok || wolf.BaseURL != "https://wolfai.top" || wolf.Name != "WolfAI" {
		t.Fatalf("%+v %v", wolf, ok)
	}
	link, ok := ByID("linkmesh")
	if !ok || link.BaseURL != "https://www.linkmesh.org" {
		t.Fatalf("%+v", link)
	}
	if _, err := Parse("custom"); err == nil {
		t.Fatal("custom is not a --provider value")
	}
	if _, err := Parse("nope"); err == nil {
		t.Fatal("accepted unknown provider")
	}
	if NormalizeID("") != "deeprouter" || NormalizeID("custom") != "custom" || NormalizeID("LINKMESH") != "linkmesh" {
		t.Fatal(NormalizeID(""), NormalizeID("custom"), NormalizeID("LINKMESH"))
	}
	if DisplayName("wolfai") != "WolfAI" || DisplayName("custom") != "Custom" || DisplayName("") != "DeepRouter" {
		t.Fatal(DisplayName("wolfai"), DisplayName("custom"), DisplayName(""))
	}
	if !KnownLabel("") || !KnownLabel("DeepRouter") || !KnownLabel("wolfai") || !KnownLabel("Custom") || KnownLabel("Azure") {
		t.Fatal("known label")
	}
	ids := ManagedIDs()
	if len(ids) != 4 || ids[3] != "custom" || !IsManaged("wolfai") || IsManaged("openai") {
		t.Fatalf("%v", ids)
	}
	if !UsesNewAPI("deeprouter") || !UsesNewAPI("WOLFAI") || UsesNewAPI("linkmesh") || UsesNewAPI("custom") {
		t.Fatal("new-api flag")
	}
	if TokenHost("wolfai", "http://127.0.0.1:9") != "wolfai.top" || TokenHost("linkmesh", "") != "www.linkmesh.org" {
		t.Fatal(TokenHost("wolfai", "http://127.0.0.1:9"), TokenHost("linkmesh", ""))
	}
	if TokenHost("custom", "https://example.com:8443/v1") != "example.com:8443" {
		t.Fatal(TokenHost("custom", "https://example.com:8443/v1"))
	}
	if PromptName("wolfai", true) != "WolfAI" || PromptName("custom", true) != "自定义网关" || PromptName("custom", false) != "Custom" {
		t.Fatal(PromptName("wolfai", true), PromptName("custom", true), PromptName("custom", false))
	}
	if !SameKeySource("", "https://deeprouter.top", "deeprouter", "http://127.0.0.1:9") {
		t.Fatal("legacy profile is deeprouter")
	}
	if SameKeySource("", "https://deeprouter.top", "wolfai", "https://wolfai.top") || SameKeySource("deeprouter", "", "linkmesh", "") {
		t.Fatal("keys crossed providers")
	}
	if !SameKeySource("wolfai", "https://wolfai.top", "wolfai", "http://127.0.0.1:9") {
		t.Fatal("preset key follows the id when the URL changes")
	}
	if !SameKeySource("custom", "http://127.0.0.1:1/v1", "custom", "http://127.0.0.1:1") {
		t.Fatal("same custom host")
	}
	if SameKeySource("custom", "http://127.0.0.1:1", "custom", "http://127.0.0.1:2") || SameKeySource("custom", "", "custom", "") {
		t.Fatal("custom hosts differ")
	}
}

func TestLinkMeshRoutes(t *testing.T) {
	if UsesNewAPI("linkmesh") {
		t.Fatal("linkmesh is sub2api, not new-api")
	}
	want := map[string]string{
		"codex":          "POST /v1/responses",
		"claude-code":    "POST /v1/messages",
		"claude-desktop": "POST /v1/messages",
		"grok":           "POST /v1/chat/completions",
		"gemini":         "POST /v1beta/models/{model}:generateContent",
	}
	got := Programs("linkmesh")
	if len(got) != len(want) {
		t.Fatalf("%d programs", len(got))
	}
	for _, prog := range got {
		route, ok := want[string(prog.App)]
		if !ok || !prog.Exposed || prog.Route != route {
			t.Fatalf("%+v", prog)
		}
	}
	if wire, force := CodexWire("linkmesh"); wire != "responses" || !force {
		t.Fatalf("wire %q force %v", wire, force)
	}
	if _, force := CodexWire("deeprouter"); force {
		t.Fatal("deeprouter should keep a saved wire_api")
	}
	if _, force := CodexWire("custom"); force {
		t.Fatal("custom has no forced wire_api")
	}
	if _, ok := ProgramFor("custom", "gemini"); ok {
		t.Fatal("custom is not route-limited")
	}
	ep, err := gateway.ParseBase("https://www.linkmesh.org")
	if err != nil {
		t.Fatal(err)
	}
	if ep.AnthropicBase != "https://www.linkmesh.org" || ep.OpenAIBase != "https://www.linkmesh.org/v1" || ep.GeminiBase() != "https://www.linkmesh.org" {
		t.Fatalf("claude %s openai %s gemini %s", ep.AnthropicBase, ep.OpenAIBase, ep.GeminiBase())
	}
	if ep.MessagesURL() != "https://www.linkmesh.org/v1/messages" || ep.ChatURL() != "https://www.linkmesh.org/v1/chat/completions" {
		t.Fatalf("messages %s chat %s", ep.MessagesURL(), ep.ChatURL())
	}
	if ep.GenerateContentURL("gemini-2.5-pro") != "https://www.linkmesh.org/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatal(ep.GenerateContentURL("gemini-2.5-pro"))
	}
}
