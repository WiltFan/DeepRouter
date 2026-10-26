// Package apps is the set of programs deeprouter can configure.
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

// Catalog is the picker order.
func Catalog() []Info {
	return []Info{
		{ID: Codex, Title: "Codex / ChatGPT", ModelKind: "gpt", Writable: true, Note: codexNote()},
		{ID: ClaudeCode, Title: "Claude Code", ModelKind: "claude", Writable: true, Note: claudeCodeNote()},
		{ID: ClaudeDesktop, Title: "Claude Desktop", ModelKind: "claude", Writable: true, Note: claudeDesktopNote()},
		{ID: Grok, Title: "Grok Build", ModelKind: "grok", Writable: true, Note: grokNote()},
		{ID: Gemini, Title: "Gemini CLI", ModelKind: "gemini", Writable: true, Note: geminiNote()},
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
		return `Codex CLI and ChatGPT desktop app share %USERPROFILE%\.codex\config.toml`
	}
	return "Codex CLI and ChatGPT desktop app share ~/.codex/config.toml"
}

// pickerNameWidth is the program-name column. Codex / ChatGPT is the longest
// title, and the width leaves the descriptions in one column.
const pickerNameWidth = 17

// FormatPickerLine is one program-picker row. Option 1 is
// "1) Codex / ChatGPT   Codex CLI and ChatGPT desktop app share ~/.codex/config.toml".
func FormatPickerLine(index int, info Info) string {
	return fmt.Sprintf("%d) %-*s %s", index, pickerNameWidth, info.Title, info.Note)
}

func grokNote() string {
	if runtime.GOOS == "windows" {
		return `%USERPROFILE%\.grok\config.toml`
	}
	return "~/.grok/config.toml"
}

func geminiNote() string {
	if runtime.GOOS == "windows" {
		return `%USERPROFILE%\.gemini\settings.json`
	}
	return "~/.gemini/settings.json"
}

// InstallHelp is the official install commands and docs for a program.
// command is the executable name. It is empty for Claude Desktop.
// how is every install command to show; Gemini CLI includes npm and, off
// Windows, Homebrew.
func InstallHelp(id ID) (command string, how []string, docs string) {
	switch id {
	case ClaudeCode:
		if runtime.GOOS == "windows" {
			return "claude", []string{"irm https://claude.ai/install.ps1 | iex"}, "https://code.claude.com/docs/en/setup"
		}
		return "claude", []string{"curl -fsSL https://claude.ai/install.sh | bash"}, "https://code.claude.com/docs/en/setup"
	case ClaudeDesktop:
		return "", []string{"https://claude.com/download"}, "https://claude.com/download"
	case Codex:
		return "codex", []string{"npm install -g @openai/codex"}, "https://developers.openai.com/codex/cli"
	case Grok:
		if runtime.GOOS == "windows" {
			return "grok", []string{"irm https://x.ai/cli/install.ps1 | iex"}, "https://docs.x.ai/build/overview"
		}
		return "grok", []string{"curl -fsSL https://x.ai/cli/install.sh | bash"}, "https://docs.x.ai/build/overview"
	case Gemini:
		how = []string{"npm install -g @google/gemini-cli"}
		if runtime.GOOS != "windows" {
			how = append(how, "brew install gemini-cli")
		}
		return "gemini", how, "https://geminicli.com/docs/get-started/installation/"
	default:
		return "", nil, ""
	}
}

// DefaultIDs is the Enter selection, shown as [1,2,3]: Codex / ChatGPT,
// Claude Code, and Claude Desktop.
func DefaultIDs() []ID {
	return []ID{Codex, ClaudeCode, ClaudeDesktop}
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
