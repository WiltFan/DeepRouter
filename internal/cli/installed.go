package cli

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"

	"github.com/zfg5118/deeprouter/internal/apps"
)

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
		fmt.Fprintf(w, "%s is not installed.\n", info.Title)
		if how != "" {
			fmt.Fprintf(w, "  Install: %s\n", how)
		}
		if docs != "" && docs != how {
			fmt.Fprintf(w, "  %s\n", docs)
		}
	}
}

func programPresent(id apps.ID, command string) bool {
	if id == apps.ClaudeDesktop {
		return claudeDesktopInstalled()
	}
	if command == "" {
		return false
	}
	path, err := cliLookPath(command)
	return err == nil && path != ""
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
