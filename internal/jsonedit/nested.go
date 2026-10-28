package jsonedit

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"
)

// Nested is one string written at an object path.
// Delete removes the last key and leaves the parent object in place.
// Missing parent objects are created. New keys are appended.
type Nested struct {
	Path   []string
	Value  string
	Delete bool
}

// SetNested sets or deletes strings at object paths.
// Key order is the document's order. Arrays and other non-object values
// stay as they were written. The result uses the file's indent: two spaces,
// four spaces, or a tab. A single-line document is written with two spaces.
func SetNested(doc []byte, updates []Nested) ([]byte, error) {
	for _, upd := range updates {
		if len(upd.Path) == 0 {
			return nil, fmt.Errorf("empty settings path")
		}
		for _, key := range upd.Path {
			if err := validKey(key); err != nil {
				return nil, err
			}
		}
		if !upd.Delete && !utf8.ValidString(upd.Value) {
			return nil, fmt.Errorf("settings value for %s is not valid UTF-8", strings.Join(upd.Path, "."))
		}
	}
	bom := []byte{}
	if bytes.HasPrefix(doc, []byte{0xEF, 0xBB, 0xBF}) {
		bom = doc[:3]
		doc = doc[3:]
	}
	unit := "  "
	var root node
	if len(bytes.TrimSpace(doc)) == 0 {
		root.kind = kindObject
	} else {
		parsed, err := parseRootObject(doc)
		if err != nil {
			return nil, err
		}
		root = valueToNode(doc, parsed)
		unit = detectIndentUnit(doc)
	}
	if err := applyNested(&root, updates); err != nil {
		return nil, err
	}
	out := []byte(prettyNode(root, 0, unit) + "\n")
	return append(append([]byte{}, bom...), out...), nil
}

func parseRootObject(doc []byte) (value, error) {
	p := parser{b: doc}
	p.skipWS()
	if p.eof() || p.b[p.i] != '{' {
		return value{}, fmt.Errorf("settings must be a JSON object")
	}
	root, err := p.parseValue()
	if err != nil {
		return value{}, fmt.Errorf("settings are not valid JSON: %w", err)
	}
	p.skipWS()
	if !p.eof() {
		return value{}, fmt.Errorf("settings have trailing data after the JSON value")
	}
	if root.kind != kindObject {
		return value{}, fmt.Errorf("settings must be a JSON object")
	}
	return root, nil
}

type node struct {
	kind   int
	raw    string
	fields []nodeField
}

type nodeField struct {
	key  string
	node node
}

func valueToNode(src []byte, v value) node {
	if v.kind != kindObject {
		return node{kind: v.kind, raw: string(src[v.start:v.end])}
	}
	n := node{kind: kindObject}
	for _, f := range v.fields {
		n.fields = append(n.fields, nodeField{key: f.key, node: valueToNode(src, f.val)})
	}
	return n
}

func applyNested(root *node, updates []Nested) error {
	for _, upd := range updates {
		if err := setPath(root, upd.Path, upd.Value, upd.Delete); err != nil {
			return err
		}
	}
	return nil
}

func setPath(n *node, path []string, value string, delete bool) error {
	if n.kind != kindObject {
		return fmt.Errorf("field %s is not an object", path[0])
	}
	key := path[0]
	idx := -1
	for i, f := range n.fields {
		if f.key == key {
			idx = i
			break
		}
	}
	if len(path) == 1 {
		if delete {
			if idx >= 0 {
				n.fields = append(n.fields[:idx], n.fields[idx+1:]...)
			}
			return nil
		}
		raw := mustJSONString(value)
		if idx >= 0 {
			n.fields[idx].node = node{kind: kindString, raw: raw}
			return nil
		}
		n.fields = append(n.fields, nodeField{key: key, node: node{kind: kindString, raw: raw}})
		return nil
	}
	if idx < 0 {
		if delete {
			return nil
		}
		n.fields = append(n.fields, nodeField{key: key, node: node{kind: kindObject}})
		idx = len(n.fields) - 1
	}
	if n.fields[idx].node.kind != kindObject {
		return fmt.Errorf("field %s is not an object", key)
	}
	return setPath(&n.fields[idx].node, path[1:], value, delete)
}

func prettyNode(n node, indent int, unit string) string {
	if n.kind != kindObject {
		return n.raw
	}
	if len(n.fields) == 0 {
		return "{}"
	}
	pad := strings.Repeat(unit, indent+1)
	closePad := strings.Repeat(unit, indent)
	var b strings.Builder
	b.WriteString("{\n")
	for i, f := range n.fields {
		b.WriteString(pad)
		b.WriteString(mustJSONString(f.key))
		b.WriteString(": ")
		child := prettyNode(f.node, indent+1, unit)
		b.WriteString(child)
		if i != len(n.fields)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(closePad)
	b.WriteByte('}')
	return b.String()
}
