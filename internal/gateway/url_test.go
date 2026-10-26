package gateway

import "testing"

func TestParseBase(t *testing.T) {
	cases := []struct {
		in        string
		anthropic string
		openai    string
	}{
		{"https://deeprouter.top", "https://deeprouter.top", "https://deeprouter.top/v1"},
		{"https://deeprouter.top/", "https://deeprouter.top", "https://deeprouter.top/v1"},
		{"https://deeprouter.top/v1", "https://deeprouter.top", "https://deeprouter.top/v1"},
		{"https://deeprouter.top/v1/", "https://deeprouter.top", "https://deeprouter.top/v1"},
		{"deeprouter.top", "https://deeprouter.top", "https://deeprouter.top/v1"},
		{"http://127.0.0.1:8080", "http://127.0.0.1:8080", "http://127.0.0.1:8080/v1"},
		{"http://127.0.0.1:8080/v1", "http://127.0.0.1:8080", "http://127.0.0.1:8080/v1"},
		{"https://example.com/gateway/v1", "https://example.com/gateway", "https://example.com/gateway/v1"},
		{"http://[::1]:9/v1", "http://[::1]:9", "http://[::1]:9/v1"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseBase(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if got.AnthropicBase != tc.anthropic || got.OpenAIBase != tc.openai {
				t.Fatalf("got anthropic %s openai %s", got.AnthropicBase, got.OpenAIBase)
			}
			if got.ModelsURL() != tc.openai+"/models" {
				t.Fatalf("models url %s", got.ModelsURL())
			}
			if got.ChatURL() != tc.openai+"/chat/completions" {
				t.Fatalf("chat url %s", got.ChatURL())
			}
			if got.MessagesURL() != tc.anthropic+"/v1/messages" {
				t.Fatalf("messages url %s", got.MessagesURL())
			}
			if got.GeminiBase() != tc.anthropic {
				t.Fatalf("gemini base %s", got.GeminiBase())
			}
		})
	}
}

func TestGeminiBaseStripsV1Beta(t *testing.T) {
	got, err := ParseBase("https://deeprouter.top/v1beta")
	if err != nil {
		t.Fatal(err)
	}
	if got.GeminiBase() != "https://deeprouter.top" {
		t.Fatalf("gemini base %s", got.GeminiBase())
	}
	got, err = ParseBase("https://deeprouter.top/v1beta/")
	if err != nil {
		t.Fatal(err)
	}
	if got.GeminiBase() != "https://deeprouter.top" {
		t.Fatalf("gemini base %s", got.GeminiBase())
	}
}

func TestParseBaseRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"   ",
		"ftp://example.com",
		"https://user:pass@example.com",
		"https://example.com/v1?x=1",
		"https://example.com/v1#frag",
		"://bad",
	} {
		t.Run(in, func(t *testing.T) {
			if _, err := ParseBase(in); err == nil {
				t.Fatal("expected error")
			}
		})
	}
}
