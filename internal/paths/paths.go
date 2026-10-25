package paths

import (
	"path/filepath"
	"runtime"
	"strings"
)

// Layout is where deeprouter-setup reads and writes on one machine.
type Layout struct {
	Home       string
	ConfigDir  string
	ClaudePath string
	CodexPath  string
	RCFile     string
	Shell      string
}

// Resolve builds a layout from the home directory and an optional Codex home.
func Resolve(home, codexHome, shell, rcFile string) Layout {
	if shell == "" {
		shell = "bash"
	}
	rc := rcFile
	if rc == "" {
		rc = DefaultRC(home, shell)
	}
	codexDir := codexHome
	if codexDir == "" {
		codexDir = filepath.Join(home, ".codex")
	}
	return Layout{
		Home:       home,
		ConfigDir:  filepath.Join(home, ".deeprouter"),
		ClaudePath: filepath.Join(home, ".claude", "settings.json"),
		CodexPath:  filepath.Join(codexDir, "config.toml"),
		RCFile:     rc,
		Shell:      shell,
	}
}

// DetectShell maps a SHELL value to the syntax we write.
func DetectShell(shellEnv, goos string) string {
	base := shellEnv
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	base = strings.TrimSuffix(strings.ToLower(base), ".exe")
	switch base {
	case "zsh", "bash", "fish":
		return base
	case "pwsh", "powershell":
		return "powershell"
	default:
		if goos == "windows" {
			return "powershell"
		}
		return "bash"
	}
}

// DefaultRC is the startup file for a shell kind.
func DefaultRC(home, shell string) string {
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config", "fish", "config.fish")
	case "powershell":
		if runtime.GOOS == "windows" {
			return filepath.Join(home, "Documents", "PowerShell", "Microsoft.PowerShell_profile.ps1")
		}
		return filepath.Join(home, ".config", "powershell", "Microsoft.PowerShell_profile.ps1")
	default:
		return filepath.Join(home, ".bashrc")
	}
}
