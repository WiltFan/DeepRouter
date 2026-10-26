package userenv

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlanApplyAndRestore(t *testing.T) {
	st := NewMem()
	if err := st.Set("ANTHROPIC_MODEL", "kept-by-user"); err != nil {
		t.Fatal(err)
	}
	first, err := Plan(st, Record{}, []Update{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-old"},
		{Key: "DEEPROUTER_API_KEY", Value: "sk-old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := first.Get("ANTHROPIC_MODEL"); ok {
		t.Fatal("empty model update should not create a value")
	}
	raw, err := first.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	again, err := Plan(st, first, []Update{
		{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-old"},
		{Key: "DEEPROUTER_API_KEY", Value: "sk-old"},
	})
	if err != nil {
		t.Fatal(err)
	}
	raw2, err := again.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != string(raw2) {
		t.Fatalf("plan not stable\n%s\n%s", raw, raw2)
	}
	if _, err := ApplyValues(st, first); err != nil {
		t.Fatal(err)
	}
	if changed, err := ApplyValues(st, first); err != nil || changed {
		t.Fatalf("second apply changed %v err %v", changed, err)
	}
	got, ok, err := st.Get("ANTHROPIC_AUTH_TOKEN")
	if err != nil || !ok || got != "sk-old" {
		t.Fatalf("token %q ok %v err %v", got, ok, err)
	}
	model, ok, err := st.Get("ANTHROPIC_MODEL")
	if err != nil || !ok || model != "kept-by-user" {
		t.Fatalf("pre-existing model %q ok %v", model, ok)
	}

	next, err := Plan(st, first, []Update{
		{Key: "ANTHROPIC_AUTH_TOKEN", Value: "sk-new"},
		{Key: "DEEPROUTER_API_KEY", Value: "sk-new"},
		{Key: "ANTHROPIC_MODEL", Value: "claude-sonnet-4-6"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if v, _ := next.Get("ANTHROPIC_BASE_URL"); v != "https://deeprouter.top" {
		t.Fatalf("base dropped: %+v", next.Values)
	}
	if _, err := ApplyValues(st, next); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.Get("ANTHROPIC_AUTH_TOKEN"); got != "sk-new" {
		t.Fatal(got)
	}

	dir := t.TempDir()
	path := RecordPath(dir)
	if err := os.WriteFile(path, mustMarshal(t, next), 0o600); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Text() != next.Text() {
		t.Fatalf("reload\n%s\n%s", loaded.Text(), next.Text())
	}
	if err := Reconcile(st, next, true, Record{}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := st.Get("ANTHROPIC_AUTH_TOKEN"); ok {
		t.Fatal("restore left the token in place")
	}
	if _, ok, _ := st.Get("DEEPROUTER_API_KEY"); ok {
		t.Fatal("restore left the shared key in place")
	}
	if _, ok, _ := st.Get("ANTHROPIC_BASE_URL"); ok {
		t.Fatal("restore left the base URL in place")
	}
	model, ok, err = st.Get("ANTHROPIC_MODEL")
	if err != nil || !ok || model != "kept-by-user" {
		t.Fatalf("restore did not put the previous model back: %q ok %v", model, ok)
	}
}

func TestReconcileLatestDropsKeysAddedLater(t *testing.T) {
	st := NewMem()
	older, err := Plan(st, Record{}, []Update{{Key: "DEEPROUTER_API_KEY", Value: "sk-old"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyValues(st, older); err != nil {
		t.Fatal(err)
	}
	newer, err := Plan(st, older, []Update{
		{Key: "DEEPROUTER_API_KEY", Value: "sk-new"},
		{Key: "ANTHROPIC_MODEL", Value: "claude-sonnet-4-6"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyValues(st, newer); err != nil {
		t.Fatal(err)
	}
	if err := Reconcile(st, newer, true, older, true); err != nil {
		t.Fatal(err)
	}
	if got, _, _ := st.Get("DEEPROUTER_API_KEY"); got != "sk-old" {
		t.Fatalf("key %q", got)
	}
	if _, ok, _ := st.Get("ANTHROPIC_MODEL"); ok {
		t.Fatal("later model survived an older restore")
	}
}

func TestLoadMissing(t *testing.T) {
	_, err := Load(filepath.Join(t.TempDir(), "missing.json"))
	if !IsNotExist(err) {
		t.Fatal(err)
	}
}

func TestMarshalStableOrder(t *testing.T) {
	rec := Record{
		Prior: []Prior{{Key: "DEEPROUTER_API_KEY", Set: false}},
		Values: []Value{
			{Key: "ANTHROPIC_BASE_URL", Value: "https://deeprouter.top"},
			{Key: "DEEPROUTER_API_KEY", Value: "sk-1"},
		},
	}
	a, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	b, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	if string(a) != string(b) || !strings.Contains(string(a), "ANTHROPIC_BASE_URL") {
		t.Fatalf("%s", a)
	}
}

func mustMarshal(t *testing.T, rec Record) []byte {
	t.Helper()
	b, err := rec.Marshal()
	if err != nil {
		t.Fatal(err)
	}
	return b
}
