// Package jsonedit updates the env object in a Claude Code settings.json
// document without rewriting the rest of the file.
package jsonedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

// KV is one string written into the env object.
// Delete removes the key instead of writing Value.
type KV struct {
	Key    string
	Value  string
	Delete bool
}

// SetEnv sets string keys on the top-level "env" object.
// An empty document becomes a new settings object.
// Keys already present keep their position; new keys are appended.
// A document that already contains a newline keeps every byte outside the
// env value. A single-line document is rewritten with indentation, still
// in the original key order.
func SetEnv(doc []byte, updates []KV) ([]byte, error) {
	for _, kv := range updates {
		if err := validKey(kv.Key); err != nil {
			return nil, err
		}
		if !utf8.ValidString(kv.Value) {
			return nil, fmt.Errorf("env value for %s is not valid UTF-8", kv.Key)
		}
	}
	bom := []byte{}
	if bytes.HasPrefix(doc, []byte{0xEF, 0xBB, 0xBF}) {
		bom = doc[:3]
		doc = doc[3:]
	}
	if len(bytes.TrimSpace(doc)) == 0 {
		out := []byte(renderFresh(updates))
		return append(append([]byte{}, bom...), out...), nil
	}
	// A single-line document is rewritten as indented JSON so a pretty env
	// object is not spliced into a compact file. A document that already
	// has a newline keeps the rest of the file as it was.

	compact := !bytes.Contains(bytes.TrimSpace(doc), []byte("\n"))
	p := parser{b: doc}
	p.skipWS()
	if p.eof() || p.b[p.i] != '{' {
		return nil, fmt.Errorf("claude settings must be a JSON object")
	}
	root, err := p.parseValue()
	if err != nil {
		return nil, fmt.Errorf("claude settings are not valid JSON: %w", err)
	}
	p.skipWS()
	if !p.eof() {
		return nil, fmt.Errorf("claude settings have trailing data after the JSON value")
	}
	if root.kind != kindObject {
		return nil, fmt.Errorf("claude settings must be a JSON object")
	}

	envIdx := -1
	for i, f := range root.fields {
		if f.key == "env" {
			if envIdx >= 0 {
				return nil, fmt.Errorf(`claude settings contain a duplicate "env" key`)
			}
			envIdx = i
		}
	}

	if envIdx == -1 {
		rendered := renderEnv(fieldsFromUpdates(nil, updates))
		out, err := insertField(doc, root, "env", rendered)
		if err != nil {
			return nil, err
		}
		return finishSettings(bom, out, compact)
	}

	envField := root.fields[envIdx]
	if envField.val.kind != kindObject {
		return nil, fmt.Errorf(`claude settings "env" must be a JSON object`)
	}
	merged, err := mergeEnv(envField.val.fields, updates)
	if err != nil {
		return nil, err
	}
	rendered := renderEnv(merged)
	out := splice(doc, envField.val.start, envField.val.end, rendered)
	return finishSettings(bom, out, compact)
}

func finishSettings(bom, out []byte, compact bool) ([]byte, error) {
	if compact {
		pretty, err := reindentDocument(out)
		if err != nil {
			return nil, err
		}
		out = pretty
	}
	return append(append([]byte{}, bom...), out...), nil
}

// reindentDocument pretty-prints objects and leaves arrays and literals as
// they were written, so key order stays the parser's order.
func reindentDocument(doc []byte) ([]byte, error) {
	p := parser{b: doc}
	p.skipWS()
	root, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	return []byte(prettyValue(doc, root, 0) + "\n"), nil
}

func prettyValue(src []byte, v value, indent int) string {
	if v.kind != kindObject {
		return string(src[v.start:v.end])
	}
	if len(v.fields) == 0 {
		return "{}"
	}
	pad := strings.Repeat("  ", indent+1)
	closePad := strings.Repeat("  ", indent)
	var b strings.Builder
	b.WriteString("{\n")
	for i, f := range v.fields {
		b.WriteString(pad)
		b.WriteString(mustJSONString(f.key))
		b.WriteString(": ")
		b.WriteString(prettyValue(src, f.val, indent+1))
		if i != len(v.fields)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(closePad)
	b.WriteByte('}')
	return b.String()
}

func validKey(k string) error {
	if k == "" || strings.ContainsAny(k, "\"\\\r\n") {
		return fmt.Errorf("invalid env key %q", k)
	}
	return nil
}

type renderedField struct {
	key string
	raw string
}

func fieldsFromUpdates(existing []renderedField, updates []KV) []renderedField {
	out := append([]renderedField{}, existing...)
	for _, kv := range updates {
		idx := -1
		for i, f := range out {
			if f.key == kv.Key {
				idx = i
				break
			}
		}
		if kv.Delete {
			if idx >= 0 {
				out = append(out[:idx], out[idx+1:]...)
			}
			continue
		}
		raw := mustJSONString(kv.Value)
		if idx >= 0 {
			out[idx].raw = raw
			continue
		}
		out = append(out, renderedField{key: kv.Key, raw: raw})
	}
	return out
}

func mergeEnv(fields []field, updates []KV) ([]renderedField, error) {
	seen := map[string]bool{}
	var existing []renderedField
	for _, f := range fields {
		if seen[f.key] {
			return nil, fmt.Errorf("duplicate key %q in env", f.key)
		}
		seen[f.key] = true
		existing = append(existing, renderedField{key: f.key, raw: strings.TrimSpace(f.raw)})
	}
	for _, kv := range updates {
		if kv.Delete {
			continue
		}
		for _, f := range fields {
			if f.key == kv.Key && f.val.kind != kindString {
				return nil, fmt.Errorf("env key %q is not a JSON string", kv.Key)
			}
		}
	}
	return fieldsFromUpdates(existing, updates), nil
}

func renderFresh(updates []KV) string {
	body := renderEnv(fieldsFromUpdates(nil, updates))
	return "{\n  \"env\": " + body + "\n}\n"
}

func renderEnv(fields []renderedField) string {
	if len(fields) == 0 {
		return "{}"
	}
	var b strings.Builder
	b.WriteString("{\n")
	for i, f := range fields {
		b.WriteString("    ")
		b.WriteString(mustJSONString(f.key))
		b.WriteString(": ")
		b.WriteString(f.raw)
		if i != len(fields)-1 {
			b.WriteString(",")
		}
		b.WriteString("\n")
	}
	b.WriteString("  }")
	return b.String()
}

func mustJSONString(s string) string {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(s); err != nil {
		return `""`
	}
	return strings.TrimSuffix(buf.String(), "\n")
}

func insertField(doc []byte, root value, key, rawValue string) ([]byte, error) {
	insertion := "\n  " + mustJSONString(key) + ": " + rawValue + "\n"
	if len(root.fields) == 0 {
		out := make([]byte, 0, len(doc)+len(insertion))
		out = append(out, doc[:root.start+1]...)
		out = append(out, insertion...)
		out = append(out, doc[root.close:]...)
		return out, nil
	}
	last := root.fields[len(root.fields)-1]
	mid := doc[last.val.end:root.close]
	prefix := append([]byte{}, doc[:last.val.end]...)
	if !bytes.Contains(mid, []byte{','}) {
		prefix = append(prefix, ',')
	}
	// Drop the old gap (whitespace, and a comma we already accounted for).
	out := append(prefix, insertion...)
	out = append(out, doc[root.close:]...)
	return out, nil
}

func splice(doc []byte, start, end int, repl string) []byte {
	out := make([]byte, 0, len(doc)+len(repl))
	out = append(out, doc[:start]...)
	out = append(out, repl...)
	out = append(out, doc[end:]...)
	return out
}

const (
	kindOther = iota
	kindObject
	kindString
)

type value struct {
	start, end int
	close      int
	kind       int
	fields     []field
}

type field struct {
	key string
	raw string
	val value
}

type parser struct {
	b []byte
	i int
}

func (p *parser) eof() bool { return p.i >= len(p.b) }

func (p *parser) skipWS() {
	for p.i < len(p.b) {
		switch p.b[p.i] {
		case ' ', '\n', '\r', '\t':
			p.i++
		default:
			return
		}
	}
}

func (p *parser) parseValue() (value, error) {
	p.skipWS()
	if p.eof() {
		return value{}, fmt.Errorf("unexpected end of JSON")
	}
	start := p.i
	switch p.b[p.i] {
	case '{':
		return p.parseObject(start)
	case '"':
		if err := p.consumeStringToken(); err != nil {
			return value{}, err
		}
		return value{start: start, end: p.i, kind: kindString, close: p.i}, nil
	case '[':
		if err := p.skipContainer('[', ']'); err != nil {
			return value{}, err
		}
		return value{start: start, end: p.i, kind: kindOther, close: p.i}, nil
	default:
		if err := p.skipLiteral(); err != nil {
			return value{}, err
		}
		return value{start: start, end: p.i, kind: kindOther, close: p.i}, nil
	}
}

func (p *parser) parseObject(start int) (value, error) {
	// caller is sitting on '{'
	p.i++
	var fields []field
	for {
		p.skipWS()
		if p.eof() {
			return value{}, fmt.Errorf("unclosed object")
		}
		if p.b[p.i] == '}' {
			closeIdx := p.i
			p.i++
			return value{start: start, end: p.i, close: closeIdx, kind: kindObject, fields: fields}, nil
		}
		if len(fields) > 0 {
			if p.b[p.i] != ',' {
				return value{}, fmt.Errorf("expected comma in object")
			}
			p.i++
			p.skipWS()
			if p.eof() {
				return value{}, fmt.Errorf("unclosed object")
			}
			if p.b[p.i] == '}' {
				return value{}, fmt.Errorf("trailing comma in object")
			}
		}
		if p.b[p.i] != '"' {
			return value{}, fmt.Errorf("expected object key")
		}
		keyStart := p.i
		if err := p.consumeStringToken(); err != nil {
			return value{}, err
		}
		keyRaw := p.b[keyStart:p.i]
		key, err := unquote(keyRaw)
		if err != nil {
			return value{}, err
		}
		p.skipWS()
		if p.eof() || p.b[p.i] != ':' {
			return value{}, fmt.Errorf("expected colon after key %q", key)
		}
		p.i++
		val, err := p.parseValue()
		if err != nil {
			return value{}, err
		}
		fields = append(fields, field{
			key: key,
			raw: string(p.b[val.start:val.end]),
			val: val,
		})
	}
}

func (p *parser) consumeStringToken() error {
	if p.eof() || p.b[p.i] != '"' {
		return fmt.Errorf("expected string")
	}
	p.i++
	for p.i < len(p.b) {
		c := p.b[p.i]
		if c == '"' {
			p.i++
			return nil
		}
		if c == '\\' {
			p.i++
			if p.i >= len(p.b) {
				return fmt.Errorf("unterminated escape")
			}
			if p.b[p.i] == 'u' {
				p.i++
				for n := 0; n < 4; n++ {
					if p.i >= len(p.b) || !isHex(p.b[p.i]) {
						return fmt.Errorf("bad unicode escape")
					}
					p.i++
				}
				continue
			}
			p.i++
			continue
		}
		if c < 0x20 {
			return fmt.Errorf("raw control character in string")
		}
		p.i++
	}
	return fmt.Errorf("unterminated string")
}

func (p *parser) skipContainer(open, close byte) error {
	if p.eof() || p.b[p.i] != open {
		return fmt.Errorf("expected container")
	}
	p.i++
	depth := 1
	inStr := false
	esc := false
	for p.i < len(p.b) {
		c := p.b[p.i]
		p.i++
		if inStr {
			if esc {
				esc = false
				continue
			}
			if c == '\\' {
				esc = true
				continue
			}
			if c == '"' {
				inStr = false
			}
			continue
		}
		switch c {
		case '"':
			inStr = true
		case open:
			depth++
		case close:
			depth--
			if depth == 0 {
				return nil
			}
		}
	}
	return fmt.Errorf("unclosed container")
}

func (p *parser) skipLiteral() error {
	if p.eof() {
		return fmt.Errorf("unexpected end of JSON")
	}
	for p.i < len(p.b) {
		c := p.b[p.i]
		if c == ',' || c == '}' || c == ']' || c == ' ' || c == '\n' || c == '\r' || c == '\t' {
			break
		}
		p.i++
	}
	if p.i == 0 {
		return fmt.Errorf("invalid JSON literal")
	}
	return nil
}

func unquote(raw []byte) (string, error) {
	var s string
	if err := json.Unmarshal(raw, &s); err != nil {
		return "", fmt.Errorf("invalid JSON string: %w", err)
	}
	return s, nil
}

func isHex(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}
