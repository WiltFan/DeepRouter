package imageskill

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zfg5118/deeprouter/internal/apps"
)

// SHA-256 of the embedded Skill v1.0.2 files (the standalone 1.0.1 package
// plus download recovery). A changed embed must change these on purpose.
var wantSHA = map[string]string{
	"SKILL.md":                     "4ffcf6b2fd04c7f060a13b1a38197881b6e5084834d403ea5a4df1c43b3b054d",
	"使用说明.md":                      "0e02f7f3324c2a7d20b79ac8008d964dae12cee5d836f39d2e534457e626470b",
	"references/official-links.md": "2bb0503f42a62bad5511532df7a17b34c5a37c13839c447624a4025134e9e1bc",
	"scripts/deeprouter_image.py":  "a86ea889fa04d648daf2ad6d3267d053e920eb19184f04ae1ff723ec2a417c50",
}

func TestEmbeddedFilesMatchDeliveredSkill(t *testing.T) {
	files := Files()
	if len(files) != len(wantSHA) || len(FileList()) != len(wantSHA) {
		t.Fatalf("embedded %d files, list %d, want %d", len(files), len(FileList()), len(wantSHA))
	}
	for rel, want := range wantSHA {
		sum := sha256.Sum256(files[rel])
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s sha256 %s, want %s", rel, got, want)
		}
	}
	// Nothing else is in the embedded FS, such as __pycache__.
	var all []string
	err := fs.WalkDir(embedded, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			all = append(all, strings.TrimPrefix(path, "embed/"))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(all)
	want := FileList()
	sort.Strings(want)
	if strings.Join(all, "\n") != strings.Join(want, "\n") {
		t.Fatalf("embedded FS has %v, want %v", all, want)
	}
	skill := string(files["SKILL.md"])
	if !strings.HasPrefix(skill, "---\nname: "+Name+"\n") || !strings.Contains(skill, "  version: \""+Version+"\"\n") {
		t.Fatalf("SKILL.md frontmatter does not say %s %s", Name, Version)
	}
}

func TestSkillsRoot(t *testing.T) {
	home := filepath.Join("home", "me")
	winHome := `C:\Users\me`
	project := filepath.Join("work", "app")
	cases := []struct {
		id       apps.ID
		loc      Locations
		root     string
		base     string
		wantFail bool
	}{
		{apps.ClaudeCode, Locations{Home: home}, filepath.Join(home, ".claude", "skills"), home, false},
		{apps.ClaudeCode, Locations{Home: winHome}, filepath.Join(winHome, ".claude", "skills"), winHome, false},
		{apps.ClaudeCode, Locations{Home: home, ClaudeConfigDir: filepath.Join("cfg", "claude")}, filepath.Join("cfg", "claude", "skills"), filepath.Join("cfg", "claude"), false},
		{apps.ClaudeCode, Locations{Home: home, ClaudeConfigDir: filepath.Join("cfg", "claude"), Project: project}, filepath.Join(project, ".claude", "skills"), project, false},
		{apps.ClaudeCode, Locations{Home: home, Project: project}, filepath.Join(project, ".claude", "skills"), project, false},
		{apps.Codex, Locations{Home: home}, filepath.Join(home, ".agents", "skills"), home, false},
		{apps.Codex, Locations{Home: winHome}, filepath.Join(winHome, ".agents", "skills"), winHome, false},
		// CLAUDE_CONFIG_DIR does not move the Codex folder.
		{apps.Codex, Locations{Home: home, ClaudeConfigDir: "cfg"}, filepath.Join(home, ".agents", "skills"), home, false},
		{apps.Codex, Locations{Home: home, Project: project}, filepath.Join(project, ".agents", "skills"), project, false},
		{apps.Grok, Locations{Home: home}, "", "", true},
		{apps.Gemini, Locations{Home: home}, "", "", true},
		{apps.ClaudeDesktop, Locations{Home: home}, "", "", true},
		{apps.Codex, Locations{}, "", "", true},
	}
	for _, c := range cases {
		root, base, err := SkillsRoot(c.id, c.loc)
		if c.wantFail {
			if err == nil {
				t.Errorf("%s %+v: no error", c.id, c.loc)
			}
			continue
		}
		if err != nil || root != c.root || base != c.base {
			t.Errorf("%s %+v: root %q base %q err %v, want %q %q", c.id, c.loc, root, base, err, c.root, c.base)
		}
		if strings.Contains(root, ".codex") {
			t.Errorf("%s uses the legacy .codex folder: %s", c.id, root)
		}
	}
	if !Supported(apps.ClaudeCode) || !Supported(apps.Codex) || Supported(apps.Grok) || Supported(apps.ClaudeDesktop) || Supported(apps.Gemini) {
		t.Fatal("Supported is wrong")
	}
}

// tree reads every file under dir, keyed by slash path. Folders end in /.
func tree(t *testing.T, dir string) map[string]string {
	t.Helper()
	out := map[string]string{}
	err := filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, path)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			out[rel+"/"] = ""
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		out[rel] = string(b)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func assertInstalled(t *testing.T, dir string) {
	t.Helper()
	got := tree(t, dir)
	want := map[string]string{"./": "", "references/": "", "scripts/": ""}
	for rel, b := range Files() {
		want[rel] = string(b)
	}
	if len(got) != len(want) {
		var names []string
		for k := range got {
			names = append(names, k)
		}
		sort.Strings(names)
		t.Fatalf("installed tree %v", names)
	}
	for rel, b := range want {
		if got[rel] != b {
			t.Fatalf("%s differs from the embedded Skill", rel)
		}
	}
	if diff, err := Compare(dir); err != nil || len(diff) != 0 {
		t.Fatalf("compare %v %v", diff, err)
	}
}

// assertNoTemp fails when an install left a staging or backup folder.
func assertNoTemp(t *testing.T, root string) {
	t.Helper()
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return
	}
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "."+Name) {
			t.Fatalf("leftover %s", e.Name())
		}
	}
}

func mustInstall(t *testing.T, id apps.ID, loc Locations) Plan {
	t.Helper()
	p, err := Check(id, loc)
	if err != nil {
		t.Fatal(err)
	}
	tx, err := Install(p)
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	return p
}

func writeSibling(t *testing.T, root string) string {
	t.Helper()
	sib := filepath.Join(root, "other-skill", "SKILL.md")
	if err := os.MkdirAll(filepath.Dir(sib), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sib, []byte("---\nname: other-skill\n---\nmine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return sib
}

func assertSibling(t *testing.T, sib string) {
	t.Helper()
	b, err := os.ReadFile(sib)
	if err != nil || string(b) != "---\nname: other-skill\n---\nmine\n" {
		t.Fatalf("sibling skill changed: %q %v", b, err)
	}
}

func TestInstallFreshCreatesExactTree(t *testing.T) {
	for _, id := range Targets() {
		home := t.TempDir()
		loc := Locations{Home: home}
		p, err := Check(id, loc)
		if err != nil || p.State != Missing {
			t.Fatalf("%s check %+v %v", id, p, err)
		}
		mustInstall(t, id, loc)
		assertInstalled(t, p.Dir)
		assertNoTemp(t, p.Root)
		if runtime.GOOS != "windows" {
			for _, rel := range FileList() {
				info, err := os.Stat(filepath.Join(p.Dir, filepath.FromSlash(rel)))
				if err != nil || info.Mode().Perm()&0o022 != 0 || info.Mode().Perm()&0o600 != 0o600 {
					t.Fatalf("%s mode %v %v", rel, info.Mode(), err)
				}
			}
			info, _ := os.Stat(p.Root)
			if info.Mode().Perm() != 0o700 {
				t.Fatalf("created skills folder mode %v", info.Mode())
			}
		}
		if p2, err := Check(id, loc); err != nil || p2.State != Same {
			t.Fatalf("after install %+v %v", p2, err)
		}
	}
}

func TestInstallSameWritesNothing(t *testing.T) {
	home := t.TempDir()
	loc := Locations{Home: home}
	p := mustInstall(t, apps.Codex, loc)
	sib := writeSibling(t, p.Root)
	old := time.Now().Add(-48 * time.Hour).Truncate(time.Second)
	skill := filepath.Join(p.Dir, "SKILL.md")
	if err := os.Chtimes(skill, old, old); err != nil {
		t.Fatal(err)
	}
	p2, err := Check(apps.Codex, loc)
	if err != nil || p2.State != Same {
		t.Fatalf("%+v %v", p2, err)
	}
	tx, err := Install(p2)
	if err != nil {
		t.Fatal(err)
	}
	tx.Commit()
	info, _ := os.Stat(skill)
	if !info.ModTime().Equal(old) {
		t.Fatalf("an identical install rewrote SKILL.md")
	}
	assertSibling(t, sib)
	assertInstalled(t, p.Dir)
}

func TestCheckFindsDifferences(t *testing.T) {
	cases := map[string]struct {
		edit func(dir string) error
		want string
	}{
		"changed": {func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "SKILL.md"), []byte("edited"), 0o644)
		}, "changed: SKILL.md"},
		"extra": {func(dir string) error {
			return os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("mine"), 0o644)
		}, "extra: notes.txt"},
		"extra folder": {func(dir string) error {
			return os.MkdirAll(filepath.Join(dir, "scripts", "__pycache__"), 0o755)
		}, "extra: scripts/__pycache__/"},
		"missing": {func(dir string) error {
			return os.Remove(filepath.Join(dir, "references", "official-links.md"))
		}, "missing: references/official-links.md"},
		"empty folder": {func(dir string) error {
			if err := os.RemoveAll(dir); err != nil {
				return err
			}
			return os.Mkdir(dir, 0o700)
		}, "missing: SKILL.md"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			loc := Locations{Home: home}
			p := mustInstall(t, apps.ClaudeCode, loc)
			sib := writeSibling(t, p.Root)
			if err := c.edit(p.Dir); err != nil {
				t.Fatal(err)
			}
			before := tree(t, p.Dir)
			got, err := Check(apps.ClaudeCode, loc)
			if err != nil || got.State != Different || !contains(got.Diff, c.want) {
				t.Fatalf("%+v %v, want %q", got, err, c.want)
			}
			// Check alone writes nothing.
			if after := tree(t, p.Dir); len(after) != len(before) {
				t.Fatal("check changed the folder")
			}
			// Replacing gives exactly the embedded tree back.
			tx, err := Install(got)
			if err != nil {
				t.Fatal(err)
			}
			tx.Commit()
			assertInstalled(t, p.Dir)
			assertNoTemp(t, p.Root)
			assertSibling(t, sib)
		})
	}
}

func TestReplaceFailureKeepsPreviousTree(t *testing.T) {
	home := t.TempDir()
	loc := Locations{Home: home}
	p := mustInstall(t, apps.Codex, loc)
	sib := writeSibling(t, p.Root)
	note := filepath.Join(p.Dir, "notes.txt")
	if err := os.WriteFile(note, []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	before := tree(t, p.Dir)
	plan, err := Check(apps.Codex, loc)
	if err != nil || plan.State != Different {
		t.Fatal(plan, err)
	}
	prev := rename
	t.Cleanup(func() { rename = prev })
	rename = func(from, to string) error {
		if strings.Contains(filepath.Base(from), ".tmp-") {
			return errors.New("injected rename failure")
		}
		return prev(from, to)
	}
	if _, err := Install(plan); err == nil || !strings.Contains(err.Error(), "injected") {
		t.Fatalf("err %v", err)
	}
	after := tree(t, p.Dir)
	if len(after) != len(before) || after["notes.txt"] != "mine" {
		t.Fatalf("previous tree not restored: %v", after)
	}
	assertNoTemp(t, p.Root)
	assertSibling(t, sib)
}

func TestWriteFailureLeavesNothing(t *testing.T) {
	home := t.TempDir()
	loc := Locations{Home: home}
	prev := writeFile
	t.Cleanup(func() { writeFile = prev })
	calls := 0
	writeFile = func(name string, data []byte, perm os.FileMode) error {
		calls++
		if calls == 3 {
			return errors.New("injected write failure")
		}
		return prev(name, data, perm)
	}
	plan, err := Check(apps.ClaudeCode, loc)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Install(plan); err == nil {
		t.Fatal("no error")
	}
	// .claude and .claude/skills were made by this install and are gone.
	if _, err := os.Lstat(filepath.Join(home, ".claude")); !os.IsNotExist(err) {
		t.Fatalf(".claude left behind: %v", err)
	}

	// A skills folder that was there before stays, with its other skills.
	sib := writeSibling(t, plan.Root)
	calls = 0
	plan, _ = Check(apps.ClaudeCode, loc)
	if _, err := Install(plan); err == nil {
		t.Fatal("no error")
	}
	assertSibling(t, sib)
	if _, err := os.Lstat(plan.Dir); !os.IsNotExist(err) {
		t.Fatal("partial Skill folder left behind")
	}
	assertNoTemp(t, plan.Root)
}

func TestRollbackRestoresPreviousTree(t *testing.T) {
	home := t.TempDir()
	loc := Locations{Home: home}
	p := mustInstall(t, apps.Codex, loc)
	if err := os.WriteFile(filepath.Join(p.Dir, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan, _ := Check(apps.Codex, loc)
	tx, err := Install(plan)
	if err != nil {
		t.Fatal(err)
	}
	assertInstalled(t, p.Dir)
	if err := tx.Rollback(); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(p.Dir, "SKILL.md")); string(b) != "mine" {
		t.Fatalf("rollback did not restore: %q", b)
	}
	assertNoTemp(t, p.Root)

	// Rolling back a fresh install removes what it created.
	home2 := t.TempDir()
	plan2, _ := Check(apps.Codex, Locations{Home: home2})
	tx2, err := Install(plan2)
	if err != nil {
		t.Fatal(err)
	}
	if err := tx2.Rollback(); err != nil {
		t.Fatal(err)
	}
	if entries, _ := os.ReadDir(home2); len(entries) != 0 {
		t.Fatalf("rollback left %v", entries)
	}
}

func symlinkOrSkip(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
}

func TestCheckRefusesSymlinks(t *testing.T) {
	t.Run("skill folder", func(t *testing.T) {
		home := t.TempDir()
		elsewhere := t.TempDir()
		root := filepath.Join(home, ".agents", "skills")
		if err := os.MkdirAll(root, 0o700); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, elsewhere, filepath.Join(root, Name))
		_, err := Check(apps.Codex, Locations{Home: home})
		if err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("err %v", err)
		}
		if entries, _ := os.ReadDir(elsewhere); len(entries) != 0 {
			t.Fatal("wrote through the symlink")
		}
	})
	t.Run("skills folder", func(t *testing.T) {
		home := t.TempDir()
		elsewhere := t.TempDir()
		if err := os.MkdirAll(filepath.Join(home, ".claude"), 0o700); err != nil {
			t.Fatal(err)
		}
		symlinkOrSkip(t, elsewhere, filepath.Join(home, ".claude", "skills"))
		if _, err := Check(apps.ClaudeCode, Locations{Home: home}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("tool folder", func(t *testing.T) {
		project := t.TempDir()
		elsewhere := t.TempDir()
		symlinkOrSkip(t, elsewhere, filepath.Join(project, ".agents"))
		if _, err := Check(apps.Codex, Locations{Home: t.TempDir(), Project: project}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
			t.Fatalf("err %v", err)
		}
	})
	t.Run("file inside skill folder", func(t *testing.T) {
		home := t.TempDir()
		p := mustInstall(t, apps.Codex, Locations{Home: home})
		outside := filepath.Join(t.TempDir(), "outside.md")
		if err := os.WriteFile(outside, Files()["SKILL.md"], 0o644); err != nil {
			t.Fatal(err)
		}
		skill := filepath.Join(p.Dir, "SKILL.md")
		_ = os.Remove(skill)
		symlinkOrSkip(t, outside, skill)
		plan, err := Check(apps.Codex, Locations{Home: home})
		if err != nil || plan.State != Different || !contains(plan.Diff, "symlink: SKILL.md") {
			t.Fatalf("%+v %v", plan, err)
		}
		tx, err := Install(plan)
		if err != nil {
			t.Fatal(err)
		}
		tx.Commit()
		assertInstalled(t, p.Dir)
		if b, _ := os.ReadFile(outside); !bytes.Equal(b, Files()["SKILL.md"]) {
			t.Fatal("symlink target changed")
		}
	})
}

func TestCheckRefusesFileInTheWay(t *testing.T) {
	home := t.TempDir()
	root := filepath.Join(home, ".agents", "skills")
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, Name), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(apps.Codex, Locations{Home: home}); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("err %v", err)
	}
	home2 := t.TempDir()
	if err := os.WriteFile(filepath.Join(home2, ".claude"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Check(apps.ClaudeCode, Locations{Home: home2}); err == nil {
		t.Fatal("no error")
	}
}

// TestPythonScriptTests runs the Skill's own offline unittest suite against
// the embedded script. It needs python3 (3.8+) on PATH. The suite replaces
// the script's opener in every test, so nothing reaches the network.
func TestPythonScriptTests(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("python3 on Windows runners may be the Store alias; Linux and macOS run this suite")
	}
	py, err := exec.LookPath("python3")
	if err != nil {
		t.Skip("python3 not on PATH")
	}
	cmd := exec.Command(py, "-I", "-B", filepath.Join("testdata", "test_deeprouter_image.py"))
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, "DEEPROUTER_") {
			env = append(env, kv)
		}
	}
	cmd.Env = append(env, "PYTHONDONTWRITEBYTECODE=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("python tests failed: %v\n%s", err, out)
	}
	if !strings.Contains(string(out), "Ran 49 tests") || !strings.Contains(string(out), "\nOK") {
		t.Fatalf("unexpected python output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join("embed", "scripts", "__pycache__")); err == nil {
		t.Fatal("python wrote __pycache__ into the embedded Skill")
	}
}
