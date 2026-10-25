package cli

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter-setup/internal/codex"
)

const (
	claudePackage = "@anthropic-ai/claude-code"
	codexPackage  = "@openai/codex"
	claudeDocs    = "https://code.claude.com/docs/en/setup"
	codexDocs     = "https://developers.openai.com/codex/cli"
)

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
		return "unknown"
	}
	return v
}

// installMissingCLIs installs claude and codex when they are absent.
// interactive prompts on stderr (default answer is no). force installs
// without a prompt, which is the --install-clis path. A declined prompt
// installs nothing.
func installMissingCLIs(e env, interactive bool, in *bufio.Reader, force bool) error {
	claude, codexCLI := detectCLIs()
	missing := missingTools(claude, codexCLI)
	if len(missing) == 0 {
		if force {
			fmt.Fprintf(e.stdout, "claude %s and codex %s are already installed.\n", versionOrUnknown(claude.Version), versionOrUnknown(codexCLI.Version))
		}
		return nil
	}
	if interactive {
		for _, tool := range []toolCLI{claude, codexCLI} {
			if tool.installed() {
				fmt.Fprintf(e.stderr, "%s %s is installed (%s).\n", tool.Name, versionOrUnknown(tool.Version), tool.Path)
				continue
			}
			fmt.Fprintf(e.stderr, "%s is not installed.\n", tool.Name)
		}
		answer, err := readLine(in, e.stderr, "Install missing CLIs with npm? [y/N] ")
		if err != nil {
			return err
		}
		if !affirmed(answer) {
			return nil
		}
	} else if !force {
		return nil
	}
	if _, err := cliLookPath("npm"); err != nil {
		fmt.Fprintln(e.stdout, "npm is not available, so the CLIs were not installed.")
		printCLIInstallInstructions(e.stdout, missing)
		return fmt.Errorf("npm is not available")
	}
	pkgs := make([]string, len(missing))
	for i, tool := range missing {
		pkgs[i] = tool.Package
	}
	args := append([]string{"install", "-g"}, pkgs...)
	fmt.Fprintf(e.stdout, "Running: npm %s\n", strings.Join(args, " "))
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	if err := cliExec(ctx, e.stdout, e.stderr, "npm", args...); err != nil {
		fmt.Fprintf(e.stderr, "npm install failed: %s\n", err.Error())
		printCLIInstallInstructions(e.stdout, missing)
		return fmt.Errorf("npm install failed")
	}
	return nil
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

func affirmed(answer string) bool {
	switch strings.ToLower(strings.TrimSpace(answer)) {
	case "y", "yes":
		return true
	default:
		return false
	}
}

func printCLIInstallInstructions(w io.Writer, missing []toolCLI) {
	fmt.Fprintln(w, "Install instructions:")
	for _, tool := range missing {
		fmt.Fprintf(w, "  %s: npm install -g %s\n", tool.Name, tool.Package)
		fmt.Fprintf(w, "  %s\n", tool.Docs)
	}
}

func printCLIStatus(w io.Writer) {
	claude, codexCLI := detectCLIs()
	for _, tool := range []toolCLI{claude, codexCLI} {
		if !tool.installed() {
			fmt.Fprintf(w, "%-12s not installed\n", tool.Name)
			continue
		}
		fmt.Fprintf(w, "%-12s %s\n", tool.Name, tool.Path)
		fmt.Fprintf(w, "  version    %s\n", versionOrUnknown(tool.Version))
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
	switch {
	case auth.EnvKey != "":
		fmt.Fprintf(e.stdout, "Codex key    %s still sets env_key = %q, so Codex requires that variable\n", shown, auth.EnvKey)
		return fmt.Errorf("codex config still requires %s", auth.EnvKey)
	case !auth.HasBearer || auth.BearerToken == "":
		fmt.Fprintf(e.stdout, "Codex key    missing from %s\n", shown)
		return fmt.Errorf("codex config has no bearer token")
	case auth.BearerToken != want:
		fmt.Fprintf(e.stdout, "Codex key    in %s does not match this profile\n", shown)
		return fmt.Errorf("codex bearer token does not match the profile")
	}
	info, err := os.Stat(path)
	if err != nil {
		fmt.Fprintf(e.stdout, "Codex key    could not read %s\n", shown)
		return err
	}
	if info.Mode().Perm()&0o077 != 0 {
		fmt.Fprintf(e.stdout, "Codex key    present in %s with mode %04o\n", shown, info.Mode().Perm())
		return fmt.Errorf("codex config mode is %04o", info.Mode().Perm())
	}
	if !codex.KeyReady(text, want) {
		fmt.Fprintf(e.stdout, "Codex key    %s does not resolve the profile key\n", shown)
		return fmt.Errorf("codex config does not resolve the key")
	}
	fmt.Fprintf(e.stdout, "Codex key    ready in %s\n", shown)
	return nil
}
