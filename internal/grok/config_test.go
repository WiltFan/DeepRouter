package grok

import (
	"strings"
	"testing"
)

func TestApplyCreatesAndReplaces(t *testing.T) {
	in := "# keep\n\n[model.other]\nmodel = \"local\"\nbase_url = \"http://127.0.0.1:9/v1\"\n\n[mcp_servers.fs]\ncommand = \"npx\"\n"
	upd := Update{Model: "grok-4", BaseURL: "https://deeprouter.top/v1", APIKey: "sk-old-key-1111", Name: "Officially"}
	got, err := Apply(in, upd)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Apply(got, upd)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent\n%s\n%s", got, again)
	}
	if !strings.Contains(got, `default = "deeprouter"`) || !strings.Contains(got, `[model.deeprouter]`) {
		t.Fatalf("missing section\n%s", got)
	}
	if !strings.Contains(got, `api_key = "sk-old-key-1111"`) || !strings.Contains(got, `api_backend = "chat_completions"`) || !strings.Contains(got, `base_url = "https://deeprouter.top/v1"`) {
		t.Fatalf("fields\n%s", got)
	}
	if strings.Contains(got, "env_key") {
		t.Fatalf("env_key written\n%s", got)
	}
	if !strings.Contains(got, `[model.other]`) || !strings.Contains(got, `command = "npx"`) || !strings.Contains(got, "# keep") {
		t.Fatalf("lost user config\n%s", got)
	}
	if strings.Count(got, "api_key = ") != 1 || strings.Count(got, "default = ") != 1 {
		t.Fatalf("duplicated\n%s", got)
	}
	upd.APIKey = "sk-new-key-2222"
	upd.Model = "grok-4.5"
	next, err := Apply(got, upd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(next, "sk-old-key-1111") || !strings.Contains(next, "sk-new-key-2222") || !strings.Contains(next, `model = "grok-4.5"`) {
		t.Fatalf("replace\n%s", next)
	}
	if ReadAPIKey(next) != "sk-new-key-2222" {
		t.Fatal(ReadAPIKey(next))
	}
}

func TestApplyKeepsModelWhenUnset(t *testing.T) {
	first, err := Apply("", Update{Model: "grok-4", BaseURL: "https://deeprouter.top/v1", APIKey: "sk-old-key-1111"})
	if err != nil {
		t.Fatal(err)
	}
	next, err := Apply(first, Update{BaseURL: "https://deeprouter.top/v1", APIKey: "sk-new-key-2222"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(next, `model = "grok-4"`) || !strings.Contains(next, "sk-new-key-2222") || strings.Contains(next, "sk-old-key-1111") {
		t.Fatalf("%s", next)
	}
	if _, err := Apply("", Update{BaseURL: "https://deeprouter.top/v1", APIKey: "sk-new-key-2222"}); err == nil {
		t.Fatal("expected a model id on a new file")
	}
}

func TestApplyDropsEnvKeyInOwnedSection(t *testing.T) {
	in := "[models]\nweb_search = \"grok-4.7\"\ndefault = \"other\"\n\n[model.deeprouter]\nmodel = \"old\"\nbase_url = \"https://api.x.ai/v1\"\napi_key = \"sk-old\"\nenv_key = \"XAI_API_KEY\"\n"
	got, err := Apply(in, Update{Model: "grok-4", BaseURL: "https://deeprouter.top/v1", APIKey: "sk-new-key-2222"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "env_key") || strings.Contains(got, "sk-old") || !strings.Contains(got, `web_search = "grok-4.7"`) {
		t.Fatalf("%s", got)
	}
	if strings.Count(got, `default = "deeprouter"`) != 1 {
		t.Fatalf("%s", got)
	}
}

func TestApplySwitchesSectionAndKeepsOtherModels(t *testing.T) {
	in := "[models]\ndefault = \"deeprouter\"\n\n[model.deeprouter]\nmodel = \"grok-4\"\nbase_url = \"https://deeprouter.top/v1\"\napi_key = \"sk-old-key-1111\"\n\n[model.other]\nmodel = \"local\"\nbase_url = \"http://127.0.0.1:9/v1\"\n"
	got, err := Apply(in, Update{
		Model:     "grok-4",
		BaseURL:   "https://wolfai.top/v1",
		APIKey:    "sk-new-key-2222",
		Name:      "WolfAI",
		SectionID: "wolfai",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "[model.deeprouter]") || strings.Contains(got, "sk-old-key-1111") || strings.Contains(got, "deeprouter.top") {
		t.Fatalf("old section remains:\n%s", got)
	}
	if !strings.Contains(got, `[model.wolfai]`) || !strings.Contains(got, `default = "wolfai"`) || !strings.Contains(got, "https://wolfai.top/v1") {
		t.Fatalf("wolfai missing:\n%s", got)
	}
	if !strings.Contains(got, "[model.other]") || !strings.Contains(got, `model = "local"`) {
		t.Fatalf("user model dropped:\n%s", got)
	}
	if ReadAPIKey(got) != "sk-new-key-2222" || ActiveSection(got) != "wolfai" {
		t.Fatal(ReadAPIKey(got), ActiveSection(got))
	}
	if ReadAPIKey(in) != "sk-old-key-1111" || ActiveSection(in) != "deeprouter" {
		t.Fatal(ReadAPIKey(in), ActiveSection(in))
	}
}
