// Package shellrc edits a marked block in a shell startup file.
package shellrc

import (
	"fmt"
	"strings"
)

const (
	// Begin marks the first line of the managed block.
	Begin = "# >>> deeprouter-setup >>>"
	// End marks the last line of the managed block.
	End = "# <<< deeprouter-setup <<<"
)

// Pair is one environment variable in the managed block.
// Drop removes the key. An empty Value that is not Drop leaves an existing key.
type Pair struct {
	Key   string
	Value string
	Drop  bool
}

// KnownKeys is the stable order written into the block.
var KnownKeys = []string{
	"ANTHROPIC_BASE_URL",
	"ANTHROPIC_AUTH_TOKEN",
	"ANTHROPIC_API_KEY",
	"ANTHROPIC_MODEL",
	"DEEPROUTER_API_KEY",
	"GOOGLE_GEMINI_BASE_URL",
	"GEMINI_API_KEY",
}

// Upsert inserts or replaces the managed block.
// Updates overwrite keys. An empty value leaves an existing key unchanged.
// Keys already in the block and not mentioned in updates are kept.
func Upsert(content, shell string, updates []Pair) (string, error) {
	nl := newline(content)
	parsed, start, end, found, err := findBlock(content)
	if err != nil {
		return "", err
	}
	merged := map[string]string{}
	var extras []string
	var rawLines []string
	for _, item := range parsed {
		switch item.kind {
		case itemKV:
			merged[item.key] = item.value
			if !isKnown(item.key) {
				extras = append(extras, item.key)
			}
		case itemRaw:
			rawLines = append(rawLines, item.raw)
		}
	}
	for _, u := range updates {
		if u.Drop {
			delete(merged, u.Key)
			extras = removeStr(extras, u.Key)
			continue
		}
		if u.Value == "" {
			continue
		}
		merged[u.Key] = u.Value
		if !isKnown(u.Key) && !contains(extras, u.Key) {
			extras = append(extras, u.Key)
		}
	}
	block := render(shell, merged, extras, rawLines, nl)
	if !found {
		base := content
		if base != "" && !strings.HasSuffix(base, nl) {
			base += nl
		}
		if base != "" && !strings.HasSuffix(base, nl+nl) {
			base += nl
		}
		return base + block, nil
	}
	prefix := content[:start]
	suffix := content[end:]
	if prefix != "" && !strings.HasSuffix(prefix, nl) {
		prefix += nl
	}
	return prefix + block + suffix, nil
}

// Remove deletes the managed block. It is a no-op when the block is absent.
func Remove(content string) (string, error) {
	_, start, end, found, err := findBlock(content)
	if err != nil || !found {
		return content, err
	}
	nl := newline(content)
	out := content[:start] + content[end:]
	// Collapse the blank line the installer left around the block.
	for strings.Contains(out, nl+nl+nl) {
		out = strings.ReplaceAll(out, nl+nl+nl, nl+nl)
	}
	return out, nil
}

// HasBlock reports whether the managed markers are present.
func HasBlock(content string) bool {
	return strings.Contains(content, Begin) && strings.Contains(content, End)
}

// Lookup returns the value of key inside the managed block.
// When the block lists the key more than once, the last value wins.
func Lookup(content, key string) (string, bool) {
	items, _, _, found, err := findBlock(content)
	if err != nil || !found {
		return "", false
	}
	var value string
	ok := false
	for _, item := range items {
		if item.kind == itemKV && item.key == key {
			value = item.value
			ok = true
		}
	}
	return value, ok
}

type itemKind int

const (
	itemKV itemKind = iota
	itemRaw
)

type item struct {
	kind  itemKind
	key   string
	value string
	raw   string
}

func findBlock(content string) ([]item, int, int, bool, error) {
	start := strings.Index(content, Begin)
	if start < 0 {
		if strings.Contains(content, End) {
			return nil, 0, 0, false, fmt.Errorf("shell startup file has a closing deeprouter-setup marker without an opening one")
		}
		return nil, 0, 0, false, nil
	}
	rest := content[start+len(Begin):]
	rel := strings.Index(rest, End)
	if rel < 0 {
		return nil, 0, 0, false, fmt.Errorf("shell startup file has an opening deeprouter-setup marker without a closing one")
	}
	end := start + len(Begin) + rel + len(End)
	// Include a single trailing newline in the replaced region so we don't pile blanks.
	if end < len(content) && content[end] == '\r' {
		end++
	}
	if end < len(content) && content[end] == '\n' {
		end++
	}
	body := content[start+len(Begin) : start+len(Begin)+rel]
	body = strings.Trim(body, "\r\n")
	var items []item
	if body != "" {
		for _, line := range splitLines(body) {
			trim := strings.TrimSpace(line)
			if trim == "" || strings.HasPrefix(trim, "#") {
				continue
			}
			if key, val, ok := parseAssign(trim); ok {
				items = append(items, item{kind: itemKV, key: key, value: val})
				continue
			}
			items = append(items, item{kind: itemRaw, raw: trim})
		}
	}
	if later := strings.Index(content[end:], Begin); later >= 0 {
		return nil, 0, 0, false, fmt.Errorf("shell startup file contains more than one deeprouter-setup block")
	}
	return items, start, end, true, nil
}

func parseAssign(line string) (string, string, bool) {
	switch {
	case strings.HasPrefix(line, "export "):
		rest := strings.TrimSpace(strings.TrimPrefix(line, "export "))
		key, val, ok := splitKeyVal(rest, "=")
		if !ok {
			return "", "", false
		}
		parsed, err := unquoteShell(val)
		if err != nil {
			return "", "", false
		}
		return key, parsed, true
	case strings.HasPrefix(line, "set "):
		fields := strings.Fields(line)
		// set -gx KEY value
		if len(fields) < 4 || fields[1] != "-gx" {
			return "", "", false
		}
		key := fields[2]
		val := strings.TrimSpace(line[strings.Index(line, key)+len(key):])
		parsed, err := unquoteShell(val)
		if err != nil {
			return "", "", false
		}
		return key, parsed, true
	case strings.HasPrefix(line, "$env:"):
		rest := strings.TrimPrefix(line, "$env:")
		key, val, ok := splitKeyVal(rest, "=")
		if !ok {
			return "", "", false
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		parsed, err := unquotePowerShell(val)
		if err != nil {
			return "", "", false
		}
		return key, parsed, true
	default:
		return "", "", false
	}
}

func splitKeyVal(s, sep string) (string, string, bool) {
	i := strings.Index(s, sep)
	if i <= 0 {
		return "", "", false
	}
	return strings.TrimSpace(s[:i]), strings.TrimSpace(s[i+len(sep):]), true
}

func render(shell string, values map[string]string, extras, raw []string, nl string) string {
	var keys []string
	for _, k := range KnownKeys {
		if _, ok := values[k]; ok {
			keys = append(keys, k)
		}
	}
	for _, k := range extras {
		if _, ok := values[k]; ok {
			keys = append(keys, k)
		}
	}
	var b strings.Builder
	b.WriteString(Begin)
	b.WriteString(nl)
	b.WriteString("# Managed by deeprouter. Run deeprouter again to change it.")
	b.WriteString(nl)
	for _, k := range keys {
		b.WriteString(assign(shell, k, values[k]))
		b.WriteString(nl)
	}
	for _, line := range raw {
		b.WriteString(line)
		b.WriteString(nl)
	}
	b.WriteString(End)
	b.WriteString(nl)
	return b.String()
}

func assign(shell, key, value string) string {
	switch shell {
	case "fish":
		return "set -gx " + key + " " + quoteShell(value)
	case "powershell":
		return "$env:" + key + " = " + quotePowerShell(value)
	default:
		return "export " + key + "=" + quoteShell(value)
	}
}

func quoteShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'"'"'`) + "'"
}

func quotePowerShell(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}

func unquoteShell(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", fmt.Errorf("empty")
	}
	if s[0] != '\'' {
		if s[0] == '"' {
			return unquoteDouble(s)
		}
		if strings.ContainsAny(s, " \t") {
			return "", fmt.Errorf("bare value")
		}
		return s, nil
	}
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] != '\'' {
			return "", fmt.Errorf("expected quote")
		}
		i++
		for i < len(s) && s[i] != '\'' {
			b.WriteByte(s[i])
			i++
		}
		if i >= len(s) {
			return "", fmt.Errorf("unterminated quote")
		}
		i++
		if i >= len(s) {
			break
		}
		if strings.HasPrefix(s[i:], `"'"`) {
			b.WriteByte('\'')
			i += 3
			continue
		}
		return "", fmt.Errorf("unexpected trailing data")
	}
	return b.String(), nil
}

func unquoteDouble(s string) (string, error) {
	if len(s) < 2 || s[0] != '"' || s[len(s)-1] != '"' {
		return "", fmt.Errorf("bad double quote")
	}
	var b strings.Builder
	esc := false
	for i := 1; i < len(s)-1; i++ {
		c := s[i]
		if esc {
			b.WriteByte(c)
			esc = false
			continue
		}
		if c == '\\' {
			esc = true
			continue
		}
		b.WriteByte(c)
	}
	return b.String(), nil
}

func unquotePowerShell(s string) (string, error) {
	s = strings.TrimSpace(s)
	if len(s) >= 2 && s[0] == '\'' && s[len(s)-1] == '\'' {
		inner := s[1 : len(s)-1]
		return strings.ReplaceAll(inner, "''", "'"), nil
	}
	if len(s) >= 2 && s[0] == '"' && s[len(s)-1] == '"' {
		return s[1 : len(s)-1], nil
	}
	return s, nil
}

func newline(content string) string {
	if strings.Contains(content, "\r\n") {
		return "\r\n"
	}
	return "\n"
}

func splitLines(s string) []string {
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.Split(s, "\n")
}

func isKnown(k string) bool {
	for _, known := range KnownKeys {
		if known == k {
			return true
		}
	}
	return false
}

func removeStr(ss []string, s string) []string {
	var out []string
	for _, item := range ss {
		if item != s {
			out = append(out, item)
		}
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, item := range ss {
		if item == s {
			return true
		}
	}
	return false
}
