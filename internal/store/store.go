// Package store persists named gateway profiles on disk.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/zfg5118/deeprouter-setup/internal/atomicfile"
)

// Profile is one gateway credential. The API key selects the group.
type Profile struct {
	Name        string `json:"name"`
	Label       string `json:"label"`
	BaseURL     string `json:"base_url"`
	APIKey      string `json:"api_key"`
	ClaudeModel string `json:"claude_model,omitempty"`
	CodexModel  string `json:"codex_model,omitempty"`
	WireAPI     string `json:"wire_api,omitempty"`
}

// Apply remembers which files setup last wired, so switch can update them.
type Apply struct {
	Claude     bool   `json:"claude"`
	Codex      bool   `json:"codex"`
	Shell      bool   `json:"shell"`
	ShellKind  string `json:"shell_kind,omitempty"`
	RCFile     string `json:"rc_file,omitempty"`
	ClaudePath string `json:"claude_path,omitempty"`
	CodexPath  string `json:"codex_path,omitempty"`
}

// State is ~/.deeprouter/config.json.
type State struct {
	Version  int       `json:"version"`
	Active   string    `json:"active_profile,omitempty"`
	Profiles []Profile `json:"profiles"`
	Apply    Apply     `json:"apply"`
}

// File is the profile database.
type File struct {
	Path string
}

func Path(configDir string) string {
	return filepath.Join(configDir, "config.json")
}

func Load(path string) (State, error) {
	raw, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return State{Version: 1}, nil
	}
	if err != nil {
		return State{}, err
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return State{Version: 1}, nil
	}
	var st State
	if err := json.Unmarshal(raw, &st); err != nil {
		return State{}, fmt.Errorf("profile config is not valid JSON: %w", err)
	}
	if st.Version > 1 {
		return State{}, fmt.Errorf("profile config version %d is newer than this build", st.Version)
	}
	if st.Version == 0 {
		st.Version = 1
	}
	return st, nil
}

func Save(path string, st State) error {
	if st.Version == 0 {
		st.Version = 1
	}
	raw, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if err := atomicfile.Write(path, raw, 0o600, false); err != nil {
		return err
	}
	return os.Chmod(path, 0o600)
}

func (st State) Find(name string) (Profile, bool) {
	for _, p := range st.Profiles {
		if p.Name == name {
			return p, true
		}
	}
	return Profile{}, false
}

func (st *State) Upsert(p Profile) {
	for i := range st.Profiles {
		if st.Profiles[i].Name == p.Name {
			st.Profiles[i] = p
			return
		}
	}
	st.Profiles = append(st.Profiles, p)
}

func (st *State) Remove(name string) error {
	if st.Active == name {
		return fmt.Errorf("profile %q is active; switch to another profile before removing it", name)
	}
	next := st.Profiles[:0]
	found := false
	for _, p := range st.Profiles {
		if p.Name == name {
			found = true
			continue
		}
		next = append(next, p)
	}
	if !found {
		return fmt.Errorf("no profile %q", name)
	}
	st.Profiles = next
	return nil
}

// ValidName reports whether name can be used as a profile id.
func ValidName(name string) error {
	if name == "" || len(name) > 64 {
		return fmt.Errorf("profile name must be 1 to 64 characters")
	}
	for i, r := range name {
		ok := (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '.' || r == '_' || r == '-'
		if !ok || (i == 0 && (r == '.' || r == '-' || r == '_')) {
			return fmt.Errorf("profile name %q must start with a letter or digit and contain only letters, digits, '.', '_' and '-'", name)
		}
	}
	return nil
}
