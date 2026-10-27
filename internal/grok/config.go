// Package grok edits the user config Grok Build reads.
//
// Grok Build stores personal defaults in ~/.grok/config.toml
// ($GROK_HOME/config.toml, or %USERPROFILE%\.grok\config.toml on Windows).
// A custom model is a [model.<id>] table with base_url (OpenAI-style, including
// /v1) and api_key. api_key is used before env_key, so the desktop-launched
// CLI does not need the key in the shell environment.
// https://docs.x.ai/build/overview
// https://docs.x.ai/build/settings
package grok

import (
	"fmt"
	"strings"

	"github.com/zfg5118/deeprouter/internal/provider"
)

const (
	// SectionID is the default [model.<id>] this tool owns.
	SectionID  = "deeprouter"
	modelsHead = "models"
)

// Update is the custom model written for the gateway.
type Update struct {
	Model      string
	ClearModel bool   // omit the model id so Grok Build uses its own default
	BaseURL    string // OpenAI-style base, including /v1
	APIKey     string
	Name       string
	APIBackend string // empty becomes chat_completions
	// SectionID is the [model.<id>] table to write. Empty means deeprouter.
	// Other managed model tables are removed. Unrelated [model.*] tables stay.
	SectionID string
}

// Apply merges Update into a Grok config document.
// Unrelated tables, including other [model.*] entries this tool does not own,
// are left in place. A second call with the same values does not duplicate keys.
func Apply(content string, upd Update) (string, error) {
	if strings.TrimSpace(upd.BaseURL) == "" {
		return "", fmt.Errorf("grok base URL is empty")
	}
	if strings.TrimSpace(upd.APIKey) == "" {
		return "", fmt.Errorf("grok API key is empty")
	}
	id := sectionID(upd)
	head := "model." + id
	if !upd.ClearModel && strings.TrimSpace(upd.Model) == "" {
		upd.Model = readFieldIn(content, head, "model")
	}
	if !upd.ClearModel && strings.TrimSpace(upd.Model) == "" {
		upd.Model = readManagedField(content, "model")
	}
	if !upd.ClearModel && strings.TrimSpace(upd.Model) == "" {
		return "", fmt.Errorf("grok model id is empty")
	}
	if upd.Name == "" {
		upd.Name = "DeepRouter"
	}
	if upd.APIBackend == "" {
		upd.APIBackend = "chat_completions"
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	lines := split(content, nl)
	lines = dropManagedModels(lines, id)
	lines = upsertAssign(lines, modelsHead, "default", tomlString(id), nl)
	want := map[string]string{
		"base_url":    tomlString(upd.BaseURL),
		"name":        tomlString(upd.Name),
		"api_key":     tomlString(upd.APIKey),
		"api_backend": tomlString(upd.APIBackend),
	}
	if !upd.ClearModel {
		want["model"] = tomlString(upd.Model)
	}
	lines = upsertSection(lines, head, want, upd.ClearModel, nl)
	return join(lines, nl), nil
}

func sectionID(upd Update) string {
	id := strings.ToLower(strings.TrimSpace(upd.SectionID))
	if id == "" {
		return SectionID
	}
	return id
}

// ReadAPIKey returns the api_key of the managed model section in use.
func ReadAPIKey(content string) string {
	return readManagedField(content, "api_key")
}

// ReadBaseURL returns the base_url of the managed model section in use.
func ReadBaseURL(content string) string {
	return readManagedField(content, "base_url")
}

// ActiveSection is the managed [model.<id>] ReadAPIKey reads.
// Empty means no managed model section stores an api_key. A key from
// another id must not be reused.
func ActiveSection(content string) string {
	def := readFieldIn(content, modelsHead, "default")
	if provider.IsManaged(def) && readFieldIn(content, "model."+def, "api_key") != "" {
		return strings.ToLower(strings.TrimSpace(def))
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	section := ""
	for _, raw := range split(content, nl) {
		if name, ok := headerName(raw); ok {
			section = name
			continue
		}
		if !strings.HasPrefix(section, "model.") {
			continue
		}
		id := strings.TrimPrefix(section, "model.")
		if strings.Contains(id, ".") || !provider.IsManaged(id) {
			continue
		}
		got, val, ok := assignment(raw)
		if ok && got == "api_key" && val != "" {
			return strings.ToLower(id)
		}
	}
	return ""
}

func readFieldIn(content, section, key string) string {
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	cur := ""
	for _, raw := range split(content, nl) {
		if name, ok := headerName(raw); ok {
			cur = name
			continue
		}
		if cur != section {
			continue
		}
		got, val, ok := assignment(raw)
		if ok && got == key {
			return val
		}
	}
	return ""
}

func readManagedField(content, key string) string {
	def := readFieldIn(content, modelsHead, "default")
	if provider.IsManaged(def) {
		if v := readFieldIn(content, "model."+def, key); v != "" {
			return v
		}
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	section := ""
	for _, raw := range split(content, nl) {
		if name, ok := headerName(raw); ok {
			section = name
			continue
		}
		if !strings.HasPrefix(section, "model.") {
			continue
		}
		id := strings.TrimPrefix(section, "model.")
		if strings.Contains(id, ".") || !provider.IsManaged(id) {
			continue
		}
		got, val, ok := assignment(raw)
		if ok && got == key && val != "" {
			return val
		}
	}
	return ""
}

func dropManagedModels(lines []string, keep string) []string {
	var out []string
	skip := false
	for _, raw := range lines {
		if name, ok := headerName(raw); ok {
			skip = foreignModel(name, keep)
		}
		if skip {
			continue
		}
		out = append(out, raw)
	}
	return out
}

func foreignModel(header, keep string) bool {
	const prefix = "model."
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	id := strings.TrimPrefix(header, prefix)
	if id == keep || strings.Contains(id, ".") {
		return false
	}
	return provider.IsManaged(id)
}

func upsertAssign(lines []string, section, key, quoted, nl string) []string {
	start, end := findSection(lines, section)
	if start < 0 {
		block := []string{"[" + section + "]", key + " = " + quoted, ""}
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, block...)
	}
	seen := false
	for i := start + 1; i < end; i++ {
		k, _, ok := assignment(lines[i])
		if !ok || k != key {
			continue
		}
		if seen {
			lines[i] = ""
			continue
		}
		indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
		lines[i] = indent + key + " = " + quoted
		seen = true
	}
	if !seen {
		lines = insert(lines, end, key+" = "+quoted)
	}
	_ = nl
	return lines
}

func upsertSection(lines []string, section string, want map[string]string, dropModel bool, nl string) []string {
	order := []string{"model", "base_url", "name", "api_key", "api_backend"}
	start, end := findSection(lines, section)
	if start < 0 {
		var block []string
		block = append(block, "["+section+"]")
		for _, key := range order {
			quoted, ok := want[key]
			if !ok {
				continue
			}
			block = append(block, key+" = "+quoted)
		}
		block = append(block, "")
		if len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) != "" {
			lines = append(lines, "")
		}
		return append(lines, block...)
	}
	seen := map[string]bool{}
	for i := start + 1; i < end; i++ {
		key, _, ok := assignment(lines[i])
		if !ok {
			continue
		}
		if key == "env_key" || (dropModel && key == "model") {
			lines[i] = ""
			continue
		}
		quoted, managed := want[key]
		if !managed {
			continue
		}
		if seen[key] {
			lines[i] = ""
			continue
		}
		indent := lines[i][:len(lines[i])-len(strings.TrimLeft(lines[i], " \t"))]
		lines[i] = indent + key + " = " + quoted
		seen[key] = true
	}
	for _, key := range order {
		if seen[key] {
			continue
		}
		quoted, ok := want[key]
		if !ok {
			continue
		}
		lines = insert(lines, end, key+" = "+quoted)
		end++
	}
	_ = nl
	return lines
}

func findSection(lines []string, name string) (start, end int) {
	start = -1
	for i, raw := range lines {
		header, ok := headerName(raw)
		if !ok {
			continue
		}
		if header == name && start < 0 {
			start = i
			continue
		}
		if start >= 0 {
			return start, i
		}
	}
	if start >= 0 {
		return start, len(lines)
	}
	return -1, -1
}

func insert(lines []string, at int, line string) []string {
	if at >= len(lines) {
		return append(lines, line)
	}
	lines = append(lines, "")
	copy(lines[at+1:], lines[at:])
	lines[at] = line
	return lines
}

func split(content, nl string) []string {
	if content == "" {
		return nil
	}
	parts := strings.Split(content, nl)
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func join(lines []string, nl string) string {
	var kept []string
	blank := false
	for _, line := range lines {
		if strings.TrimSpace(line) == "" && line == "" {
			if blank {
				continue
			}
			blank = true
			kept = append(kept, "")
			continue
		}
		blank = strings.TrimSpace(line) == ""
		kept = append(kept, line)
	}
	out := strings.Join(kept, nl)
	if !strings.HasSuffix(out, nl) {
		out += nl
	}
	return out
}

func headerName(raw string) (string, bool) {
	code := strings.TrimSpace(stripComment(raw))
	if !strings.HasPrefix(code, "[") || !strings.HasSuffix(code, "]") {
		return "", false
	}
	inner := strings.TrimSpace(code[1 : len(code)-1])
	inner = strings.Trim(inner, "\"'")
	inner = strings.ReplaceAll(inner, "\"", "")
	inner = strings.ReplaceAll(inner, " ", "")
	if inner == "" {
		return "", false
	}
	return inner, true
}

func assignment(raw string) (key, value string, ok bool) {
	code := strings.TrimSpace(stripComment(raw))
	if code == "" || strings.HasPrefix(code, "[") {
		return "", "", false
	}
	eq := strings.Index(code, "=")
	if eq < 0 {
		return "", "", false
	}
	key = strings.TrimSpace(code[:eq])
	if key == "" || strings.ContainsAny(key, " \t.[]") {
		return "", "", false
	}
	value, ok = unquote(strings.TrimSpace(code[eq+1:]))
	return key, value, ok
}

func unquote(s string) (string, bool) {
	if len(s) < 2 || s[0] != '"' {
		return s, true
	}
	var b strings.Builder
	for i := 1; i < len(s); i++ {
		c := s[i]
		if c == '\\' && i+1 < len(s) {
			i++
			switch s[i] {
			case '\\', '"':
				b.WriteByte(s[i])
			case 'n':
				b.WriteByte('\n')
			default:
				b.WriteByte(s[i])
			}
			continue
		}
		if c == '"' {
			return b.String(), true
		}
		b.WriteByte(c)
	}
	return "", false
}

func stripComment(line string) string {
	inSingle, inDouble := false, false
	for i := 0; i < len(line); i++ {
		c := line[i]
		if c == '\\' && inDouble && i+1 < len(line) {
			i++
			continue
		}
		if c == '\'' && !inDouble {
			inSingle = !inSingle
		} else if c == '"' && !inSingle {
			inDouble = !inDouble
		} else if c == '#' && !inSingle && !inDouble {
			return line[:i]
		}
	}
	return line
}

func tomlString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteRune(r)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}
