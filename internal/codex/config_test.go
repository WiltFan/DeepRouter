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
experimental_bearer_token = "sk-codex-token"
wire_api = "responses"
requires_openai_auth = false
`
	got, err := Apply(in, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Officially",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "responses",
		BearerToken:   "sk-codex-token",
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
		BearerToken:   "sk-codex-token",
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
		BearerToken:   "sk-codex-token",
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
env_key = "OTHER_KEY"
`
	got, err := Apply(in, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Azure",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "chat",
		BearerToken:   "sk-codex-token",
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
	if strings.Contains(got, "env_key") {
		t.Fatalf("deeprouter env_key remains:\n%s", got)
	}
	if !strings.Contains(got, `experimental_bearer_token = "sk-codex-token"`) {
		t.Fatalf("bearer token missing:\n%s", got)
	}
	if !KeyReady(got, "sk-codex-token") {
		t.Fatalf("key not ready:\n%s", got)
	}
	again, err := Apply(got, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Azure",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "chat",
		BearerToken:   "sk-codex-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent\n%s\n%s", got, again)
	}
}

func TestApplyRemovesIgnoredResponseStorageKey(t *testing.T) {
	in := `disable_response_storage = true # old
sandbox_mode = "workspace-write"
model = "old"

[sandbox]
mode = "read-only"

[profiles.zdr]
disable_response_storage = true
model = "o3"

[model_providers."deeprouter"]
name = "Old"
disable_response_storage = true
custom_note = "keep"
base_url = "https://old.example/v1"
`
	upd := Update{
		Model:         "gpt-4.1",
		ProviderLabel: "Officially",
		BaseURL:       "https://deeprouter.top/v1",
		WireAPI:       "responses",
		BearerToken:   "sk-codex-token",
	}
	want := `sandbox_mode = "workspace-write"
model = "gpt-4.1"
model_provider = "deeprouter"

[sandbox]
mode = "read-only"

[profiles.zdr]
disable_response_storage = true
model = "o3"

[model_providers."deeprouter"]
name = "Officially"
custom_note = "keep"
base_url = "https://deeprouter.top/v1"
experimental_bearer_token = "sk-codex-token"
wire_api = "responses"
requires_openai_auth = false
`
	got, err := Apply(in, upd)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	again, err := Apply(got, upd)
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent\n%s\n%s", got, again)
	}

	fresh, err := Apply("", upd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(fresh, removedCodexKey) {
		t.Fatalf("fresh config wrote %s:\n%s", removedCodexKey, fresh)
	}

	only := "disable_response_storage = true\n\n[sandbox]\nmode = \"read-only\"\n"
	stripped, err := Apply(only, upd)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(stripped, removedCodexKey) {
		t.Fatalf("root key remains:\n%s", stripped)
	}
	if !strings.Contains(stripped, "model_provider = \"deeprouter\"\n\n[sandbox]\nmode = \"read-only\"\n") {
		t.Fatalf("unrelated table changed:\n%s", stripped)
	}
}

func TestApplyLeavesModelWhenUnset(t *testing.T) {
	in := "model = \"gpt-old\"\n"
	got, err := Apply(in, Update{BaseURL: "https://deeprouter.top/v1", ProviderLabel: "DeepRouter", BearerToken: "sk-codex-token"})
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
	got, err := Apply(in, Update{BaseURL: "https://deeprouter.top/v1", ProviderLabel: "DeepRouter", BearerToken: "sk-codex-token"})
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

func TestApplyRequiresBearerAndRoundTripsEscapes(t *testing.T) {
	if _, err := Apply("model = \"x\"\n", Update{BaseURL: "https://deeprouter.top/v1"}); err == nil {
		t.Fatal("empty bearer token was accepted")
	}
	token := "sk-\"quoted\"\\path"
	got, err := Apply("", Update{
		ProviderLabel: "DeepRouter",
		BaseURL:       "https://deeprouter.top/v1",
		BearerToken:   token,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !KeyReady(got, token) {
		t.Fatalf("round trip failed:\n%s", got)
	}
	auth := ReadAuth(got)
	if auth.BearerToken != token || auth.EnvKey != "" {
		t.Fatalf("auth %+v", auth)
	}
}

func TestReadAuthIgnoresOtherProviders(t *testing.T) {
	in := `
env_key = "ROOT"
[model_providers.openai]
env_key = "OPENAI_API_KEY"
experimental_bearer_token = "other"

[model_providers.deeprouter]
experimental_bearer_token = "sk-ours"
`
	auth := ReadAuth(in)
	if auth.BearerToken != "sk-ours" || auth.EnvKey != "" {
		t.Fatalf("%+v", auth)
	}
	if KeyReady(in, "sk-ours") != true || KeyReady(in, "other") {
		t.Fatal("ready check")
	}
}

func TestApplySwitchesProviderAndDropsOldSection(t *testing.T) {
	in := `model = "gpt-4.1"
model_provider = "deeprouter"
disable_response_storage = true

[sandbox]
mode = "read-only"

[model_providers.openai]
name = "OpenAI"
base_url = "https://api.openai.com/v1"

[model_providers."deeprouter"]
name = "DeepRouter"
base_url = "https://deeprouter.top/v1"
experimental_bearer_token = "sk-old"
disable_response_storage = true
custom_note = "drop-with-section"

[profiles.dev]
model = "inside"
`
	got, err := Apply(in, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "WolfAI",
		ProviderID:    "wolfai",
		BaseURL:       "https://wolfai.top/v1",
		WireAPI:       "responses",
		BearerToken:   "sk-codex-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "model_providers.deeprouter") || strings.Contains(got, `model_providers."deeprouter"`) || strings.Contains(got, "custom_note") {
		t.Fatalf("old section remains:\n%s", got)
	}
	if strings.Contains(got, "disable_response_storage") {
		t.Fatalf("removed key remains:\n%s", got)
	}
	if !strings.Contains(got, `model_provider = "wolfai"`) || !strings.Contains(got, "[model_providers.wolfai]") || !strings.Contains(got, "https://wolfai.top/v1") {
		t.Fatalf("wolfai missing:\n%s", got)
	}
	if !strings.Contains(got, "[model_providers.openai]") || !strings.Contains(got, "[sandbox]") || !strings.Contains(got, "[profiles.dev]") || !strings.Contains(got, `model = "inside"`) {
		t.Fatalf("unrelated tables changed:\n%s", got)
	}
	auth := ReadAuth(got)
	if auth.BearerToken != "sk-codex-token" || auth.EnvKey != "" {
		t.Fatalf("auth %+v\n%s", auth, got)
	}
	again, err := Apply(got, Update{
		Model:         "gpt-4.1",
		ProviderLabel: "WolfAI",
		ProviderID:    "wolfai",
		BaseURL:       "https://wolfai.top/v1",
		WireAPI:       "responses",
		BearerToken:   "sk-codex-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != got {
		t.Fatalf("not idempotent\n%s\n%s", got, again)
	}
}
