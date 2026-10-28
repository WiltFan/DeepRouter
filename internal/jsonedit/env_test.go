package jsonedit

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestSetEnvFreshAndIdempotent(t *testing.T) {
	updates := []KV{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: `sk-"quoted`},
		{Key: "ANTHROPIC_MODEL", Value: "claude-sonnet-4-6"},
	}
	first, err := SetEnv(nil, updates)
	if err != nil {
		t.Fatal(err)
	}
	second, err := SetEnv(first, updates)
	if err != nil {
		t.Fatal(err)
	}
	if string(first) != string(second) {
		t.Fatalf("not idempotent\nfirst:\n%s\nsecond:\n%s", first, second)
	}
	assertEnv(t, first, map[string]string{
		"ANTHROPIC_BASE_URL":   "https://deeprouter.top",
		"ANTHROPIC_AUTH_TOKEN": `sk-"quoted`,
		"ANTHROPIC_MODEL":      "claude-sonnet-4-6",
	})
}

func TestSetEnvPreservesSiblingKeys(t *testing.T) {
	in := []byte("{\n  \"permissions\": {\"allow\": [\"Bash\"]},\n  \"theme\": \"dark\"\n}\n")
	out, err := SetEnv(in, []KV{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(out), `"permissions": {"allow": ["Bash"]}`) {
		t.Fatalf("permissions were rewritten:\n%s", out)
	}
	if !strings.Contains(string(out), `"theme": "dark"`) {
		t.Fatalf("theme missing:\n%s", out)
	}
	again, err := SetEnv(out, []KV{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != string(out) {
		t.Fatalf("not idempotent\n%s\n%s", out, again)
	}
	// A later model update must keep the token and the siblings.
	withModel, err := SetEnv(out, []KV{{Key: "ANTHROPIC_MODEL", Value: "claude-opus-4-6"}})
	if err != nil {
		t.Fatal(err)
	}
	assertEnv(t, withModel, map[string]string{
		"ANTHROPIC_BASE_URL":   "https://deeprouter.top",
		"ANTHROPIC_AUTH_TOKEN": "sk-test",
		"ANTHROPIC_MODEL":      "claude-opus-4-6",
	})
	var root map[string]any
	if err := json.Unmarshal(withModel, &root); err != nil {
		t.Fatal(err)
	}
	if root["theme"] != "dark" {
		t.Fatalf("theme %v", root["theme"])
	}
}

func TestSetEnvPreservesOtherEnvValues(t *testing.T) {
	in := []byte(`{"env":{"KEEP":"yes","COUNT":1},"ok":true}`)
	out, err := SetEnv(in, []KV{{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-1"}})
	if err != nil {
		t.Fatal(err)
	}
	var root struct {
		Env map[string]any `json:"env"`
		OK  bool           `json:"ok"`
	}
	if err := json.Unmarshal(out, &root); err != nil {
		t.Fatal(err)
	}
	if !root.OK || root.Env["KEEP"] != "yes" || root.Env["COUNT"] != float64(1) {
		t.Fatalf("%+v", root)
	}
	if root.Env["ANTHROPIC_AUTH_TOKEN"] != "sk-1" {
		t.Fatal(root.Env)
	}
	text := string(out)
	if strings.Contains(text, `{"env":{`) || !strings.Contains(text, "\n") {
		t.Fatalf("single-line settings were not reindented:\n%s", text)
	}
	again, err := SetEnv(out, []KV{{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(again) != text {
		t.Fatalf("not idempotent\n%s\n%s", text, again)
	}
}

func TestSetEnvReindentsSingleLine(t *testing.T) {
	in := []byte(`{"env":{"KEEP":"yes","COUNT":1},"ok":true}`)
	out, err := SetEnv(in, []KV{{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-1"}})
	if err != nil {
		t.Fatal(err)
	}
	text := string(out)
	keep := strings.Index(text, `"KEEP"`)
	count := strings.Index(text, `"COUNT"`)
	token := strings.Index(text, `"ANTHROPIC_AUTH_TOKEN"`)
	ok := strings.Index(text, `"ok"`)
	if keep < 0 || count < keep || token < count || ok < token {
		t.Fatalf("key order:\n%s", text)
	}
	if !strings.Contains(text, "\n  \"env\": {\n    \"KEEP\": \"yes\",\n") {
		t.Fatalf("indent:\n%s", text)
	}
	compact := []byte(`{"permissions":{"allow":["Bash"]},"theme":"dark"}`)
	withEnv, err := SetEnv(compact, []KV{{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-test"}})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(withEnv), `{"permissions"`) || !strings.Contains(string(withEnv), `"allow": ["Bash"]`) {
		t.Fatalf("array or permissions:\n%s", withEnv)
	}
	bomIn := append([]byte{0xEF, 0xBB, 0xBF}, in...)
	bomOut, err := SetEnv(bomIn, []KV{{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(bomOut, []byte{0xEF, 0xBB, 0xBF}) {
		t.Fatalf("bom dropped:\n%s", bomOut)
	}
}

func TestSetEnvRejectsInvalid(t *testing.T) {
	if _, err := SetEnv([]byte(`[]`), []KV{{Key: "A", Value: "b"}}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := SetEnv([]byte(`{"env":[]}`), []KV{{Key: "A", Value: "b"}}); err == nil {
		t.Fatal("expected error")
	}
	if _, err := SetEnv([]byte(`{"env":{"A":1}}`), []KV{{Key: "A", Value: "b"}}); err == nil {
		t.Fatal("expected error for non-string overwrite")
	}
	if _, err := SetEnv([]byte(`not json`), nil); err == nil {
		t.Fatal("expected error")
	}
}

func assertEnv(t *testing.T, doc []byte, want map[string]string) {
	t.Helper()
	var root struct {
		Env map[string]string `json:"env"`
	}
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatalf("unmarshal: %v\n%s", err, doc)
	}
	for k, v := range want {
		if root.Env[k] != v {
			t.Fatalf("env[%s]=%q want %q\n%s", k, root.Env[k], v, doc)
		}
	}
}
