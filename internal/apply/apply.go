// Package apply writes Claude Code, Codex, Claude Desktop, and shell configuration for one profile.
package apply

import (
	"fmt"
	"os"

	"github.com/zfg5118/deeprouter-setup/internal/atomicfile"
	"github.com/zfg5118/deeprouter-setup/internal/backup"
	"github.com/zfg5118/deeprouter-setup/internal/claudedesktop"
	"github.com/zfg5118/deeprouter-setup/internal/codex"
	"github.com/zfg5118/deeprouter-setup/internal/gateway"
	"github.com/zfg5118/deeprouter-setup/internal/grok"
	"github.com/zfg5118/deeprouter-setup/internal/jsonedit"
	"github.com/zfg5118/deeprouter-setup/internal/shellrc"
)

// Request describes one setup or switch.
type Request struct {
	ClaudePath string
	CodexPath  string
	RCFile     string
	Shell      string
	// DesktopDir is Claude Desktop's local configLibrary. Empty skips it.
	DesktopDir string
	GrokPath   string

	BaseURL     string
	APIKey      string
	Label       string
	ClaudeModel string
	CodexModel  string
	// DesktopModel overrides ClaudeModel for Claude Desktop when set.
	DesktopModel string
	GrokModel    string
	WireAPI      string

	// Per-app keys override APIKey for that file. Empty uses APIKey.
	ClaudeKey  string
	DesktopKey string
	CodexKey   string
	GrokKey    string

	TouchClaude  bool
	TouchDesktop bool
	TouchCodex   bool
	TouchGrok    bool
	TouchShell   bool
	// AppsExplicit means TouchDesktop is independent of TouchClaude.
	// Older callers leave it false, and a Claude write still updates DesktopDir.
	AppsExplicit bool

	// SetClaudeModel writes ANTHROPIC_MODEL even when ClaudeModel is non-empty.
	// When false, an existing model is left alone.
	SetClaudeModel  bool
	SetDesktopModel bool
	SetCodexModel   bool
	SetGrokModel    bool

	Backups backup.Store
}

// Report is what Apply changed.
type Report struct {
	AnthropicBase string
	OpenAIBase    string
	Changed       []string
	Unchanged     []string
	// DesktopProfile is the Claude Desktop configuration that holds the gateway.
	DesktopProfile string
	// GrokPath is the Grok Build config that holds the gateway.
	GrokPath string
}

// Apply backs up the files it is about to touch, then writes them.
// A second call with the same inputs does not duplicate keys or blocks.
func Apply(req Request) (Report, error) {
	ep, err := gateway.ParseBase(req.BaseURL)
	if err != nil {
		return Report{}, err
	}
	if req.APIKey == "" {
		return Report{}, fmt.Errorf("API key is empty")
	}
	if req.WireAPI == "" {
		req.WireAPI = "responses"
	}
	label := req.Label
	if label == "" {
		label = "DeepRouter"
	}

	var report Report
	report.AnthropicBase = ep.AnthropicBase
	report.OpenAIBase = ep.OpenAIBase

	type planned struct {
		path  string
		mode  os.FileMode
		keep  bool
		next  []byte
		touch bool
	}
	var plans []planned

	if req.TouchClaude {
		if req.ClaudePath == "" {
			return Report{}, fmt.Errorf("claude settings path is empty")
		}
		cur, err := readOptional(req.ClaudePath)
		if err != nil {
			return Report{}, err
		}
		var updates []jsonedit.KV
		updates = append(updates,
			jsonedit.KV{Key: "ANTHROPIC_BASE_URL", Value: ep.AnthropicBase},
			jsonedit.KV{Key: "ANTHROPIC_AUTH_TOKEN", Value: req.claudeKey()},
		)
		if req.SetClaudeModel && req.ClaudeModel != "" {
			updates = append(updates, jsonedit.KV{Key: "ANTHROPIC_MODEL", Value: req.ClaudeModel})
		}
		next, err := jsonedit.SetEnv(cur, updates)
		if err != nil {
			return Report{}, err
		}
		plans = append(plans, planned{path: req.ClaudePath, mode: 0o600, keep: false, next: next, touch: true})
	}

	if req.TouchCodex {
		if req.CodexPath == "" {
			return Report{}, fmt.Errorf("codex config path is empty")
		}
		cur, err := readOptional(req.CodexPath)
		if err != nil {
			return Report{}, err
		}
		model := ""
		if req.SetCodexModel {
			model = req.CodexModel
		}
		next, err := codex.Apply(string(cur), codex.Update{
			Model:         model,
			ProviderLabel: label,
			BaseURL:       ep.OpenAIBase,
			WireAPI:       req.WireAPI,
			BearerToken:   req.codexKey(),
		})
		if err != nil {
			return Report{}, err
		}
		// The bearer token lives in this file, so it is mode 0600 even when
		// an older config was world-readable.
		plans = append(plans, planned{path: req.CodexPath, mode: 0o600, keep: false, next: []byte(next), touch: true})
	}

	if req.TouchShell {
		if req.RCFile == "" {
			return Report{}, fmt.Errorf("shell startup path is empty")
		}
		cur, err := readOptional(req.RCFile)
		if err != nil {
			return Report{}, err
		}
		var pairs []shellrc.Pair
		if req.TouchClaude {
			pairs = append(pairs,
				shellrc.Pair{Key: "ANTHROPIC_BASE_URL", Value: ep.AnthropicBase},
				shellrc.Pair{Key: "ANTHROPIC_AUTH_TOKEN", Value: req.claudeKey()},
			)
			if req.SetClaudeModel && req.ClaudeModel != "" {
				pairs = append(pairs, shellrc.Pair{Key: "ANTHROPIC_MODEL", Value: req.ClaudeModel})
			}
		}
		if req.TouchCodex {
			pairs = append(pairs, shellrc.Pair{Key: "DEEPROUTER_API_KEY", Value: req.codexKey()})
		}
		// A full setup (both tools) always writes the shared key and the Claude vars.
		if req.TouchClaude && req.TouchCodex {
			// already included
		}
		shell := req.Shell
		if shell == "" {
			shell = "bash"
		}
		next, err := shellrc.Upsert(string(cur), shell, pairs)
		if err != nil {
			return Report{}, err
		}
		existed := false
		if info, err := os.Stat(req.RCFile); err == nil && info.Mode().IsRegular() {
			existed = true
		}
		mode := os.FileMode(0o644)
		plans = append(plans, planned{path: req.RCFile, mode: mode, keep: existed, next: []byte(next), touch: true})
	}

	if req.writeDesktop() {
		model := req.DesktopModel
		setModel := req.SetDesktopModel
		if model == "" {
			model = req.ClaudeModel
			setModel = req.SetClaudeModel
		}
		files, err := claudedesktop.Plan(req.DesktopDir, claudedesktop.Update{
			BaseURL:  ep.AnthropicBase,
			APIKey:   req.desktopKey(),
			Model:    model,
			SetModel: setModel,
		})
		if err != nil {
			return Report{}, err
		}
		report.DesktopProfile = files.ProfilePath
		// The API key lives in the profile, so both files are mode 0600.
		plans = append(plans,
			planned{path: files.ProfilePath, mode: 0o600, keep: false, next: files.Profile, touch: true},
			planned{path: files.MetaPath, mode: 0o600, keep: false, next: files.Meta, touch: true},
		)
	}

	if req.TouchGrok {
		if req.GrokPath == "" {
			return Report{}, fmt.Errorf("grok config path is empty")
		}
		cur, err := readOptional(req.GrokPath)
		if err != nil {
			return Report{}, err
		}
		model := ""
		if req.SetGrokModel {
			model = req.GrokModel
		}
		next, err := grok.Apply(string(cur), grok.Update{
			Model:   model,
			BaseURL: ep.OpenAIBase,
			APIKey:  req.grokKey(),
			Name:    label,
		})
		if err != nil {
			return Report{}, err
		}
		report.GrokPath = req.GrokPath
		plans = append(plans, planned{path: req.GrokPath, mode: 0o600, keep: false, next: []byte(next), touch: true})
	}

	var changing []string
	for _, p := range plans {
		cur, err := readOptional(p.path)
		if err != nil {
			return Report{}, err
		}
		if string(cur) == string(p.next) {
			report.Unchanged = append(report.Unchanged, p.path)
			continue
		}
		changing = append(changing, p.path)
	}
	for _, path := range changing {
		if err := req.Backups.EnsureOriginal(path); err != nil {
			return Report{}, err
		}
	}
	if _, err := req.Backups.SaveHistory(changing); err != nil {
		return Report{}, err
	}
	for _, p := range plans {
		cur, err := readOptional(p.path)
		if err != nil {
			return Report{}, err
		}
		if string(cur) == string(p.next) {
			continue
		}
		if err := atomicfile.Write(p.path, p.next, p.mode, p.keep); err != nil {
			return Report{}, err
		}
		report.Changed = append(report.Changed, p.path)
	}
	return report, nil
}

func (req Request) writeDesktop() bool {
	if req.DesktopDir == "" {
		return false
	}
	if req.AppsExplicit {
		return req.TouchDesktop
	}
	return req.TouchClaude
}

func (req Request) claudeKey() string  { return firstKey(req.ClaudeKey, req.APIKey) }
func (req Request) desktopKey() string { return firstKey(req.DesktopKey, req.APIKey) }
func (req Request) codexKey() string   { return firstKey(req.CodexKey, req.APIKey) }
func (req Request) grokKey() string    { return firstKey(req.GrokKey, req.APIKey) }

func firstKey(specific, fallback string) string {
	if specific != "" {
		return specific
	}
	return fallback
}

func readOptional(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	info, statErr := os.Lstat(path)
	if statErr == nil && !info.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	return data, nil
}
