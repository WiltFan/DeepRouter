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
	appsFlag := fs.String("apps", "", "codex, claude-code, claude-desktop, grok, gemini (default: claude-code,codex)")
	project := fs.String("project", "", "install into this project directory instead of the user folder")
	force := fs.Bool("force", false, "replace an existing image-generate folder that differs")
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
	printSkillDone(stdout, loc, plans)
	if printSkillLegacyKept(stderr, loc.Home, plans) {
		return 1, nil
	}
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

// skillTargetRows is the target picker, in imageskill.Targets order. The
// titles name what loads the folder, not the whole product: Codex covers
// Codex in the ChatGPT desktop app, but not ChatGPT Chat.
func skillTargetRows() []apps.Info {
	var rows []apps.Info
	for _, id := range imageskill.Targets() {
		rows = append(rows, apps.Info{ID: id, Title: skillTitle(id), Note: skillTargetNote(id)})
	}
	return rows
}

// parseSkillTargets reads --apps or the picker answer. Empty is
// imageskill.DefaultTargets, Codex and Claude Code. Separators are the
// program picker's.
func parseSkillTargets(answer string) ([]apps.ID, error) {
	for _, field := range strings.FieldsFunc(answer, func(r rune) bool { return strings.ContainsRune(", \t、，\u3000", r) }) {
		if strings.EqualFold(strings.TrimSpace(field), "chatgpt") {
			return nil, fmt.Errorf("%s", ui(
				"ChatGPT Chat and Work do not load a local Skill folder. For Codex in the ChatGPT desktop app, Codex CLI, or the IDE extension, use codex",
				"ChatGPT 的 Chat 和 Work 不读取本地 Skill 文件夹。ChatGPT 桌面版里的 Codex、Codex CLI 和 IDE 插件请选 codex",
			))
		}
	}
	ids, err := apps.ParseIndexed(answer, skillTargetRows(), imageskill.DefaultTargets())
	if err != nil {
		return nil, err
	}
	for _, id := range ids {
		if !imageskill.Supported(id) {
			return nil, fmt.Errorf("the image Skill installs only for codex, claude-code, claude-desktop, grok, and gemini, not %q", id)
		}
	}
	return ids, nil
}

// checkSkillTargets checks every destination before anything is written,
// so one unsafe destination stops the whole install. Programs that read
// the same folder share one plan, and the folder is written once.
func checkSkillTargets(targets []apps.ID, loc imageskill.Locations) ([]imageskill.Plan, error) {
	return imageskill.CheckAll(targets, loc)
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
// already installed. Skipped plans are left alone.
func installSkill(plans []imageskill.Plan) error {
	var done []*imageskill.Txn
	for _, p := range plans {
		if p.Skipped() {
			continue
		}
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

// skillTitle is a program's name in the Skill picker and lists.
func skillTitle(id apps.ID) string {
	switch id {
	case apps.Codex:
		return "Codex"
	case apps.ClaudeCode:
		return "Claude Code"
	case apps.ClaudeDesktop:
		return "Claude Desktop"
	case apps.Grok:
		return "Grok Build"
	case apps.Gemini:
		return "Gemini CLI"
	}
	return string(id)
}

// skillTargetNote says which part of a program loads the Skill folder.
func skillTargetNote(id apps.ID) string {
	switch id {
	case apps.Codex:
		return ui("Codex CLI, IDE extension, and Codex in the ChatGPT desktop app (not ChatGPT Chat/Work)",
			"Codex CLI、IDE 插件和 ChatGPT 桌面版里的 Codex（不含 ChatGPT 的 Chat/Work）")
	case apps.ClaudeCode:
		return ui("Claude Code CLI", "Claude Code 命令行")
	case apps.ClaudeDesktop:
		return ui("local sessions in the Code tab only (not Chat, Cowork, SSH, or cloud sessions)",
			"只用于 Code 标签页的本地会话（不含 Chat、Cowork、SSH 和云端会话）")
	case apps.Grok:
		return ui("the grok CLI / TUI (not Grok chat)", "grok 命令行 / TUI（不含 Grok 聊天）")
	case apps.Gemini:
		return ui("the gemini CLI (not the Gemini app); shares .agents/skills with Codex",
			"gemini 命令行（不含 Gemini 应用）；和 Codex 共用 .agents/skills")
	}
	return ""
}

// skillPlanTitles is the programs that read p.Dir, as "Codex, Gemini CLI".
func skillPlanTitles(p imageskill.Plan) string {
	var names []string
	for _, id := range p.Apps {
		names = append(names, skillTitle(id))
	}
	return strings.Join(names, ui(", ", "、"))
}

// skillPlanNotes explains a shared folder, Grok Build reading
// ~/.agents/skills, and Claude Desktop ignoring CLAUDE_CONFIG_DIR.
func skillPlanNotes(loc imageskill.Locations, p imageskill.Plan) []string {
	var notes []string
	if len(p.Apps) > 1 {
		notes = append(notes, fmt.Sprintf(ui(
			"Shared folder: %s all load Skills from this documented path, so it is written once.",
			"共用文件夹：%s 都从这个官方路径读取 Skill，所以只写一次。"), skillPlanTitles(p)))
	}
	if imageskill.SharesAgents(p) {
		notes = append(notes, ui(
			"Grok Build also discovers user Skills in ~/.agents/skills, so no second copy goes to ~/.grok/skills.",
			"Grok Build 也会读取 ~/.agents/skills 里的用户级 Skill，所以不再往 ~/.grok/skills 写第二份。"))
	}
	if apps.Contains(p.Apps, apps.ClaudeDesktop) && loc.Project == "" && strings.TrimSpace(loc.ClaudeConfigDir) != "" && !apps.Contains(p.Apps, apps.ClaudeCode) {
		notes = append(notes, ui(
			"CLAUDE_CONFIG_DIR is set, but it is a Claude Code CLI setting: Claude Desktop documents ~/.claude/skills, while Claude Code reads $CLAUDE_CONFIG_DIR/skills. They are separate folders.",
			"设置了 CLAUDE_CONFIG_DIR，但它是 Claude Code 命令行的设置：Claude Desktop 文档写的是 ~/.claude/skills，Claude Code 读取 $CLAUDE_CONFIG_DIR/skills，这是两个不同的文件夹。"))
	}
	return notes
}

func printSkillPlans(w io.Writer, loc imageskill.Locations, plans []imageskill.Plan, state func(imageskill.Plan) string) {
	for _, p := range plans {
		line := fmt.Sprintf("  %-16s %s", skillPlanTitles(p), textutil.DisplayPath(loc.Home, p.Dir))
		if state != nil {
			if st := state(p); st != "" {
				line += "  " + st
			}
		}
		fmt.Fprintln(w, line)
		for _, note := range skillPlanNotes(loc, p) {
			fmt.Fprintf(w, "    %s\n", note)
		}
	}
}

func printSkillConflicts(w io.Writer, home string, plans []imageskill.Plan) {
	for _, p := range plans {
		fmt.Fprintf(w, ui("%s already has a different %s folder:\n", "%s 已有一个内容不同的 %s 文件夹：\n"), skillPlanTitles(p), imageskill.Name)
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

// printSkillLegacyKept explains each old deeprouter-image-api folder that
// was kept because it is not an unchanged bundled Skill, and reports
// whether any destination was skipped because of one.
func printSkillLegacyKept(w io.Writer, home string, plans []imageskill.Plan) bool {
	skipped := false
	for _, p := range plans {
		if p.Legacy != imageskill.LegacyKept {
			continue
		}
		fmt.Fprintf(w, ui(
			"Kept %s: it is not an unmodified copy of the previously bundled Skill, so deeprouter did not delete it.\n",
			"保留了 %s：它不是未改动过的旧版内置 Skill，deeprouter 不会删除它。\n"), textutil.DisplayPath(home, p.LegacyDir))
		if p.Skipped() {
			skipped = true
			fmt.Fprintf(w, ui(
				"  The new %s folder was not added beside it, so the Skill is not discovered twice. Move or delete the old folder yourself, then install again.\n",
				"  没有在旁边新装 %s，以免同一个 Skill 被发现两次。请自行移走或删除旧文件夹，然后重新安装。\n"), imageskill.Name)
		} else {
			fmt.Fprintf(w, ui(
				"  %s is there too, so programs may list the Skill twice. Remove the old folder if you no longer need it.\n",
				"  %s 也在这里，程序可能会把这个 Skill 列出两次。不再需要旧文件夹时请自行删除。\n"), imageskill.Name)
		}
	}
	return skipped
}

func printSkillDone(w io.Writer, loc imageskill.Locations, plans []imageskill.Plan) {
	fmt.Fprintf(w, ui("Image Skill %s %s:\n", "生图 Skill %s %s：\n"), imageskill.Name, imageskill.Version)
	printSkillPlans(w, loc, plans, func(p imageskill.Plan) string {
		if p.Skipped() {
			return fmt.Sprintf(ui("(not installed: the old %s folder was kept)", "（没有安装：保留了旧的 %s 文件夹）"), imageskill.LegacyName)
		}
		if p.Legacy == imageskill.LegacyBundled {
			switch p.State {
			case imageskill.Same:
				return fmt.Sprintf(ui("(already installed; removed the unchanged old %s)", "（已经安装；删除了未改动的旧版 %s）"), imageskill.LegacyName)
			case imageskill.Different:
				return fmt.Sprintf(ui("(replaced; removed the unchanged old %s)", "（已替换；删除了未改动的旧版 %s）"), imageskill.LegacyName)
			}
			return fmt.Sprintf(ui("(installed; replaced the unchanged old %s)", "（已安装；替换了未改动的旧版 %s）"), imageskill.LegacyName)
		}
		switch p.State {
		case imageskill.Same:
			return ui("(already installed, unchanged)", "（已经安装，没有改动）")
		case imageskill.Different:
			return ui("(replaced)", "（已替换）")
		}
		return ui("(installed)", "（已安装）")
	})
	fmt.Fprintln(w, ui(
		"This install made no API call and was not billed.",
		"这次安装没有调用任何接口，不产生费用。",
	))
	fmt.Fprintln(w, ui(
		"Installing did not write any key. When it runs, the Skill uses the image address and token saved with deeprouter (Image Skill in its program list), or else DEEPROUTER_API_KEY.",
		"安装没有写入任何令牌。Skill 运行时使用 deeprouter 程序列表里“生图Skill”保存的生图地址和令牌；没有保存时读取 DEEPROUTER_API_KEY。",
	))
	fmt.Fprintln(w, ui(
		"Only the Skill folder was written; no program settings were changed. Running the Skill's script needs Python 3.8+.",
		"只写入了 Skill 文件夹，没有改动任何程序的配置。运行 Skill 的脚本需要 Python 3.8+。",
	))
	fmt.Fprintln(w, ui(
		"Generating an image is a separate step later and may be billed.",
		"之后生成图片是另外一步，可能产生费用。",
	))
	for _, line := range skillReloadHints(skillPlanApps(plans)) {
		fmt.Fprintln(w, line)
	}
}

// skillReloadHints is how each selected program finds the Skill, using only
// the ways its official docs give.
func skillReloadHints(ids []apps.ID) []string {
	var lines, cli []string
	for _, id := range []apps.ID{apps.ClaudeCode, apps.Codex} {
		if apps.Contains(ids, id) {
			cli = append(cli, skillTitle(id))
		}
	}
	if len(cli) > 0 {
		lines = append(lines, fmt.Sprintf(ui(
			"Start a new %s session if the Skill does not show up.",
			"如果看不到这个 Skill，请新开一个 %s 会话。"), strings.Join(cli, ui(" or ", " 或 "))))
	}
	if apps.Contains(ids, apps.ClaudeDesktop) {
		lines = append(lines, ui(
			"Claude Desktop: start a new local session in the Code tab, then type / or use + > Slash commands to find it. Chat, Cowork, SSH, and cloud sessions do not read this folder.",
			"Claude Desktop：在 Code 标签页新开一个本地会话，输入 / 或点 + > Slash commands 查看。Chat、Cowork、SSH 和云端会话不读取这个文件夹。"))
	}
	if apps.Contains(ids, apps.Grok) {
		lines = append(lines, ui(
			"Grok Build: open the extensions view with /skills in the grok TUI, or run grok inspect to list discovered Skills.",
			"Grok Build：在 grok TUI 里用 /skills 打开扩展列表，或运行 grok inspect 查看发现的 Skill。"))
	}
	if apps.Contains(ids, apps.Gemini) {
		lines = append(lines, ui(
			"Gemini CLI: run /skills reload in a running session.",
			"Gemini CLI：在已打开的会话里运行 /skills reload。"))
	}
	return lines
}

// cmdSkillMenu is deeprouter skill in a terminal.
func cmdSkillMenu(stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	if !stdinIsTerminal(stdin) {
		fmt.Fprintln(stderr, ui("Run deeprouter skill in a terminal.", "请在终端中运行 deeprouter skill。"))
		fmt.Fprintln(stderr, ui(
			"For a non-interactive install, use: deeprouter skill install [--apps claude-code,codex] [--project DIR] [--force]",
			"非交互安装请用：deeprouter skill install [--apps claude-code,codex] [--project 目录] [--force]",
		))
		fmt.Fprintln(stderr, ui(
			"--apps can also name claude-desktop, grok, or gemini. Without --apps it installs for claude-code and codex.",
			"--apps 还可以写 claude-desktop、grok 或 gemini。不写 --apps 时装到 claude-code 和 codex。",
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

	_, code, err := skillInstallSteps(in, stdout, stderr, loc, g, nil)
	return code, err
}

// skillInstallSteps installs or updates the bundled Skill for the current
// user (every project) and prints what it did. It asks which programs to
// install for unless targets names them. It does not ask for a scope or a
// final yes: only a different folder already there needs a confirmation.
// A single project is the --project flag of skill install. code 0 with a
// nil error means the Skill is now in place for plans. Any other result has
// already been reported (a cancel prints its own line) and is the
// command's result.
func skillInstallSteps(in *bufio.Reader, stdout, stderr io.Writer, loc imageskill.Locations, g *skillGuard, targets []apps.ID) ([]imageskill.Plan, int, error) {
	if len(targets) == 0 {
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, ui(
			"Choose where to install the Skill. Press Enter for Codex and Claude Code (1,2).",
			"选择要安装 Skill 的程序。直接回车装 Codex 和 Claude Code（1,2）。",
		))
		fmt.Fprintln(stderr, chooseProgramsExample(promptLocaleZH()))
		rows := skillTargetRows()
		for {
			for i, info := range rows {
				fmt.Fprintf(stderr, "%d) %-15s %s\n", i+1, info.Title, info.Note)
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
	}

	plans, err := checkSkillTargets(targets, loc)
	if err != nil {
		fmt.Fprintln(stderr, ui("Nothing was written.", "没有写入任何文件。"))
		return nil, 1, err
	}
	var todo []imageskill.Plan
	for _, p := range plans {
		if p.Writes() {
			todo = append(todo, p)
		}
	}
	if len(todo) == 0 {
		fmt.Fprintln(stderr)
		printSkillDone(stdout, loc, plans)
		printSkillLegacyKept(stderr, loc.Home, plans)
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
	printSkillPlans(stderr, loc, todo, func(p imageskill.Plan) string {
		if p.Legacy == imageskill.LegacyBundled {
			return fmt.Sprintf(ui("(and remove the unchanged old %s)", "（并删除未改动的旧版 %s）"), imageskill.LegacyName)
		}
		return ""
	})
	if err := g.write(func() error { return installSkill(plans) }); err != nil {
		return nil, 1, err
	}
	fmt.Fprintln(stderr)
	printSkillDone(stdout, loc, plans)
	printSkillLegacyKept(stderr, loc.Home, plans)
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
