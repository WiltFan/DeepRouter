package apps

import "testing"

func TestParseSelection(t *testing.T) {
	got, err := ParseSelection("", true)
	if err != nil || len(got) != 3 || got[0] != ClaudeCode || got[2] != Codex {
		t.Fatalf("%v %v", got, err)
	}
	got, err = ParseSelection("4,1", false)
	if err != nil || len(got) != 2 || got[0] != Grok || got[1] != ClaudeCode {
		t.Fatalf("%v %v", got, err)
	}
	got, err = ParseSelection("grok, claude-desktop", false)
	if err != nil || got[0] != Grok || got[1] != ClaudeDesktop {
		t.Fatalf("%v %v", got, err)
	}
	got, err = ParseSelection("5", false)
	info, ok := ByID(Gemini)
	if err != nil || got[0] != Gemini || !ok || info.Writable {
		t.Fatalf("%v writable %v", got, info.Writable)
	}
	if _, err := ParseSelection("9", false); err == nil {
		t.Fatal("expected range error")
	}
	if _, err := ParseSelection("nope", false); err == nil {
		t.Fatal("expected unknown program")
	}
}
