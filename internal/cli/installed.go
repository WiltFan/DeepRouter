package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zfg5118/deeprouter/internal/apps"
)

// programExtraDirs lists bin directories that npm and Homebrew use even when
// they are missing from PATH. Tests replace it.
var programExtraDirs = defaultExtraBinDirs

// notInstalledLine says the configuration is still written. The sentence is
// intentionally not an error: the setup succeeds, and the program works
// after it is installed.
func notInstalledLine(name string) string {
	if promptLocaleZH() {
		return name + " 尚未安装。配置仍会写入，安装之后即可使用。"
	}
	return name + " is not installed yet. Its configuration is written anyway and works after you install it."
}

func codexMissingNote() string {
	if promptLocaleZH() {
		return "codex 尚未安装。配置仍会写入，安装之后即可使用。ChatGPT 桌面版共用这份配置，如果只用桌面版，不安装命令行也可以。"
	}
	return "codex is not installed yet. Its configuration is written anyway and works after you install it. The ChatGPT desktop app uses the same config, so N is fine if you only use the desktop app."
}

// noticePrograms prints a line for each selected program that is not installed,
// including the official install command and docs. Configuration is still written.
func noticePrograms(w io.Writer, ids []apps.ID) {
	for _, id := range ids {
		info, ok := apps.ByID(id)
		if !ok {
			continue
		}
		command, how, docs := apps.InstallHelp(id)
		if programPresent(id, command) {
			continue
		}
		fmt.Fprintln(w, notInstalledLine(info.Title))
		for _, line := range how {
			if line == "" {
				continue
			}
			fmt.Fprintf(w, "  %s %s\n", ui("Install:", "安装："), line)
		}
		if docs != "" && !containsString(how, docs) {
			fmt.Fprintf(w, "  %s\n", docs)
		}
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
		return claudeDesktopInstalled()
	}
	if command == "" {
		return false
	}
	if path, err := cliLookPath(command); err == nil && path != "" {
		return true
	}
	for _, dir := range programExtraDirs() {
		for _, candidate := range namedCandidates(dir, command) {
			if isExecutable(candidate) {
				return true
			}
		}
	}
	return false
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
		add(filepath.Join(os.Getenv("APPDATA"), "npm"))
		add(filepath.Join(os.Getenv("LOCALAPPDATA"), "pnpm"))
		add(filepath.Join(os.Getenv("ProgramFiles"), "nodejs"))
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

func namedCandidates(dir, name string) []string {
	if runtime.GOOS == "windows" {
		return []string{
			filepath.Join(dir, name+".exe"),
			filepath.Join(dir, name+".cmd"),
			filepath.Join(dir, name+".bat"),
			filepath.Join(dir, name),
		}
	}
	return []string{filepath.Join(dir, name)}
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

func claudeDesktopInstalled() bool {
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/Applications/Claude.app"}
	case "windows":
		local := os.Getenv("LOCALAPPDATA")
		roaming := os.Getenv("APPDATA")
		candidates = []string{
			filepath.Join(local, "AnthropicClaude", "claude.exe"),
			filepath.Join(local, "Programs", "Claude", "Claude.exe"),
			filepath.Join(roaming, "Claude", "claude.exe"),
		}
	default:
		if path, err := cliLookPath("claude-desktop"); err == nil && path != "" {
			return true
		}
		home, _ := os.UserHomeDir()
		candidates = []string{
			"/usr/bin/claude-desktop",
			filepath.Join(home, ".local", "bin", "claude-desktop"),
		}
	}
	for _, path := range candidates {
		if path == "" || path == string(filepath.Separator) {
			continue
		}
		info, err := os.Stat(path)
		if err == nil && (info.IsDir() || info.Mode().IsRegular()) {
			return true
		}
	}
	return false
}
