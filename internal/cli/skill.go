package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"sync"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/imageskill"
	"github.com/zfg5118/deeprouter/internal/textutil"
)

// cmdSkill is deeprouter skill: a menu in a terminal, or skill install.
func cmdSkill(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	if len(args) == 0 {
		return cmdSkillMenu(stdout, stderr, stdin)
	}
	switch args[0] {
	case "--help", "-h", "help":
		fmt.Fprint(stdout, ui(skillHelp, skillHelpZH))
		return 0, nil
	case "install":
		return cmdSkillInstall(args[1:], stdout, stderr)
	default:
		fmt.Fprint(stderr, ui(skillHelp, skillHelpZH))
		return 2, fmt.Errorf("unknown skill command %q", args[0])
	}
}

func cmdSkillInstall(args []string, stdout, stderr io.Writer) (int, error) {
	fs := newFlagSet("skill install", stderr, skillHelp, skillHelpZH)
	appsFlag := fs.String("apps", "", "claude-code and/or codex (default: both)")
	project := fs.String("project", "", "install into this project directory instead of the user folder")
	force := fs.Bool("force", false, "replace an existing deeprouter-image-api folder that differs")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, ui(skillHelp, skillHelpZH))
		return 2, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	targets, err := parseSkillTargets(*appsFlag)
	if err != nil {
		return 2, err
	}
	loc, err := skillLocations()
	if err != nil {
		return 1, err
	}
	if visited(fs)["project"] {
		if loc.Project, err = projectDir(*project); err != nil {
			return 2, err
		}
	}
	plans, err := checkSkillTargets(targets, loc)
	if err != nil {
		return 1, err
	}
	if conflicts := skillConflicts(plans); len(conflicts) > 0 && !*force {
		printSkillConflicts(stderr, loc.Home, conflicts)
		fmt.Fprintln(stderr, ui(
			"Nothing was written. Pass --force to replace the folder with the bundled Skill.",
			"没有写入任何文件。传入 --force 会用内置的 Skill 替换这个文件夹。",
		))
		return 1, nil
	}
	if err := installSkill(plans); err != nil {
		return 1, err
	}
	printSkillDone(stdout, loc.Home, plans)
	return 0, nil
}

// skillLocations reads the home directory and CLAUDE_CONFIG_DIR. It reads
// no config file and no key.
func skillLocations() (imageskill.Locations, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return imageskill.Locations{}, fmt.Errorf("cannot find the home directory: %w", err)
	}
	loc := imageskill.Locations{Home: home}
	if dir := strings.TrimSpace(os.Getenv("CLAUDE_CONFIG_DIR")); dir != "" {
		if abs, err := filepath.Abs(expandHome(dir, home)); err == nil {
			loc.ClaudeConfigDir = abs
		}
	}
	return loc, nil
}

// projectDir is dir as an absolute path. It must be an existing folder.
func projectDir(dir string) (string, error) {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return "", fmt.Errorf("--project needs a directory")
	}
	if home, err := os.UserHomeDir(); err == nil {
		dir = expandHome(dir, home)
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(abs)
	if err != nil {
		return "", fmt.Errorf("project directory %s does not exist", abs)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("project directory %s is not a directory", abs)
	}
	return abs, nil
}

func expandHome(path, home string) string {
	if path == "~" {
		return home
	}
	if strings.HasPrefix(path, "~/") || strings.HasPrefix(path, `~\`) {
		return filepath.Join(home, path[2:])
	}
	return path
}

// skillTargetRows is the target picker, in imageskill.Targets order.
func skillTargetRows() []apps.Info {
	var rows []apps.Info
	for _, id := range imageskill.Targets() {
		info, _ := apps.ByID(id)
		rows = append(rows, info)
	}
	return rows
}

// parseSkillTargets reads --apps or the picker answer. Empty is every
// target. Separators are the program picker's.
func parseSkillTargets(answer string) ([]apps.ID, error) {
	ids, err := apps.ParseIndexed(answer, skillTargetRows(), imageskill.Targets())
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !imageskill.Supported(id) {
			return nil, fmt.Errorf("the image Skill installs only for claude-code and codex, not %q", id)
		}
	}
	return ids, nil
}

// checkSkillTargets checks every destination before anything is written,
// so one unsafe destination stops the whole install.
func checkSkillTargets(targets []apps.ID, loc imageskill.Locations) ([]imageskill.Plan, error) {
	var plans []imageskill.Plan
	seen := map[string]bool{}
	for _, id := range targets {
		p, err := imageskill.Check(id, loc)
		if err != nil {
			return nil, err
		}
		if seen[p.Dir] {
			continue
		}
		seen[p.Dir] = true
		plans = append(plans, p)
	}
	return plans, nil
}

func skillConflicts(plans []imageskill.Plan) []imageskill.Plan {
	var out []imageskill.Plan
	for _, p := range plans {
		if p.State == imageskill.Different {
			out = append(out, p)
		}
	}
	return out
}

// installSkill installs every plan, or none: a failure undoes the ones
// already installed.
func installSkill(plans []imageskill.Plan) error {
	var done []*imageskill.Txn
	for _, p := range plans {
		t, err := imageskill.Install(p)
		if err != nil {
			for i := len(done) - 1; i >= 0; i-- {
				_ = done[i].Rollback()
			}
			return err
		}
		done = append(done, t)
	}
	for _, t := range done {
		t.Commit()
	}
	return nil
}

func skillTitle(id apps.ID) string {
	info, _ := apps.ByID(id)
	return info.Title
}

func printSkillConflicts(w io.Writer, home string, plans []imageskill.Plan) {
	for _, p := range plans {
		fmt.Fprintf(w, ui("%s already has a different %s folder:\n", "%s 已有一个内容不同的 %s 文件夹：\n"), skillTitle(p.App), imageskill.Name)
		fmt.Fprintf(w, "  %s\n", textutil.DisplayPath(home, p.Dir))
		for _, d := range p.Diff {
			fmt.Fprintf(w, "    %s\n", skillDiffLine(d))
		}
	}
}

// skillDiffLine translates the kind before the colon, as in "changed: SKILL.md".
func skillDiffLine(d string) string {
	if !promptLocaleZH() {
		return d
	}
	kind, rel, ok := strings.Cut(d, ": ")
	if !ok {
		return d
	}
	zh := map[string]string{"changed": "已修改", "missing": "缺少", "extra": "多出", "symlink": "符号链接"}[kind]
	if zh == "" {
		return d
	}
	return zh + "：" + rel
}

func printSkillDone(w io.Writer, home string, plans []imageskill.Plan) {
	fmt.Fprintf(w, ui("Image Skill %s %s:\n", "生图 Skill %s %s：\n"), imageskill.Name, imageskill.Version)
	for _, p := range plans {
		state := ui("(installed)", "（已安装）")
		switch p.State {
		case imageskill.Same:
			state = ui("(already installed, unchanged)", "（已经安装，没有改动）")
		case imageskill.Different:
			state = ui("(replaced)", "（已替换）")
		}
		fmt.Fprintf(w, "  %-16s %s  %s\n", skillTitle(p.App), textutil.DisplayPath(home, p.Dir), state)
	}
	fmt.Fprintln(w, ui(
		"This install made no API call and was not billed.",
		"这次安装没有调用任何接口，不产生费用。",
	))
	fmt.Fprintln(w, ui(
		"Installing did not write any key. When it runs, the Skill uses the image address and token saved with deeprouter (main menu, Image Skill), or else DEEPROUTER_API_KEY.",
		"安装没有写入任何令牌。Skill 运行时使用 deeprouter 主菜单“生图Skill”保存的生图地址和令牌；没有保存时读取 DEEPROUTER_API_KEY。",
	))
	fmt.Fprintln(w, ui(
		"Generating an image is a separate step later and may be billed.",
		"之后生成图片是另外一步，可能产生费用。",
	))
	fmt.Fprintln(w, ui(
		"Start a new Claude Code or Codex session if the Skill does not show up.",
		"如果看不到这个 Skill，请新开一个 Claude Code 或 Codex 会话。",
	))
}

// cmdSkillMenu is deeprouter skill in a terminal.
func cmdSkillMenu(stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	if !stdinIsTerminal(stdin) {
		fmt.Fprintln(stderr, ui("Run deeprouter skill in a terminal.", "请在终端中运行 deeprouter skill。"))
		fmt.Fprintln(stderr, ui(
			"For a non-interactive install, use: deeprouter skill install [--apps claude-code,codex] [--project DIR] [--force]",
			"非交互安装请用：deeprouter skill install [--apps claude-code,codex] [--project 目录] [--force]",
		))
		return 2, nil
	}
	loc, err := skillLocations()
	if err != nil {
		return 1, err
	}
	g := guardSkill(stderr)
	defer g.stop()
	in := bufio.NewReader(stdin)

	fmt.Fprintln(stderr, "DeepRouter Skill")
	fmt.Fprintf(stderr, "  1) %s  (%s %s)\n", ui("Install image Skill", "安装生图Skill"), imageskill.Name, imageskill.Version)
	fmt.Fprintf(stderr, "  2) %s\n", ui("Quit", "退出"))
	for {
		line, err := readLine(in, stderr, ui("Choose [1]: ", "选择 [1]："))
		if err != nil {
			return skillCancelled(stderr, err)
		}
		if line == "" || line == "1" {
			break
		}
		if line == "2" || strings.EqualFold(line, "q") {
			return skillCancelled(stderr, io.EOF)
		}
		fmt.Fprintln(stderr, ui("Choose 1 or 2.", "请选择 1 或 2。"))
	}

	_, code, err := skillInstallSteps(in, stdout, stderr, loc, g)
	return code, err
}

// skillInstallSteps asks for the programs and the scope, then installs or
// updates the bundled Skill and prints what it did. code 0 with a nil error
// means the Skill is now in place for plans. Any other result has already
// been reported (a cancel prints its own line) and is the command's result.
func skillInstallSteps(in *bufio.Reader, stdout, stderr io.Writer, loc imageskill.Locations, g *skillGuard) ([]imageskill.Plan, int, error) {
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui(
		"Choose where to install the Skill. Press Enter for both.",
		"选择要安装 Skill 的程序。直接回车两个都装。",
	))
	fmt.Fprintln(stderr, chooseProgramsExample(promptLocaleZH()))
	rows := skillTargetRows()
	var targets []apps.ID
	for {
		for i, info := range rows {
			fmt.Fprintf(stderr, "%d) %s\n", i+1, info.Title)
		}
		line, err := readLine(in, stderr, ui("Programs [1,2]: ", "程序 [1,2]："))
		if err != nil {
			code, err := skillCancelled(stderr, err)
			return nil, code, err
		}
		if targets, err = parseSkillTargets(line); err == nil {
			break
		}
		printError(stderr, err)
	}

	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui("Install for:", "安装范围："))
	fmt.Fprintln(stderr, ui("  1) This user, all projects (default)", "  1) 当前用户，所有项目（默认）"))
	fmt.Fprintln(stderr, ui("  2) One project", "  2) 单个项目"))
	for {
		line, err := readLine(in, stderr, ui("Scope [1]: ", "范围 [1]："))
		if err != nil {
			code, err := skillCancelled(stderr, err)
			return nil, code, err
		}
		if line == "" || line == "1" {
			break
		}
		if line != "2" {
			fmt.Fprintln(stderr, ui("Choose 1 or 2.", "请选择 1 或 2。"))
			continue
		}
		cwd, _ := os.Getwd()
		for {
			line, err := readLine(in, stderr, fmt.Sprintf(ui("Project directory [%s]: ", "项目目录 [%s]："), cwd))
			if err != nil {
				code, err := skillCancelled(stderr, err)
				return nil, code, err
			}
			if line == "" {
				line = cwd
			}
			if loc.Project, err = projectDir(line); err == nil {
				break
			}
			printError(stderr, err)
		}
		break
	}

	plans, err := checkSkillTargets(targets, loc)
	if err != nil {
		fmt.Fprintln(stderr, ui("Nothing was written.", "没有写入任何文件。"))
		return nil, 1, err
	}
	var todo []imageskill.Plan
	for _, p := range plans {
		if p.State != imageskill.Same {
			todo = append(todo, p)
		}
	}
	if len(todo) == 0 {
		fmt.Fprintln(stderr)
		printSkillDone(stdout, loc.Home, plans)
		return plans, 0, nil
	}
	if conflicts := skillConflicts(plans); len(conflicts) > 0 {
		fmt.Fprintln(stderr)
		printSkillConflicts(stderr, loc.Home, conflicts)
		ok, err := askYes(in, stderr, ui(
			"Replace it with the bundled Skill? Your changes in that folder will be lost. [y/N] ",
			"用内置的 Skill 替换它？这个文件夹里的改动会丢失。[y/N] ",
		), false)
		if err != nil {
			code, err := skillCancelled(stderr, err)
			return nil, code, err
		}
		if !ok {
			code, err := skillCancelled(stderr, io.EOF)
			return nil, code, err
		}
	}
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui("deeprouter will write:", "将写入："))
	for _, p := range todo {
		fmt.Fprintf(stderr, "  %-16s %s\n", skillTitle(p.App), textutil.DisplayPath(loc.Home, p.Dir))
	}
	ok, err := askYes(in, stderr, ui("Install? [Y/n] ", "安装？[Y/n] "), true)
	if err != nil {
		code, err := skillCancelled(stderr, err)
		return nil, code, err
	}
	if !ok {
		code, err := skillCancelled(stderr, io.EOF)
		return nil, code, err
	}
	if err := g.write(func() error { return installSkill(plans) }); err != nil {
		return nil, 1, err
	}
	fmt.Fprintln(stderr)
	printSkillDone(stdout, loc.Home, plans)
	return plans, 0, nil
}

// askYes reads y or n. Enter is def. Anything else asks again.
func askYes(in *bufio.Reader, stderr io.Writer, prompt string, def bool) (bool, error) {
	for {
		line, err := readLine(in, stderr, prompt)
		if err != nil {
			return false, err
		}
		switch strings.ToLower(line) {
		case "":
			return def, nil
		case "y", "yes", "是":
			return true, nil
		case "n", "no", "否":
			return false, nil
		}
	}
}

func skillCancelled(stderr io.Writer, err error) (int, error) {
	if !errors.Is(err, io.EOF) {
		return 1, err
	}
	fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
	return 2, nil
}

// skillGuard handles Ctrl-C in deeprouter skill. Before the write nothing
// has changed. An interrupt during the write waits for it to finish, since
// the install either completes or undoes itself.
type skillGuard struct {
	mu    sync.Mutex
	wrote bool
	sig   chan os.Signal
	quit  chan struct{}
}

func guardSkill(stderr io.Writer) *skillGuard {
	g := &skillGuard{sig: make(chan os.Signal, 1), quit: make(chan struct{})}
	signal.Notify(g.sig, os.Interrupt)
	go func() {
		select {
		case <-g.sig:
			exitProcess(g.interrupted(stderr))
		case <-g.quit:
		}
	}()
	return g
}

func (g *skillGuard) interrupted(stderr io.Writer) int {
	g.mu.Lock()
	fmt.Fprintln(stderr)
	if g.wrote {
		fmt.Fprintln(stderr, ui("Interrupted. The Skill was already installed.", "已中断。Skill 已经安装好。"))
	} else {
		fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
	}
	return 130
}

func (g *skillGuard) write(install func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	err := install()
	g.wrote = err == nil
	return err
}

// hold runs write with the interrupt handler waiting, so Ctrl-C cannot
// stop the process in the middle of it. It does not change what the
// handler reports about the Skill install.
func (g *skillGuard) hold(write func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return write()
}

func (g *skillGuard) stop() {
	signal.Stop(g.sig)
	close(g.quit)
}
