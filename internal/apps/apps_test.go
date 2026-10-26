package apps

import (
	"fmt"
	"runtime"
	"strings"
	"testing"
)

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
	if err != nil || got[0] != Gemini || !ok || !info.Writable || info.ModelKind != "gemini" {
		t.Fatalf("%v writable %v kind %s", got, info.Writable, info.ModelKind)
	}
	if _, err := ParseSelection("9", false); err == nil {
		t.Fatal("expected range error")
	}
	if _, err := ParseSelection("nope", false); err == nil {
		t.Fatal("expected unknown program")
	}
}

func TestCodexPickerNamesChatGPTDesktop(t *testing.T) {
	info, ok := ByID(Codex)
	if !ok || info.ID != Codex || info.Title != "Codex" {
		t.Fatalf("%+v", info)
	}
	note := "Codex CLI and ChatGPT desktop app share ~/.codex/config.toml"
	if runtime.GOOS == "windows" {
		note = `Codex CLI and ChatGPT desktop app share %USERPROFILE%\.codex\config.toml`
	}
	if info.Note != note {
		t.Fatalf("note %q", info.Note)
	}
	line := fmt.Sprintf("  3) %-16s %s", info.Title, info.Note)
	if !strings.HasPrefix(line, "  3) Codex            "+note) {
		t.Fatalf("line %q", line)
	}
}
