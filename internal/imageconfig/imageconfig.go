// Package imageconfig stores the image Skill's own API address and token.
//
// The file is ~/.deeprouter/image-skill.json. It is separate from the tool
// profiles in ~/.deeprouter/config.json and from every program's config, so
// saving it changes nothing else. The file is written with mode 0600 in a
// 0700 folder. That restricts who can read it; it is not encrypted.
//
// deeprouter_image.py in the Skill reads the same file. Keep the field names
// and the base rules in ParseBase in step with that script.
package imageconfig

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/zfg5118/deeprouter/internal/textutil"
)

// FileName is the config file name under the deeprouter config folder.
const FileName = "image-skill.json"

// maxSize caps what Load reads. A real file is a few hundred bytes.
const maxSize = 64 * 1024

// DefaultModel is the model the Skill sends when neither --model nor this
// file names one. It is a default, not a check that a token may use it.
const DefaultModel = "gpt-image-2.6-flare"

const (
	ProtocolImages = "openai-images"
	ProtocolGemini = "gemini-generate-content"
)

// maxModelLen bounds a saved model id.
const maxModelLen = 200

// Config is the image Skill's address and token.
type Config struct {
	Version int `json:"version"`
	// Provider is the preset id (deeprouter, wolfai, linkmesh) or custom.
	Provider string `json:"provider"`
	// BaseURL may include a gateway prefix, without a trailing API version.
	BaseURL string `json:"base_url"`
	APIKey  string `json:"api_key"`
	// Model is the image model the Skill sends unless --model is given.
	// Empty means DefaultModel for Images. Saving it does not mean it was tested.
	Model              string `json:"model,omitempty"`
	Protocol           string `json:"protocol"`
	GeminiCatalogModel string `json:"gemini_catalog_model,omitempty"`
}

func parseProtocol(value string) (string, error) {
	switch value {
	case "", ProtocolImages:
		return ProtocolImages, nil
	case ProtocolGemini:
		return value, nil
	default:
		return "", fmt.Errorf("unknown image Skill protocol")
	}
}

// Path is the config file inside configDir (~/.deeprouter).
func Path(configDir string) string {
	return filepath.Join(configDir, FileName)
}

// ParseBase checks an image API address and returns it normalized: https
// only, with a host, no username or password, no query or fragment, and no
// trailing slash, /v1 or /v1beta. The tool wizard's gateway.ParseBase still accepts
// http; this one does not.
func ParseBase(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", fmt.Errorf("image API address is empty")
	}
	if strings.ContainsAny(raw, "?#") {
		return "", fmt.Errorf("image API address must not include a query string or fragment")
	}
	if strings.IndexFunc(raw, func(r rune) bool { return r <= ' ' || r == 0x7f || r == '\\' }) >= 0 {
		return "", fmt.Errorf("image API address is not a valid URL")
	}
	if !strings.HasPrefix(strings.ToLower(raw), "https://") {
		return "", fmt.Errorf("image API address must start with https://")
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("image API address is not a valid URL")
	}
	if u.User != nil || strings.Contains(u.Host, "@") {
		return "", fmt.Errorf("image API address must not include a username or password")
	}
	if u.Hostname() == "" {
		return "", fmt.Errorf("image API address is missing a host")
	}
	if p := u.Port(); p != "" {
		if strings.Trim(p, "0123456789") != "" || len(p) > 5 {
			return "", fmt.Errorf("image API address is not a valid URL")
		}
	}
	if u.Opaque != "" || u.RawQuery != "" || u.Fragment != "" {
		return "", fmt.Errorf("image API address is not a valid URL")
	}
	path := strings.TrimRight(u.EscapedPath(), "/")
	for _, suffix := range []string{"/v1", "/v1beta"} {
		if strings.HasSuffix(strings.ToLower(path), suffix) {
			path = strings.TrimRight(path[:len(path)-len(suffix)], "/")
			break
		}
	}
	return "https://" + strings.ToLower(u.Host) + path, nil
}

var geminiPathID = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`)
var geminiImageMarker = regexp.MustCompile(`-image(?:[._-]|$)`)

func GeminiModelID(raw string) (string, error) {
	id := strings.TrimPrefix(strings.TrimSpace(raw), "models/")
	if _, err := ParseModel(id); err != nil || !geminiPathID.MatchString(id) {
		return "", fmt.Errorf("Gemini model ID must be a safe single path segment")
	}
	return id, nil
}

func IsGeminiImage(id string, catalogImage bool) bool {
	id, err := GeminiModelID(id)
	if err != nil {
		return false
	}
	low := strings.ToLower(id)
	for _, marker := range []string{"gpt-", "claude-", "deepseek-", "qwen-", "dall-e", "dalle", "flux", "grok", "doubao", "midjourney", "imagen", "veo", "sora", "video", "kling", "wan-", "seedance", "runway", "hailuo", "minimax", "luma"} {
		if strings.HasPrefix(low, marker) || strings.Contains(low, "-"+marker) {
			return false
		}
	}
	return !strings.HasPrefix(low, "mj_") && !strings.HasPrefix(low, "mj-") &&
		(catalogImage || strings.Contains(low, "nano-banana") ||
			strings.HasPrefix(low, "gemini-") && geminiImageMarker.MatchString(low))
}

// ParseModel checks a model id for the config file. Only its form is
// checked: not empty, UTF-8, at most 200 bytes, no space or control
// character, no backslash, and nothing path-like (a leading slash, "..",
// or "://"). Whether the gateway knows the id is not checked.
func ParseModel(raw string) (string, error) {
	s := strings.TrimSpace(raw)
	switch {
	case s == "":
		return "", fmt.Errorf("model id is empty")
	case !utf8.ValidString(s), len(s) > maxModelLen:
		return "", fmt.Errorf("model id is not valid")
	case strings.IndexFunc(s, func(r rune) bool { return unicode.IsSpace(r) || unicode.IsControl(r) || r == '\\' }) >= 0:
		return "", fmt.Errorf("model id must not contain spaces, control characters, or backslashes")
	case strings.HasPrefix(s, "/"), strings.Contains(s, ".."), strings.Contains(s, "://"):
		return "", fmt.Errorf("model id must not look like a path or URL")
	}
	return s, nil
}

// Host is the host (with any port) a normalized base sends the token to.
func Host(base string) string {
	u, err := url.Parse(base)
	if err != nil {
		return ""
	}
	return u.Host
}

func IsGoogleBase(base string) bool {
	u, err := url.Parse(base)
	return err == nil && strings.EqualFold(u.Hostname(), "generativelanguage.googleapis.com")
}

// Load reads the config. ok is false when the file does not exist. A
// symbolic link, a folder, or a file that is not valid JSON is an error.
func Load(path string) (cfg Config, ok bool, err error) {
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return Config{}, false, nil
	}
	if err != nil {
		return Config{}, false, err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return Config{}, false, fmt.Errorf("%s is a symbolic link; not reading through it", path)
	}
	if !info.Mode().IsRegular() {
		return Config{}, false, fmt.Errorf("%s is not a regular file", path)
	}
	if info.Size() > maxSize {
		return Config{}, false, fmt.Errorf("image Skill config %s is too large", path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return Config{}, false, err
	}
	if err := json.Unmarshal(b, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("image Skill config is not valid JSON: %s", path)
	}
	cfg.Protocol, err = parseProtocol(cfg.Protocol)
	if err != nil {
		return Config{}, false, err
	}
	return cfg, true, nil
}

// Complete reports whether cfg has a valid address and token, which is when
// the Skill script uses it.
func (c Config) Complete() bool {
	if _, err := ParseBase(c.BaseURL); err != nil {
		return false
	}
	_, err := textutil.ValidateKey(c.APIKey)
	return err == nil
}

// Hooks the tests replace to make the write fail.
var (
	rename    = os.Rename
	writeFile = func(f *os.File, b []byte) error { _, err := f.Write(b); return err }
)

// Save writes cfg to path with mode 0600. The folder is created with mode
// 0700. The data goes to a new temporary file in the same folder, which is
// renamed over path only after it is complete, so a failure leaves the
// previous file as it was. A symbolic link at the folder or the file stops
// the write.
func Save(path string, cfg Config) error {
	base, err := ParseBase(cfg.BaseURL)
	if err != nil {
		return err
	}
	key, err := textutil.ValidateKey(cfg.APIKey)
	if err != nil {
		return err
	}
	if cfg.Protocol, err = parseProtocol(cfg.Protocol); err != nil {
		return err
	}
	if cfg.Protocol == ProtocolGemini {
		if cfg.Model, err = GeminiModelID(cfg.Model); err != nil {
			return err
		}
		if !IsGeminiImage(cfg.Model, cfg.GeminiCatalogModel == cfg.Model) {
			return fmt.Errorf("Gemini native requires a Gemini image model")
		}
	} else if cfg.Model != "" {
		if cfg.Model, err = ParseModel(cfg.Model); err != nil {
			return err
		}
	}
	if cfg.Protocol != ProtocolGemini || cfg.GeminiCatalogModel != cfg.Model {
		cfg.GeminiCatalogModel = ""
	}
	cfg.BaseURL, cfg.APIKey, cfg.Version = base, key, 1
	dir := filepath.Dir(path)
	if err := ensureDir(dir); err != nil {
		return err
	}
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&fs.ModeSymlink != 0 {
			return fmt.Errorf("%s is a symbolic link; not writing through it", path)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("%s is not a regular file", path)
		}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	// CreateTemp opens with O_EXCL and mode 0600, so the temporary name
	// cannot be a link planted in advance.
	f, err := os.CreateTemp(dir, "."+FileName+".tmp-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	fail := func(err error) error {
		_ = f.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := f.Chmod(0o600); err != nil && runtime.GOOS != "windows" {
		return fail(err)
	}
	if err := writeFile(f, data); err != nil {
		return fail(err)
	}
	if err := f.Sync(); err != nil {
		return fail(err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return nil
}

// ensureDir makes dir with mode 0700, or checks that it is a real folder.
// An existing folder is narrowed to 0700 on Unix; deeprouter creates it
// that way and keeps tokens in it.
func ensureDir(dir string) error {
	info, err := os.Lstat(dir)
	if errors.Is(err, fs.ErrNotExist) {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
		return os.Chmod(dir, 0o700)
	}
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 {
		return fmt.Errorf("%s is a symbolic link; not writing through it", dir)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s exists and is not a directory", dir)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o700 {
		return os.Chmod(dir, 0o700)
	}
	return nil
}
