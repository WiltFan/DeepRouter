package codex

import (
	"strings"
	"testing"
)

func TestApplyPreservesUnrelatedConfig(t *testing.T) {
	in := `# keep
model = "gpt-old" # daily driver
model_provider = "openai"

[sandbox]
mode = "read-only"

[model_providers.openai]
name = "OpenAI"
base_url = "https://api.openai.com/v1"
env_key = "OPENAI_API_KEY"

[profiles.dev]
model = "inside"
`
	want := `# keep
model = "gpt-4.1"
model_provider = "deeprouter"

[sandbox]
mode = "read-only"

[model_providers.openai]
name = "OpenAI"
base_url = "https://api.openai.com/v1"
env_key = "OPENAI_API_KEY"

[profiles.dev]
model = "inside"

[model_providers.deeprouter]
name = "Officially"
base_url = "https://deeprouter.top/v1"
env_key = "DEEPROUTER_API_KEY"
wire_api = "responses"
requires_openai_auth = false
`
	got, err := Apply(in, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Officially",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "responses",
	})
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	again, err := Apply(got, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Officially",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "responses",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent\n%s\n%s", got, again)
	}
}

func TestApplyInsertsRootKeysBeforeTables(t *testing.T) {
	in := "# comment\n\n[sandbox]\nmode = \"read-only\"\n"
	got, err := Apply(in, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "DeepRouter",
		BaseURL:       "https://deeprouter.top/v1",
	})
	if err != nil {
		t.Fatal(err)
	}
	modelAt := strings.Index(got, "model = ")
	tableAt := strings.Index(got, "[sandbox]")
	if modelAt < 0 || tableAt < 0 || modelAt > tableAt {
		t.Fatalf("model key is not in the root table:\n%s", got)
	}
	if !strings.Contains(got, "model_provider = \"deeprouter\"\n\n[sandbox]") {
		t.Fatalf("root keys were not placed before the first table:\n%s", got)
	}
	if strings.Contains(got, "model = \"inside\"") {
		t.Fatal(got)
	}
}

func TestApplyUpdatesExistingProviderAndKeepsCustomKey(t *testing.T) {
	in := `model = "old"
model_provider = "deeprouter"

[model_providers.deeprouter]
name = "Old"
base_url = "https://old.example/v1"
env_key = "DEEPROUTER_API_KEY"
wire_api = "responses"
requires_openai_auth = false
custom_note = "keep"
`
	got, err := Apply(in, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Azure",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "[model_providers.deeprouter]") != 1 {
		t.Fatalf("duplicated section:\n%s", got)
	}
	if !strings.Contains(got, "custom_note = \"keep\"") {
		t.Fatalf("dropped custom key:\n%s", got)
	}
	if !strings.Contains(got, "name = \"Azure\"") || !strings.Contains(got, "wire_api = \"chat\"") {
		t.Fatalf("provider not updated:\n%s", got)
	}
	if strings.Contains(got, "https://old.example") {
		t.Fatalf("old url remains:\n%s", got)
	}
	again, err := Apply(got, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Azure",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "chat",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent\n%s\n%s", got, again)
	}
}

func TestApplyLeavesModelWhenUnset(t *testing.T) {
	in := "model = \"gpt-old\"\n"
	got, err := Apply(in, Update{BaseURL: "https://deeprouter.top/v1", ProviderLabel: "DeepRouter"})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got, "model = \"gpt-old\"") {
		t.Fatalf("model changed:\n%s", got)
	}
	if !strings.Contains(got, "model_provider = \"deeprouter\"") {
		t.Fatalf("provider missing:\n%s", got)
	}
}

func TestApplyQuotedHeader(t *testing.T) {
	in := "[model_providers.\"deeprouter\"]\nname = \"Old\"\n"
	got, err := Apply(in, Update{BaseURL: "https://deeprouter.top/v1", ProviderLabel: "DeepRouter"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(got, "deeprouter") < 1 {
		t.Fatal(got)
	}
	if strings.Count(got, "name = ") != 1 {
		t.Fatalf("duplicated name:\n%s", got)
	}
	if !strings.Contains(got, "base_url = \"https://deeprouter.top/v1\"") {
		t.Fatalf("missing base\n%s", got)
	}
}
