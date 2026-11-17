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

// SHA-256 of the embedded Skill v1.0.4 files (the standalone 1.0.1 package,
// download recovery from 1.0.2, the image address/token sources from 1.0.3,
// and the saved model / gpt-image-2.6-flare default from 1.0.4). A
// changed embed must change these on purpose.
var wantSHA = map[string]string{
	"SKILL.md":                     "e2c6818e9cf8749d9fe6db7a96d1a3e75ba78fd0f09f1cbd204498a39b6218dc",
	"使用说明.md":                      "cdb290b54858c9bf261115974520d15085f83bb65fc8b04d26548fc73c7217d5",
	"references/official-links.md": "b805d9cf89d89974c9971856a8cb42ddec3626731160b1c8a1570b1b00362f4a",
	"scripts/deeprouter_image.py":  "02eb3958d0950a50afe36cce74bd8780a59b85acc5a18c3fd361ba11c04ef539",
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
		// Claude Desktop documents ~/.claude/skills; CLAUDE_CONFIG_DIR is a
		// Claude Code CLI setting and does not move it.
		{apps.ClaudeDesktop, Locations{Home: home}, filepath.Join(home, ".claude", "skills"), home, false},
		{apps.ClaudeDesktop, Locations{Home: home, ClaudeConfigDir: filepath.Join("cfg", "claude")}, filepath.Join(home, ".claude", "skills"), home, false},
		{apps.ClaudeDesktop, Locations{Home: home, Project: project}, filepath.Join(project, ".claude", "skills"), project, false},
		// Gemini CLI uses the .agents/skills alias, never .gemini/skills.
		{apps.Gemini, Locations{Home: home}, filepath.Join(home, ".agents", "skills"), home, false},
		{apps.Gemini, Locations{Home: home, Project: project}, filepath.Join(project, ".agents", "skills"), project, false},
		{apps.Grok, Locations{Home: home}, filepath.Join(home, ".grok", "skills"), home, false},
		{apps.Grok, Locations{Home: home, Project: project}, filepath.Join(project, ".grok", "skills"), project, false},
		{apps.ID("chatgpt"), Locations{Home: home}, "", "", true},
		{apps.ID("cursor"), Locations{Home: home}, "", "", true},
		{apps.Codex, Locations{}, "", "", true},
		{apps.Grok, Locations{}, "", "", true},
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
		if strings.Contains(root, ".codex") || strings.Contains(root, ".gemini") {
			t.Errorf("%s uses a folder it should not: %s", c.id, root)
		}
	}
	for _, id := range []apps.ID{apps.Codex, apps.ClaudeCode, apps.ClaudeDesktop, apps.Grok, apps.Gemini} {
		if !Supported(id) {
			t.Errorf("%s not supported", id)
		}
	}
	if Supported("chatgpt") || Supported("cursor") {
		t.Fatal("Supported accepts a program without a Skill folder")
	}
	if got := DefaultTargets(); len(got) != 2 || got[0] != apps.Codex || got[1] != apps.ClaudeCode {
		t.Fatalf("DefaultTargets %v, want codex and claude-code only", got)
	}
	if got := Targets(); len(got) != 5 || got[0] != apps.Codex || got[1] != apps.ClaudeCode {
		t.Fatalf("Targets %v", got)
	}
}

// planDirs is each plan's folder with its programs, for comparison.
func planDirs(plans []Plan) map[string][]apps.ID {
	out := map[string][]apps.ID{}
	for _, p := range plans {
		out[p.Dir] = p.Apps
	}
	return out
}

func sameIDs(a, b []apps.ID) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func TestCheckAllSharedFolders(t *testing.T) {
	home := t.TempDir()
	project := t.TempDir()
	cfg := t.TempDir()
	user := Locations{Home: home}
	proj := Locations{Home: home, Project: project}
	skill := func(base string, parts ...string) string {
		return filepath.Join(append(append([]string{base}, parts...), Name)...)
	}
	cases := []struct {
		name string
		ids  []apps.ID
		loc  Locations
		want map[string][]apps.ID
	}{
		{"default pair", DefaultTargets(), user, map[string][]apps.ID{
			skill(home, ".agents", "skills"): {apps.Codex},
			skill(home, ".claude", "skills"): {apps.ClaudeCode},
		}},
		{"gemini alone uses .agents", []apps.ID{apps.Gemini}, user, map[string][]apps.ID{
			skill(home, ".agents", "skills"): {apps.Gemini},
		}},
		{"gemini and codex share", []apps.ID{apps.Gemini, apps.Codex}, user, map[string][]apps.ID{
			skill(home, ".agents", "skills"): {apps.Codex, apps.Gemini},
		}},
		{"grok alone uses .grok", []apps.ID{apps.Grok}, user, map[string][]apps.ID{
			skill(home, ".grok", "skills"): {apps.Grok},
		}},
		{"grok with codex shares .agents", []apps.ID{apps.Grok, apps.Codex}, user, map[string][]apps.ID{
			skill(home, ".agents", "skills"): {apps.Codex, apps.Grok},
		}},
		{"grok with gemini shares .agents", []apps.ID{apps.Gemini, apps.Grok}, user, map[string][]apps.ID{
			skill(home, ".agents", "skills"): {apps.Grok, apps.Gemini},
		}},
		{"grok with claude code keeps .grok", []apps.ID{apps.Grok, apps.ClaudeCode}, user, map[string][]apps.ID{
			skill(home, ".grok", "skills"):   {apps.Grok},
			skill(home, ".claude", "skills"): {apps.ClaudeCode},
		}},
		{"grok and codex in a project", []apps.ID{apps.Grok, apps.Codex}, proj, map[string][]apps.ID{
			skill(project, ".grok", "skills"):   {apps.Grok},
			skill(project, ".agents", "skills"): {apps.Codex},
		}},
		{"claude code and desktop share", []apps.ID{apps.ClaudeDesktop, apps.ClaudeCode}, user, map[string][]apps.ID{
			skill(home, ".claude", "skills"): {apps.ClaudeCode, apps.ClaudeDesktop},
		}},
		{"claude code and desktop split by CLAUDE_CONFIG_DIR", []apps.ID{apps.ClaudeCode, apps.ClaudeDesktop}, Locations{Home: home, ClaudeConfigDir: cfg}, map[string][]apps.ID{
			skill(cfg, "skills"):             {apps.ClaudeCode},
			skill(home, ".claude", "skills"): {apps.ClaudeDesktop},
		}},
		{"CLAUDE_CONFIG_DIR is ~/.claude", []apps.ID{apps.ClaudeCode, apps.ClaudeDesktop}, Locations{Home: home, ClaudeConfigDir: filepath.Join(home, ".claude") + string(filepath.Separator)}, map[string][]apps.ID{
			skill(home, ".claude", "skills"): {apps.ClaudeCode, apps.ClaudeDesktop},
		}},
		{"all five, user", Targets(), user, map[string][]apps.ID{
			skill(home, ".agents", "skills"): {apps.Codex, apps.Grok, apps.Gemini},
			skill(home, ".claude", "skills"): {apps.ClaudeCode, apps.ClaudeDesktop},
		}},
		{"all five, project", Targets(), proj, map[string][]apps.ID{
			skill(project, ".agents", "skills"): {apps.Codex, apps.Gemini},
			skill(project, ".claude", "skills"): {apps.ClaudeCode, apps.ClaudeDesktop},
			skill(project, ".grok", "skills"):   {apps.Grok},
		}},
	}
	for _, c := range cases {
		plans, err := CheckAll(c.ids, c.loc)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		got := planDirs(plans)
		if len(got) != len(c.want) || len(plans) != len(c.want) {
			t.Fatalf("%s: got %v, want %v", c.name, got, c.want)
		}
		for dir, ids := range c.want {
			if !sameIDs(got[dir], ids) {
				t.Fatalf("%s: %s has %v, want %v (all %v)", c.name, dir, got[dir], ids, got)
			}
		}
		for _, p := range plans {
			if strings.Contains(p.Dir, ".gemini") || strings.Contains(p.Dir, ".codex") {
				t.Fatalf("%s: writes %s", c.name, p.Dir)
			}
		}
		if got := SharesAgents(plans[0]); got != (apps.Contains(plans[0].Apps, apps.Grok) && c.loc.Project == "" && strings.Contains(plans[0].Dir, ".agents")) {
			t.Fatalf("%s: SharesAgents %v", c.name, got)
		}
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("CheckAll wrote %v", entries)
	}
}

func TestCheckAllLinkedClaudeConfigDirIsOneFolder(t *testing.T) {
	home := t.TempDir()
	if err := os.Mkdir(filepath.Join(home, ".claude"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "claude-link")
	symlinkOrSkip(t, filepath.Join(home, ".claude"), link)
	plans, err := CheckAll([]apps.ID{apps.ClaudeCode, apps.ClaudeDesktop}, Locations{Home: home, ClaudeConfigDir: link})
	if err != nil {
		t.Fatal(err)
	}
	if len(plans) != 1 || !sameIDs(plans[0].Apps, []apps.ID{apps.ClaudeCode, apps.ClaudeDesktop}) {
		t.Fatalf("plans %+v", plans)
	}
}

func TestCheckAllStopsOnAnyUnsafeTarget(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, ".grok"), 0o700); err != nil {
		t.Fatal(err)
	}
	symlinkOrSkip(t, t.TempDir(), filepath.Join(home, ".grok", "skills"))
	if _, err := CheckAll([]apps.ID{apps.ClaudeCode, apps.Grok}, Locations{Home: home}); err == nil || !strings.Contains(err.Error(), "symbolic link") {
		t.Fatalf("err %v", err)
	}
	// With Codex selected Grok shares ~/.agents/skills, so .grok is not used.
	if plans, err := CheckAll([]apps.ID{apps.Codex, apps.Grok}, Locations{Home: home}); err != nil || len(plans) != 1 {
		t.Fatalf("%+v %v", plans, err)
	}
}

func TestCheckAllSharedConflictIsOnePlan(t *testing.T) {
	home := t.TempDir()
	loc := Locations{Home: home}
	p := mustInstall(t, apps.Codex, loc)
	if err := os.WriteFile(filepath.Join(p.Dir, "SKILL.md"), []byte("mine"), 0o644); err != nil {
		t.Fatal(err)
	}
	plans, err := CheckAll([]apps.ID{apps.Codex, apps.Gemini, apps.Grok}, loc)
	if err != nil || len(plans) != 1 || plans[0].State != Different || len(plans[0].Apps) != 3 {
		t.Fatalf("%+v %v", plans, err)
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
	if !strings.Contains(string(out), "Ran 82 tests") || !strings.Contains(string(out), "\nOK") {
		t.Fatalf("unexpected python output:\n%s", out)
	}
	if _, err := os.Stat(filepath.Join("embed", "scripts", "__pycache__")); err == nil {
		t.Fatal("python wrote __pycache__ into the embedded Skill")
	}
}
