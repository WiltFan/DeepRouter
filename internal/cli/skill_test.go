package cli

import (
	"bytes"
	"context"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

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
	if code != 0 || !strings.Contains(out, "Install image Skill") || !strings.Contains(out, "~/.agents/skills/deeprouter-image-api") ||
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
		"~/.claude/skills/deeprouter-image-api", "~/.agents/skills/deeprouter-image-api",
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
	for _, bad := range []string{"grok", "gemini", "claude-desktop", "codex,grok", "cursor", "3"} {
		home := t.TempDir()
		_, errOut, code := runSkill(t, home, nil, "skill", "install", "--apps", bad)
		if code != 2 || !strings.Contains(errOut, "deeprouter:") {
			t.Fatalf("%s: %d %s", bad, code, errOut)
		}
		assertEmptyHome(t, home)
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
	out, errOut, code := runSkillMenu(t, home, "\n\n\n\n")
	if code != 0 {
		t.Fatalf("%d\n%s\n%s", code, out, errOut)
	}
	for _, want := range []string{"1) Install image Skill", "Programs [1,2]: ", "Scope [1]: ", "deeprouter will write:", "Install? [Y/n]"} {
		if !strings.Contains(errOut, want) {
			t.Fatalf("missing %q:\n%s", want, errOut)
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
	_, errOut, code := runSkill(t, home, bytes.NewReader([]byte("\n2、1\n\ny\n")), "--lang", "zh", "skill")
	if code != 0 || !strings.Contains(errOut, "1) 安装生图Skill") || !strings.Contains(errOut, "程序 [1,2]：") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	assertSkillTree(t, codexSkillDir(home))
}

func TestSkillMenuPicksOneTarget(t *testing.T) {
	home := t.TempDir()
	// 1) Codex / ChatGPT, 2) Claude Code. A bad answer asks again.
	_, errOut, code := runSkillMenu(t, home, "1\ngrok\n2\n\n\n")
	if code != 0 || !strings.Contains(errOut, `not "grok"`) {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, claudeSkillDir(home))
	if _, err := os.Lstat(filepath.Join(home, ".agents")); !os.IsNotExist(err) {
		t.Fatal("codex written")
	}
}

func TestSkillMenuProject(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	missing := filepath.Join(project, "nope")
	_, errOut, code := runSkillMenu(t, home, "\n1\n2\n"+missing+"\n"+project+"\ny\n")
	if code != 0 || !strings.Contains(errOut, "does not exist") {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, filepath.Join(project, ".agents", "skills", imageskill.Name))
	assertEmptyHome(t, home)
}

func TestSkillMenuProjectDefaultsToWorkingDir(t *testing.T) {
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
	_, errOut, code := runSkillMenu(t, home, "\n2\n2\n\n\n")
	if code != 0 {
		t.Fatalf("%d\n%s", code, errOut)
	}
	assertSkillTree(t, filepath.Join(project, ".claude", "skills", imageskill.Name))
	assertEmptyHome(t, home)
}

func TestSkillMenuCancelWritesNothing(t *testing.T) {
	cases := map[string]string{
		"quit":            "2\n",
		"eof at menu":     "",
		"eof at programs": "\n",
		"eof at scope":    "\n\n",
		"eof at project":  "\n\n2\n",
		"no at confirm":   "\n\n\nn\n",
		"eof at confirm":  "\n\n\n",
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
	for name, input := range map[string]string{"declined": "\n1\n\nn\n", "enter": "\n1\n\n\n", "eof": "\n1\n\n"} {
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
		out, errOut, code := runSkillMenu(t, home, "\n1\n\ny\n\n")
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
		out, errOut, code := runSkillMenu(t, home, "\n\n\n")
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
