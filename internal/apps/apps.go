// Package apps is the set of programs deeprouter-setup can configure.
package apps

import (
	"fmt"
	"runtime"
	"strings"
)

// ID is a program the user can select.
type ID string

const (
	ClaudeCode    ID = "claude-code"
	ClaudeDesktop ID = "claude-desktop"
	Codex         ID = "codex"
	Grok          ID = "grok"
	Gemini        ID = "gemini"
)

// Info is one row in the program picker.
type Info struct {
	ID        ID
	Title     string
	ModelKind string
	Writable  bool
	// Note is a short path or the reason a program is not written.
	Note string
}

// GeminiReason explains why Gemini CLI is listed but not configured.
// Gemini CLI sends Google GenAI generateContent requests. A custom
// GOOGLE_GEMINI_BASE_URL still uses that protocol and x-goog-api-key.
// This gateway serves OpenAI /v1 and Anthropic /v1/messages.
const GeminiReason = "Gemini CLI cannot use this gateway. It speaks the Gemini generateContent API (GOOGLE_GEMINI_BASE_URL changes the Google GenAI host, not an OpenAI or Anthropic base URL). No Gemini config is written."

// Catalog is the picker order.
func Catalog() []Info {
	return []Info{
		{ID: ClaudeCode, Title: "Claude Code", ModelKind: "claude", Writable: true, Note: claudeCodeNote()},
		{ID: ClaudeDesktop, Title: "Claude Desktop", ModelKind: "claude", Writable: true, Note: claudeDesktopNote()},
		{ID: Codex, Title: "Codex", ModelKind: "gpt", Writable: true, Note: codexNote()},
		{ID: Grok, Title: "Grok Build", ModelKind: "grok", Writable: true, Note: grokNote()},
		{ID: Gemini, Title: "Gemini CLI", ModelKind: "", Writable: false, Note: "not available: Gemini API only, not this gateway"},
	}
}

func claudeCodeNote() string {
	if runtime.GOOS == "windows" {
		return `%USERPROFILE%\.claude\settings.json`
	}
	return "~/.claude/settings.json"
}

func claudeDesktopNote() string {
	if runtime.GOOS == "windows" {
		return `%APPDATA%\Claude\configLibrary`
	}
	return "Chat, Cowork, and the Code tab (configLibrary)"
}

func codexNote() string {
	if runtime.GOOS == "windows" {
		return `CLI and desktop app share %USERPROFILE%\.codex\config.toml`
	}
	return "CLI and desktop app share ~/.codex/config.toml"
}

func grokNote() string {
	if runtime.GOOS == "windows" {
		return `%USERPROFILE%\.grok\config.toml`
	}
	return "~/.grok/config.toml"
}

// DefaultIDs is the Enter selection: the programs setup configured before
// the picker existed. Codex's desktop app is the same file as the CLI.
func DefaultIDs() []ID {
	return []ID{ClaudeCode, ClaudeDesktop, Codex}
}

// ByID returns the catalog entry.
func ByID(id ID) (Info, bool) {
	for _, info := range Catalog() {
		if info.ID == id {
			return info, true
		}
	}
	return Info{}, false
}

// ParseSelection reads "1,3" or "claude-code,grok".
// An empty answer is the default set when emptyIsDefault is set.
func ParseSelection(answer string, emptyIsDefault bool) ([]ID, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		if emptyIsDefault {
			return DefaultIDs(), nil
		}
		return nil, fmt.Errorf("no programs selected")
	}
	fields := strings.FieldsFunc(answer, func(r rune) bool {
		return r == ',' || r == ' ' || r == '\t'
	})
	if len(fields) == 0 {
		return nil, fmt.Errorf("no programs selected")
	}
	cat := Catalog()
	var out []ID
	seen := map[ID]bool{}
	for _, field := range fields {
		field = strings.TrimSpace(field)
		var id ID
		if n, err := parseIndex(field); err == nil {
			if n < 1 || n > len(cat) {
				return nil, fmt.Errorf("program %d is out of range", n)
			}
			id = cat[n-1].ID
		} else if info, ok := lookup(field); ok {
			id = info.ID
		} else {
			return nil, fmt.Errorf("unknown program %q", field)
		}
		if seen[id] {
			continue
		}
		seen[id] = true
		out = append(out, id)
	}
	return out, nil
}

// WritableOnly drops programs that cannot be configured.
func WritableOnly(ids []ID) []ID {
	var out []ID
	for _, id := range ids {
		info, ok := ByID(id)
		if ok && info.Writable {
			out = append(out, id)
		}
	}
	return out
}

// Contains reports whether id is in the selection.
func Contains(ids []ID, id ID) bool {
	for _, item := range ids {
		if item == id {
			return true
		}
	}
	return false
}

func lookup(name string) (Info, bool) {
	for _, info := range Catalog() {
		if strings.EqualFold(string(info.ID), name) || strings.EqualFold(info.Title, name) {
			return info, true
		}
	}
	return Info{}, false
}

func parseIndex(s string) (int, error) {
	if s == "" {
		return 0, fmt.Errorf("empty")
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("not a number")
		}
		n = n*10 + int(r-'0')
	}
	return n, nil
}
