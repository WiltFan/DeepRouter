package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/zfg5118/deeprouter/internal/apps"
)

// programExtraDirs lists bin directories that npm and Homebrew use even when
// they are missing from PATH. Tests replace it.
var programExtraDirs = defaultExtraBinDirs

var desktopAppPaths = standardDesktopAppPaths

func noticePrograms(w io.Writer, ids []apps.ID) {
	for _, id := range ids {
		command, how, docs := apps.InstallHelp(id)
		if command != "" {
			if path, err := cliLookPath(command); err == nil && path != "" {
				fmt.Fprintln(w, command+ui(": found on PATH.", "：已在 PATH 中找到。"))
			} else if programPresent(id, command) {
				fmt.Fprintln(w, command+ui(": not found on PATH, but found in a standard bin directory.", "：不在 PATH 中，但在常用 bin 目录中找到。"))
			} else {
				fmt.Fprintln(w, command+ui(": not found on PATH or in standard bin directories.", "：未在 PATH 或常用 bin 目录中找到。"))
				printProgramInstallHelp(w, how, docs)
				if id == apps.Pi {
					fmt.Fprintln(w, ui("  The npm option requires Node.js 22.19+.", "  npm 安装方式需要 Node.js 22.19+。"))
				}
			}
		}
		switch id {
		case apps.ClaudeDesktop:
			desktopStatus(w, id, "Claude Desktop", "https://claude.com/download")
		case apps.Codex:
			desktopStatus(w, id, "ChatGPT Desktop", "https://chatgpt.com/download/")
			fmt.Fprintln(w, ui("  ChatGPT Desktop and Codex CLI share the Codex config.", "  ChatGPT 桌面版与 Codex CLI 共用 Codex 配置。"))
		case apps.OpenCode:
			desktopStatus(w, id, "OpenCode Desktop", "https://opencode.ai/docs/#desktop")
			fmt.Fprintln(w, ui("  OpenCode CLI and a local OpenCode Desktop server share the global config.", "  OpenCode CLI 与桌面版本地服务器共用全局配置。"))
		}
	}
}

func desktopStatus(w io.Writer, id apps.ID, name, docs string) {
	if desktopPresent(id) {
		fmt.Fprintln(w, name+ui(": desktop presence confirmed.", "：已确认桌面版存在。"))
	} else {
		fmt.Fprintln(w, name+ui(": desktop presence not confirmed.", "：未能确认桌面版是否存在。"))
		printProgramInstallHelp(w, nil, docs)
	}
}

func printProgramInstallHelp(w io.Writer, how []string, docs string) {
	for _, line := range how {
		fmt.Fprintln(w, "  "+ui("Install: ", "安装：")+line)
	}
	if docs != "" && !containsString(how, docs) {
		fmt.Fprintln(w, "  "+docs)
	}
}

func containsString(list []string, want string) bool {
	for _, item := range list {
		if item == want {
			return true
		}
	}
	return false
}

func programPresent(id apps.ID, command string) bool {
	if id == apps.ClaudeDesktop {
		return desktopPresent(apps.ClaudeDesktop)
	}
	if command == "" {
		return false
	}
	if path, err := cliLookPath(command); err == nil && path != "" {
		return true
	}
	_, full := standardBinPath(command)
	return full != ""
}

func standardBinPath(command string) (string, string) {
	for _, dir := range programExtraDirs() {
		for _, candidate := range namedCandidates(dir, command) {
			if isExecutable(candidate) {
				return dir, candidate
			}
		}
	}
	return "", ""
}

func defaultExtraBinDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	add := func(dir string) {
		if dir != "" && dir != "." && dir != string(filepath.Separator) {
			dirs = append(dirs, dir)
		}
	}
	if runtime.GOOS == "windows" {
		for _, entry := range []struct{ env, name string }{{"APPDATA", "npm"}, {"LOCALAPPDATA", "pnpm"}, {"ProgramFiles", "nodejs"}} {
			if parent := os.Getenv(entry.env); filepath.IsAbs(parent) {
				add(filepath.Join(parent, entry.name))
			}
		}
	} else {
		add("/opt/homebrew/bin")
		add("/usr/local/bin")
		if home != "" {
			add(filepath.Join(home, ".local", "bin"))
			add(filepath.Join(home, ".npm-global", "bin"))
			add(filepath.Join(home, "Library", "pnpm"))
			add(filepath.Join(home, ".local", "share", "pnpm"))
			add(filepath.Join(home, ".yarn", "bin"))
		}
	}
	if prefix := os.Getenv("NPM_CONFIG_PREFIX"); prefix != "" {
		add(filepath.Join(prefix, "bin"))
	}
	return dirs
}

// commandOffPath reports an npm-global binary that exists but is not on PATH.
// dir is the directory to add, and full is the executable.
func commandOffPath(command string) (dir, full string) {
	if path, err := cliLookPath(command); err == nil && path != "" {
		return "", ""
	}
	prefix, err := cachedNpmPrefix()
	if err != nil {
		return "", ""
	}
	prefix = strings.TrimSpace(prefix)
	if prefix == "" {
		return "", ""
	}
	dir = prefix
	if runtime.GOOS != "windows" {
		dir = filepath.Join(prefix, "bin")
	}
	for _, candidate := range namedCandidates(dir, command) {
		if isExecutable(candidate) {
			return dir, candidate
		}
	}
	return "", ""
}

// cliRunLine is the "then run" hint. When the command exists only in the npm
// global prefix, it explains how to put that directory on PATH.
func cliRunLine(or bool, command string) string {
	verbEN, verbZH := "Then run:  ", "然后运行："
	if or {
		verbEN, verbZH = "Or run:    ", "或者运行："
	}
	if path, err := cliLookPath(command); err == nil && path != "" {
		return ui(verbEN+command, verbZH+command)
	}
	dir, full := standardBinPath(command)
	if dir == "" {
		return ui(verbEN+command, verbZH+command)
	}
	return pathHint(runtime.GOOS, verbEN, verbZH, command, dir, full)
}

func pathHint(goos, verbEN, verbZH, command, dir, full string) string {
	if goos == "windows" {
		return ui(
			fmt.Sprintf("%s%s is not on PATH.\n  Add it for this window:  set PATH=%s;%%PATH%%\n  Or run:  %s", verbEN, command, dir, full),
			fmt.Sprintf("%s%s 不在 PATH 里。本窗口可以这样加上：set PATH=%s;%%PATH%%\n  或直接运行：%s", verbZH, command, dir, full),
		)
	}
	return ui(
		fmt.Sprintf("%s%s is not on PATH. Add its directory, or run it by full path.\n  export PATH=\"%s:$PATH\"\n  %s", verbEN, command, dir, full),
		fmt.Sprintf("%s%s 不在 PATH 里。把目录加进 PATH，或用完整路径运行。\n  export PATH=\"%s:$PATH\"\n  %s", verbZH, command, dir, full),
	)
}

func namedCandidates(dir, name string) []string {
	if runtime.GOOS != "windows" {
		return []string{filepath.Join(dir, name)}
	}
	return windowsCandidates(dir, name, os.Getenv("PATHEXT"))
}

func windowsCandidates(dir, name, extensions string) []string {
	if extensions == "" {
		extensions = ".com;.exe;.bat;.cmd"
	}
	paths := []string{filepath.Join(dir, name)}
	for _, extension := range strings.Split(strings.ToLower(extensions), ";") {
		if len(extension) > 1 && extension[0] == '.' && !strings.ContainsAny(extension, `/\:`) {
			paths = append(paths, filepath.Join(dir, name+extension))
		}
	}
	return paths
}

func isExecutable(path string) bool {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode()&0o111 != 0
}

func standardDesktopAppPaths(id apps.ID) []string {
	if runtime.GOOS == "darwin" && id == apps.ClaudeDesktop {
		return []string{"/Applications/Claude.app"}
	}
	return nil
}

func desktopPresent(id apps.ID) bool {
	for _, path := range desktopAppPaths(id) {
		info, err := os.Stat(path)
		if err == nil && info.IsDir() && strings.EqualFold(filepath.Ext(path), ".app") {
			return true
		}
	}
	return false
}
