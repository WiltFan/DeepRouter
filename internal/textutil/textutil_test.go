package textutil

import "testing"

func TestMask(t *testing.T) {
	if Mask("") != "(not set)" {
		t.Fatal(Mask(""))
	}
	if Mask("short") != "****" {
		t.Fatal(Mask("short"))
	}
	got := Mask("sk-test-key-1234")
	if got != "sk-t…1234" {
		t.Fatalf("got %s", got)
	}
}

func TestValidateKey(t *testing.T) {
	got, err := ValidateKey("  sk-abc  ")
	if err != nil || got != "sk-abc" {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := ValidateKey("bad\nkey"); err == nil {
		t.Fatal("expected error")
	}
	if _, err := ValidateKey("  "); err == nil {
		t.Fatal("expected error")
	}
}

func TestDisplayPath(t *testing.T) {
	if got := DisplayPath("/home/u", "/home/u/.zshrc"); got != "~/.zshrc" {
		t.Fatal(got)
	}
	if got := DisplayPath("/home/u", "/etc/hosts"); got != "/etc/hosts" {
		t.Fatal(got)
	}
}
