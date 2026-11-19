package jsonedit

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"
)

func ConfigJSON(doc []byte, jsonc bool) ([]byte, error) {
	doc = bytes.TrimPrefix(doc, []byte{0xef, 0xbb, 0xbf})
	if !utf8.Valid(doc) {
		return nil, fmt.Errorf("configuration is not valid UTF-8")
	}
	if jsonc {
		var err error
		doc, err = maskJSONC(doc, true)
		if err != nil {
			return nil, err
		}
	}
	if len(bytes.TrimSpace(doc)) == 0 {
		return doc, nil
	}
	if !json.Valid(doc) {
		return nil, fmt.Errorf("invalid JSON configuration syntax")
	}
	decoder := json.NewDecoder(bytes.NewReader(doc))
	decoder.UseNumber()
	if err := uniqueJSONKeys(decoder); err != nil {
		return nil, err
	}
	return doc, nil
}

func uniqueJSONKeys(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	switch token {
	case json.Delim('{'):
		keys := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name := key.(string)
			if keys[name] {
				return fmt.Errorf("duplicate key in configuration")
			}
			keys[name] = true
			if err := uniqueJSONKeys(decoder); err != nil {
				return err
			}
		}
	case json.Delim('['):
		for decoder.More() {
			if err := uniqueJSONKeys(decoder); err != nil {
				return err
			}
		}
	default:
		return nil
	}
	_, err = decoder.Token()
	return err
}

func maskJSONC(doc []byte, removeTrailing bool) ([]byte, error) {
	masked := bytes.Clone(doc)
	previous, before := -1, -1
	for index := 0; index < len(doc); {
		start := index
		switch doc[index] {
		case ' ', '\t', '\n', '\r':
			index++
			continue
		case '"':
			scan := parser{b: doc, i: index}
			if err := scan.consumeStringToken(); err != nil {
				return nil, fmt.Errorf("invalid string in JSONC")
			}
			index = scan.i
		case '/':
			if index+1 >= len(doc) {
				return nil, fmt.Errorf("invalid JSONC comment")
			}
			index += 2
			switch doc[start+1] {
			case '/':
				for index < len(doc) && doc[index] != '\n' && doc[index] != '\r' {
					index++
				}
			case '*':
				end := bytes.Index(doc[index:], []byte("*/"))
				if end < 0 {
					return nil, fmt.Errorf("unterminated JSONC block comment")
				}
				index += end + 2
			default:
				return nil, fmt.Errorf("invalid JSONC comment")
			}
			for offset := start; offset < index; offset++ {
				if doc[offset] != '\n' && doc[offset] != '\r' {
					masked[offset] = ' '
				}
			}
			continue
		case '}', ']':
			if removeTrailing && previous >= 0 && doc[previous] == ',' && before >= 0 && !strings.ContainsRune("{[,:", rune(doc[before])) {
				masked[previous] = ' '
			}
			index++
		default:
			index++
		}
		before, previous = previous, start
	}
	return masked, nil
}

func SetNestedJSONC(doc []byte, updates []Nested) ([]byte, error) {
	var bom []byte
	if bytes.HasPrefix(doc, []byte{0xef, 0xbb, 0xbf}) {
		bom, doc = doc[:3], doc[3:]
	}
	for _, update := range updates {
		if len(update.Path) == 0 || update.Delete || update.Raw != nil || !utf8.ValidString(update.Value) {
			return nil, fmt.Errorf("invalid JSONC update")
		}
		for _, key := range update.Path {
			if err := validKey(key); err != nil {
				return nil, err
			}
		}
		masked, err := ConfigJSON(doc, true)
		if err != nil {
			return nil, err
		}
		if len(bytes.TrimSpace(masked)) == 0 {
			if len(bytes.TrimSpace(doc)) != 0 {
				return nil, fmt.Errorf("expected a JSONC object")
			}
			doc = append(bytes.Clone(doc), '{', '}')
			masked = append(masked, '{', '}')
		}
		root, err := parseRootObject(masked)
		if err != nil {
			return nil, err
		}
		doc, err = setJSONCPath(doc, root, update.Path, update.Value)
		if err != nil {
			return nil, err
		}
	}
	return append(bytes.Clone(bom), doc...), nil
}

func setJSONCPath(doc []byte, root value, path []string, contents string) ([]byte, error) {
	for _, member := range root.fields {
		if member.key != path[0] {
			continue
		}
		if len(path) == 1 {
			current, err := unquote(doc[member.val.start:member.val.end])
			if err == nil && current == contents {
				return doc, nil
			}
			if strings.ContainsAny(string(doc[member.val.start:member.val.start+1]), "{[") {
				return nil, fmt.Errorf("expected a scalar setting")
			}
			return spliceJSONC(doc, member.val.start, member.val.end, mustJSONString(contents)), nil
		}
		if member.val.kind != kindObject {
			return nil, fmt.Errorf("settings path contains a non-object field")
		}
		return setJSONCPath(doc, member.val, path[1:], contents)
	}
	entry := mustJSONString(contents)
	for index := len(path) - 1; index > 0; index-- {
		entry = "{" + mustJSONString(path[index]) + ": " + entry + "}"
	}
	entry = mustJSONString(path[0]) + ": " + entry
	hasTrailing := false
	if len(root.fields) > 0 {
		end := root.fields[len(root.fields)-1].val.end
		tail, err := maskJSONC(doc[end:root.close], false)
		if err != nil {
			return nil, err
		}
		hasTrailing = bytes.Contains(tail, []byte(","))
		if !hasTrailing {
			doc = spliceJSONC(doc, end, end, ",")
			root.close++
		}
	}
	if hasTrailing {
		entry += ","
	}
	lineStart := bytes.LastIndexByte(doc[:root.close], '\n') + 1
	if lineStart > root.start && len(bytes.TrimSpace(doc[lineStart:root.close])) == 0 {
		newline := "\n"
		if lineStart >= 2 && doc[lineStart-2] == '\r' {
			newline = "\r\n"
		}
		indent := string(doc[lineStart:root.close]) + detectIndentUnit(doc)
		return spliceJSONC(doc, lineStart, lineStart, indent+entry+newline), nil
	}
	return spliceJSONC(doc, root.close, root.close, " "+entry), nil
}

func spliceJSONC(doc []byte, start, end int, replacement string) []byte {
	result := make([]byte, 0, len(doc)-(end-start)+len(replacement))
	result = append(result, doc[:start]...)
	result = append(result, replacement...)
	return append(result, doc[end:]...)
}
