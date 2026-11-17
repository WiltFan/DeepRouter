// Package imageskill installs the DeepRouter image Skill into the Skill
// directories of Codex, Claude Code, Claude Desktop (local Code sessions),
// Grok Build, and Gemini CLI.
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
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/zfg5118/deeprouter/internal/apps"
)

// Name is the Skill folder name and the name in SKILL.md frontmatter.
const Name = "image-generate"

// LegacyName is the folder name of Skill 1.0.5 and earlier. See Legacy.
const LegacyName = "deeprouter-image-api"

// Version is metadata.version in the embedded SKILL.md.
const Version = "1.0.6"

// The four Skill files, verbatim from embed/ (Skill 1.0.6: the delivered
// standalone 1.0.1, the download-recovery helper from 1.0.2, the image
// address and token sources from 1.0.3, the saved model with the
// gpt-image-2.6-flare default from 1.0.4, the gpt-image-* / dall-e
// model-list docs and the program-list Image Skill item from 1.0.5, and the
// image-generate name from 1.0.6). Listing them one by one keeps a stray
// __pycache__ or editor file out of the binary.
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

// Targets is every program the Skill can be installed into, in picker
// order. Each loads Skills from a documented folder:
//
//	Codex           https://learn.chatgpt.com/docs/build-skills
//	Claude Code     https://code.claude.com/docs/en/skills
//	Claude Desktop  https://code.claude.com/docs/en/desktop (local Code tab only)
//	Grok Build      https://docs.x.ai/build/features/skills-plugins-marketplaces
//	Gemini CLI      https://github.com/google-gemini/gemini-cli/blob/main/docs/cli/skills.md
//
// ChatGPT Chat / Work, Claude Chat / Cowork, and Grok or Gemini chat apps do
// not load a local Skill folder, so they are not targets.
func Targets() []apps.ID {
	return []apps.ID{apps.Codex, apps.ClaudeCode, apps.ClaudeDesktop, apps.Grok, apps.Gemini}
}

// DefaultTargets is what an empty --apps and Enter in the picker install.
// It stays Codex and Claude Code; the other targets are opt-in.
func DefaultTargets() []apps.ID {
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
	// everything it would keep in ~/.claude there instead. Claude Desktop
	// does not document reading it.
	ClaudeConfigDir string
	// Project is a project directory. Empty means a user-level install.
	Project string
}

// SkillsRoot is the directory that holds Skill folders for id, and base is
// the directory under it that deeprouter does not check or create: the
// home directory, CLAUDE_CONFIG_DIR, or the project.
//
//	Claude Code, user:        ~/.claude/skills, or $CLAUDE_CONFIG_DIR/skills
//	Claude Desktop, user:     ~/.claude/skills (CLAUDE_CONFIG_DIR is not used)
//	Claude Code / Desktop, project: <project>/.claude/skills
//	Codex, Gemini CLI, user:  ~/.agents/skills
//	Codex, Gemini CLI, project: <project>/.agents/skills
//	Grok Build, user:         ~/.grok/skills
//	Grok Build, project:      <project>/.grok/skills
//
// Codex still reads the older ~/.codex/skills, but ~/.agents/skills is the
// documented place, and CODEX_HOME does not move it. Gemini CLI reads
// .gemini/skills too, but .agents/skills is its documented alias and wins
// within a tier, so the Skill is not copied to .gemini/skills. Grok Build
// also reads ~/.agents/skills; CheckAll uses that when Codex or Gemini CLI
// is selected too.
func SkillsRoot(id apps.ID, loc Locations) (root, base string, err error) {
	base = loc.Home
	if loc.Project != "" {
		base = loc.Project
	}
	switch id {
	case apps.ClaudeCode:
		if loc.Project == "" && strings.TrimSpace(loc.ClaudeConfigDir) != "" {
			base = loc.ClaudeConfigDir
			root = filepath.Join(base, "skills")
		} else {
			root = filepath.Join(base, ".claude", "skills")
		}
	case apps.ClaudeDesktop:
		root = filepath.Join(base, ".claude", "skills")
	case apps.Codex, apps.Gemini:
		root = filepath.Join(base, ".agents", "skills")
	case apps.Grok:
		root = filepath.Join(base, ".grok", "skills")
	default:
		return "", "", fmt.Errorf("the image Skill cannot be installed for %q (supported: %s)", id, idList(Targets()))
	}
	if base == "" {
		return "", "", fmt.Errorf("cannot find the home directory")
	}
	return root, base, nil
}

func idList(ids []apps.ID) string {
	var out []string
	for _, id := range ids {
		out = append(out, string(id))
	}
	return strings.Join(out, ", ")
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

// Legacy is what is at the old deeprouter-image-api folder next to the
// Skill folder, in the same skills root.
type Legacy int

const (
	// NoLegacy means there is no old folder.
	NoLegacy Legacy = iota
	// LegacyBundled means the old folder holds exactly the files of a Skill
	// deeprouter installed under the old name. Installing removes it once
	// the new folder is in place, so the Skill is not discovered twice.
	LegacyBundled
	// LegacyKept means the old folder is something else: changed, with
	// extra files, a symlink, or not a folder. deeprouter never removes it.
	// When the new folder is missing too, nothing is installed there (see
	// Plan.Skipped).
	LegacyKept
)

// legacyBundles is SHA-256 of every file of each Skill deeprouter installed
// as deeprouter-image-api: 1.0.2 (v0.3.18), 1.0.4 (v0.3.19, commit
// 2806116), and the two unreleased 1.0.5 builds (3723534, 451a241). An old
// folder counts as bundled only when it holds exactly one of these file
// sets, byte for byte, and nothing else.
var legacyBundles = []map[string]string{
	{ // 1.0.2
		"SKILL.md":                     "4ffcf6b2fd04c7f060a13b1a38197881b6e5084834d403ea5a4df1c43b3b054d",
		"使用说明.md":                      "0e02f7f3324c2a7d20b79ac8008d964dae12cee5d836f39d2e534457e626470b",
		"references/official-links.md": "2bb0503f42a62bad5511532df7a17b34c5a37c13839c447624a4025134e9e1bc",
		"scripts/deeprouter_image.py":  "a86ea889fa04d648daf2ad6d3267d053e920eb19184f04ae1ff723ec2a417c50",
	},
	{ // 1.0.4
		"SKILL.md":                     "e2c6818e9cf8749d9fe6db7a96d1a3e75ba78fd0f09f1cbd204498a39b6218dc",
		"使用说明.md":                      "cdb290b54858c9bf261115974520d15085f83bb65fc8b04d26548fc73c7217d5",
		"references/official-links.md": "b805d9cf89d89974c9971856a8cb42ddec3626731160b1c8a1570b1b00362f4a",
		"scripts/deeprouter_image.py":  "02eb3958d0950a50afe36cce74bd8780a59b85acc5a18c3fd361ba11c04ef539",
	},
	{ // 1.0.5, 3723534
		"SKILL.md":                     "e8274d5ded940a385203f9e297d057106be22d9b831550ffc092836363a6e78a",
		"使用说明.md":                      "11d67f60fd0abe9b28e5d74c2075f02935c05fbe39d95766bb34e90d9e2dad11",
		"references/official-links.md": "e307612088cf32e434b886c44747f1ad1ece578544e7451b90de7e2853f2fa9f",
		"scripts/deeprouter_image.py":  "02eb3958d0950a50afe36cce74bd8780a59b85acc5a18c3fd361ba11c04ef539",
	},
	{ // 1.0.5, 451a241
		"SKILL.md":                     "e989de4c786b44610b1b26b102c9883d4ca21f4e49a152c6b4d580dec41489a5",
		"使用说明.md":                      "63de6114e6abf5ca058a05256f3f111d099aca0b03a742cfec7f5732d2246d87",
		"references/official-links.md": "e307612088cf32e434b886c44747f1ad1ece578544e7451b90de7e2853f2fa9f",
		"scripts/deeprouter_image.py":  "0eca6e5dd97ae58aaf137a16c23da8f94e616d8bd21cb705b433389158b8a54a",
	},
}

// Plan is one install destination, checked but not written.
type Plan struct {
	// Apps is every selected program that loads the Skill from Dir, in
	// Targets order. Programs that share a folder share one Plan.
	Apps  []apps.ID
	Base  string
	Root  string
	Dir   string
	State State
	// Diff lists what differs from the embedded Skill when State is
	// Different, such as "changed: SKILL.md" or "extra: notes.txt".
	Diff []string
	// LegacyDir is the old deeprouter-image-api folder in Root, and Legacy
	// is what is there.
	LegacyDir string
	Legacy    Legacy
}

// Skipped reports whether nothing is installed at p: the Skill folder is
// missing and an old folder deeprouter did not ship is kept. Adding the
// new folder beside it would make programs list the Skill twice.
func (p Plan) Skipped() bool {
	return p.State == Missing && p.Legacy == LegacyKept
}

// Writes reports whether installing p changes anything on disk.
func (p Plan) Writes() bool {
	return !p.Skipped() && (p.State != Same || p.Legacy == LegacyBundled)
}

// Check looks at the destination for id without writing. An error means
// deeprouter will not write there at all: a symlink in the way, a file where
// a folder belongs, or a folder that cannot be read.
func Check(id apps.ID, loc Locations) (Plan, error) {
	root, base, err := SkillsRoot(id, loc)
	if err != nil {
		return Plan{}, err
	}
	p := Plan{Apps: []apps.ID{id}, Base: base, Root: root, Dir: filepath.Join(root, Name), LegacyDir: filepath.Join(root, LegacyName)}
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
	if p.Legacy, err = checkLegacy(p.LegacyDir); err != nil {
		return Plan{}, err
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

// checkLegacy looks at the old deeprouter-image-api folder. Only a folder
// holding exactly one of legacyBundles is LegacyBundled; anything else that
// exists, including a symlink or a file, is LegacyKept.
func checkLegacy(dir string) (Legacy, error) {
	info, err := os.Lstat(dir)
	if os.IsNotExist(err) {
		return NoLegacy, nil
	}
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return LegacyKept, nil
	}
	ok, err := isLegacyBundle(dir)
	if err != nil {
		return 0, err
	}
	if ok {
		return LegacyBundled, nil
	}
	return LegacyKept, nil
}

// isLegacyBundle reports whether dir holds exactly the files of one of
// legacyBundles: only plain files and folders, no extras, every byte equal.
func isLegacyBundle(dir string) (bool, error) {
	got := map[string]string{}
	var dirs []string
	plain := true
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
		if d.IsDir() {
			dirs = append(dirs, filepath.ToSlash(rel))
			return nil
		}
		if !d.Type().IsRegular() {
			plain = false
			return fs.SkipAll
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(b)
		got[filepath.ToSlash(rel)] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil || !plain {
		return false, err
	}
	// An empty or unknown folder is an extra too.
	for _, d := range dirs {
		if !wantsDir(got, d) {
			return false, nil
		}
	}
	for _, want := range legacyBundles {
		if sameSums(got, want) {
			return true, nil
		}
	}
	return false, nil
}

func sameSums(a, b map[string]string) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if b[k] != v {
			return false
		}
	}
	return true
}

// CheckAll checks every destination for ids before anything is written, so
// one unsafe destination stops the whole install. Each folder appears once,
// with every program that reads it. For a user-level install, Grok Build
// shares ~/.agents/skills when Codex or Gemini CLI is selected too, instead
// of a second copy in ~/.grok/skills.
func CheckAll(ids []apps.ID, loc Locations) ([]Plan, error) {
	agents := loc.Project == "" && (apps.Contains(ids, apps.Codex) || apps.Contains(ids, apps.Gemini))
	var plans []Plan
	for _, id := range ids {
		at := id
		if id == apps.Grok && agents {
			at = apps.Codex
		}
		p, err := Check(at, loc)
		if err != nil {
			return nil, err
		}
		if i := findPlan(plans, p.Dir); i >= 0 {
			if !apps.Contains(plans[i].Apps, id) {
				plans[i].Apps = inTargetOrder(append(plans[i].Apps, id))
			}
			continue
		}
		p.Apps = []apps.ID{id}
		plans = append(plans, p)
	}
	return plans, nil
}

// SharesAgents reports whether Grok Build in p loads the Skill from the
// ~/.agents/skills alias rather than its own ~/.grok/skills.
func SharesAgents(p Plan) bool {
	return apps.Contains(p.Apps, apps.Grok) && filepath.Base(filepath.Dir(p.Root)) == ".agents"
}

func findPlan(plans []Plan, dir string) int {
	for i, p := range plans {
		if sameDir(p.Dir, dir) {
			return i
		}
	}
	return -1
}

// sameDir reports whether a and b are one folder. Besides equal paths, it
// catches two spellings of one existing folder, such as CLAUDE_CONFIG_DIR
// set to a link to ~/.claude, so the folder is not written twice.
func sameDir(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if a == b {
		return true
	}
	ea, ra := existingPart(a)
	eb, rb := existingPart(b)
	if ea == "" || eb == "" || ra != rb {
		return false
	}
	ia, errA := os.Stat(ea)
	ib, errB := os.Stat(eb)
	return errA == nil && errB == nil && os.SameFile(ia, ib)
}

// existingPart splits path into its deepest existing folder and the rest.
func existingPart(path string) (dir, rest string) {
	for {
		if _, err := os.Stat(path); err == nil {
			return path, rest
		}
		parent := filepath.Dir(path)
		if parent == path {
			return "", rest
		}
		rest = filepath.Join(filepath.Base(path), rest)
		path = parent
	}
}

func inTargetOrder(ids []apps.ID) []apps.ID {
	var out []apps.ID
	for _, id := range Targets() {
		if apps.Contains(ids, id) {
			out = append(out, id)
		}
	}
	return out
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

func wantsDir[V any](want map[string]V, rel string) bool {
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
