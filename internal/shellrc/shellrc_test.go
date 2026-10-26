package shellrc

import (
	"os/exec"
	"strings"
	"testing"
)

func TestUpsertBashIdempotentAndPreserves(t *testing.T) {
	in := "export KEEP_ME=1\n"
	updates := []Pair{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "a'b\"c$d"},
		{Key: "ANTHROPIC_MODEL", Value: "claude-sonnet-4-6"},
		{Key: "DEEPROUTER_API_KEY", Value: "a'b\"c$d"},
	}
	first, err := Upsert(in, "zsh", updates)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(first, "export KEEP_ME=1") {
		t.Fatalf("lost user line:\n%s", first)
	}
	if strings.Count(first, Begin) != 1 || strings.Count(first, End) != 1 {
		t.Fatalf("markers:\n%s", first)
	}
	second, err := Upsert(first, "zsh", updates)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("not idempotent\n%s\n%s", first, second)
	}
	// Empty model update must not drop the saved model.
	third, err := Upsert(second, "zsh", []Pair{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://example.test"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(third, "ANTHROPIC_MODEL=") || !strings.Contains(third, "DEEPROUTER_API_KEY=") {
		t.Fatalf("dropped keys:\n%s", third)
	}
	if !strings.Contains(third, "https://example.test") {
		t.Fatalf("url not updated:\n%s", third)
	}
	if strings.Count(third, Begin) != 1 {
		t.Fatal(third)
	}
}

func TestUpsertFishAndPowerShell(t *testing.T) {
	fish, err := Upsert("", "fish", []Pair{{Key: "DEEPROUTER_API_KEY", Value: "sk'x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(fish, "set -gx DEEPROUTER_API_KEY 'sk'\"'\"'x'") {
		t.Fatalf("fish quote:\n%s", fish)
	}
	ps, err := Upsert("", "powershell", []Pair{{Key: "DEEPROUTER_API_KEY", Value: "sk'x"}})
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ps, "$env:DEEPROUTER_API_KEY = 'sk''x'") {
		t.Fatalf("powershell quote:\n%s", ps)
	}
	again, err := Upsert(ps, "powershell", []Pair{{Key: "DEEPROUTER_API_KEY", Value: "sk'x"}})
	if err != nil {
		t.Fatal(err)
	}
	if again != ps {
		t.Fatalf("powershell not idempotent\n%s\n%s", ps, again)
	}
}

func TestRemove(t *testing.T) {
	in := "export KEEP_ME=1\n"
	updated, err := Upsert(in, "bash", []Pair{{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"}})
	if err != nil {
		t.Fatal(err)
	}
	out, err := Remove(updated)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(out, "deeprouter-setup") || strings.Contains(out, "ANTHROPIC_BASE_URL") {
		t.Fatalf("block remains:\n%s", out)
	}
	if !strings.Contains(out, "KEEP_ME=1") {
		t.Fatalf("lost user line:\n%s", out)
	}
}

func TestBashRoundTrip(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not installed")
	}
	key := "a'b\"c$d`e"
	file, err := Upsert("export KEEP_ME=1\n", "bash", []Pair{
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: key},
		{Key: "DEEPROUTER_API_KEY", Value: key},
	})
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("bash", "-c", "source /dev/stdin; printf '%s' \"$ANTHROPIC_AUTH_TOKEN\"")
	cmd.Stdin = strings.NewReader(file)
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("source failed: %v\n%s", err, file)
	}
	if string(out) != key {
		t.Fatalf("got %q want %q\n%s", out, key, file)
	}
}

func TestLookupAndReplaceKey(t *testing.T) {
	first, err := Upsert("export KEEP_ME=1\n", "zsh", []Pair{
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-old-key-1111"},
		{Key: "DEEPROUTER_API_KEY", Value: "sk-old-key-1111"},
	})
	if err != nil {
		t.Fatal(err)
	}
	got, ok := Lookup(first, "DEEPROUTER_API_KEY")
	if !ok || got != "sk-old-key-1111" {
		t.Fatalf("lookup %q %v\n%s", got, ok, first)
	}
	if _, ok := Lookup(first, "MISSING"); ok {
		t.Fatal("missing key reported present")
	}
	second, err := Upsert(first, "zsh", []Pair{
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-new-key-2222"},
		{Key: "DEEPROUTER_API_KEY", Value: "sk-new-key-2222"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(second, "sk-old-key-1111") || strings.Count(second, "DEEPROUTER_API_KEY=") != 1 || strings.Count(second, Begin) != 1 {
		t.Fatalf("replace:\n%s", second)
	}
	if !strings.Contains(second, "KEEP_ME=1") {
		t.Fatalf("lost user line:\n%s", second)
	}
	again, err := Upsert(second, "zsh", []Pair{
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-new-key-2222"},
		{Key: "DEEPROUTER_API_KEY", Value: "sk-new-key-2222"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if again != second {
		t.Fatalf("not idempotent\n%s\n%s", second, again)
	}
	if _, ok := Lookup("export DEEPROUTER_API_KEY=outside\n", "DEEPROUTER_API_KEY"); ok {
		t.Fatal("lookup matched a line outside the managed block")
	}
}

func TestBrokenMarker(t *testing.T) {
	if _, err := Upsert(Begin+"\nexport A=1\n", "bash", nil); err == nil {
		t.Fatal("expected error")
	}
}
