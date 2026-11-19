package cli

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/imageskill"
)

// runSkill is run with the UI language reset first, since --lang zh sets
// a package variable that would leak into the next run.
func runSkill(t *testing.T, home string, stdin *bytes.Reader, args ...string) (string, string, int) {
	t.Helper()
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
	return run(t, home, stdin, args...)
}

func claudeSkillDir(home string) string {
	return filepath.Join(home, ".claude", "skills", imageskill.Name)
}

func codexSkillDir(home string) string {
	return filepath.Join(home, ".agents", "skills", imageskill.Name)
}

// assertSkillTree fails unless dir holds exactly the embedded Skill files.
func assertSkillTree(t *testing.T, dir string) {
	t.Helper()
	want := imageskill.Files()
	n := 0
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if d.Name() == "__pycache__" {
				t.Errorf("__pycache__ installed")
			}
			return nil
		}
		rel, _ := filepath.Rel(dir, path)
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		exp, ok := want[filepath.ToSlash(rel)]
		if !ok {
			t.Errorf("extra file %s", rel)
		} else if !bytes.Equal(b, exp) {
			t.Errorf("%s differs from the embedded Skill", rel)
		}
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if n != len(want) {
		t.Fatalf("%s has %d files, want %d", dir, n, len(want))
	}
}

// skillTreeList is every path under dir, for nothing-written checks.
func skillTreeList(t *testing.T, dir string) []string {
	t.Helper()
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err == nil && path != dir {
			rel, _ := filepath.Rel(dir, path)
			out = append(out, rel)
		}
		return nil
	})
	return out
}

func assertEmptyHome(t *testing.T, home string) {
	t.Helper()
	if got := skillTreeList(t, home); len(got) != 0 {
		t.Fatalf("wrote %v", got)
	}
}

func TestSkillHelp(t *testing.T) {
	home := t.TempDir()
	out, _, code := runSkill(t, home, nil, "skill", "--help")
	if code != 0 || !strings.Contains(out, "Install image Skill") || !strings.Contains(out, "~/.agents/skills/image-generate") ||
		!strings.Contains(out, "--force") || !strings.Contains(out, "CLAUDE_CONFIG_DIR") {
		t.Fatalf("%d\n%s", code, out)
	}
	out, _, code = runSkill(t, home, nil, "--lang", "zh", "skill", "install", "--help")
	if code != 0 {
		t.Fatal(code)
	}
	_, errOut, _ := runSkill(t, home, nil, "--lang", "zh", "skill", "install", "--help")
	if !strings.Contains(out+errOut, "安装生图Skill") || !strings.Contains(out+errOut, "--project") {
		t.Fatalf("zh help:\n%s%s", out, errOut)
	}
	en, _, _ := runSkill(t, home, nil, "help")
	zh, _, _ := runSkill(t, home, nil, "--lang", "zh", "help")
	if !strings.Contains(en, "  skill       Install the DeepRouter image Skill") || !strings.Contains(zh, "  skill       把 DeepRouter 生图 Skill") {
		t.Fatalf("root help:\n%s\n%s", en, zh)
	}
	_, errOut, code = runSkill(t, home, nil, "skill", "nope")
	if code != 2 || !strings.Contains(errOut, `unknown skill command "nope"`) {
		t.Fatalf("%d %s", code, errOut)
	}
	_, errOut, code = runSkill(t, home, nil, "skill", "install", "extra")
	if code != 2 || !strings.Contains(errOut, `unexpected argument "extra"`) {
		t.Fatalf("%d %s", code, errOut)
	}
	assertEmptyHome(t, home)
}

func TestSkillInstallDefaultsToBothUserLevel(t *testing.T) {
	home := t.TempDir()
	out, errOut, code := runSkill(t, home, nil, "skill", "install")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	assertSkillTree(t, codexSkillDir(home))
	for _, want := range []string{
		"~/.claude/skills/image-generate", "~/.agents/skills/image-generate",
		"made no API call", "DEEPROUTER_API_KEY", "did not write any key", "may be billed",
	} {
		if !strings.Contains(out, filepath.FromSlash(want)) && !strings.Contains(out, want) {
			t.Fatalf("output missing %q:\n%s", want, out)
		}
	}
	if _, err := os.Lstat(filepath.Join(home, ".codex")); !os.IsNotExist(err) {
		t.Fatal("wrote the legacy ~/.codex folder")
	}
	// Exactly the two Skill folders and their parents exist.
	for _, rel := range skillTreeList(t, home) {
		top := strings.Split(filepath.ToSlash(rel), "/")
		if top[0] != ".claude" && top[0] != ".agents" {
			t.Fatalf("unexpected %s", rel)
		}
	}
}

func TestSkillInstallAppsFlag(t *testing.T) {
	home := t.TempDir()
	_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "claude-code")
	if code != 0 {
		t.Fatal(errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	if _, err := os.Lstat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("codex written without being selected")
	}
	home2 := t.TempDir()
	if _, errOut, code := runSkill(t, home2, nil, "skill", "install", "--apps", "codex，claude-code"); code != 0 {
		t.Fatal(errOut)
	}
	assertSkillTree(t, claudeSkillDir(home2))
	assertSkillTree(t, codexSkillDir(home2))
}

func TestSkillInstallRejectsOtherApps(t *testing.T) {
	for _, bad := range []string{"chatgpt", "ChatGPT", "codex,chatgpt", "cursor", "claud-code", "gemini-app", "8", "0"} {
		home := t.TempDir()
		_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", bad)
		if code != 2 || !strings.Contains(errOut, "deeprouter:") {
			t.Fatalf("%s: %d %s", bad, code, errOut)
		}
		assertEmptyHome(t, home)
	}
	// ChatGPT is not a silent alias for Codex: the error says why.
	_, errOut, _ := runSkill(t, t.TempDir(), nil, "skill", "install", "--apps", "chatgpt")
	if !strings.Contains(errOut, "local Skill loading is unverified") || !strings.Contains(errOut, "use codex") {
		t.Fatal(errOut)
	}
}

func TestSkillOpenCodeAndPiOptIn(t *testing.T) {
	home, overrides := t.TempDir(), t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(overrides, "xdg"))
	t.Setenv("OPENCODE_CONFIG_DIR", filepath.Join(overrides, "oc"))
	t.Setenv("OPENCODE_CONFIG", filepath.Join(overrides, "explicit", "opencode.jsonc"))
	t.Setenv("PI_CODING_AGENT_DIR", "~/pi-agent")
	image := filepath.Join(home, ".deeprouter", "image-skill.json")
	provider := filepath.Join(overrides, "oc", "opencode.jsonc")
	piProvider := filepath.Join(home, "pi-agent", "models.json")
	for _, path := range []string{image, provider, piProvider} {
		mustMkdir(t, filepath.Dir(path))
		mustWrite(t, path, "fake-config-sentinel", 0o600)
	}
	stubCLIs(t, func(string) (string, error) { return "", os.ErrNotExist }, nil)
	out, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "opencode,pi")
	if code != 0 {
		t.Fatal(errOut)
	}
	for _, fragment := range []string{"@image-generate", "/skill:image-generate", "opencode: not found on PATH", "pi: not found on PATH", "OpenCode Desktop: desktop presence not confirmed"} {
		if !strings.Contains(out, fragment) {
			t.Fatalf("missing %q in %s", fragment, out)
		}
	}
	assertSkillTree(t, filepath.Join(overrides, "oc", "skills", imageskill.Name))
	assertSkillTree(t, filepath.Join(home, "pi-agent", "skills", imageskill.Name))
	for _, path := range []string{image, provider, piProvider} {
		if mustRead(t, path) != "fake-config-sentinel" {
			t.Fatalf("changed %s", path)
		}
	}
	for _, path := range []string{filepath.Join(overrides, "explicit"), filepath.Join(overrides, "xdg")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected path %s: %v", path, err)
		}
	}
}

func TestSkillInstallProject(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	out, errOut, code := runSkill(t, home, nil, "skill", "install", "--project", project)
	if code != 0 {
		t.Fatal(errOut)
	}
	assertSkillTree(t, filepath.Join(project, ".claude", "skills", imageskill.Name))
	assertSkillTree(t, filepath.Join(project, ".agents", "skills", imageskill.Name))
	assertEmptyHome(t, home)
	if !strings.Contains(out, project) {
		t.Fatalf("project path not shown:\n%s", out)
	}

	// A missing directory, a file, and an empty value write nothing.
	file := filepath.Join(project, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, bad := range []string{filepath.Join(project, "missing"), file, ""} {
		home := t.TempDir()
		_, errOut, code := runSkill(t, home, nil, "skill", "install", "--project", bad)
		if code != 2 || !strings.Contains(errOut, "project") {
			t.Fatalf("%q: %d %s", bad, code, errOut)
		}
		assertEmptyHome(t, home)
	}
	if _, err := os.Lstat(filepath.Join(project, "missing")); !os.IsNotExist(err) {
		t.Fatal("created the missing project")
	}
}

func TestSkillInstallClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	isolateHome(t, home)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	uiLang = ""
	var out, errOut bytes.Buffer
	if code := mainIO([]string{"skill", "install", "--apps", "claude-code"}, "0.1.0", &out, &errOut, bytes.NewReader(nil)); code != 0 {
		t.Fatal(errOut.String())
	}
	assertSkillTree(t, filepath.Join(cfg, "skills", imageskill.Name))
	assertEmptyHome(t, home)
	// CLAUDE_CONFIG_DIR does not move the Codex folder.
	if code := mainIO([]string{"skill", "install", "--apps", "codex"}, "0.1.0", &out, &errOut, bytes.NewReader(nil)); code != 0 {
		t.Fatal(errOut.String())
	}
	assertSkillTree(t, codexSkillDir(home))
}

func TestSkillInstallConflictAndForce(t *testing.T) {
	edits := map[string]func(dir string) error{
		"modified": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("my edit"), 0o644)
		},
		"extra": func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "my-notes.md"), []byte("mine"), 0o644)
		},
		"missing": func(dir string) error {
			return os.Remove(filepath.Join(dir, "scripts", "deeprouter_image.py"))
		},
	}
	for name, edit := range edits {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			if _, errOut, code := runSkill(t, home, nil, "skill", "install"); code != 0 {
				t.Fatal(errOut)
			}
			sibling := filepath.Join(home, ".claude", "skills", "other", "SKILL.md")
			if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(sibling, []byte("other"), 0o644); err != nil {
				t.Fatal(err)
			}
			dir := claudeSkillDir(home)
			if err := edit(dir); err != nil {
				t.Fatal(err)
			}
			before := skillTreeList(t, home)
			out, errOut, code := runSkill(t, home, nil, "skill", "install")
			if code != 1 || !strings.Contains(errOut, "--force") || !strings.Contains(errOut, "Nothing was written") {
				t.Fatalf("%d\n%s\n%s", code, out, errOut)
			}
			if after := skillTreeList(t, home); strings.Join(after, "\n") != strings.Join(before, "\n") {
				t.Fatalf("refused install changed files:\n%v\n%v", before, after)
			}
			out, errOut, code = runSkill(t, home, nil, "skill", "install", "--force")
			if code != 0 || !strings.Contains(out, "(replaced)") {
				t.Fatalf("%d\n%s\n%s", code, out, errOut)
			}
			assertSkillTree(t, dir)
			assertSkillTree(t, codexSkillDir(home))
			if b, _ := os.ReadFile(sibling); string(b) != "other" {
				t.Fatal("sibling skill changed")
			}
			entries, _ := os.ReadDir(filepath.Dir(dir))
			if len(entries) != 2 {
				t.Fatalf("skills folder has %v", entries)
			}
		})
	}
}

func TestSkillInstallRepeatIsNoop(t *testing.T) {
	home := t.TempDir()
	if _, errOut, code := runSkill(t, home, nil, "skill", "install"); code != 0 {
		t.Fatal(errOut)
	}
	out, errOut, code := runSkill(t, home, nil, "skill", "install")
	if code != 0 || strings.Count(out, "(already installed, unchanged)") != 2 {
		t.Fatalf("%d\n%s\n%s", code, out, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	assertSkillTree(t, codexSkillDir(home))
}

func TestSkillInstallRefusesSymlink(t *testing.T) {
	home := t.TempDir()
	target := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, imageskill.Name)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_, errOut, code := runSkill(t, home, nil, "skill", "install", "--force")
	if code != 1 || !strings.Contains(errOut, "symbolic link") {
		t.Fatalf("%d %s", code, errOut)
	}
	if got := skillTreeList(t, target); len(got) != 0 {
		t.Fatalf("wrote through the symlink: %v", got)
	}
	// The check runs for every target first, so Claude Code was not written either.
	if _, err := os.Lstat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("wrote Claude Code before refusing Codex")
	}
}

// TestSkillInstallNoKeyNoNetwork sets a key and saved configs full of a
// sentinel, and fails on any outbound dial.
func TestSkillInstallNoKeyNoNetwork(t *testing.T) {
	const sentinel = "sk-SENTINEL-do-not-copy-0123456789"
	home := t.TempDir()
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", sentinel)
	for rel, text := range map[string]string{
		".deeprouter/config.json":  `{"profiles":[{"name":"default","api_key":"` + sentinel + `"}]}`,
		".claude/settings.json":    `{"env":{"ANTHROPIC_AUTH_TOKEN":"` + sentinel + `"}}`,
		".codex/config.toml":       `experimental_bearer_token = "` + sentinel + `"`,
		".codex/auth.json":         `{"OPENAI_API_KEY":"` + sentinel + `"}`,
		".env":                     "DEEPROUTER_API_KEY=" + sentinel + "\n",
		".agents/skills/other/key": sentinel,
	} {
		path := filepath.Join(home, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	tr := http.DefaultTransport.(*http.Transport)
	prevDial := tr.DialContext
	dials := 0
	tr.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
		dials++
		return nil, io.ErrUnexpectedEOF
	}
	t.Cleanup(func() { tr.DialContext = prevDial })

	uiLang = ""
	var out, errOut bytes.Buffer
	if code := mainIO([]string{"skill", "install"}, "0.1.0", &out, &errOut, bytes.NewReader(nil)); code != 0 {
		t.Fatal(errOut.String())
	}
	if dials != 0 {
		t.Fatalf("install dialed %d times", dials)
	}
	if strings.Contains(out.String()+errOut.String(), sentinel) {
		t.Fatal("output contains the key")
	}
	for _, dir := range []string{claudeSkillDir(home), codexSkillDir(home)} {
		assertSkillTree(t, dir)
		_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, _ := os.ReadFile(path); bytes.Contains(b, []byte(sentinel)) {
					t.Errorf("%s contains the key", path)
				}
			}
			return nil
		})
	}
}

func TestSkillMenuNonTTY(t *testing.T) {
	home := t.TempDir()
	_, errOut, code := runSkill(t, home, bytes.NewReader([]byte("1\n\n\n\n")), "skill")
	if code != 2 || !strings.Contains(errOut, "Run deeprouter skill in a terminal.") || !strings.Contains(errOut, "deeprouter skill install") {
		t.Fatalf("%d %s", code, errOut)
	}
	if strings.Contains(errOut, "Choose [1]") {
		t.Fatal("prompted without a terminal")
	}
	assertEmptyHome(t, home)
	_, errOut, _ = runSkill(t, home, nil, "--lang", "zh", "skill")
	if !strings.Contains(errOut, "请在终端中运行 deeprouter skill。") {
		t.Fatal(errOut)
	}
}

func runSkillMenu(t *testing.T, home, input string) (string, string, int) {
	t.Helper()
	withTerminal(t)
	return runSkill(t, home, bytes.NewReader([]byte(input)), "skill")
}

func TestSkillMenuDefaults(t *testing.T) {
	home := t.TempDir()
	out, errOut, code := runSkillMenu(t, home, "\n\n")
	if code != 0 {
		t.Fatalf("%d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{"1) Install image Skill", "Programs [1,2]: ", "deeprouter will write:"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("missing %q:\n%s", want, errOut)
		}
	}
	for _, not := range []string{"Install for:", "Scope [1]", "Project directory", "Install? [Y/n]"} {
		if strings.Contains(errOut, not) {
			t.Fatalf("asked %q:\n%s", not, errOut)
		}
	}
	if strings.Contains(errOut+out, "token:") || strings.Contains(errOut, "令牌：") {
		t.Fatal("asked for a key")
	}
	assertSkillTree(t, claudeSkillDir(home))
	assertSkillTree(t, codexSkillDir(home))
	if !strings.Contains(out, "made no API call") {
		t.Fatal(out)
	}
}

func TestSkillMenuZH(t *testing.T) {
	home := t.TempDir()
	withTerminal(t)
	_, errOut, code := runSkill(t, home, bytes.NewReader([]byte("\n2、1\n")), "--lang", "zh", "skill")
	if code != 0 || !strings.Contains(errOut, "1) 安装生图Skill") || !strings.Contains(errOut, "程序 [1,2]：") ||
		strings.Contains(errOut, "范围") || strings.Contains(errOut, "安装？") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	assertSkillTree(t, codexSkillDir(home))
}

func TestSkillMenuPicksOneTarget(t *testing.T) {
	home := t.TempDir()
	// 1) Codex, 2) Claude Code, ... 5) Gemini CLI. A bad answer asks again.
	_, errOut, code := runSkillMenu(t, home, "1\nchatgpt\n8\n2\n")
	if code != 0 || !strings.Contains(errOut, "ChatGPT Chat/Work local Skill loading is unverified") || !strings.Contains(errOut, "program 8 is out of range") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	if _, err := os.Lstat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("codex written")
	}
}

// TestSkillMenuInstallsForTheUserNotTheProject: the menu has no scope
// question, so it installs into the user Skill folders even when run from
// a project. One project is skill install --project only.
func TestSkillMenuInstallsForTheUserNotTheProject(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(project); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	_, errOut, code := runSkillMenu(t, home, "\n2\n")
	if code != 0 || strings.Contains(errOut, "Project directory") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	if entries, _ := os.ReadDir(project); len(entries) != 0 {
		t.Fatalf("wrote into the project: %v", entries)
	}
}

func TestSkillMenuCancelWritesNothing(t *testing.T) {
	cases := map[string]string{
		"quit":                  "2\n",
		"eof at menu":           "",
		"eof at programs":       "\n",
		"eof after bad program": "\n9\n",
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			_, errOut, code := runSkillMenu(t, home, input)
			if code != 2 || !strings.Contains(errOut, "Cancelled. Nothing was written.") {
				t.Fatalf("%d\n%s", code, errOut)
			}
			assertEmptyHome(t, home)
		})
	}
}

func TestSkillMenuConflict(t *testing.T) {
	setup := func(t *testing.T) (string, string) {
		home := t.TempDir()
		if _, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "codex"); code != 0 {
			t.Fatal(errOut)
		}
		skill := filepath.Join(codexSkillDir(home), "SKILL.md")
		if err := os.WriteFile(skill, []byte("my edit"), 0o644); err != nil {
			t.Fatal(err)
		}
		return home, skill
	}
	for name, input := range map[string]string{"declined": "\n1\nn\n", "enter": "\n1\n\n", "eof": "\n1\n"} {
		t.Run(name, func(t *testing.T) {
			home, skill := setup(t)
			_, errOut, code := runSkillMenu(t, home, input)
			if code != 2 || !strings.Contains(errOut, "changed: SKILL.md") || !strings.Contains(errOut, "[y/N]") {
				t.Fatalf("%d\n%s", code, errOut)
			}
			if b, _ := os.ReadFile(skill); string(b) != "my edit" {
				t.Fatal("overwritten after cancel")
			}
		})
	}
	t.Run("accepted", func(t *testing.T) {
		home, _ := setup(t)
		out, errOut, code := runSkillMenu(t, home, "\n1\ny\n")
		if code != 0 || !strings.Contains(out, "(replaced)") {
			t.Fatalf("%d\n%s\n%s", code, out, errOut)
		}
		assertSkillTree(t, codexSkillDir(home))
	})
	t.Run("already installed", func(t *testing.T) {
		home := t.TempDir()
		if _, errOut, code := runSkill(t, home, nil, "skill", "install"); code != 0 {
			t.Fatal(errOut)
		}
		out, errOut, code := runSkillMenu(t, home, "\n\n")
		if code != 0 || strings.Count(out, "(already installed, unchanged)") != 2 || strings.Contains(errOut, "Install? [Y/n]") {
			t.Fatalf("%d\n%s\n%s", code, out, errOut)
		}
	})
}

func TestSkillGuardMessages(t *testing.T) {
	uiLang = "en"
	t.Cleanup(func() { uiLang = "" })
	var buf bytes.Buffer
	g := &skillGuard{}
	if code := g.interrupted(&buf); code != 130 || !strings.Contains(buf.String(), "Nothing was written") {
		t.Fatalf("%d %s", code, buf.String())
	}
	buf.Reset()
	g = &skillGuard{wrote: true}
	if code := g.interrupted(&buf); code != 130 || !strings.Contains(buf.String(), "already installed") {
		t.Fatalf("%d %s", code, buf.String())
	}
}

// skillDirs is every image-generate folder under the roots, relative
// to its root and with forward slashes.
func skillDirs(t *testing.T, roots ...string) []string {
	t.Helper()
	var out []string
	for _, root := range roots {
		_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err == nil && d.IsDir() && d.Name() == imageskill.Name {
				rel, _ := filepath.Rel(root, path)
				out = append(out, filepath.Base(root)+":"+filepath.ToSlash(rel))
				return filepath.SkipDir
			}
			return nil
		})
	}
	return out
}

func TestSkillInstallNewTargets(t *testing.T) {
	const s = "/skills/" + imageskill.Name
	cases := []struct {
		apps    string
		project bool
		want    []string // folders under HOME ("h:") or the project ("p:")
		notes   []string
	}{
		{"claude-desktop", false, []string{"h:.claude" + s}, nil},
		{"gemini", false, []string{"h:.agents" + s}, nil},
		{"grok", false, []string{"h:.grok" + s}, nil},
		{"grok,codex", false, []string{"h:.agents" + s}, []string{"Shared folder: Codex, Grok Build", "Grok Build also discovers user Skills in ~/.agents/skills"}},
		{"gemini,codex", false, []string{"h:.agents" + s}, []string{"Shared folder: Codex, Gemini CLI"}},
		{"grok,gemini", false, []string{"h:.agents" + s}, []string{"Shared folder: Grok Build, Gemini CLI"}},
		{"claude-code,claude-desktop", false, []string{"h:.claude" + s}, []string{"Shared folder: Claude Code, Claude Desktop"}},
		{"grok,claude-code", false, []string{"h:.claude" + s, "h:.grok" + s}, nil},
		{"codex,claude-code,claude-desktop,grok,gemini", false, []string{"h:.agents" + s, "h:.claude" + s}, []string{"Shared folder: Codex, Grok Build, Gemini CLI", "Shared folder: Claude Code, Claude Desktop"}},
		{"claude-desktop", true, []string{"p:.claude" + s}, nil},
		{"gemini", true, []string{"p:.agents" + s}, nil},
		{"grok", true, []string{"p:.grok" + s}, nil},
		{"grok,codex", true, []string{"p:.agents" + s, "p:.grok" + s}, nil},
		{"3,4,5", true, []string{"p:.agents" + s, "p:.claude" + s, "p:.grok" + s}, nil},
	}
	for _, c := range cases {
		t.Run(fmt.Sprintf("%s project=%v", c.apps, c.project), func(t *testing.T) {
			home := filepath.Join(t.TempDir(), "h")
			project := filepath.Join(t.TempDir(), "p")
			for _, dir := range []string{home, project} {
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			args := []string{"skill", "install", "--apps", c.apps}
			if c.project {
				args = append(args, "--project", project)
			}
			out, errOut, code := runSkill(t, home, nil, args...)
			if code != 0 {
				t.Fatalf("%d\n%s\n%s", code, out, errOut)
			}
			got := skillDirs(t, home, project)
			sort.Strings(got)
			if strings.Join(got, " ") != strings.Join(c.want, " ") {
				t.Fatalf("installed %v, want %v\n%s", got, c.want, out)
			}
			for _, rel := range got {
				root := home
				if strings.HasPrefix(rel, "p:") {
					root = project
				}
				assertSkillTree(t, filepath.Join(root, filepath.FromSlash(rel[2:])))
			}
			for _, note := range c.notes {
				if !strings.Contains(out, note) {
					t.Fatalf("missing %q:\n%s", note, out)
				}
			}
			if strings.Count(out, imageskill.Name+"  (installed)") != len(c.want) {
				t.Fatalf("each folder should be listed once:\n%s", out)
			}
			for _, bad := range []string{".gemini", ".codex"} {
				for _, root := range []string{home, project} {
					if _, err := os.Lstat(filepath.Join(root, bad)); !os.IsNotExist(err) {
						t.Fatalf("created %s in %s", bad, root)
					}
				}
			}
			if c.project {
				assertEmptyHome(t, home)
			}
		})
	}
}

func TestSkillInstallReloadHints(t *testing.T) {
	home := t.TempDir()
	out, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "claude-desktop,grok,gemini")
	if code != 0 {
		t.Fatal(errOut)
	}
	for _, want := range []string{
		"Claude Desktop: start a new local session in the Code tab, then type / or use + > Slash commands",
		"Chat, Cowork, SSH, and cloud sessions do not read this folder.",
		"Grok Build: open the extensions view with /skills in the grok TUI, or run grok inspect",
		"Gemini CLI: run /skills reload",
		"no program settings were changed", "Python 3.8+",
	} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Start a new Claude Code") || strings.Contains(out, "Codex session") {
		t.Fatalf("hints for programs that were not selected:\n%s", out)
	}
	out, _, _ = runSkill(t, t.TempDir(), nil, "skill", "install")
	if !strings.Contains(out, "Start a new Claude Code or Codex session") || strings.Contains(out, "Grok") || strings.Contains(out, "Gemini") || strings.Contains(out, "Claude Desktop") {
		t.Fatalf("default install hints:\n%s", out)
	}
}

func TestSkillInstallDesktopIgnoresClaudeConfigDir(t *testing.T) {
	home := t.TempDir()
	cfg := t.TempDir()
	isolateHome(t, home)
	t.Setenv("CLAUDE_CONFIG_DIR", cfg)
	uiLang = ""
	var out, errOut bytes.Buffer
	if code := mainIO([]string{"skill", "install", "--apps", "claude-code,claude-desktop"}, "0.1.0", &out, &errOut, bytes.NewReader(nil)); code != 0 {
		t.Fatal(errOut.String())
	}
	assertSkillTree(t, filepath.Join(cfg, "skills", imageskill.Name))
	assertSkillTree(t, claudeSkillDir(home))
	if !strings.Contains(out.String(), "They are separate folders.") || strings.Contains(out.String(), "Shared folder") {
		t.Fatal(out.String())
	}
	// CLAUDE_CONFIG_DIR at ~/.claude is one folder for both.
	home2 := t.TempDir()
	isolateHome(t, home2)
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home2, ".claude"))
	if err := os.Mkdir(filepath.Join(home2, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	if code := mainIO([]string{"skill", "install", "--apps", "claude-desktop,claude-code"}, "0.1.0", &out, &errOut, bytes.NewReader(nil)); code != 0 {
		t.Fatal(errOut.String())
	}
	assertSkillTree(t, claudeSkillDir(home2))
	if !strings.Contains(out.String(), "Shared folder: Claude Code, Claude Desktop") || strings.Contains(out.String(), "separate folders") {
		t.Fatal(out.String())
	}
}

func TestSkillInstallSharedConflictAndForce(t *testing.T) {
	home := t.TempDir()
	if _, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "codex"); code != 0 {
		t.Fatal(errOut)
	}
	skill := filepath.Join(codexSkillDir(home), "SKILL.md")
	mustWriteFile(t, skill, "my edit")
	before := skillTreeList(t, home)
	_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "gemini,codex,grok")
	if code != 1 || strings.Count(errOut, "already has a different") != 1 || !strings.Contains(errOut, "Codex, Grok Build, Gemini CLI already has") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	if after := skillTreeList(t, home); strings.Join(after, "\n") != strings.Join(before, "\n") {
		t.Fatalf("refused install changed files:\n%v\n%v", before, after)
	}
	out, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "gemini,codex,grok", "--force")
	if code != 0 || strings.Count(out, "(replaced)") != 1 {
		t.Fatalf("%d\n%s\n%s", code, out, errOut)
	}
	assertSkillTree(t, codexSkillDir(home))
	if _, err := os.Lstat(filepath.Join(home, ".grok")); !os.IsNotExist(err) {
		t.Fatal("wrote a second copy for Grok")
	}
	entries, _ := os.ReadDir(filepath.Dir(codexSkillDir(home)))
	if len(entries) != 1 {
		t.Fatalf("skills folder has %v", entries)
	}
}

// TestSkillInstallRollsBackEveryFolder makes the second folder fail to
// write, so the first is undone and nothing is left.
func TestSkillInstallRollsBackEveryFolder(t *testing.T) {
	if runtime.GOOS == "windows" {
		// Mode 0500 does not make a Windows folder read-only, so the
		// install would succeed. TestSkillInstallRollsBackAfterLateFailure
		// covers rollback on every OS.
		t.Skip("folder modes do not block writes on Windows")
	}
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	home := t.TempDir()
	locked := filepath.Join(home, ".grok", "skills")
	if err := os.MkdirAll(locked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o700) })
	_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "claude-code,claude-desktop,grok")
	if code != 1 || !strings.Contains(errOut, "deeprouter:") {
		t.Fatalf("%d %s", code, errOut)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("the Claude folder was not rolled back")
	}
	if got := skillTreeList(t, filepath.Join(home, ".grok")); strings.Join(got, ",") != "skills" {
		t.Fatalf("left %v", got)
	}
}

// TestSkillInstallRollsBackAfterLateFailure checks every folder, then puts a
// file where the second one's parent folder belongs, as if it appeared after
// the check. Installing the second fails, and the first is undone. This uses
// no folder modes, so it also runs on Windows.
func TestSkillInstallRollsBackAfterLateFailure(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)
	plans, err := checkSkillTargets([]apps.ID{apps.ClaudeCode, apps.ClaudeDesktop, apps.Grok}, imageskill.Locations{Home: home})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 || plans[1].Dir != filepath.Join(home, ".grok", "skills", imageskill.Name) {
		t.Fatalf("%+v", plans)
	}
	mustWriteFile(t, filepath.Join(home, ".grok"), "not a folder")
	if err := installSkill(plans); err == nil {
		t.Fatal("install succeeded with a file in the way")
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatal("the Claude folder was not rolled back")
	}
	if got := skillTreeList(t, home); strings.Join(got, ",") != ".grok" {
		t.Fatalf("left %v", got)
	}
}

// TestSkillInstallLeavesProgramConfig installs for every target and checks
// that no program's own settings were created or changed.
func TestSkillInstallLeavesProgramConfig(t *testing.T) {
	const sentinel = "sk-SENTINEL-do-not-copy-0123456789"
	for _, withConfig := range []bool{false, true} {
		home := t.TempDir()
		project := t.TempDir()
		if withConfig {
			for rel, text := range map[string]string{
				".claude/settings.json": `{"env":{"ANTHROPIC_AUTH_TOKEN":"` + sentinel + `"}}`,
				".codex/config.toml":    `experimental_bearer_token = "` + sentinel + `"`,
				".grok/config.toml":     `api_key = "` + sentinel + `"`,
				".gemini/settings.json": `{"apiKey":"` + sentinel + `"}`,
				".gemini/.env":          "GEMINI_API_KEY=" + sentinel + "\n",
			} {
				mustWriteFile(t, filepath.Join(home, filepath.FromSlash(rel)), text)
			}
		}
		before := toolFiles(t, home)
		for _, extra := range [][]string{nil, {"--project", project}} {
			args := append([]string{"skill", "install", "--apps", "codex,claude-code,claude-desktop,grok,gemini"}, extra...)
			out, errOut, code := runSkill(t, home, nil, args...)
			if code != 0 {
				t.Fatal(errOut)
			}
			if strings.Contains(out+errOut, sentinel) {
				t.Fatal("output contains the key")
			}
		}
		assertSameFiles(t, before, toolFiles(t, home))
		for _, root := range []string{home, project} {
			_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
				if err == nil && !d.IsDir() && strings.Contains(filepath.ToSlash(path), "/skills/"+imageskill.Name+"/") {
					if b, _ := os.ReadFile(path); bytes.Contains(b, []byte(sentinel)) {
						t.Errorf("%s contains the key", path)
					}
				}
				return nil
			})
		}
		for _, rel := range []string{".claude/settings.json", ".codex/config.toml", ".grok/config.toml", ".gemini/settings.json"} {
			if _, err := os.Lstat(filepath.Join(project, filepath.FromSlash(rel))); !os.IsNotExist(err) {
				t.Fatalf("project %s created", rel)
			}
		}
	}
}

func TestSkillPlanAppsKeepsSharedPrograms(t *testing.T) {
	plans, err := imageskill.CheckAll(imageskill.Targets(), imageskill.Locations{Home: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 2 {
		t.Fatalf("%+v", plans)
	}
	got := skillPlanApps(plans)
	if fmt.Sprint(got) != fmt.Sprint(imageskill.Targets()) {
		t.Fatalf("skillPlanApps %v", got)
	}
}

func TestSkillMenuNewTargets(t *testing.T) {
	t.Run("rows and Enter default", func(t *testing.T) {
		home := t.TempDir()
		_, errOut, code := runSkillMenu(t, home, "\n\n")
		if code != 0 {
			t.Fatal(errOut)
		}
		menuText := strings.Join(strings.Fields(errOut), " ")
		for _, want := range []string{
			"Press Enter for Codex and Claude Code (1,2)",
			"1) Codex Codex CLI, IDE extension, and Codex in the ChatGPT desktop app. ChatGPT Chat/Work local Skill loading is unverified.",
			"2) Claude Code Claude Code CLI", "3) Claude Desktop local sessions in the Code tab only (not Chat, Cowork, SSH, or cloud sessions)",
			"4) Grok Build the grok CLI / TUI (not Grok chat)", "5) Gemini CLI the gemini CLI (not the Gemini app)",
			"Programs [1,2]:",
		} {
			if !strings.Contains(menuText, want) {
				t.Fatalf("missing %q:\n%s", want, errOut)
			}
		}
		if got := skillDirs(t, home); len(got) != 2 {
			t.Fatalf("Enter installed %v", got)
		}
		for _, bad := range []string{".grok", ".gemini"} {
			if _, err := os.Lstat(filepath.Join(home, bad)); !os.IsNotExist(err) {
				t.Fatalf("Enter wrote %s", bad)
			}
		}
	})
	t.Run("shared folder listed once", func(t *testing.T) {
		home := t.TempDir()
		out, errOut, code := runSkillMenu(t, home, "\n1,4,5\n")
		if code != 0 {
			t.Fatal(errOut)
		}
		if !strings.Contains(errOut, "Codex, Grok Build, Gemini CLI "+filepath.FromSlash("~/.agents/skills/image-generate")) ||
			!strings.Contains(errOut, "Shared folder: Codex, Grok Build, Gemini CLI") || strings.Count(errOut, imageskill.Name+"\n") != 1 {
			t.Fatalf("%s\n%s", errOut, out)
		}
		assertSkillTree(t, codexSkillDir(home))
		if got := skillDirs(t, home); len(got) != 1 {
			t.Fatalf("installed %v", got)
		}
	})
	// One project is the explicit --project flag; the menu has no scope.
	t.Run("project flag", func(t *testing.T) {
		home := t.TempDir()
		project := t.TempDir()
		_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "claude-desktop,grok", "--project", project)
		if code != 0 {
			t.Fatal(errOut)
		}
		assertSkillTree(t, filepath.Join(project, ".claude", "skills", imageskill.Name))
		assertSkillTree(t, filepath.Join(project, ".grok", "skills", imageskill.Name))
		assertEmptyHome(t, home)
	})
	t.Run("cancel", func(t *testing.T) {
		home := t.TempDir()
		_, errOut, code := runSkillMenu(t, home, "\n")
		if code != 2 || !strings.Contains(errOut, "Cancelled. Nothing was written.") {
			t.Fatalf("%d %s", code, errOut)
		}
		assertEmptyHome(t, home)
	})
}

func TestSkillHelpListsTargets(t *testing.T) {
	home := t.TempDir()
	en, _, _ := runSkill(t, home, nil, "skill", "--help")
	zh, _, _ := runSkill(t, home, nil, "--lang", "zh", "skill", "--help")
	for _, out := range []string{en, zh} {
		for _, want := range []string{"claude-desktop", "grok", "gemini", "~/.grok/skills", "claude-code,codex", "Cowork"} {
			if !strings.Contains(out, want) {
				t.Fatalf("missing %q:\n%s", want, out)
			}
		}
		if strings.Contains(out, "Codex / ChatGPT") || strings.Contains(out, "~/.gemini/skills/deeprouter") {
			t.Fatalf("help names ChatGPT as a target or a .gemini folder:\n%s", out)
		}
	}
	if !strings.Contains(en, "(default: claude-code,codex)") || !strings.Contains(zh, "默认 claude-code,codex") {
		t.Fatal("default not stated")
	}
}

// writeLegacySkill copies Skill 1.0.4, the last release installed as
// deeprouter-image-api, to root/deeprouter-image-api.
func writeLegacySkill(t *testing.T, root string) string {
	t.Helper()
	src := filepath.Join("..", "imageskill", "testdata", "legacy-1.0.4")
	dir := filepath.Join(root, imageskill.LegacyName)
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dir, rel), 0o755)
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(dir, rel), b, 0o644)
	})
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestSkillInstallNewNameAndFrontmatter(t *testing.T) {
	home := t.TempDir()
	if _, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "claude-code"); code != 0 {
		t.Fatal(errOut)
	}
	dir := claudeSkillDir(home)
	if filepath.Base(dir) != "image-generate" || imageskill.Name != "image-generate" {
		t.Fatalf("installed to %s", dir)
	}
	b, err := os.ReadFile(filepath.Join(dir, "SKILL.md"))
	if err != nil || !strings.HasPrefix(string(b), "---\nname: image-generate\n") {
		t.Fatalf("SKILL.md frontmatter: %.40q %v", b, err)
	}
	if _, err := os.Lstat(filepath.Join(home, ".claude", "skills", imageskill.LegacyName)); !os.IsNotExist(err) {
		t.Fatal("wrote the old folder name")
	}
}

func TestSkillInstallMigratesBundledLegacy(t *testing.T) {
	home := t.TempDir()
	claudeOld := writeLegacySkill(t, filepath.Join(home, ".claude", "skills"))
	codexOld := writeLegacySkill(t, filepath.Join(home, ".agents", "skills"))
	other := filepath.Join(home, ".claude", "skills", "other-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(other), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(other, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	neighbor := filepath.Join(home, ".agents", "skills", "notes.txt")
	if err := os.WriteFile(neighbor, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	out, errOut, code := runSkill(t, home, nil, "skill", "install")
	if code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	assertSkillTree(t, codexSkillDir(home))
	for _, old := range []string{claudeOld, codexOld} {
		if _, err := os.Lstat(old); !os.IsNotExist(err) {
			t.Fatalf("%s still there", old)
		}
	}
	for _, f := range []string{other, neighbor} {
		if b, err := os.ReadFile(f); err != nil || string(b) != "mine" {
			t.Fatalf("%s changed", f)
		}
	}
	if strings.Count(out, "(installed; replaced the unchanged old deeprouter-image-api)") != 2 || strings.Contains(errOut, "Kept") {
		t.Fatalf("%s\n%s", out, errOut)
	}
	if got := skillDirs(t, filepath.Join(home, ".claude"), filepath.Join(home, ".agents")); len(got) != 2 {
		t.Fatalf("skill folders %v", got)
	}
	if leftovers, _ := filepath.Glob(filepath.Join(home, "*", "skills", ".image-generate.*")); len(leftovers) != 0 {
		t.Fatalf("leftover %v", leftovers)
	}
}

func TestSkillInstallKeepsChangedLegacy(t *testing.T) {
	for _, force := range []bool{false, true} {
		t.Run(fmt.Sprintf("force=%v", force), func(t *testing.T) {
			home := t.TempDir()
			old := writeLegacySkill(t, filepath.Join(home, ".claude", "skills"))
			note := filepath.Join(old, "notes.txt")
			if err := os.WriteFile(note, []byte("mine"), 0o644); err != nil {
				t.Fatal(err)
			}
			args := []string{"skill", "install"}
			if force {
				args = append(args, "--force")
			}
			out, errOut, code := runSkill(t, home, nil, args...)
			if code != 1 {
				t.Fatalf("code %d\n%s\n%s", code, out, errOut)
			}
			// The changed old folder stays, and no second copy goes beside it.
			if b, err := os.ReadFile(note); err != nil || string(b) != "mine" {
				t.Fatalf("old folder changed: %v", err)
			}
			if _, err := os.Lstat(filepath.Join(old, "SKILL.md")); err != nil {
				t.Fatal(err)
			}
			if _, err := os.Lstat(claudeSkillDir(home)); !os.IsNotExist(err) {
				t.Fatal("installed image-generate beside a changed old folder")
			}
			// The other destination is installed as usual.
			assertSkillTree(t, codexSkillDir(home))
			for _, want := range []string{
				"Kept " + filepath.FromSlash("~/.claude/skills/deeprouter-image-api") + ": it is not an unmodified copy of the previously bundled Skill",
				"The new image-generate folder was not added beside it, so the Skill is not discovered twice.",
			} {
				if !strings.Contains(errOut, want) {
					t.Fatalf("missing %q:\n%s", want, errOut)
				}
			}
			if !strings.Contains(out, "(not installed: the old deeprouter-image-api folder was kept)") {
				t.Fatal(out)
			}
		})
	}
}

func TestSkillMenuKeepsChangedLegacyZH(t *testing.T) {
	home := t.TempDir()
	old := writeLegacySkill(t, filepath.Join(home, ".claude", "skills"))
	if err := os.WriteFile(filepath.Join(old, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	withTerminal(t)
	_, errOut, code := runSkill(t, home, bytes.NewReader([]byte("\n2\n")), "--lang", "zh", "skill")
	if code != 0 || !strings.Contains(errOut, "它不是未改动过的旧版内置 Skill，deeprouter 不会删除它。") ||
		!strings.Contains(errOut, "没有在旁边新装 image-generate，以免同一个 Skill 被发现两次。") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	if b, err := os.ReadFile(filepath.Join(old, "SKILL.md")); err != nil || string(b) != "mine" {
		t.Fatal("old folder changed")
	}
	if _, err := os.Lstat(claudeSkillDir(home)); !os.IsNotExist(err) {
		t.Fatal("installed beside a changed old folder")
	}
}

func TestSkillInstallProjectMigratesLegacy(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	old := writeLegacySkill(t, filepath.Join(project, ".grok", "skills"))
	_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", "grok", "--project", project)
	if code != 0 {
		t.Fatal(errOut)
	}
	assertSkillTree(t, filepath.Join(project, ".grok", "skills", "image-generate"))
	if _, err := os.Lstat(old); !os.IsNotExist(err) {
		t.Fatal("old project folder still there")
	}
	assertEmptyHome(t, home)
}
