package textutil

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Mask hides an API key, keeping a short prefix and suffix when it is long enough.
func Mask(secret string) string {
	if secret == "" {
		return "(not set)"
	}
	if len(secret) < 12 {
		return "****"
	}
	return secret[:4] + "…" + secret[len(secret)-4:]
}

// MaskConfirm is the parenthetical shown after a key is typed or kept.
// A long key keeps three leading characters and four trailing characters:
// sk-****abcd, 51 chars.
func MaskConfirm(secret string) string {
	runes := []rune(secret)
	n := len(runes)
	var shown string
	switch {
	case n >= 8:
		shown = string(runes[:3]) + "****" + string(runes[n-4:])
	case n >= 4:
		shown = "****" + string(runes[n-4:])
	default:
		shown = "****"
	}
	return fmt.Sprintf("%s, %d chars", shown, n)
}

// ValidateKey rejects empty keys and keys that cannot sit on one shell line.
func ValidateKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errKey("API key is empty")
	}
	if strings.ContainsAny(key, "\r\n\x00") {
		return "", errKey("API key must be a single line")
	}
	if !utf8.ValidString(key) {
		return "", errKey("API key must be valid UTF-8")
	}
	if len(key) > 4096 {
		return "", errKey("API key is too long")
	}
	return key, nil
}

type keyError string

func (e keyError) Error() string { return string(e) }

func errKey(s string) error { return keyError(s) }

// DisplayPath rewrites a path under home with a leading tilde.
func DisplayPath(home, path string) string {
	if home == "" || path == "" {
		return path
	}
	home = strings.TrimRight(home, `/\`)
	if path == home {
		return "~"
	}
	sep := string(osPathSep(path, home))
	prefix := home + sep
	if strings.HasPrefix(path, prefix) {
		return "~" + sep + path[len(prefix):]
	}
	return path
}

func osPathSep(path, home string) byte {
	if strings.Contains(path, `\`) || strings.Contains(home, `\`) {
		return '\\'
	}
	return '/'
}
