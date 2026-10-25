// Package codex edits ~/.codex/config.toml without discarding the rest of the file.
package codex

import (
	"fmt"
	"strings"
)

const (
	// ProviderID is the model_providers table this tool owns.
	ProviderID = "deeprouter"
	// EnvKey is exported from the shell startup file for compatibility.
	// The deeprouter provider does not set env_key: Codex returns
	// "Missing environment variable" when that variable is unset, and it
	// does that before it reads experimental_bearer_token.
	EnvKey = "DEEPROUTER_API_KEY"
)

// Update is the provider entry written into config.toml.
type Update struct {
	Model         string // empty leaves the root model key unchanged
	ProviderLabel string
	BaseURL       string // OpenAI-style base, including /v1
	WireAPI       string
	// BearerToken is written as experimental_bearer_token so Codex can
	// authenticate without DEEPROUTER_API_KEY in the process environment.
	BearerToken string
}

// Apply merges Update into a Codex config document.
// model and model_provider stay in the root table, before the first [table],
// which is where Codex reads them.
func Apply(content string, upd Update) (string, error) {
	if strings.TrimSpace(upd.BaseURL) == "" {
		return "", fmt.Errorf("codex base URL is empty")
	}
	if strings.TrimSpace(upd.BearerToken) == "" {
		return "", fmt.Errorf("codex bearer token is empty")
	}
	if upd.WireAPI == "" {
		upd.WireAPI = "responses"
	}
	if upd.ProviderLabel == "" {
		upd.ProviderLabel = "DeepRouter"
	}
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}

	lines := parse(content, nl)
	providerHeader := "model_providers." + ProviderID

	seenRoot := map[string]bool{}
	seenProvider := map[string]bool{}
	providerStart, providerEnd := -1, -1
	for i, ln := range lines {
		if ln.kind == kindHeader && ln.section == providerHeader {
			providerStart = i
			providerEnd = len(lines)
			for j := i + 1; j < len(lines); j++ {
				if lines[j].kind == kindHeader {
					providerEnd = j
					break
				}
			}
			break
		}
	}

	for i := range lines {
		ln := &lines[i]
		if ln.section == "" && ln.kind == kindAssign && !seenRoot[ln.key] {
			switch ln.key {
			case "model":
				if upd.Model != "" {
					ln.raw = ln.indent + "model = " + tomlString(upd.Model)
				}
				seenRoot["model"] = true
			case "model_provider":
				ln.raw = ln.indent + "model_provider = " + tomlString(ProviderID)
				seenRoot["model_provider"] = true
			}
		}
		if providerStart >= 0 && i > providerStart && i < providerEnd && ln.kind == kindAssign {
			// Any env_key in this provider makes Codex require the variable
			// and skip experimental_bearer_token. Drop every copy.
			if ln.key == "env_key" {
				ln.skip = true
				continue
			}
			if seenProvider[ln.key] && managedProviderKey(ln.key) {
				ln.skip = true
				continue
			}
			if !managedProviderKey(ln.key) {
				continue
			}
			seenProvider[ln.key] = true
			switch ln.key {
			case "name":
				ln.raw = ln.indent + "name = " + tomlString(upd.ProviderLabel)
			case "base_url":
				ln.raw = ln.indent + "base_url = " + tomlString(upd.BaseURL)
			case "experimental_bearer_token":
				ln.raw = ln.indent + "experimental_bearer_token = " + tomlString(upd.BearerToken)
			case "wire_api":
				ln.raw = ln.indent + "wire_api = " + tomlString(upd.WireAPI)
			case "requires_openai_auth":
				ln.raw = ln.indent + "requires_openai_auth = false"
			}
		}
	}

	var rootInserts []string
	if upd.Model != "" && !seenRoot["model"] {
		rootInserts = append(rootInserts, "model = "+tomlString(upd.Model))
	}
	if !seenRoot["model_provider"] {
		rootInserts = append(rootInserts, "model_provider = "+tomlString(ProviderID))
	}

	out := make([]string, 0, len(lines)+8)
	insertedRoot := false
	anchor := lastNonBlankRoot(lines)
	for i, ln := range lines {
		if ln.skip {
			continue
		}
		out = append(out, ln.raw)
		if !insertedRoot && i == anchor {
			out = append(out, rootInserts...)
			insertedRoot = true
		}
	}
	if !insertedRoot {
		out = append(rootInserts, out...)
	}

	missing := missingProviderKeys(upd, seenProvider)
	if providerStart < 0 {
		if len(out) > 0 && strings.TrimSpace(out[len(out)-1]) != "" {
			out = append(out, "")
		}
		out = append(out, "["+providerHeader+"]")
		out = append(out, missing...)
	} else if len(missing) > 0 {
		out = insertAfterSection(out, providerHeader, missing)
	}

	text := strings.Join(out, nl)
	if text != "" && !strings.HasSuffix(text, nl) {
		text += nl
	}
	return text, nil
}

func missingProviderKeys(upd Update, seen map[string]bool) []string {
	var missing []string
	if !seen["name"] {
		missing = append(missing, "name = "+tomlString(upd.ProviderLabel))
	}
	if !seen["base_url"] {
		missing = append(missing, "base_url = "+tomlString(upd.BaseURL))
	}
	if !seen["experimental_bearer_token"] {
		missing = append(missing, "experimental_bearer_token = "+tomlString(upd.BearerToken))
	}
	if !seen["wire_api"] {
		missing = append(missing, "wire_api = "+tomlString(upd.WireAPI))
	}
	if !seen["requires_openai_auth"] {
		missing = append(missing, "requires_openai_auth = false")
	}
	return missing
}

func insertAfterSection(out []string, header string, missing []string) []string {
	start := -1
	for i, ln := range out {
		if name, ok := headerName(ln); ok && name == header {
			start = i
			break
		}
	}
	if start < 0 {
		return append(out, missing...)
	}
	end := len(out)
	for i := start + 1; i < len(out); i++ {
		if _, ok := headerName(out[i]); ok {
			end = i
			break
		}
	}
	at := end
	for i := end - 1; i > start; i-- {
		if strings.TrimSpace(out[i]) != "" {
			at = i + 1
			break
		}
	}
	next := make([]string, 0, len(out)+len(missing))
	next = append(next, out[:at]...)
	next = append(next, missing...)
	next = append(next, out[at:]...)
	return next
}

func lastNonBlankRoot(lines []line) int {
	last := -1
	for i, ln := range lines {
		if ln.kind == kindHeader {
			break
		}
		if strings.TrimSpace(ln.raw) != "" {
			last = i
		}
	}
	return last
}

type lineKind int

const (
	kindRaw lineKind = iota
	kindHeader
	kindAssign
)

type line struct {
	raw     string
	kind    lineKind
	key     string
	indent  string
	section string
	skip    bool
}

func parse(content, nl string) []line {
	if content == "" {
		return nil
	}
	parts := strings.Split(content, nl)
	if len(parts) > 0 && parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	lines := make([]line, len(parts))
	section := ""
	inML := byte(0)
	for i, raw := range parts {
		lines[i].raw = raw
		if inML != 0 {
			if strings.Contains(raw, strings.Repeat(string(inML), 3)) {
				inML = 0
			}
			lines[i].section = section
			continue
		}
		if q, open := opensMultiline(raw); open {
			inML = q
			lines[i].section = section
			continue
		}
		if name, ok := headerName(raw); ok {
			section = name
			lines[i].kind = kindHeader
			lines[i].section = section
			continue
		}
		lines[i].section = section
		if key, indent, ok := assignment(raw); ok {
			lines[i].kind = kindAssign
			lines[i].key = key
			lines[i].indent = indent
		}
	}
	return lines
}

func opensMultiline(raw string) (byte, bool) {
	if strings.Count(raw, `"""`)%2 == 1 {
		return '"', true
	}
	if strings.Count(raw, `'''`)%2 == 1 {
		return '\'', true
	}
	return 0, false
}

func assignment(raw string) (key, indent string, ok bool) {
	code := stripComment(raw)
	trim := strings.TrimSpace(code)
	if trim == "" || strings.HasPrefix(trim, "[") {
		return "", "", false
	}
	eq := strings.Index(code, "=")
	if eq < 0 {
		return "", "", false
	}
	left := code[:eq]
	key = strings.TrimSpace(left)
	if key == "" || strings.ContainsAny(key, " \t.[]\"'") {
		return "", "", false
	}
	indentLen := len(left) - len(strings.TrimLeft(left, " \t"))
	return key, left[:indentLen], true
}

func headerName(raw string) (string, bool) {
	code := strings.TrimSpace(stripComment(raw))
	if code == "" || !strings.HasPrefix(code, "[") || !strings.HasSuffix(code, "]") {
		return "", false
	}
	inner := strings.TrimSpace(code[1 : len(code)-1])
	inner = strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(inner, "["), "]"))
	if inner == "" {
		return "", false
	}
	return canonical(inner), true
}

func canonical(header string) string {
	header = strings.ReplaceAll(header, "\"", "")
	header = strings.ReplaceAll(header, "'", "")
	header = strings.ReplaceAll(header, " ", "")
	return header
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
			continue
		}
		if c == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if c == '#' && !inSingle && !inDouble {
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
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04X`, r)
			} else {
				b.WriteRune(r)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func managedProviderKey(k string) bool {
	switch k {
	case "name", "base_url", "experimental_bearer_token", "wire_api", "requires_openai_auth":
		return true
	default:
		return false
	}
}

// Auth is how the deeprouter provider authenticates, as written in config.toml.
// Codex uses experimental_bearer_token only when env_key is absent.
type Auth struct {
	BearerToken string
	HasBearer   bool
	EnvKey      string
}

// ReadAuth finds the deeprouter provider's credential lines.
func ReadAuth(content string) Auth {
	nl := "\n"
	if strings.Contains(content, "\r\n") {
		nl = "\r\n"
	}
	var auth Auth
	in := false
	for _, ln := range parse(content, nl) {
		if ln.kind == kindHeader {
			in = ln.section == "model_providers."+ProviderID
			continue
		}
		if !in || ln.kind != kindAssign {
			continue
		}
		switch ln.key {
		case "experimental_bearer_token":
			if v, ok := quotedValue(ln.raw); ok {
				auth.BearerToken = v
				auth.HasBearer = true
			}
		case "env_key":
			if v, ok := quotedValue(ln.raw); ok && v != "" {
				auth.EnvKey = v
			} else {
				auth.EnvKey = EnvKey
			}
		}
	}
	return auth
}

// KeyReady reports whether Codex can authenticate from this document alone.
// want is the profile key. A matching bearer token with no env_key is ready.
func KeyReady(content, want string) bool {
	auth := ReadAuth(content)
	return auth.EnvKey == "" && auth.HasBearer && auth.BearerToken != "" && auth.BearerToken == want
}

func quotedValue(raw string) (string, bool) {
	code := strings.TrimSpace(stripComment(raw))
	eq := strings.Index(code, "=")
	if eq < 0 {
		return "", false
	}
	rest := strings.TrimSpace(code[eq+1:])
	if rest == "" {
		return "", false
	}
	if rest[0] == '"' {
		var b strings.Builder
		for i := 1; i < len(rest); i++ {
			c := rest[i]
			if c == '\\' && i+1 < len(rest) {
				i++
				switch rest[i] {
				case '\\', '"':
					b.WriteByte(rest[i])
				case 'n':
					b.WriteByte('\n')
				case 'r':
					b.WriteByte('\r')
				case 't':
					b.WriteByte('\t')
				case 'u':
					if i+4 >= len(rest) {
						return "", false
					}
					var r rune
					if _, err := fmt.Sscanf(rest[i+1:i+5], "%04X", &r); err != nil {
						return "", false
					}
					b.WriteRune(r)
					i += 4
				default:
					b.WriteByte(rest[i])
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
	if rest[0] == '\'' {
		end := strings.IndexByte(rest[1:], '\'')
		if end < 0 {
			return "", false
		}
		return rest[1 : 1+end], true
	}
	return strings.Fields(rest)[0], true
}
