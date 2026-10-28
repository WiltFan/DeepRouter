package provider

import (
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/gateway"
)

func TestPresets(t *testing.T) {
	if Default().ID != "deeprouter" || Default().BaseURL != "https://deeprouter.top" {
		t.Fatalf("default %+v", Default())
	}
	all := All()
	if len(all) != 7 || all[3].ID != "openai-api" || all[6].ID != "xai" {
		t.Fatalf("%d presets %+v", len(all), all)
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
	if len(ids) != 8 || ids[len(ids)-1] != "custom" || !IsManaged("wolfai") || !IsManaged("openai-api") || IsManaged("openai") || IsManaged("azure") {
		t.Fatalf("%v", ids)
	}
	if !UsesNewAPI("deeprouter") || !UsesNewAPI("WOLFAI") || UsesNewAPI("linkmesh") || UsesNewAPI("custom") || UsesNewAPI("openai") {
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
	if wire, force := CodexWire("openai"); wire != "responses" || !force {
		t.Fatalf("openai wire %q force %v", wire, force)
	}
	if wire, force := CodexWire("xai"); wire != "responses" || !force {
		t.Fatalf("xai wire %q force %v", wire, force)
	}
	if _, force := CodexWire("anthropic"); force {
		t.Fatal("anthropic does not configure Codex")
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

func TestOfficialPresetsMatchClientRoutes(t *testing.T) {
	openai, ok := ByID("openai")
	if !ok || !openai.Official || openai.BaseURL != "https://api.openai.com" || openai.Name != "OpenAI" {
		t.Fatalf("%+v %v", openai, ok)
	}
	if _, err := Parse("openai"); err != nil {
		t.Fatal(err)
	}
	if got := Offered("openai"); len(got) != 1 || got[0] != "codex" {
		t.Fatalf("openai offered %v", got)
	}
	ep, err := gateway.ParseBase(openai.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if ep.OpenAIBase != "https://api.openai.com/v1" || ep.ResponsesURL() != "https://api.openai.com/v1/responses" {
		t.Fatalf("openai urls %+v %s", ep, ep.ResponsesURL())
	}

	anth, _ := ByID("anthropic")
	if anth.BaseURL != "https://api.anthropic.com" || anth.Name != "Anthropic/Claude" {
		t.Fatal(anth)
	}
	if got := Offered("anthropic"); len(got) != 2 || got[0] != "claude-code" || got[1] != "claude-desktop" {
		t.Fatalf("anthropic offered %v", got)
	}
	ep, err = gateway.ParseBase(anth.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if ep.AnthropicBase != "https://api.anthropic.com" || ep.MessagesURL() != "https://api.anthropic.com/v1/messages" {
		t.Fatalf("anthropic %s %s", ep.AnthropicBase, ep.MessagesURL())
	}
	if DefaultPrograms("anthropic")[0] != "claude-code" {
		t.Fatal(DefaultPrograms("anthropic"))
	}

	gem, _ := ByID("gemini")
	if gem.BaseURL != "https://generativelanguage.googleapis.com" {
		t.Fatal(gem)
	}
	if got := Offered("gemini"); len(got) != 1 || got[0] != "gemini" {
		t.Fatalf("gemini offered %v", got)
	}
	ep, err = gateway.ParseBase(gem.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if ep.GeminiBase() != "https://generativelanguage.googleapis.com" || ep.GenerateContentURL("gemini-2.5-pro") != "https://generativelanguage.googleapis.com/v1beta/models/gemini-2.5-pro:generateContent" {
		t.Fatalf("gemini %s %s", ep.GeminiBase(), ep.GenerateContentURL("gemini-2.5-pro"))
	}

	xai, _ := ByID("xai")
	if xai.BaseURL != "https://api.x.ai" || xai.Name != "xAI Grok" {
		t.Fatal(xai)
	}
	if got := Offered("xai"); len(got) != 2 || got[0] != "codex" || got[1] != "grok" {
		t.Fatalf("xai offered %v", got)
	}
	ep, err = gateway.ParseBase(xai.BaseURL)
	if err != nil {
		t.Fatal(err)
	}
	if ep.OpenAIBase != "https://api.x.ai/v1" || ep.ResponsesURL() != "https://api.x.ai/v1/responses" || ep.ChatURL() != "https://api.x.ai/v1/chat/completions" {
		t.Fatalf("xai %+v", ep)
	}
	if !strings.Contains(FlagList(), "openai") || !strings.HasSuffix(FlagList(), "or xai") {
		t.Fatal(FlagList())
	}
}

func TestForClientsKeepsGatewaysAndFiltersOfficialPresets(t *testing.T) {
	ids := func(list []Preset) []string {
		out := make([]string, len(list))
		for i, p := range list {
			out[i] = p.ID
		}
		return out
	}
	gateways := []string{"deeprouter", "wolfai", "linkmesh"}
	if got := ids(ForClients(nil)); len(got) != 7 || got[0] != "deeprouter" || got[6] != "xai" {
		t.Fatalf("empty %v", got)
	}
	codex := ids(ForClients([]apps.ID{apps.Codex}))
	if strings.Join(codex, ",") != "deeprouter,wolfai,linkmesh,openai-api,xai" {
		t.Fatalf("codex %v", codex)
	}
	claude := ids(ForClients([]apps.ID{apps.ClaudeCode, apps.ClaudeDesktop}))
	if strings.Join(claude, ",") != "deeprouter,wolfai,linkmesh,anthropic" {
		t.Fatalf("claude %v", claude)
	}
	gemini := ids(ForClients([]apps.ID{apps.Gemini}))
	if strings.Join(gemini, ",") != "deeprouter,wolfai,linkmesh,gemini" {
		t.Fatalf("gemini %v", gemini)
	}
	grok := ids(ForClients([]apps.ID{apps.Grok}))
	if strings.Join(grok, ",") != "deeprouter,wolfai,linkmesh,xai" {
		t.Fatalf("grok %v", grok)
	}
	both := ids(ForClients([]apps.ID{apps.Codex, apps.Grok}))
	if strings.Join(both, ",") != "deeprouter,wolfai,linkmesh,xai" {
		t.Fatalf("codex+grok %v", both)
	}
	mixed := ids(ForClients([]apps.ID{apps.Codex, apps.ClaudeCode}))
	if strings.Join(mixed, ",") != strings.Join(gateways, ",") {
		t.Fatalf("codex+claude %v", mixed)
	}
	all := ids(ForClients([]apps.ID{apps.Codex, apps.ClaudeCode, apps.ClaudeDesktop, apps.Grok, apps.Gemini}))
	if strings.Join(all, ",") != strings.Join(gateways, ",") {
		t.Fatalf("all clients %v", all)
	}
}
