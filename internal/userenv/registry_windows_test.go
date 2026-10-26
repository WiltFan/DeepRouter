//go:build windows

package userenv

import (
	"fmt"
	"os"
	"testing"
)

func TestRegistryRoundTrip(t *testing.T) {
	prev := broadcastEnvChange
	broadcastEnvChange = func() {}
	t.Cleanup(func() { broadcastEnvChange = prev })

	path := fmt.Sprintf(`Software\deeprouter-setup-test-%d`, os.Getpid())
	t.Cleanup(func() {
		if err := DeleteKey(path); err != nil {
			t.Error(err)
		}
	})
	st := registryStore{path: path}
	if _, ok, err := st.Get("DEEPROUTER_API_KEY"); err != nil || ok {
		t.Fatalf("empty key ok %v err %v", ok, err)
	}
	rec, err := Plan(st, Record{}, []Update{{Key: "DEEPROUTER_API_KEY", Value: "sk-test-value"}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ApplyValues(st, rec); err != nil {
		t.Fatal(err)
	}
	got, ok, err := st.Get("DEEPROUTER_API_KEY")
	if err != nil || !ok || got != "sk-test-value" {
		t.Fatalf("got %q ok %v err %v", got, ok, err)
	}
	if err := Reconcile(st, rec, true, Record{}, false); err != nil {
		t.Fatal(err)
	}
	if _, ok, err := st.Get("DEEPROUTER_API_KEY"); err != nil || ok {
		t.Fatalf("after restore ok %v err %v", ok, err)
	}
}
