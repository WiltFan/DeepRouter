//go:build !windows

package e2e

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/zfg5118/deeprouter/internal/imageskill"
)

// TestSkillAccept is the black-box acceptance suite for deeprouter skill.
//
// It builds the real binary, runs skill / skill install against a temporary
// HOME (and temporary projects), drives the interactive menu on a real
// pseudoterminal, and then runs the *installed* deeprouter_image.py through
// generate -> download 403 -> recovery file -> recover.
//
// Mock boundary: no real network and no billing. The binary's only HTTP call
// is the image Skill's GET /v1/models, sent right after the token: in S18 to a
// local TLS mock trusted via SSL_CERT_FILE, and in S17 (public preset and
// example hosts) to a separate local deny proxy that refuses it and records
// the host, so it fails offline. Otherwise the proxy variables point at a
// local proxy that records anything that reaches it, and the suite fails if
// something does. In the script, only
// OPENER (HTTP) is replaced; check_result_url, redact, NoRedirect and the
// bare result GET run unchanged (the script no longer resolves the result host
// itself). This does not prove a live 403 from a real result host,
// and it does not prove Claude Code or Codex will discover the Skill.
//
// Linux only. Logs: /tmp/deeprouter-skill-install/e2e-logs/run-*/ (or
// $DEEPROUTER_SKILL_E2E_LOGS), with SUMMARY.md and summary.json.
//
//	go test ./e2e -run SkillAccept -count=1 -v
func TestSkillAccept(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the skill acceptance suite is only run on Linux")
	}
	if testing.Short() {
		t.Skip("builds the binary; skipped with -short")
	}
	s := newSkillSuite(t)
	realBefore := s.realHomeSnapshot()

	t.Run("S00_build", s.build)
	if s.bin == "" {
		s.finish(t)
		t.FailNow()
	}
	// The binary is built once, outside any subtest's temp dir.
	defer os.RemoveAll(filepath.Dir(s.bin))
	t.Run("S01_both_user", s.bothUser)
	t.Run("S02_single_target", s.singleTarget)
	t.Run("S03_claude_config_dir", s.claudeConfigDir)
	t.Run("S04_S06_repeat_conflict_force", s.repeatConflictForce)
	t.Run("S07_illegal_apps", s.illegalApps)
	t.Run("S08_illegal_project", s.illegalProject)
	t.Run("S09_valid_project", s.validProject)
	t.Run("S10_symlink_refusal", s.symlinks)
	t.Run("S11_non_tty", s.nonTTY)
	t.Run("S12_pty", s.pty)
	t.Run("S13_installed_script", s.installedScript)
	t.Run("S16_main_menu", s.mainMenu)
	t.Run("S17_image_config", s.imageConfig)
	t.Run("S18_image_models", s.imageModels)
	t.Run("S19_new_targets", s.newTargets)
	t.Run("S20_legacy_folder", s.legacyFolder)

	t.Run("S14_isolation", func(t *testing.T) {
		sc := s.scenario(t, "S14", "isolation: real HOME untouched, no HTTP reached the proxy")
		after := s.realHomeSnapshot()
		for path, was := range realBefore {
			sc.check("real "+path+" unchanged", after[path] == was, "before="+was+" after="+after[path])
		}
		leaks := s.leaked()
		sc.check("no request reached the deny proxy", len(leaks) == 0, strings.Join(leaks, ", "))
	})
	s.finish(t)
}

const (
	// skillE2EKey is fake. It is only set for the script driver and must not
	// appear in any log or written file.
	skillE2EKey = "sk-test-e2eFAKEnotARealKey0000000000"
	// skillE2ESig is the fake URL signature in skill_script_driver.py.
	skillE2ESig = "e2e5ign4ture0f8e7d6c5b4a39281706f5e4d3c2b1a0fedcba98765432"
)

type skillRun struct {
	Label      string   `json:"label,omitempty"`
	Argv       []string `json:"argv"`
	Cwd        string   `json:"cwd"`
	Env        []string `json:"env"`
	Stdin      string   `json:"stdin,omitempty"`
	Exit       int      `json:"exit"`
	Stdout     string   `json:"stdout"`
	Stderr     string   `json:"stderr"`
	Transcript string   `json:"transcript,omitempty"`
	Details    string   `json:"details,omitempty"`
}

type skillCheck struct {
	Name   string `json:"name"`
	OK     bool   `json:"ok"`
	Detail string `json:"detail,omitempty"`
}

type skillScenario struct {
	ID      string       `json:"id"`
	Title   string       `json:"title"`
	Status  string       `json:"status"`
	Runs    []skillRun   `json:"runs"`
	Checks  []skillCheck `json:"checks"`
	LogFile string       `json:"log"`
	t       *testing.T
}

type skillSuite struct {
	root      string
	bin       string
	python    string
	logDir    string
	proxy     string
	leaked    func() []string
	realHome  string
	scenarios []*skillScenario
	started   time.Time
}

func newSkillSuite(t *testing.T) *skillSuite {
	base := os.Getenv("DEEPROUTER_SKILL_E2E_LOGS")
	if base == "" {
		base = "/tmp/deeprouter-skill-install/e2e-logs"
	}
	now := time.Now()
	logDir := filepath.Join(base, "run-"+now.Format("20060102-150405"))
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatal(err)
	}
	proxy, leaked := denyProxy(t)
	realHome, _ := os.UserHomeDir()
	py, _ := exec.LookPath("python3")
	t.Logf("skill acceptance logs: %s", logDir)
	return &skillSuite{root: moduleRoot(t), python: py, logDir: logDir, proxy: proxy, leaked: leaked, realHome: realHome, started: now}
}

// scenario starts a log record. It is written when the subtest ends.
func (s *skillSuite) scenario(t *testing.T, id, title string) *skillScenario {
	sc := &skillScenario{ID: id, Title: title, t: t}
	s.scenarios = append(s.scenarios, sc)
	t.Cleanup(func() {
		switch {
		case t.Skipped():
			sc.Status = "SKIP"
		case t.Failed() || !sc.allOK():
			sc.Status = "FAIL"
		default:
			sc.Status = "PASS"
		}
		sc.LogFile = filepath.Join(s.logDir, id+".log")
		_ = os.WriteFile(sc.LogFile, []byte(sc.text()), 0o644)
	})
	return sc
}

func (sc *skillScenario) allOK() bool {
	for _, c := range sc.Checks {
		if !c.OK {
			return false
		}
	}
	return len(sc.Checks) > 0
}

func (sc *skillScenario) check(name string, ok bool, detail string) bool {
	sc.t.Helper()
	sc.Checks = append(sc.Checks, skillCheck{Name: name, OK: ok, Detail: detail})
	if !ok {
		sc.t.Errorf("%s: %s: %s", sc.ID, name, detail)
	}
	return ok
}

func (sc *skillScenario) text() string {
	var b strings.Builder
	fmt.Fprintf(&b, "# %s %s\nresult: %s\n", sc.ID, sc.Title, sc.Status)
	for i, r := range sc.Runs {
		fmt.Fprintf(&b, "\n## run %d %s\ncwd: %s\nargv: %s\nenv:\n", i+1, r.Label, r.Cwd, shellJoin(r.Argv))
		for _, kv := range r.Env {
			fmt.Fprintf(&b, "  %s\n", kv)
		}
		if r.Stdin != "" {
			fmt.Fprintf(&b, "stdin: %q\n", r.Stdin)
		}
		fmt.Fprintf(&b, "exit: %d\n--- stdout ---\n%s\n--- stderr ---\n%s\n", r.Exit, r.Stdout, r.Stderr)
		if r.Transcript != "" {
			fmt.Fprintf(&b, "--- pty transcript ---\n%s\n", r.Transcript)
		}
		if r.Details != "" {
			fmt.Fprintf(&b, "--- details ---\n%s\n", r.Details)
		}
	}
	b.WriteString("\n## assertions\n")
	for _, c := range sc.Checks {
		mark := "PASS"
		if !c.OK {
			mark = "FAIL"
		}
		fmt.Fprintf(&b, "[%s] %s", mark, c.Name)
		if c.Detail != "" {
			fmt.Fprintf(&b, " (%s)", c.Detail)
		}
		b.WriteString("\n")
	}
	return b.String()
}

// env is the complete environment for a child: nothing is inherited except
// PATH. DEEPROUTER_API_KEY is never set here. over[imageListProxy], when
// set, replaces the suite's deny proxy for this child and is not passed on.
func (s *skillSuite) env(home string, over map[string]string) []string {
	proxy := s.proxy
	vars := map[string]string{
		"PATH":              os.Getenv("PATH"),
		"HOME":              home,
		"USERPROFILE":       home,
		"APPDATA":           filepath.Join(home, "AppData", "Roaming"),
		"LOCALAPPDATA":      filepath.Join(home, "AppData", "Local"),
		"XDG_CONFIG_HOME":   filepath.Join(home, ".config"),
		"TMPDIR":            os.TempDir(),
		"CLAUDE_CONFIG_DIR": "",
		"CODEX_HOME":        "",
		"GROK_HOME":         "",
		"GEMINI_CLI_HOME":   "",
		"DEEPROUTER_LANG":   "en",
		// Every run starts with no image address or key in the environment.
		"DEEPROUTER_API_KEY":        "",
		"DEEPROUTER_IMAGE_BASE_URL": "",
		"DEEPROUTER_IMAGE_API_KEY":  "",
		"LANG":                      "en_US.UTF-8",
		"LC_ALL":                    "en_US.UTF-8",
	}
	for k, v := range over {
		if k == imageListProxy {
			proxy = v
			continue
		}
		vars[k] = v
	}
	var env []string
	for k, v := range vars {
		env = append(env, k+"="+v)
	}
	sort.Strings(env)
	return withDenyProxy(env, proxy)
}

// shownEnv is env for the log: PATH and proxies are abbreviated and a key is
// never written.
func shownEnv(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		switch {
		case k == "PATH":
			out = append(out, "PATH=<inherited>")
		case strings.EqualFold(k, "HTTP_PROXY") || strings.EqualFold(k, "HTTPS_PROXY") || strings.EqualFold(k, "ALL_PROXY"):
			out = append(out, k+"=<local deny proxy>")
		case (k == "DEEPROUTER_API_KEY" || k == "DEEPROUTER_IMAGE_API_KEY" || k == "DEEPROUTER_E2E_EXPECT_KEY") && kv != k+"=":
			out = append(out, k+"=<fake sk-test- key, value not logged>")
		default:
			out = append(out, kv)
		}
	}
	return out
}

// run executes the built binary. stdin "" means /dev/null.
func (s *skillSuite) run(sc *skillScenario, home, cwd string, over map[string]string, stdin string, args ...string) skillRun {
	sc.t.Helper()
	if cwd == "" {
		cwd = home
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.bin, args...)
	cmd.Dir = cwd
	cmd.Env = s.env(home, over)
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	code := 0
	if err != nil {
		if exit, ok := err.(*exec.ExitError); ok {
			code = exit.ExitCode()
		} else {
			sc.t.Fatal(err)
		}
	}
	r := skillRun{Argv: append([]string{"deeprouter"}, args...), Cwd: cwd, Env: shownEnv(cmd.Env), Stdin: stdin,
		Exit: code, Stdout: stdout.String(), Stderr: stderr.String()}
	sc.Runs = append(sc.Runs, r)
	return r
}

// install is a setup step that must succeed.
func (s *skillSuite) install(sc *skillScenario, home string, args ...string) {
	sc.t.Helper()
	r := s.run(sc, home, "", nil, "", append([]string{"skill", "install"}, args...)...)
	sc.Runs[len(sc.Runs)-1].Label = "(setup)"
	if r.Exit != 0 {
		sc.t.Fatalf("setup install failed: %d\n%s", r.Exit, r.Stderr)
	}
}

func (s *skillSuite) build(t *testing.T) {
	sc := s.scenario(t, "S00", "build the real binary")
	out := filepath.Join(t.TempDir(), "deeprouter")
	args := []string{"build", "-trimpath", "-ldflags", "-s -w -X main.version=0.1.0-skill-e2e", "-o", out, "."}
	cmd := exec.Command("go", args...)
	cmd.Dir = s.root
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	b, err := cmd.CombinedOutput()
	r := skillRun{Argv: append([]string{"CGO_ENABLED=0", "go"}, args...), Cwd: s.root, Env: []string{"CGO_ENABLED=0", "(rest inherited for the Go toolchain only)"}, Stdout: string(b)}
	if err != nil {
		r.Exit = 1
	}
	sc.Runs = append(sc.Runs, r)
	if !sc.check("go build succeeded", err == nil, fmt.Sprint(err)) {
		return
	}
	// The suite binary lives outside t.TempDir of this subtest.
	dir, err := os.MkdirTemp("", "deeprouter-skill-e2e-bin-")
	if err != nil {
		t.Fatal(err)
	}
	s.bin = filepath.Join(dir, "deeprouter")
	if err := os.Rename(out, s.bin); err != nil {
		t.Fatal(err)
	}
	home := t.TempDir()
	v := s.run(sc, home, "", nil, "", "version")
	sc.check("binary reports the e2e version", v.Exit == 0 && strings.Contains(v.Stdout+v.Stderr, "0.1.0-skill-e2e"), v.Stdout)
	repo := map[string][]byte{}
	for _, rel := range imageskill.FileList() {
		b, err := os.ReadFile(filepath.Join(s.root, "internal", "imageskill", "embed", filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		repo[rel] = b
	}
	same := true
	for rel, b := range imageskill.Files() {
		same = same && bytes.Equal(repo[rel], b)
	}
	sc.check("embed tree has exactly four files", len(imageskill.FileList()) == 4, strings.Join(imageskill.FileList(), ", "))
	sc.check("repo embed files equal the embedded Skill", same && len(repo) == 4, "")
}

// skillDir is <base>/<.claude|.agents>/skills/image-generate.
func skillDir(base, hidden string) string {
	return filepath.Join(base, hidden, "skills", imageskill.Name)
}

// treeDiff is "" when dir holds exactly the embedded Skill, byte for byte.
func treeDiff(dir string) string {
	want := imageskill.Files()
	var problems []string
	got := map[string]bool{}
	err := filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		rel = filepath.ToSlash(rel)
		switch {
		case d.Type()&fs.ModeSymlink != 0:
			problems = append(problems, "symlink "+rel)
		case d.IsDir():
		case !d.Type().IsRegular():
			problems = append(problems, "special "+rel)
		default:
			got[rel] = true
			b, _ := os.ReadFile(p)
			if w, ok := want[rel]; !ok {
				problems = append(problems, "extra "+rel)
			} else if !bytes.Equal(b, w) {
				problems = append(problems, "changed "+rel)
			}
		}
		return nil
	})
	if err != nil {
		return err.Error()
	}
	for rel := range want {
		if !got[rel] {
			problems = append(problems, "missing "+rel)
		}
	}
	sort.Strings(problems)
	return strings.Join(problems, ", ")
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// snapTree records every path under dir: type, mode, mtime, and a content
// hash or link target. Two equal snapshots mean nothing was written.
func snapTree(dir string) map[string]string {
	m := map[string]string{}
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		info, err := os.Lstat(p)
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(dir, p)
		v := fmt.Sprintf("%v %d", info.Mode(), info.ModTime().UnixNano())
		switch {
		case info.Mode()&fs.ModeSymlink != 0:
			target, _ := os.Readlink(p)
			v += " -> " + target
		case info.Mode().IsRegular():
			b, _ := os.ReadFile(p)
			v += fmt.Sprintf(" %x", sha256.Sum256(b))
		}
		m[rel] = v
		return nil
	})
	return m
}

func snapDiff(a, b map[string]string) string {
	var out []string
	for k, v := range a {
		if w, ok := b[k]; !ok {
			out = append(out, "removed "+k)
		} else if v != w {
			out = append(out, "changed "+k)
		}
	}
	for k := range b {
		if _, ok := a[k]; !ok {
			out = append(out, "added "+k)
		}
	}
	sort.Strings(out)
	return strings.Join(out, ", ")
}

func mode(p string) os.FileMode {
	info, err := os.Stat(p)
	if err != nil {
		return 0
	}
	return info.Mode().Perm()
}

func (s *skillSuite) bothUser(t *testing.T) {
	sc := s.scenario(t, "S01", "both targets, user level")
	home := t.TempDir()
	r := s.run(sc, home, "", nil, "", "skill", "install")
	claude, codex := skillDir(home, ".claude"), skillDir(home, ".agents")
	sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("Claude tree equals embed (4 files)", treeDiff(claude) == "", treeDiff(claude))
	sc.check("Codex tree equals embed (4 files)", treeDiff(codex) == "", treeDiff(codex))
	sc.check("stdout names both user folders", strings.Contains(r.Stdout, "~/.claude/skills/"+imageskill.Name) && strings.Contains(r.Stdout, "~/.agents/skills/"+imageskill.Name), "")
	sc.check("stdout says no API call / no billing", strings.Contains(r.Stdout, "made no API call and was not billed"), "")
	sc.check("Skill folders 0700, files 0644", mode(claude) == 0o700 && mode(codex) == 0o700 && mode(filepath.Join(claude, "SKILL.md")) == 0o644 && mode(filepath.Join(codex, "scripts", "deeprouter_image.py")) == 0o644,
		fmt.Sprintf("%o %o", mode(claude), mode(codex)))
	sc.check("created .claude/.agents are 0700", mode(filepath.Join(home, ".claude")) == 0o700 && mode(filepath.Join(home, ".agents", "skills")) == 0o700, "")
	var top []string
	entries, _ := os.ReadDir(home)
	for _, e := range entries {
		top = append(top, e.Name())
	}
	sc.check("nothing else written in HOME", strings.Join(top, ",") == ".agents,.claude", strings.Join(top, ","))
	sc.check("no staging folders left", len(globAll(filepath.Join(home, "*", "skills", ".image-generate.*"))) == 0, "")
}

// shellJoin quotes argv for a POSIX shell, so empty and spaced values show.
// A leading ~ is quoted too: the binary, not the shell, expanded it.
func shellJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if a != "" && !strings.ContainsAny(a, " \t\n'\"\\$`|&;<>()*?[]#~") {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

func globAll(pattern string) []string {
	m, _ := filepath.Glob(pattern)
	return m
}

func (s *skillSuite) singleTarget(t *testing.T) {
	for _, tc := range []struct{ id, app, want, not string }{
		{"S02a", "claude-code", ".claude", ".agents"},
		{"S02b", "codex", ".agents", ".claude"},
	} {
		t.Run(tc.app, func(t *testing.T) {
			sc := s.scenario(t, tc.id, "single target --apps "+tc.app)
			home := t.TempDir()
			r := s.run(sc, home, "", nil, "", "skill", "install", "--apps", tc.app)
			sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
			sc.check(tc.want+" tree equals embed", treeDiff(skillDir(home, tc.want)) == "", treeDiff(skillDir(home, tc.want)))
			sc.check(tc.not+" not created", !exists(filepath.Join(home, tc.not)), "")
		})
	}
}

func (s *skillSuite) claudeConfigDir(t *testing.T) {
	sc := s.scenario(t, "S03", "custom CLAUDE_CONFIG_DIR")
	home := t.TempDir()
	cfg := filepath.Join(t.TempDir(), "claude-config")
	if err := os.Mkdir(cfg, 0o755); err != nil {
		t.Fatal(err)
	}
	r := s.run(sc, home, "", map[string]string{"CLAUDE_CONFIG_DIR": cfg}, "", "skill", "install")
	dir := filepath.Join(cfg, "skills", imageskill.Name)
	sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("Claude Skill under $CLAUDE_CONFIG_DIR/skills", treeDiff(dir) == "", treeDiff(dir))
	sc.check("$HOME/.claude not created", !exists(filepath.Join(home, ".claude")), "")
	sc.check("Codex still under $HOME/.agents/skills", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
	sc.check("stdout shows the CLAUDE_CONFIG_DIR path", strings.Contains(r.Stdout, dir), "")
}

func (s *skillSuite) repeatConflictForce(t *testing.T) {
	home := t.TempDir()
	claude, codex := skillDir(home, ".claude"), skillDir(home, ".agents")
	t.Run("S04_repeat", func(t *testing.T) {
		sc := s.scenario(t, "S04", "repeat install of an identical tree")
		s.install(sc, home)
		before := snapTree(home)
		time.Sleep(1100 * time.Millisecond)
		r := s.run(sc, home, "", nil, "", "skill", "install")
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("both reported already installed", strings.Count(r.Stdout, "(already installed, unchanged)") == 2, r.Stdout)
		sc.check("HOME bytes and mtimes unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
	})
	sibling := filepath.Join(home, ".claude", "skills", "my-other-skill", "SKILL.md")
	t.Run("S05_conflict", func(t *testing.T) {
		sc := s.scenario(t, "S05", "conflict without --force")
		if treeDiff(claude) != "" {
			s.install(sc, home)
		}
		if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, sibling, "user's own skill\n")
		f, err := os.OpenFile(filepath.Join(claude, "SKILL.md"), os.O_APPEND|os.O_WRONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		_, _ = f.WriteString("\n<!-- my local edit -->\n")
		f.Close()
		mustWrite(t, filepath.Join(claude, "notes.txt"), "my notes\n")
		before := snapTree(home)
		r := s.run(sc, home, "", nil, "", "skill", "install")
		sc.check("exit 1", r.Exit == 1, fmt.Sprint(r.Exit))
		sc.check("stderr lists changed SKILL.md and extra notes.txt", strings.Contains(r.Stderr, "changed: SKILL.md") && strings.Contains(r.Stderr, "extra: notes.txt"), r.Stderr)
		sc.check("stderr says nothing written and mentions --force", strings.Contains(r.Stderr, "Nothing was written") && strings.Contains(r.Stderr, "--force"), "")
		sc.check("HOME unchanged (edit, extra file, sibling skill, Codex tree)", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
	})
	t.Run("S06_force", func(t *testing.T) {
		sc := s.scenario(t, "S06", "--force replaces the conflicting tree")
		codexBefore := snapTree(codex)
		siblingBefore := snapTree(filepath.Dir(sibling))
		r := s.run(sc, home, "", nil, "", "skill", "install", "--force")
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("Claude tree equals embed exactly", treeDiff(claude) == "", treeDiff(claude))
		sc.check("extra notes.txt gone", !exists(filepath.Join(claude, "notes.txt")), "")
		sc.check("stdout says replaced / unchanged", strings.Contains(r.Stdout, "(replaced)") && strings.Contains(r.Stdout, "(already installed, unchanged)"), r.Stdout)
		sc.check("Codex tree untouched (mtimes)", snapDiff(codexBefore, snapTree(codex)) == "", snapDiff(codexBefore, snapTree(codex)))
		sc.check("sibling skill untouched", snapDiff(siblingBefore, snapTree(filepath.Dir(sibling))) == "", "")
		sc.check("no staging/backup folders left", len(globAll(filepath.Join(home, ".claude", "skills", ".image-generate.*"))) == 0, "")
	})
}

func mustWrite(t *testing.T, path, body string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

// noWrites runs args with HOME and cwd snapshotted and expects exit want.
func (s *skillSuite) noWrites(sc *skillScenario, home, cwd string, over map[string]string, stdin string, want int, args ...string) skillRun {
	sc.t.Helper()
	h, c := snapTree(home), snapTree(cwd)
	r := s.run(sc, home, cwd, over, stdin, args...)
	sc.check(fmt.Sprintf("%q exit %d", args, want), r.Exit == want, fmt.Sprint(r.Exit))
	sc.check(fmt.Sprintf("%q wrote nothing in HOME", args), snapDiff(h, snapTree(home)) == "", snapDiff(h, snapTree(home)))
	if cwd != home {
		sc.check(fmt.Sprintf("%q wrote nothing in cwd", args), snapDiff(c, snapTree(cwd)) == "", snapDiff(c, snapTree(cwd)))
	}
	return r
}

func (s *skillSuite) illegalApps(t *testing.T) {
	sc := s.scenario(t, "S07", "illegal --apps")
	home := t.TempDir()
	for _, app := range []string{"chatgpt", "codex,chatgpt", "cursor", "claud-code", "gemini-app", "8"} {
		r := s.noWrites(sc, home, home, nil, "", 2, "skill", "install", "--apps", app)
		sc.check(fmt.Sprintf("--apps %s explains the error", app), strings.HasPrefix(r.Stderr, "deeprouter: ") && (strings.Contains(r.Stderr, "local Skill loading is unverified") || strings.Contains(r.Stderr, "unknown program") || strings.Contains(r.Stderr, "out of range")), firstLine(r.Stderr))
	}
	r := s.run(sc, home, "", nil, "", "skill", "install", "--apps", "chatgpt")
	sc.check("chatgpt is not a silent codex alias", strings.Contains(r.Stderr, "ChatGPT Chat/Work local Skill loading is unverified") && strings.Contains(r.Stderr, "use codex"), firstLine(r.Stderr))
	sc.check("HOME still empty", len(snapTree(home)) == 1, fmt.Sprint(len(snapTree(home))))
}

func (s *skillSuite) illegalProject(t *testing.T) {
	sc := s.scenario(t, "S08", "illegal --project")
	home := t.TempDir()
	cwd := t.TempDir()
	file := filepath.Join(home, "file.txt")
	mustWrite(t, file, "not a folder\n")
	cases := [][]string{
		{"--project", filepath.Join(home, "missing")},
		{"--project", file},
		{"--project", ""},
		{"--project="},
		{"--project", "   "},
		{"--project"},
	}
	for _, extra := range cases {
		s.noWrites(sc, home, cwd, nil, "", 2, append([]string{"skill", "install"}, extra...)...)
	}
}

func (s *skillSuite) validProject(t *testing.T) {
	sc := s.scenario(t, "S09", "valid --project DIR (absolute, ~/, relative)")
	home := t.TempDir()
	cwd := t.TempDir()
	proj := t.TempDir()
	hBefore, cBefore := snapTree(home), snapTree(cwd)
	r := s.run(sc, home, cwd, nil, "", "skill", "install", "--project", proj)
	sc.check("absolute: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("absolute: DIR/.claude/skills tree", treeDiff(skillDir(proj, ".claude")) == "", treeDiff(skillDir(proj, ".claude")))
	sc.check("absolute: DIR/.agents/skills tree", treeDiff(skillDir(proj, ".agents")) == "", treeDiff(skillDir(proj, ".agents")))
	sc.check("absolute: cwd untouched", snapDiff(cBefore, snapTree(cwd)) == "", snapDiff(cBefore, snapTree(cwd)))
	sc.check("absolute: HOME untouched", snapDiff(hBefore, snapTree(home)) == "", snapDiff(hBefore, snapTree(home)))

	tilde := filepath.Join(home, "myproj")
	if err := os.Mkdir(tilde, 0o755); err != nil {
		t.Fatal(err)
	}
	r = s.run(sc, home, cwd, nil, "", "skill", "install", "--project", "~/myproj", "--apps", "codex")
	sc.check("~/myproj: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("~/myproj: expanded against HOME", treeDiff(skillDir(tilde, ".agents")) == "" && !exists(filepath.Join(tilde, ".claude")), treeDiff(skillDir(tilde, ".agents")))
	sc.check("~/myproj: no literal ~ folder in cwd", !exists(filepath.Join(cwd, "~")), "")

	rel := filepath.Join(cwd, "relproj")
	if err := os.Mkdir(rel, 0o755); err != nil {
		t.Fatal(err)
	}
	r = s.run(sc, home, cwd, nil, "", "skill", "install", "--project", "relproj", "--apps", "claude-code")
	sc.check("relproj: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("relproj: resolved against cwd", treeDiff(skillDir(rel, ".claude")) == "" && !exists(filepath.Join(cwd, ".claude")), "")
}

func (s *skillSuite) symlinks(t *testing.T) {
	type tc struct {
		id, name string
		link     func(home, proj string) string // returns the link path
		args     []string
	}
	cases := []tc{
		{"S10a", "skill dir is a symlink", func(h, _ string) string { return filepath.Join(h, ".claude", "skills", imageskill.Name) }, nil},
		{"S10b", "skill dir is a symlink, with --force", func(h, _ string) string { return filepath.Join(h, ".claude", "skills", imageskill.Name) }, []string{"--force"}},
		{"S10c", "skills folder is a symlink", func(h, _ string) string { return filepath.Join(h, ".agents", "skills") }, nil},
		{"S10d", ".claude is a symlink", func(h, _ string) string { return filepath.Join(h, ".claude") }, nil},
		{"S10e", "project .agents is a symlink", func(_, p string) string { return filepath.Join(p, ".agents") }, []string{"--project", "PROJ"}},
		{"S10f", "dangling symlink at the skill dir", func(h, _ string) string { return filepath.Join(h, ".agents", "skills", imageskill.Name) }, []string{"--apps", "codex"}},
	}
	for _, c := range cases {
		t.Run(c.id, func(t *testing.T) {
			sc := s.scenario(t, c.id, "symlink refusal: "+c.name)
			home, proj, outside := t.TempDir(), t.TempDir(), t.TempDir()
			target := filepath.Join(outside, "target")
			if c.id != "S10f" {
				if err := os.Mkdir(target, 0o755); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, filepath.Join(target, "marker.txt"), "keep me\n")
			}
			link := c.link(home, proj)
			if err := os.MkdirAll(filepath.Dir(link), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, link); err != nil {
				t.Fatal(err)
			}
			args := []string{"skill", "install"}
			for _, a := range c.args {
				args = append(args, strings.ReplaceAll(a, "PROJ", proj))
			}
			o, p := snapTree(outside), snapTree(proj)
			r := s.noWrites(sc, home, home, nil, "", 1, args...)
			sc.check("link target unchanged", snapDiff(o, snapTree(outside)) == "", snapDiff(o, snapTree(outside)))
			sc.check("project unchanged", snapDiff(p, snapTree(proj)) == "", snapDiff(p, snapTree(proj)))
			sc.check("stderr names the symbolic link", strings.Contains(r.Stderr, "symbolic link"), firstLine(r.Stderr))
			if c.id == "S10f" {
				sc.check("dangling target not created", !exists(target), "")
			}
		})
	}
}

func (s *skillSuite) nonTTY(t *testing.T) {
	sc := s.scenario(t, "S11", "non-TTY deeprouter skill")
	home := t.TempDir()
	hint := "deeprouter skill install [--apps claude-code,codex] [--project DIR] [--force]"
	r := s.noWrites(sc, home, home, nil, "", 2, "skill")
	sc.check("stdin /dev/null: prints the install command", strings.Contains(r.Stderr, hint), r.Stderr)
	r = s.noWrites(sc, home, home, nil, "1\n\n\n\ny\n", 2, "skill")
	sc.check("stdin pipe with answers: still refuses, prints the install command", strings.Contains(r.Stderr, hint), r.Stderr)
	r = s.noWrites(sc, home, home, map[string]string{"DEEPROUTER_LANG": "zh"}, "", 2, "skill")
	sc.check("zh: prints the Chinese hint", strings.Contains(r.Stderr, "非交互安装请用") && strings.Contains(r.Stderr, "--project 目录"), r.Stderr)
}

type ptyStep struct {
	Expect string `json:"expect"`
	Send   string `json:"send"`
}

// runPTY runs deeprouter skill on a pseudoterminal and answers each prompt.
func (s *skillSuite) runPTY(sc *skillScenario, home, cwd string, steps []ptyStep) skillRun {
	sc.t.Helper()
	return s.runPTYArgs(sc, home, cwd, nil, []string{"skill"}, steps)
}

// runPTYArgs runs deeprouter with args on a pseudoterminal and answers each
// prompt. over adds environment variables, as in run.
func (s *skillSuite) runPTYArgs(sc *skillScenario, home, cwd string, over map[string]string, args []string, steps []ptyStep) skillRun {
	sc.t.Helper()
	if cwd == "" {
		cwd = home
	}
	transcript := filepath.Join(s.logDir, sc.ID+".pty.txt")
	plan, _ := json.Marshal(steps)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	argv, _ := json.Marshal(args)
	cmd := exec.CommandContext(ctx, s.python, "-I", "-B", "-c", skillPTYScript, s.bin, transcript, string(plan), string(argv))
	cmd.Dir = cwd
	cmd.Env = s.env(home, over)
	out, err := cmd.Output()
	var res struct {
		Exit     int  `json:"exit"`
		TimedOut bool `json:"timed_out"`
		Reached  int  `json:"reached"`
	}
	if err != nil || json.Unmarshal(out, &res) != nil {
		sc.t.Fatalf("pty driver: %v\n%s", err, out)
	}
	tr, _ := os.ReadFile(transcript)
	var sent []string
	for _, st := range steps {
		send := st.Send
		if strings.HasPrefix(send, "sk-") {
			send = "<fake sk-test- key, not logged>\r"
		}
		sent = append(sent, fmt.Sprintf("after %q send %q", st.Expect, send))
	}
	r := skillRun{Label: "(pty)", Argv: append([]string{"deeprouter"}, args...), Cwd: cwd, Env: shownEnv(cmd.Env),
		Stdin: strings.Join(sent, "; "), Exit: res.Exit, Transcript: string(tr) + "\n(transcript file: " + transcript + ")"}
	sc.Runs = append(sc.Runs, r)
	sc.check("pty run did not time out", !res.TimedOut, "")
	sc.check("every prompt was seen", res.Reached == len(steps), fmt.Sprintf("%d/%d", res.Reached, len(steps)))
	return r
}

func (s *skillSuite) pty(t *testing.T) {
	if s.python == "" {
		t.Skip("python3 is required to allocate a terminal")
	}
	menu := ptyStep{"Choose [1]: ", "\r"}
	programs := ptyStep{"Programs [1,2]: ", "\r"}

	t.Run("S12a_default", func(t *testing.T) {
		sc := s.scenario(t, "S12a", "PTY: Enter at every prompt installs both user-level trees, with no scope or install question")
		home := t.TempDir()
		r := s.runPTY(sc, home, "", []ptyStep{menu, programs})
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("no scope or Install? question", !strings.Contains(r.Transcript, "Install for:") && !strings.Contains(r.Transcript, "Scope [1]") &&
			!strings.Contains(r.Transcript, "Install? [Y/n]"), "")
		sc.check("Claude tree equals embed", treeDiff(skillDir(home, ".claude")) == "", treeDiff(skillDir(home, ".claude")))
		sc.check("Codex tree equals embed", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
		sc.check("transcript lists what will be written", strings.Contains(r.Transcript, "deeprouter will write:"), "")
		sc.check("transcript shows the done message", strings.Contains(r.Transcript, "made no API call"), "")
	})
	cancels := []struct {
		id, name string
		steps    []ptyStep
	}{
		{"S12b1", "PTY cancel: Quit at the menu", []ptyStep{{"Choose [1]: ", "2\r"}}},
		{"S12c1", "PTY EOF (Ctrl-D) at the menu", []ptyStep{{"Choose [1]: ", "\x04"}}},
		{"S12c3", "PTY EOF (Ctrl-D) at Programs", []ptyStep{menu, {"Programs [1,2]: ", "\x04"}}},
	}
	for _, c := range cancels {
		t.Run(c.id, func(t *testing.T) {
			sc := s.scenario(t, c.id, c.name)
			home := t.TempDir()
			before := snapTree(home)
			r := s.runPTY(sc, home, "", c.steps)
			sc.check("exit 2", r.Exit == 2, fmt.Sprint(r.Exit))
			sc.check("says cancelled, nothing written", strings.Contains(r.Transcript, "Cancelled. Nothing was written."), "")
			sc.check("HOME unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
		})
	}
	t.Run("S12d_user_level_from_a_project", func(t *testing.T) {
		sc := s.scenario(t, "S12d", "PTY: run from a project folder, Claude Code only, still installs for the user; the project is untouched")
		home, cwd := t.TempDir(), t.TempDir()
		cb := snapTree(cwd)
		r := s.runPTY(sc, home, cwd, []ptyStep{menu, {"Programs [1,2]: ", "2\r"}})
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("no project question", !strings.Contains(r.Transcript, "Project directory") && !strings.Contains(r.Transcript, "Scope [1]"), "")
		sc.check("user .claude tree equals embed", treeDiff(skillDir(home, ".claude")) == "", treeDiff(skillDir(home, ".claude")))
		sc.check("user .agents not created", !exists(filepath.Join(home, ".agents")), "")
		sc.check("cwd unchanged", snapDiff(cb, snapTree(cwd)) == "", snapDiff(cb, snapTree(cwd)))
	})
	t.Run("S12e_conflict_default_no", func(t *testing.T) {
		sc := s.scenario(t, "S12e", "PTY: Enter at the replace prompt keeps a changed tree")
		home := t.TempDir()
		s.install(sc, home, "--apps", "codex")
		mustWrite(t, filepath.Join(skillDir(home, ".agents"), "SKILL.md"), "edited\n")
		before := snapTree(home)
		r := s.runPTY(sc, home, "", []ptyStep{menu, {"Programs [1,2]: ", "1\r"}, {"[y/N] ", "\r"}})
		sc.check("exit 2", r.Exit == 2, fmt.Sprint(r.Exit))
		sc.check("HOME unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
	})
	t.Run("S12f_rows_enter_default", func(t *testing.T) {
		sc := s.scenario(t, "S12f", "PTY: new targets are listed; Enter still installs only Codex and Claude Code")
		home := t.TempDir()
		r := s.runPTY(sc, home, "", []ptyStep{menu, programs})
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		for _, row := range []string{"1) Codex ", "2) Claude Code ", "3) Claude Desktop  local sessions in the Code tab only", "4) Grok Build ", "5) Gemini CLI ", "6) OpenCode ", "7) Pi Coding CLI ", "ChatGPT Chat/Work local Skill loading is unverified", "Press Enter for Codex and Claude Code (1,2)"} {
			sc.check("menu shows "+row, strings.Contains(r.Transcript, row), "")
		}
		sc.check("Enter installed Claude Code and Codex", treeDiff(skillDir(home, ".claude")) == "" && treeDiff(skillDir(home, ".agents")) == "", "")
		sc.check("Enter did not write .grok or .gemini", !exists(filepath.Join(home, ".grok")) && !exists(filepath.Join(home, ".gemini")), "")
	})
	t.Run("S12g_shared_user", func(t *testing.T) {
		sc := s.scenario(t, "S12g", "PTY: Codex + Grok Build + Gemini CLI, user level, one shared folder")
		home := t.TempDir()
		r := s.runPTY(sc, home, "", []ptyStep{menu, {"Programs [1,2]: ", "1,4,5\r"}})
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("one .agents tree equals embed", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
		sc.check("no second copy in .grok, nothing in .gemini", !exists(filepath.Join(home, ".grok")) && !exists(filepath.Join(home, ".gemini")), "")
		sc.check("write list names the shared folder once", strings.Count(r.Transcript, "Codex, Grok Build, Gemini CLI ~/.agents/skills/"+imageskill.Name) == 2, "")
		sc.check("share hint shown", strings.Contains(r.Transcript, "Shared folder: Codex, Grok Build, Gemini CLI") && strings.Contains(r.Transcript, "Grok Build also discovers user Skills in ~/.agents/skills"), "")
		sc.check("official reload hints shown", strings.Contains(r.Transcript, "grok inspect") && strings.Contains(r.Transcript, "/skills reload"), "")
	})
	t.Run("S12h_project_flag_desktop_grok", func(t *testing.T) {
		sc := s.scenario(t, "S12h", "skill install --project DIR --apps claude-desktop,grok: one project, HOME untouched")
		home, proj := t.TempDir(), t.TempDir()
		hb := snapTree(home)
		r := s.run(sc, home, "", nil, "", "skill", "install", "--apps", "claude-desktop,grok", "--project", proj)
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("project .claude tree equals embed", treeDiff(skillDir(proj, ".claude")) == "", treeDiff(skillDir(proj, ".claude")))
		sc.check("project .grok tree equals embed", treeDiff(skillDir(proj, ".grok")) == "", treeDiff(skillDir(proj, ".grok")))
		sc.check("project .agents not created", !exists(filepath.Join(proj, ".agents")), "")
		sc.check("HOME unchanged", snapDiff(hb, snapTree(home)) == "", snapDiff(hb, snapTree(home)))
		sc.check("Desktop hint names the Code tab", strings.Contains(r.Stdout+r.Stderr, "new local session in the Code tab"), "")
	})
	t.Run("S12i_cancel_after_bad_targets", func(t *testing.T) {
		sc := s.scenario(t, "S12i", "PTY: a bad program answer asks again; Ctrl-D there writes nothing")
		home := t.TempDir()
		before := snapTree(home)
		r := s.runPTY(sc, home, "", []ptyStep{menu, {"Programs [1,2]: ", "9\r"}, {"out of range", ""}, {"Programs [1,2]: ", "\x04"}})
		sc.check("exit 2", r.Exit == 2, fmt.Sprint(r.Exit))
		sc.check("HOME unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
	})
	t.Run("S12j_shared_conflict", func(t *testing.T) {
		sc := s.scenario(t, "S12j", "PTY: a changed shared folder is asked about once; y replaces it")
		home := t.TempDir()
		s.install(sc, home, "--apps", "claude-code")
		mustWrite(t, filepath.Join(skillDir(home, ".claude"), "SKILL.md"), "edited\n")
		before := snapTree(home)
		r := s.runPTY(sc, home, "", []ptyStep{menu, {"Programs [1,2]: ", "2,3\r"}, {"[y/N] ", "\r"}})
		sc.check("Enter at [y/N] exits 2", r.Exit == 2, fmt.Sprint(r.Exit))
		sc.check("folder listed once", strings.Count(r.Transcript, "already has a different") == 1 && strings.Contains(r.Transcript, "Claude Code, Claude Desktop already has"), "")
		sc.check("HOME unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
		r = s.runPTY(sc, home, "", []ptyStep{menu, {"Programs [1,2]: ", "2,3\r"}, {"[y/N] ", "y\r"}})
		sc.check("y exits 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("tree equals embed", treeDiff(skillDir(home, ".claude")) == "", treeDiff(skillDir(home, ".claude")))
		sc.check("replaced once", strings.Count(r.Transcript, "(replaced)") == 1, "")
	})
}

// skillPTYScript runs argv[1] on a pseudoterminal with the JSON argument
// list in argv[4]. argv[3] is a JSON list of {expect, send}: after each
// expect appears in the output, send is typed. The transcript goes to argv[2]; a JSON status goes to stdout.
// Everything has a deadline, and the child's group is killed on timeout.
const skillPTYScript = `
import errno, fcntl, json, os, select, signal, sys, time
bin_, transcript, plan, args = sys.argv[1], sys.argv[2], json.loads(sys.argv[3]), json.loads(sys.argv[4])
pid, fd = os.forkpty()
if pid == 0:
    os.execv(bin_, [bin_] + args)
fcntl.fcntl(fd, fcntl.F_SETFL, fcntl.fcntl(fd, fcntl.F_GETFL) | os.O_NONBLOCK)
buf, pos, reached, status, timed_out = b"", 0, 0, None, False
deadline = time.time() + 30

def read():
    global buf
    try:
        chunk = os.read(fd, 4096)
    except (BlockingIOError, InterruptedError):
        return True
    except OSError:
        return False
    if not chunk:
        return False
    buf += chunk
    return True

def poll():
    global status
    try:
        done, st = os.waitpid(pid, os.WNOHANG)
    except ChildProcessError:
        return
    if done:
        status = st

while time.time() < deadline:
    if reached < len(plan):
        want = plan[reached]["expect"].encode()
        i = buf.find(want, pos)
        if i >= 0:
            pos = i + len(want)
            time.sleep(0.05)
            os.write(fd, plan[reached]["send"].encode())
            reached += 1
            continue
    if select.select([fd], [], [], 0.2)[0] and not read():
        break  # slave closed: the child is gone
    if status is None:
        poll()
    if status is not None:
        end = time.time() + 1
        while time.time() < end and select.select([fd], [], [], 0.1)[0] and read():
            pass
        break
else:
    timed_out = True
    try:
        os.killpg(pid, signal.SIGKILL)
    except OSError:
        pass
if status is None:
    try:
        _, status = os.waitpid(pid, 0)
    except ChildProcessError:
        status = 0
code = os.WEXITSTATUS(status) if os.WIFEXITED(status) else 128 + os.WTERMSIG(status)
with open(transcript, "wb") as f:
    f.write(buf.replace(b"\r\n", b"\n"))
print(json.dumps({"exit": code, "timed_out": timed_out, "reached": reached}))
`

func (s *skillSuite) installedScript(t *testing.T) {
	if s.python == "" {
		t.Skip("python3 is required for the installed script")
	}
	home := t.TempDir()
	work := t.TempDir()
	codex := skillDir(home, ".agents")
	script := filepath.Join(codex, "scripts", "deeprouter_image.py")
	setup := s.scenario(t, "S13", "installed script: install, then drive generate/403/recover")
	s.install(setup, home)
	setup.check("installed tree equals embed before running the script", treeDiff(codex) == "", treeDiff(codex))
	setup.check("script under test is the installed copy, not the repo", !strings.HasPrefix(script, s.root), script)

	result := filepath.Join(s.logDir, "S13.driver.json")
	driver := filepath.Join(s.root, "e2e", "testdata", "skill_script_driver.py")
	args := []string{"-I", "-B", driver, "--script", script, "--work", work, "--out", result}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, s.python, args...)
	cmd.Dir = work
	cmd.Env = s.env(home, map[string]string{"DEEPROUTER_API_KEY": skillE2EKey})
	out, err := cmd.CombinedOutput()
	setup.Runs = append(setup.Runs, skillRun{Label: "(script driver)", Argv: append([]string{"python3"}, args...), Cwd: work,
		Env: shownEnv(cmd.Env), Stdout: string(out), Exit: exitCode(err)})
	if !setup.check("driver exited 0", err == nil, fmt.Sprint(err)) {
		return
	}
	raw, err := os.ReadFile(result)
	if err != nil {
		t.Fatal(err)
	}
	var res struct {
		Cases []struct {
			Name     string           `json:"name"`
			Argv     []string         `json:"argv"`
			Exit     int              `json:"exit"`
			Stdout   string           `json:"stdout"`
			Stderr   string           `json:"stderr"`
			Requests []map[string]any `json:"requests"`
			Info     map[string]any   `json:"info"`
			Checks   []skillCheck     `json:"checks"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(raw, &res); err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for i, c := range res.Cases {
		names[c.Name] = true
		c := c
		t.Run(c.Name, func(t *testing.T) {
			sc := s.scenario(t, fmt.Sprintf("S13.%d", i+1), "installed script: "+c.Name)
			reqs, _ := json.MarshalIndent(c.Requests, "", "  ")
			info, _ := json.MarshalIndent(c.Info, "", "  ")
			sc.Runs = append(sc.Runs, skillRun{Label: "(in-process main(argv) of the installed script)",
				Argv: append([]string{"deeprouter_image.py"}, c.Argv...), Cwd: work, Env: shownEnv(cmd.Env),
				Exit: c.Exit, Stdout: c.Stdout, Stderr: c.Stderr,
				Details: "requests seen by the fake opener:\n" + string(reqs) + "\ninfo:\n" + string(info)})
			for _, ch := range c.Checks {
				sc.check(ch.Name, ch.OK, ch.Detail)
			}
		})
	}
	for _, want := range []string{"generate_then_download_403", "recover_success", "recover_403_again", "generate_b64_saved_url_403", "safety_controls"} {
		setup.check("driver ran "+want, names[want], "")
	}
	setup.check("running the script left the installed Skill unchanged (no __pycache__)", treeDiff(codex) == "", treeDiff(codex))
	setup.check("fake key not written to any file in HOME or the work dir", !filesContain(home, skillE2EKey) && !filesContain(work, skillE2EKey), "")
}

func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if exit, ok := err.(*exec.ExitError); ok {
		return exit.ExitCode()
	}
	return -1
}

func filesContain(dir, needle string) bool {
	found := false
	_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
		if err == nil && d.Type().IsRegular() {
			if b, err := os.ReadFile(p); err == nil && bytes.Contains(b, []byte(needle)) {
				found = true
			}
		}
		return nil
	})
	return found
}

// realHomeSnapshot fingerprints the real user Skill folders the suite must
// never touch.
func (s *skillSuite) realHomeSnapshot() map[string]string {
	out := map[string]string{}
	if s.realHome == "" {
		return out
	}
	// The real image config is only stat'ed, never read.
	cfg := filepath.Join(s.realHome, ".deeprouter", "image-skill.json")
	if info, err := os.Lstat(cfg); err == nil {
		out[cfg] = fmt.Sprintf("%v %d %d", info.Mode(), info.Size(), info.ModTime().UnixNano())
	} else {
		out[cfg] = "absent"
	}
	for _, hidden := range []string{".claude", ".agents", ".grok", ".gemini"} {
		p := skillDir(s.realHome, hidden)
		if !exists(p) {
			out[p] = "absent"
			continue
		}
		var parts []string
		snap := snapTree(p)
		for k, v := range snap {
			parts = append(parts, k+"="+v)
		}
		sort.Strings(parts)
		out[p] = fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(parts, "\n"))))
	}
	return out
}

type skillSummaryRow struct {
	ID     string   `json:"id"`
	Title  string   `json:"title"`
	Status string   `json:"status"`
	Argv   []string `json:"commands"`
	Exits  []int    `json:"exit_codes"`
	Checks int      `json:"checks"`
	Failed []string `json:"failed_checks,omitempty"`
	Log    string   `json:"log"`
}

const skillMockBoundary = "Mock boundary: no real network, no real billing, no real API key. " +
	"The deeprouter binary runs for real against a temporary HOME; its proxy variables point at a local deny proxy that records any request " +
	"(S17 runs use their own local deny proxy, which refuses the image model step's GET /v1/models to a preset or example host and records it). " +
	"The installed deeprouter_image.py is imported and driven through main(argv); only OPENER (an in-process fake HTTP opener) is replaced, and socket connect/getaddrinfo are made to fail. " +
	"check_result_url, redact, NoRedirect and the header-less result GET run unchanged. " +
	"This does not prove a live 403 from a real result host (e.g. download.xmimage2.cc.cd), and does not prove Claude Code, Codex, Claude Desktop, Grok Build, or Gemini CLI will discover the Skill; " +
	"S19 and S12f-S12j only show the folders land in the documented locations. " +
	"Run on Linux only."

// finish writes SUMMARY.md and summary.json, then checks that no log holds
// the fake key or the fake URL signature.
func (s *skillSuite) finish(t *testing.T) {
	write := func() {
		var rows []skillSummaryRow
		pass, fail, skip := 0, 0, 0
		for _, sc := range s.scenarios {
			row := skillSummaryRow{ID: sc.ID, Title: sc.Title, Status: sc.Status, Checks: len(sc.Checks), Log: sc.LogFile}
			for _, r := range sc.Runs {
				row.Argv = append(row.Argv, shellJoin(r.Argv))
				row.Exits = append(row.Exits, r.Exit)
			}
			for _, c := range sc.Checks {
				if !c.OK {
					row.Failed = append(row.Failed, c.Name)
				}
			}
			switch sc.Status {
			case "PASS":
				pass++
			case "SKIP":
				skip++
			default:
				fail++
			}
			rows = append(rows, row)
		}
		doc := map[string]any{"started": s.started.Format(time.RFC3339), "finished": time.Now().Format(time.RFC3339),
			"goos": runtime.GOOS, "log_dir": s.logDir, "pass": pass, "fail": fail, "skip": skip,
			"mock_boundary": skillMockBoundary, "scenarios": rows}
		js, _ := json.MarshalIndent(doc, "", "  ")
		var md strings.Builder
		fmt.Fprintf(&md, "# deeprouter skill acceptance suite\n\nRun: %s (%s)  \nResult: %d pass, %d fail, %d skip  \nLogs: %s\n\n%s\n\n",
			s.started.Format(time.RFC3339), runtime.GOOS, pass, fail, skip, s.logDir, skillMockBoundary)
		md.WriteString("| ID | Scenario | Commands | Exit | Result | Log |\n|---|---|---|---|---|---|\n")
		for _, r := range rows {
			cmds := make([]string, len(r.Argv))
			for i, a := range r.Argv {
				cmds[i] = "`" + strings.ReplaceAll(a, "|", "\\|") + "`"
			}
			fmt.Fprintf(&md, "| %s | %s | %s | %v | %s | %s |\n", r.ID, r.Title, strings.Join(cmds, "<br>"), r.Exits, r.Status, filepath.Base(r.Log))
		}
		for _, dir := range []string{s.logDir, filepath.Dir(s.logDir)} {
			_ = os.WriteFile(filepath.Join(dir, "summary.json"), js, 0o644)
			_ = os.WriteFile(filepath.Join(dir, "SUMMARY.md"), []byte(md.String()), 0o644)
		}
	}
	write()
	sc := &skillScenario{ID: "S15", Title: "log hygiene: no fake key or URL signature in any log", t: t}
	for _, needle := range []string{skillE2EKey, skillE2ESig, "X-Amz-Signature=", imageE2EKeyA, imageE2EKeyB, imageE2ELegacy, imageE2EListA, imageE2EListB, imageE2EList401} {
		sc.check("logs do not contain "+needle[:12]+"...", !filesContain(s.logDir, needle), "")
	}
	sc.Status = "PASS"
	if !sc.allOK() {
		sc.Status = "FAIL"
	}
	sc.LogFile = filepath.Join(s.logDir, "S15.log")
	_ = os.WriteFile(sc.LogFile, []byte(sc.text()), 0o644)
	s.scenarios = append(s.scenarios, sc)
	write()
}

// newTargets installs Claude Desktop, Grok Build, and Gemini CLI from the
// command line and checks only the documented folders were written.
func (s *skillSuite) newTargets(t *testing.T) {
	type tc struct {
		id, apps string
		project  bool
		want     []string
	}
	for _, c := range []tc{
		{"S19a", "claude-desktop", false, []string{".claude"}},
		{"S19b", "gemini", false, []string{".agents"}},
		{"S19c", "grok", false, []string{".grok"}},
		{"S19d", "grok,codex", false, []string{".agents"}},
		{"S19e", "codex,claude-code,claude-desktop,grok,gemini", false, []string{".agents", ".claude"}},
		{"S19f", "grok,codex", true, []string{".agents", ".grok"}},
		{"S19g", "claude-desktop,gemini,grok", true, []string{".agents", ".claude", ".grok"}},
		{"S19h", "opencode,pi", false, []string{".config/opencode", ".pi/agent"}},
		{"S19i", "opencode,pi", true, []string{".opencode", ".pi"}},
		{"S19j", "codex,opencode,pi", true, []string{".agents"}},
	} {
		t.Run(c.id, func(t *testing.T) {
			sc := s.scenario(t, c.id, fmt.Sprintf("--apps %s (project=%v)", c.apps, c.project))
			home, proj := t.TempDir(), t.TempDir()
			// Program config that must not change.
			conf := map[string]string{
				".claude/settings.json":           `{"env":{"ANTHROPIC_AUTH_TOKEN":"` + skillE2EKey + `"}}`,
				".codex/config.toml":              "model = \"x\"\n",
				".grok/config.toml":               "[skills]\n",
				".gemini/settings.json":           "{}\n",
				".config/opencode/opencode.jsonc": "{/* untouched */}\n",
				".pi/agent/models.json":           "{}\n",
				".deeprouter/image-skill.json":    `{"unused":"` + skillE2EKey + `"}`,
			}
			for rel, body := range conf {
				p := filepath.Join(home, filepath.FromSlash(rel))
				if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
					t.Fatal(err)
				}
				mustWrite(t, p, body)
			}
			confBefore := map[string]string{}
			for rel := range conf {
				confBefore[rel] = snapTree(filepath.Dir(filepath.Join(home, filepath.FromSlash(rel))))[filepath.Base(rel)]
			}
			args := []string{"skill", "install", "--apps", c.apps}
			base := home
			if c.project {
				args = append(args, "--project", proj)
				base = proj
			}
			r := s.run(sc, home, "", nil, "", args...)
			sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
			for _, hidden := range []string{".claude", ".agents", ".grok", ".gemini", ".opencode", ".pi", ".config/opencode", ".pi/agent"} {
				want := false
				for _, w := range c.want {
					want = want || w == hidden
				}
				if want {
					sc.check(hidden+" tree equals embed", treeDiff(skillDir(base, hidden)) == "", treeDiff(skillDir(base, hidden)))
				} else {
					sc.check(hidden+" Skill not written", !exists(skillDir(base, hidden)), "")
				}
			}
			if c.project {
				for _, hidden := range []string{".claude", ".agents", ".grok", ".gemini", ".opencode", ".pi", ".config/opencode", ".pi/agent"} {
					sc.check("HOME "+hidden+" Skill not written", !exists(skillDir(home, hidden)), "")
				}
			}
			for rel, was := range confBefore {
				now := snapTree(filepath.Dir(filepath.Join(home, filepath.FromSlash(rel))))[filepath.Base(rel)]
				sc.check(rel+" unchanged", now == was, "")
			}
			for _, hidden := range []string{".claude", ".agents", ".grok"} {
				sc.check("no key in "+hidden+" Skill", !filesContain(skillDir(base, hidden), skillE2EKey), "")
			}
			sc.check("no key in output", !strings.Contains(r.Stdout+r.Stderr, skillE2EKey), "")
		})
	}
}

// copyLegacy copies Skill 1.0.4, the last release installed as
// deeprouter-image-api, to root/deeprouter-image-api.
func copyLegacy(t *testing.T, root string) string {
	t.Helper()
	src := filepath.Join("..", "internal", "imageskill", "testdata", "legacy-1.0.4")
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

func (s *skillSuite) legacyFolder(t *testing.T) {
	t.Run("S20a_bundled_migrates", func(t *testing.T) {
		sc := s.scenario(t, "S20a", "an unmodified old deeprouter-image-api is replaced by image-generate")
		home := t.TempDir()
		claudeOld := copyLegacy(t, filepath.Join(home, ".claude", "skills"))
		codexOld := copyLegacy(t, filepath.Join(home, ".agents", "skills"))
		sibling := filepath.Join(home, ".claude", "skills", "my-other-skill", "SKILL.md")
		if err := os.MkdirAll(filepath.Dir(sibling), 0o755); err != nil {
			t.Fatal(err)
		}
		mustWrite(t, sibling, "user's own skill\n")
		siblingBefore := snapTree(filepath.Dir(sibling))
		r := s.run(sc, home, "", nil, "", "skill", "install")
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("new folders are .../skills/image-generate", filepath.Base(skillDir(home, ".claude")) == "image-generate", skillDir(home, ".claude"))
		sc.check("Claude tree equals embed", treeDiff(skillDir(home, ".claude")) == "", treeDiff(skillDir(home, ".claude")))
		sc.check("Codex tree equals embed", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
		sc.check("both old folders gone", !exists(claudeOld) && !exists(codexOld), "")
		sc.check("stdout says the old copies were replaced", strings.Count(r.Stdout, "replaced the unchanged old deeprouter-image-api") == 2, r.Stdout)
		sc.check("sibling skill untouched", snapDiff(siblingBefore, snapTree(filepath.Dir(sibling))) == "", "")
		sc.check("no staging/backup folders left", len(globAll(filepath.Join(home, "*", "skills", ".image-generate.*"))) == 0, "")
	})
	t.Run("S20b_changed_kept", func(t *testing.T) {
		sc := s.scenario(t, "S20b", "a changed old deeprouter-image-api is kept, even with --force, and image-generate is not added beside it")
		home := t.TempDir()
		old := copyLegacy(t, filepath.Join(home, ".claude", "skills"))
		mustWrite(t, filepath.Join(old, "notes.txt"), "my notes\n")
		before := snapTree(old)
		r := s.run(sc, home, "", nil, "", "skill", "install", "--force")
		sc.check("exit 1", r.Exit == 1, fmt.Sprint(r.Exit))
		sc.check("old folder unchanged", snapDiff(before, snapTree(old)) == "", snapDiff(before, snapTree(old)))
		sc.check("no image-generate beside it", !exists(skillDir(home, ".claude")), "")
		sc.check("Codex still installed", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
		sc.check("stderr explains why", strings.Contains(r.Stderr, "Kept ~/.claude/skills/deeprouter-image-api: it is not an unmodified copy") &&
			strings.Contains(r.Stderr, "was not added beside it"), r.Stderr)
	})
	t.Run("S20c_pty_changed_kept", func(t *testing.T) {
		if s.python == "" {
			t.Skip("python3 is required to allocate a terminal")
		}
		sc := s.scenario(t, "S20c", "PTY: a changed old folder is kept and the warning is shown")
		home := t.TempDir()
		old := copyLegacy(t, filepath.Join(home, ".claude", "skills"))
		mustWrite(t, filepath.Join(old, "SKILL.md"), "mine\n")
		before := snapTree(old)
		r := s.runPTY(sc, home, "", []ptyStep{{"Choose [1]: ", "\r"}, {"Programs [1,2]: ", "2\r"}})
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("old folder unchanged", snapDiff(before, snapTree(old)) == "", "")
		sc.check("no image-generate beside it", !exists(skillDir(home, ".claude")), "")
		sc.check("transcript shows the warning", strings.Contains(r.Transcript, "was not added beside it"), "")
	})
}
