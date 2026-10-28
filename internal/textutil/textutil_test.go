package textutil

import (
	"strings"
	"testing"
)

func TestMask(t *testing.T) {
	if Mask("") != "(not set)" {
		t.Fatal(Mask(""))
	}
	if MaskConfirm("") != "****, 0 chars" {
		t.Fatal(MaskConfirm(""))
	}
	if Mask("short") != "****" || MaskConfirm("short") != "****, 5 chars" {
		t.Fatalf("short %s confirm %s", Mask("short"), MaskConfirm("short"))
	}
	// The body is the whole secret. Showing the tail would reveal it.
	if Mask("sk-abcd") != "sk-****" || strings.Contains(Mask("sk-abcd"), "abcd") {
		t.Fatal(Mask("sk-abcd"))
	}
	if Mask("sk-abc") != "sk-****" || strings.Contains(Mask("sk-abc"), "abc") {
		t.Fatal(Mask("sk-abc"))
	}
	got := Mask("sk-test-key-1234")
	if got != "sk-****1234" || strings.Contains(got, "sk-t") {
		t.Fatalf("got %s", got)
	}
	if MaskConfirm("sk-test-key-1234") != "sk-****1234, 16 chars" {
		t.Fatal(MaskConfirm("sk-test-key-1234"))
	}
	secret := "sk-" + strings.Repeat("x", 44) + "abcd"
	if Mask(secret) != "sk-****abcd" || MaskConfirm(secret) != "sk-****abcd, 51 chars" {
		t.Fatalf("mask %s confirm %s", Mask(secret), MaskConfirm(secret))
	}
	other := "tok-" + strings.Repeat("m", 20) + "wxyz"
	if Mask(other) != "****wxyz" || strings.Contains(Mask(other), "tok") {
		t.Fatal(Mask(other))
	}
	if MaskConfirm(other) != "****wxyz, 28 chars" {
		t.Fatal(MaskConfirm(other))
	}
	if Mask("abcdefg") != "****" {
		t.Fatal(Mask("abcdefg"))
	}
	uni := "sk-" + strings.Repeat("你", 8) + "结尾四字"
	if Mask(uni) != "sk-****结尾四字" || strings.Contains(Mask(uni), "你") {
		t.Fatal(Mask(uni))
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
