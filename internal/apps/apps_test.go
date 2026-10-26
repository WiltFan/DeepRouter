package apps

import (
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

func TestCodexPickerLine(t *testing.T) {
	info, ok := ByID(Codex)
	if !ok || info.ID != "codex" || info.Title != "Codex / ChatGPT" {
		t.Fatalf("%+v", info)
	}
	var col = -1
	for i, row := range Catalog() {
		line := FormatPickerLine(i+1, row)
		at := strings.Index(line, row.Note)
		if at < 0 {
			t.Fatalf("note missing: %q", line)
		}
		if col < 0 {
			col = at
		} else if at != col {
			t.Fatalf("description column %d, want %d: %q", at, col, line)
		}
	}
	if runtime.GOOS == "windows" {
		return
	}
	const want = "3) Codex / ChatGPT   Codex CLI and ChatGPT desktop app share ~/.codex/config.toml"
	if got := FormatPickerLine(3, info); got != want {
		t.Fatalf("%q", got)
	}
}
