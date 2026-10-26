package cli

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter-setup/internal/codex"
	"github.com/zfg5118/deeprouter-setup/internal/gateway"
	"github.com/zfg5118/deeprouter-setup/internal/groups"
	"github.com/zfg5118/deeprouter-setup/internal/shellrc"
	"github.com/zfg5118/deeprouter-setup/internal/store"
	"github.com/zfg5118/deeprouter-setup/internal/textutil"
)

// cmdWizard is the no-argument setup flow used from a terminal.
func cmdWizard(stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	fmt.Fprintln(stderr, "Set up Claude Code and Codex. Press Enter to accept a default.")
	in := bufio.NewReader(stdin)
	key, err := wizardKey(e, in)
	if err != nil {
		return 2, err
	}

	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, "Official-key group:")
	for i, g := range groups.Official {
		fmt.Fprintf(stderr, "  %d) %-12s %s\n", i+1, g.Name, g.Label)
	}
	groupLine, err := readLine(in, stderr, "Group [officially]: ")
	if err != nil {
		return 2, err
	}
	group, err := chooseGroup(groupLine)
	if err != nil {
		return 2, err
	}

	baseLine, err := readLine(in, stderr, "Gateway ["+gateway.DefaultBaseURL+"]: ")
	if err != nil {
		return 2, err
	}
	baseURL := strings.TrimSpace(baseLine)
	if baseURL == "" {
		baseURL = gateway.DefaultBaseURL
	}
	if _, err := gateway.ParseBase(baseURL); err != nil {
		return 2, err
	}

	fmt.Fprintf(stderr, "Fetching models from %s ...\n", baseURL)
	ids, err := fetchModelIDs(baseURL, key, 30*time.Second)
	if err != nil {
		return 1, fmt.Errorf("could not list models: %s", err.Error())
	}
	fmt.Fprintf(stderr, "%d models.\n\n", len(ids))

	claude, err := promptModel(in, stderr, "Claude", "claude", ids)
	if err != nil {
		return 2, err
	}
	codex, err := promptModel(in, stderr, "Codex", "gpt", ids)
	if err != nil {
		return 2, err
	}

	prof := store.Profile{
		Name:        group.Name,
		Label:       group.Label,
		BaseURL:     baseURL,
		APIKey:      key,
		ClaudeModel: claude,
		CodexModel:  codex,
		WireAPI:     "responses",
	}
	report, err := e.saveProfile(prof, true, true, true, true, true)
	if err != nil {
		return 1, err
	}
	printSetup(e, prof, report, true, true, true, false)

	fmt.Fprintln(stdout)
	ep, probeReport, err := probeTarget(target{
		name:        prof.Name,
		label:       prof.Label,
		baseURL:     prof.BaseURL,
		key:         prof.APIKey,
		claudeModel: prof.ClaudeModel,
		codexModel:  prof.CodexModel,
	}, 30*time.Second)
	if err != nil {
		return 1, err
	}
	printReport(stdout, target{
		name:  prof.Name,
		label: prof.Label,
		key:   prof.APIKey,
	}, ep, probeReport)
	cliErr := installMissingCLIs(e, true, in, false)
	printNextSteps(e, true)
	keyErr := e.confirmCodexKey(e.layout.CodexPath, prof.APIKey)
	if !probeReport.OK || cliErr != nil || keyErr != nil {
		return 1, nil
	}
	return 0, nil
}

// wizardKey asks for an API key when stdin is a terminal.
// An existing key (environment or config already written) is the default:
// Enter keeps it, and a typed value replaces it. Input is masked.
// When stdin is not a terminal, the existing key is used with no prompt.
func wizardKey(e env, in *bufio.Reader) (string, error) {
	existing, fromEnv, err := detectKey(e)
	if err != nil {
		return "", err
	}
	if stdinIsTerminal(e.stdin) {
		line, err := promptSecretLine(e.stdin, e.stderr, in, keyPrompt(existing))
		if err != nil {
			return "", err
		}
		if line == "" {
			if existing == "" {
				return "", fmt.Errorf("API key is empty")
			}
			return existing, nil
		}
		return textutil.ValidateKey(line)
	}
	if existing != "" {
		if fromEnv {
			fmt.Fprintf(e.stderr, "Using DEEPROUTER_API_KEY (%s).\n", textutil.Mask(existing))
		} else {
			fmt.Fprintf(e.stderr, "Using saved API key (%s).\n", textutil.Mask(existing))
		}
		return existing, nil
	}
	return "", fmt.Errorf("API key required. Pass --api-key, set DEEPROUTER_API_KEY, or use --api-key-stdin")
}

// detectKey returns a key already available to this run.
// DEEPROUTER_API_KEY wins, because a sourced shell startup file puts it in
// the environment. An invalid environment value is an error. An invalid
// saved value is skipped so the prompt can ask for a new one.
func detectKey(e env) (key string, fromEnv bool, err error) {
	if raw := strings.TrimSpace(os.Getenv("DEEPROUTER_API_KEY")); raw != "" {
		key, err = textutil.ValidateKey(raw)
		if err != nil {
			return "", false, err
		}
		return key, true, nil
	}
	for _, candidate := range []string{
		activeProfileKey(e),
		readClaudeKey(e.layout.ClaudePath),
		readCodexKey(e.layout.CodexPath),
		readShellKey(e.layout.RCFile),
	} {
		key, err = textutil.ValidateKey(candidate)
		if err == nil {
			return key, false, nil
		}
	}
	return "", false, nil
}

func activeProfileKey(e env) string {
	if e.state.Active == "" {
		return ""
	}
	p, ok := e.state.Find(e.state.Active)
	if !ok {
		return ""
	}
	return p.APIKey
}

func readClaudeKey(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	var doc struct {
		Env map[string]any `json:"env"`
	}
	if err := json.Unmarshal(b, &doc); err != nil {
		return ""
	}
	v, ok := doc.Env["ANTHROPIC_AUTH_TOKEN"].(string)
	if !ok {
		return ""
	}
	return v
}

func readCodexKey(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return codex.ReadAuth(string(b)).BearerToken
}

func readShellKey(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	v, ok := shellrc.Lookup(string(b), "DEEPROUTER_API_KEY")
	if !ok {
		return ""
	}
	return v
}

func keyPrompt(existing string) string {
	if existing == "" {
		return "API key: "
	}
	return fmt.Sprintf("API key [%s]: ", textutil.Mask(existing))
}

func promptModel(in *bufio.Reader, stderr io.Writer, label, kind string, ids []string) (string, error) {
	matched := gateway.FilterIDs(ids, kind)
	def := gateway.PickDefault(ids, kind)
	if len(matched) == 0 {
		fmt.Fprintf(stderr, "No %s model id containing %q was listed.\n", label, kind)
		line, err := readLine(in, stderr, label+" model id: ")
		if err != nil {
			return "", err
		}
		if strings.TrimSpace(line) == "" {
			return "", fmt.Errorf("a %s model id is required", label)
		}
		return strings.TrimSpace(line), nil
	}
	fmt.Fprintf(stderr, "%s models (Enter for %s):\n", label, def)
	for i, id := range matched {
		fmt.Fprintf(stderr, "  %d) %s\n", i+1, id)
	}
	line, err := readLine(in, stderr, fmt.Sprintf("%s model [%s]: ", label, def))
	if err != nil {
		return "", err
	}
	return chooseModel(line, def, matched)
}

func chooseGroup(answer string) (groups.Group, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		answer = "officially"
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n < 1 || n > len(groups.Official) {
			return groups.Group{}, fmt.Errorf("group %d is out of range", n)
		}
		return groups.Official[n-1], nil
	}
	g, ok := groups.Lookup(answer)
	if !ok {
		return groups.Group{}, fmt.Errorf("unknown group %q", answer)
	}
	return g, nil
}

func chooseModel(answer, def string, ids []string) (string, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		if def == "" {
			return "", fmt.Errorf("model id is required")
		}
		return def, nil
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n < 1 || n > len(ids) {
			return "", fmt.Errorf("model %d is out of range", n)
		}
		return ids[n-1], nil
	}
	return answer, nil
}

func readLine(in *bufio.Reader, stderr io.Writer, label string) (string, error) {
	fmt.Fprint(stderr, label)
	s, err := in.ReadString('\n')
	s = strings.TrimSpace(s)
	if err != nil && err != io.EOF {
		return "", err
	}
	return s, nil
}

func printNextSteps(e env, shell bool) {
	fmt.Fprintln(e.stdout, "\nNext:")
	if shell {
		fmt.Fprintln(e.stdout, sourceHint(e.layout.Shell, e.show(e.layout.RCFile)))
	}
	fmt.Fprintln(e.stdout, "Then run:  claude")
	fmt.Fprintln(e.stdout, "Or run:    codex")
	fmt.Fprintln(e.stdout, "Quit and reopen Codex and Claude Desktop. They do not read the shell startup file.")
}
