// Package imageskill installs the DeepRouter image Skill into the Skill
// directories of Claude Code and Codex / ChatGPT.
//
// The Skill files are embedded in the binary, so an install is offline. It
// never downloads a newer copy, never runs the Skill's script, never calls an
// API, and never writes an API key. When it runs, the Skill reads its own
// address and key: DEEPROUTER_IMAGE_BASE_URL + DEEPROUTER_IMAGE_API_KEY, the
// image config saved by deeprouter (see internal/imageconfig), or else
// DEEPROUTER_API_KEY.
package imageskill

import (
	"bytes"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zfg5118/deeprouter/internal/apps"
)

// Name is the Skill folder name and the name in SKILL.md frontmatter.
const Name = "deeprouter-image-api"

// Version is metadata.version in the embedded SKILL.md.
const Version = "1.0.4"

// The four Skill files, verbatim from embed/ (Skill 1.0.4: the delivered
// standalone 1.0.1, the download-recovery helper from 1.0.2, the image
// address and token sources from 1.0.3, and the saved model with the
// gpt-image-2.6-flare default from 1.0.4). Listing them one
// by one keeps a stray __pycache__ or editor file out of the binary.
//
//go:embed embed/SKILL.md embed/使用说明.md embed/references/official-links.md embed/scripts/deeprouter_image.py
var embedded embed.FS

// Files maps each Skill-relative path, with forward slashes, to its bytes.
func Files() map[string][]byte {
	out := map[string][]byte{}
	for _, rel := range FileList() {
		b, err := embedded.ReadFile("embed/" + rel)
		if err != nil {
			panic("imageskill: missing embedded file " + rel)
		}
		out[rel] = b
	}
	return out
}

// FileList is the Skill-relative paths in install order.
func FileList() []string {
	return []string{
		"SKILL.md",
		"使用说明.md",
		"references/official-links.md",
		"scripts/deeprouter_image.py",
	}
}

// Targets is the programs the Skill can be installed into, in picker order.
// Both load Skills from a documented folder:
// https://code.claude.com/docs/en/skills and
// https://developers.openai.com/codex/skills/
func Targets() []apps.ID {
	return []apps.ID{apps.Codex, apps.ClaudeCode}
}

// Supported reports whether id is an install target.
func Supported(id apps.ID) bool {
	return apps.Contains(Targets(), id)
}

// Locations is what the Skill folder depends on.
type Locations struct {
	// Home is the user's home directory (%USERPROFILE% on Windows).
	Home string
	// ClaudeConfigDir is CLAUDE_CONFIG_DIR. When set, Claude Code keeps
	// everything it would keep in ~/.claude there instead.
	ClaudeConfigDir string
	// Project is a project directory. Empty means a user-level install.
	Project string
}

// SkillsRoot is the directory that holds Skill folders for id, and base is
// the directory under it that deeprouter does not check or create: the
// home directory, CLAUDE_CONFIG_DIR, or the project.
//
//	Claude Code, user:     ~/.claude/skills, or $CLAUDE_CONFIG_DIR/skills
//	Claude Code, project:  <project>/.claude/skills
//	Codex, user:           ~/.agents/skills
//	Codex, project:        <project>/.agents/skills
//
// Codex still reads the older ~/.codex/skills, but ~/.agents/skills is the
// documented place, and CODEX_HOME does not move it.
func SkillsRoot(id apps.ID, loc Locations) (root, base string, err error) {
	switch id {
	case apps.ClaudeCode:
		switch {
		case loc.Project != "":
			base = loc.Project
			root = filepath.Join(base, ".claude", "skills")
		case strings.TrimSpace(loc.ClaudeConfigDir) != "":
			base = loc.ClaudeConfigDir
			root = filepath.Join(base, "skills")
		default:
			base = loc.Home
			root = filepath.Join(base, ".claude", "skills")
		}
	case apps.Codex:
		if loc.Project != "" {
			base = loc.Project
		} else {
			base = loc.Home
		}
		root = filepath.Join(base, ".agents", "skills")
	default:
		return "", "", fmt.Errorf("the image Skill cannot be installed for %q (supported: claude-code, codex)", id)
	}
	if base == "" {
		return "", "", fmt.Errorf("cannot find the home directory")
	}
	return root, base, nil
}

// State is what is at the Skill folder now.
type State int

const (
	// Missing means nothing is there; install creates it.
	Missing State = iota
	// Same means the folder already holds exactly the embedded files.
	Same
	// Different means the folder exists with other content. Installing
	// replaces it, which needs --force or a confirmation.
	Different
)

// Plan is one install destination, checked but not written.
type Plan struct {
	App   apps.ID
	Base  string
	Root  string
	Dir   string
	State State
	// Diff lists what differs from the embedded Skill when State is
	// Different, such as "changed: SKILL.md" or "extra: notes.txt".
	Diff []string
}

// Check looks at the destination for id without writing. An error means
// deeprouter will not write there at all: a symlink in the way, a file where
// a folder belongs, or a folder that cannot be read.
func Check(id apps.ID, loc Locations) (Plan, error) {
	root, base, err := SkillsRoot(id, loc)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{App: id, Base: base, Root: root, Dir: filepath.Join(root, Name)}
	// Every folder from base down to the Skill folder must be a real
	// folder or missing. A symlink would send the write somewhere the user
	// did not name.
	rel, err := filepath.Rel(base, p.Dir)
	if err != nil || rel == "." || strings.HasPrefix(rel, "..") {
		return Plan{}, fmt.Errorf("%s is not under %s", p.Dir, base)
	}
	cur := base
	for _, part := range strings.Split(rel, string(filepath.Separator)) {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if os.IsNotExist(err) {
			break
		}
		if err != nil {
			return Plan{}, err
		}
		if info.Mode()&fs.ModeSymlink != 0 {
			return Plan{}, fmt.Errorf("%s is a symbolic link; not writing through it", cur)
		}
		if !info.IsDir() {
			return Plan{}, fmt.Errorf("%s exists and is not a directory", cur)
		}
	}
	if _, err := os.Lstat(p.Dir); os.IsNotExist(err) {
		p.State = Missing
		return p, nil
	}
	diff, err := Compare(p.Dir)
	if err != nil {
		return Plan{}, err
	}
	if len(diff) == 0 {
		p.State = Same
	} else {
		p.State = Different
		p.Diff = diff
	}
	return p, nil
}

// Compare lists how dir differs from the embedded Skill: changed, missing,
// and extra files, and anything that is not a plain file or folder. An empty
// list means the tree is identical.
func Compare(dir string) ([]string, error) {
	want := Files()
	var diff []string
	seen := map[string]bool{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			diff = append(diff, "symlink: "+rel)
		case d.IsDir():
			if !wantsDir(want, rel) {
				diff = append(diff, "extra: "+rel+"/")
				return fs.SkipDir
			}
		case !d.Type().IsRegular():
			diff = append(diff, "extra: "+rel)
		default:
			b, ok := want[rel]
			if !ok {
				diff = append(diff, "extra: "+rel)
				return nil
			}
			seen[rel] = true
			got, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if !bytes.Equal(got, b) {
				diff = append(diff, "changed: "+rel)
			}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	for _, rel := range FileList() {
		if !seen[rel] && !contains(diff, "changed: "+rel) && !contains(diff, "symlink: "+rel) {
			diff = append(diff, "missing: "+rel)
		}
	}
	sort.Strings(diff)
	return diff, nil
}

func wantsDir(want map[string][]byte, rel string) bool {
	for name := range want {
		if strings.HasPrefix(name, rel+"/") {
			return true
		}
	}
	return false
}

func contains(list []string, s string) bool {
	for _, item := range list {
		if item == s {
			return true
		}
	}
	return false
}
