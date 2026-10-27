package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/codex"
)

const (
	claudePackage     = "@anthropic-ai/claude-code"
	codexPackage      = "@openai/codex"
	geminiPackage     = "@google/gemini-cli"
	claudeDocs        = "https://code.claude.com/docs/en/setup"
	codexDocs         = "https://developers.openai.com/codex/cli"
	geminiDocs        = "https://geminicli.com/docs/get-started/installation/"
	npmMirrorRegistry = "https://registry.npmmirror.com"
)

// npmRegistryUnreachable reports whether a short TCP dial to the public npm
// registry failed. Tests replace it. A China locale does not need the dial.
var npmRegistryUnreachable = func() bool {
	dialer := net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.Dial("tcp", "registry.npmjs.org:443")
	if err != nil {
		return true
	}
	_ = conn.Close()
	return false
}

// npmCLIOrder is the install order for CLIs that ship as npm packages.
// Grok Build and Claude Desktop are not in this list.
var npmCLIOrder = []apps.ID{apps.ClaudeCode, apps.Codex, apps.Gemini}

// cliLookPath and cliExec are replaced in tests.
var cliLookPath = exec.LookPath

var cliExec = func(ctx context.Context, stdout, stderr io.Writer, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr
	return cmd.Run()
}

type toolCLI struct {
	Name    string
	Package string
	Docs    string
	Path    string
	Version string
}

func (t toolCLI) installed() bool {
	return t.Path != ""
}

func detectCLIs() (claude, codexCLI toolCLI) {
	claude = inspectCLI("claude", claudePackage, claudeDocs)
	codexCLI = inspectCLI("codex", codexPackage, codexDocs)
	return claude, codexCLI
}

func inspectCLI(name, pkg, docs string) toolCLI {
	info := toolCLI{Name: name, Package: pkg, Docs: docs}
	path, err := cliLookPath(name)
	if err != nil || path == "" {
		return info
	}
	info.Path = path
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var buf bytes.Buffer
	_ = cliExec(ctx, &buf, &buf, name, "--version")
	info.Version = firstLine(buf.String())
	return info
}

func firstLine(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

func versionOrUnknown(v string) string {
	if strings.TrimSpace(v) == "" {
		return ui("unknown", "未知版本")
	}
	return v
}

// installMissingCLIs reports CLIs for the selected programs and installs the
// npm ones that are absent. interactive prompts on stderr (default answer
// is no). force installs without a prompt, which is the --install-clis path.
// A declined prompt installs nothing. Claude Desktop has no CLI. The ChatGPT
// desktop app shares the Codex config, so a missing codex CLI says that N is
// fine. Grok Build is not an npm package; its official install command is
// printed instead of being added to the prompt.
func installMissingCLIs(e env, interactive bool, in *bufio.Reader, force bool, ids []apps.ID) error {
	if !interactive && !force {
		return nil
	}
	selected := map[apps.ID]bool{}
	for _, id := range ids {
		selected[id] = true
	}
	if interactive {
		for _, id := range []apps.ID{apps.Codex, apps.ClaudeCode, apps.Grok, apps.Gemini} {
			if !selected[id] {
				continue
			}
			tool := npmOrHint(id)
			if tool.command == "" {
				continue
			}
			info := inspectCLI(tool.command, tool.pkg, tool.docs)
			if info.installed() {
				fmt.Fprintln(e.stderr, ui(
					fmt.Sprintf("%s %s is installed (%s).", info.Name, versionOrUnknown(info.Version), info.Path),
					fmt.Sprintf("已安装 %s %s（%s）。", info.Name, versionOrUnknown(info.Version), info.Path),
				))
				continue
			}
			if id == apps.Codex {
				fmt.Fprintln(e.stderr, codexMissingNote())
				continue
			}
			fmt.Fprintln(e.stderr, notInstalledLine(info.Name))
			if tool.pkg == "" {
				printInstallHint(e.stderr, id)
			}
		}
	} else if selected[apps.Grok] {
		info := inspectCLI("grok", "", "")
		if !info.installed() {
			fmt.Fprintln(e.stderr, notInstalledLine("grok"))
			printInstallHint(e.stderr, apps.Grok)
		}
	}
	var missing []toolCLI
	for _, id := range npmCLIOrder {
		if !selected[id] {
			continue
		}
		tool := npmOrHint(id)
		if tool.pkg == "" {
			continue
		}
		info := inspectCLI(tool.command, tool.pkg, tool.docs)
		if !info.installed() {
			missing = append(missing, info)
		}
	}
	if len(missing) == 0 {
		if force && selected[apps.ClaudeCode] && selected[apps.Codex] {
			claude, codexCLI := detectCLIs()
			if claude.installed() && codexCLI.installed() {
				fmt.Fprintln(e.stdout, ui(
					fmt.Sprintf("claude %s and codex %s are already installed.", versionOrUnknown(claude.Version), versionOrUnknown(codexCLI.Version)),
					fmt.Sprintf("claude %s 和 codex %s 都已安装。", versionOrUnknown(claude.Version), versionOrUnknown(codexCLI.Version)),
				))
			}
		}
		return nil
	}
	useMirror := false
	if interactive {
		china := chinaLocale()
		zh := zhUI()
		answer, err := readLine(in, e.stderr, npmInstallPrompt(china, zh))
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
		accept := affirmed(answer)
		if china {
			accept = defaultYes(answer)
		}
		if !accept {
			return nil
		}
		if suggestNPMMirror() {
			if china {
				fmt.Fprintln(e.stderr, npmMirrorHint(zh))
				fmt.Fprintln(e.stderr, "  npm config set registry https://registry.npmmirror.com")
			} else {
				fmt.Fprintln(e.stderr, ui(
					"npmjs.org may be slow from mainland China. To use the mirror for later installs, run this yourself:",
					"中国大陆访问 npmjs.org 可能较慢。若希望以后也用镜像，请自己执行：",
				))
				fmt.Fprintln(e.stderr, "  npm config set registry https://registry.npmmirror.com")
			}
			mirrorAnswer, err := readLine(in, e.stderr, npmMirrorPrompt(china, zh))
			if errors.Is(err, io.EOF) {
				return nil
			}
			if err != nil {
				return err
			}
			if china {
				useMirror = defaultYes(mirrorAnswer)
			} else {
				useMirror = affirmed(mirrorAnswer)
			}
		}
	}
	if _, err := cliLookPath("npm"); err != nil {
		fmt.Fprintln(e.stdout, ui("npm is not available, so the CLIs were not installed.", "没有 npm，所以没有安装这些命令行工具。"))
		printCLIInstallInstructions(e.stdout, missing)
		return fmt.Errorf("npm is not available")
	}
	pkgs := make([]string, len(missing))
	for i, tool := range missing {
		pkgs[i] = tool.Package
	}
	args := []string{"install", "-g"}
	if useMirror {
		args = append(args, "--registry", npmMirrorRegistry)
	}
	args = append(args, pkgs...)
	fmt.Fprintln(e.stdout, ui("Running: ", "正在运行：")+"npm "+strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := cliExec(ctx, e.stdout, e.stderr, "npm", args...); err != nil {
		fmt.Fprintln(e.stderr, ui("npm install failed: ", "npm 安装失败：")+err.Error())
		printCLIInstallInstructions(e.stdout, missing)
		return fmt.Errorf("npm install failed")
	}
	return nil
}

type npmTool struct {
	command string
	pkg     string
	docs    string
}

func npmOrHint(id apps.ID) npmTool {
	switch id {
	case apps.ClaudeCode:
		return npmTool{command: "claude", pkg: claudePackage, docs: claudeDocs}
	case apps.Codex:
		return npmTool{command: "codex", pkg: codexPackage, docs: codexDocs}
	case apps.Gemini:
		return npmTool{command: "gemini", pkg: geminiPackage, docs: geminiDocs}
	case apps.Grok:
		return npmTool{command: "grok"}
	default:
		return npmTool{}
	}
}

func printInstallHint(w io.Writer, id apps.ID) {
	_, how, docs := apps.InstallHelp(id)
	for _, line := range how {
		if line == "" {
			continue
		}
		fmt.Fprintln(w, "  "+ui("Install: ", "安装：")+line)
	}
	if docs != "" && !containsString(how, docs) {
		fmt.Fprintf(w, "  %s\n", docs)
	}
}

func missingTools(tools ...toolCLI) []toolCLI {
	var missing []toolCLI
	for _, tool := range tools {
		if !tool.installed() {
			missing = append(missing, tool)
		}
	}
	return missing
}

// suggestNPMMirror is true when the locale or time zone looks like mainland
// China, or when the public npm registry does not answer a short dial.
// The installer never runs `npm config set`; the caller may pass --registry
// for this install only.
func suggestNPMMirror() bool {
	if chinaLocale() {
		return true
	}
	return npmRegistryUnreachable()
}

func chinaLocale() bool {
	switch os.Getenv("TZ") {
	case "Asia/Shanghai", "Asia/Chongqing", "Asia/Harbin", "Asia/Urumqi", "Asia/Kashgar", "PRC":
		return true
	}
	for _, key := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		v := os.Getenv(key)
		if strings.HasPrefix(v, "zh_CN") || strings.HasPrefix(v, "zh-CN") {
			return true
		}
	}
	if runtime.GOOS == "windows" && windowsZoneSuggestsChina() {
		return true
	}
	return false
}

func windowsZoneSuggestsChina() bool {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "tzutil", "/g").Output()
	if err != nil {
		return false
	}
	name := strings.ToLower(string(out))
	return strings.Contains(name, "shanghai") || strings.Contains(name, "china")
}

func affirmed(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

// defaultYes accepts Enter, y, and yes. n and anything else decline.
func defaultYes(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "", "y", "yes":
		return true
	default:
		return false
	}
}

func npmMirrorHint(zh bool) string {
	if zh {
		return "中国大陆访问 npmjs.org 往往较慢。下面的问题只影响这一次安装，不会修改已保存的 npm 配置。若希望以后也用镜像，请自己执行："
	}
	return "npmjs.org is often slow in mainland China. The next question applies only to this install and does not change your saved npm config. To use the mirror for later installs, run this yourself:"
}

func npmMirrorPrompt(china, zh bool) string {
	mark := "[y/N] "
	if china {
		mark = "[Y/n] "
	}
	if zh {
		return "这一次安装使用 https://registry.npmmirror.com ？" + mark
	}
	return "Use https://registry.npmmirror.com for this install only? " + mark
}

func npmInstallPrompt(china, zh bool) string {
	mark := "[y/N] "
	if china {
		mark = "[Y/n] "
	}
	if zh {
		return "用 npm 安装缺少的命令行工具？" + mark
	}
	return "Install missing CLIs with npm? " + mark
}

func printCLIInstallInstructions(w io.Writer, missing []toolCLI) {
	fmt.Fprintln(w, ui("Install instructions:", "安装说明："))
	for _, tool := range missing {
		fmt.Fprintf(w, "  %s: npm install -g %s\n", tool.Name, tool.Package)
		fmt.Fprintf(w, "  %s\n", tool.Docs)
	}
}

func printCLIStatus(w io.Writer) {
	claude, codexCLI := detectCLIs()
	for _, tool := range []toolCLI{claude, codexCLI} {
		if !tool.installed() {
			fmt.Fprintf(w, "%-12s %s\n", tool.Name, ui("not installed", "尚未安装"))
			continue
		}
		fmt.Fprintf(w, "%-12s %s\n", tool.Name, tool.Path)
		fmt.Fprintln(w, "  "+pad(ui("version", "版本"), 11)+versionOrUnknown(tool.Version))
	}
}

func cliStatusPayload() map[string]any {
	claude, codexCLI := detectCLIs()
	return map[string]any{
		"claude": cliPayload(claude),
		"codex":  cliPayload(codexCLI),
	}
}

func cliPayload(tool toolCLI) map[string]any {
	return map[string]any{
		"installed": tool.installed(),
		"path":      tool.Path,
		"version":   tool.Version,
	}
}

func (e env) confirmCodexKey(path, want string) error {
	text := readText(path)
	shown := e.show(path)
	auth := codex.ReadAuth(text)
	say := func(en, zh string) {
		fmt.Fprintln(e.stdout, ui("Codex key    "+en, pad("Codex 令牌", 13)+zh))
	}
	switch {
	case auth.EnvKey != "":
		say(fmt.Sprintf("%s still sets env_key = %q, so Codex requires that variable", shown, auth.EnvKey),
			fmt.Sprintf("%s 仍设置了 env_key = %q，Codex 会要求这个环境变量", shown, auth.EnvKey))
		return fmt.Errorf("codex config still requires %s", auth.EnvKey)
	case !auth.HasBearer || auth.BearerToken == "":
		say("missing from "+shown, shown+" 里没有令牌")
		return fmt.Errorf("codex config has no bearer token")
	case auth.BearerToken != want:
		say("in "+shown+" does not match this profile", shown+" 里的令牌和当前配置不一致")
		return fmt.Errorf("codex bearer token does not match the profile")
	}
	info, err := os.Stat(path)
	if err != nil {
		say("could not read "+shown, "无法读取 "+shown)
		return err
	}
	if runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		say(fmt.Sprintf("present in %s with mode %04o", shown, info.Mode().Perm()),
			fmt.Sprintf("已写入 %s，但文件权限是 %04o，其他用户也能读取", shown, info.Mode().Perm()))
		return fmt.Errorf("codex config mode is %04o", info.Mode().Perm())
	}
	if !codex.KeyReady(text, want) {
		say(shown+" does not resolve the profile key", shown+" 没有用上当前配置的令牌")
		return fmt.Errorf("codex config does not resolve the key")
	}
	say("ready in "+shown, "已写入 "+shown)
	return nil
}
