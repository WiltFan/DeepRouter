package textutil

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Mask hides an API key. A long key that starts with "sk-" is shown as
// sk-**** plus its last four characters. Any other long key is **** plus
// its last four characters, so nothing in front of the tail is revealed.
// A key too short to hide a middle is only stars (and the sk- label, when
// it has one). An empty key is "(not set)".
func Mask(secret string) string {
	if secret == "" {
		return "(not set)"
	}
	return maskShown(secret)
}

// MaskConfirm is the parenthetical shown after a key is typed or kept:
// sk-****abcd, 51 chars. It uses the same visible characters as Mask.
func MaskConfirm(secret string) string {
	return fmt.Sprintf("%s, %d chars", maskShown(secret), len([]rune(secret)))
}

// maskShown reveals at most the last four characters. "sk-" is a label, not
// a secret, and only when at least four secret characters stay hidden
// between that label and the tail. Anything shorter, and any key that does
// not start with sk-, never shows characters from the front.
func maskShown(secret string) string {
	runes := []rune(secret)
	n := len(runes)
	const label = "sk-"
	labelN := len([]rune(label))
	if strings.HasPrefix(secret, label) {
		if n >= labelN+4+4 {
			return label + "****" + string(runes[n-4:])
		}
		return label + "****"
	}
	if n >= 4+4 {
		return "****" + string(runes[n-4:])
	}
	return "****"
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
