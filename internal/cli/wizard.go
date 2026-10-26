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

	"github.com/zfg5118/deeprouter-setup/internal/apply"
	"github.com/zfg5118/deeprouter-setup/internal/apps"
	"github.com/zfg5118/deeprouter-setup/internal/claudedesktop"
	"github.com/zfg5118/deeprouter-setup/internal/codex"
	"github.com/zfg5118/deeprouter-setup/internal/gateway"
	"github.com/zfg5118/deeprouter-setup/internal/grok"
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
	in := bufio.NewReader(stdin)
	terminal := stdinIsTerminal(e.stdin)
	var selection []apps.ID
	if terminal {
		fmt.Fprintln(stderr, "Choose which programs to configure. Press Enter for the default.")
		selection, err = promptPrograms(in, stderr)
		if err != nil {
			return 2, err
		}
		if apps.Contains(selection, apps.Gemini) {
			writable := apps.WritableOnly(selection)
			if len(writable) == 0 {
				return 2, fmt.Errorf("%s", apps.GeminiReason)
			}
			fmt.Fprintln(stderr, apps.GeminiReason)
			selection = writable
		}
	} else {
		fmt.Fprintln(stderr, "Set up Claude Code and Codex. Press Enter to accept a default.")
		selection = apps.DefaultIDs()
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

	type chosen struct {
		id    apps.ID
		key   string
		model string
	}
	var choices []chosen
	if !terminal {
		key, err := wizardKey(e, in)
		if err != nil {
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
		codexModel, err := promptModel(in, stderr, "Codex", "gpt", ids)
		if err != nil {
			return 2, err
		}
		for _, id := range selection {
			model := claude
			if id == apps.Codex {
				model = codexModel
			}
			choices = append(choices, chosen{id: id, key: key, model: model})
		}
	} else {
		var cachedKey string
		var cachedIDs []string
		var haveIDs bool
		for _, id := range selection {
			info, _ := apps.ByID(id)
			existing := existingAppKey(e, id)
			line, err := promptSecretLine(e.stdin, e.stderr, in, appKeyPrompt(info.Title, existing))
			if err != nil {
				return 2, err
			}
			var key string
			if line == "" {
				if existing == "" {
					return 2, fmt.Errorf("API key is empty")
				}
				key = existing
			} else {
				key, err = textutil.ValidateKey(line)
				if err != nil {
					return 2, err
				}
			}
			if !haveIDs || key != cachedKey {
				fmt.Fprintf(stderr, "Fetching models from %s ...\n", baseURL)
				cachedIDs, err = fetchModelIDs(baseURL, key, 30*time.Second)
				if err != nil {
					return 1, fmt.Errorf("could not list models: %s", err.Error())
				}
				fmt.Fprintf(stderr, "%d models.\n\n", len(cachedIDs))
				cachedKey = key
				haveIDs = true
			}
			model, err := promptModel(in, stderr, info.Title, info.ModelKind, cachedIDs)
			if err != nil {
				return 2, err
			}
			choices = append(choices, chosen{id: id, key: key, model: model})
		}
	}
	if len(choices) == 0 {
		return 2, fmt.Errorf("no programs selected")
	}

	prof := store.Profile{
		Name:    group.Name,
		Label:   group.Label,
		BaseURL: baseURL,
		APIKey:  choices[0].key,
		WireAPI: "responses",
	}
	req := apply.Request{AppsExplicit: terminal, TouchShell: !terminal}
	for _, choice := range choices {
		switch choice.id {
		case apps.ClaudeCode:
			req.TouchClaude = true
			req.SetClaudeModel = choice.model != ""
			prof.ClaudeModel = choice.model
			req.ClaudeKey = choice.key
			if choice.key != prof.APIKey {
				prof.ClaudeKey = choice.key
			}
		case apps.ClaudeDesktop:
			req.TouchDesktop = true
			req.SetDesktopModel = choice.model != ""
			prof.DesktopModel = choice.model
			req.DesktopKey = choice.key
			if choice.key != prof.APIKey {
				prof.DesktopKey = choice.key
			}
		case apps.Codex:
			req.TouchCodex = true
			req.SetCodexModel = choice.model != ""
			prof.CodexModel = choice.model
			req.CodexKey = choice.key
			if choice.key != prof.APIKey {
				prof.CodexKey = choice.key
			}
		case apps.Grok:
			req.TouchGrok = true
			req.SetGrokModel = choice.model != ""
			prof.GrokModel = choice.model
			req.GrokKey = choice.key
			if choice.key != prof.APIKey {
				prof.GrokKey = choice.key
			}
		}
	}
	report, err := e.saveProfile(prof, req)
	if err != nil {
		return 1, err
	}
	printSetup(e, prof, report, req.TouchClaude, req.TouchCodex, req.TouchShell, false)

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
	printNextSteps(e, req)
	var keyErr error
	if req.TouchCodex {
		keyErr = e.confirmCodexKey(e.layout.CodexPath, prof.KeyFor(string(apps.Codex)))
	}
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

func promptPrograms(in *bufio.Reader, stderr io.Writer) ([]apps.ID, error) {
	for i, info := range apps.Catalog() {
		fmt.Fprintf(stderr, "  %d) %-16s %s\n", i+1, info.Title, info.Note)
	}
	line, err := readLine(in, stderr, "Programs [1,2,3]: ")
	if err != nil {
		return nil, err
	}
	return apps.ParseSelection(line, true)
}

func appKeyPrompt(title, existing string) string {
	if existing == "" {
		return title + " API key: "
	}
	return fmt.Sprintf("%s API key [%s]: ", title, textutil.Mask(existing))
}

func existingAppKey(e env, id apps.ID) string {
	var fromFile string
	switch id {
	case apps.ClaudeCode:
		fromFile = readClaudeKey(e.layout.ClaudePath)
	case apps.ClaudeDesktop:
		fromFile = claudedesktop.ReadAPIKey(e.layout.DesktopLibrary)
	case apps.Codex:
		fromFile = readCodexKey(e.layout.CodexPath)
	case apps.Grok:
		fromFile = grok.ReadAPIKey(readText(e.layout.GrokPath))
	}
	for _, candidate := range []string{fromFile, e.profileKey(string(id)), readShellKey(e.layout.RCFile), os.Getenv("DEEPROUTER_API_KEY")} {
		key, err := textutil.ValidateKey(candidate)
		if err == nil {
			return key
		}
	}
	return ""
}

func (e env) profileKey(app string) string {
	if e.state.Active == "" {
		return ""
	}
	p, ok := e.state.Find(e.state.Active)
	if !ok {
		return ""
	}
	return p.KeyFor(app)
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

func printNextSteps(e env, req apply.Request) {
	fmt.Fprintln(e.stdout, "\nNext:")
	if req.TouchShell {
		fmt.Fprintln(e.stdout, sourceHint(e.layout.Shell, e.show(e.layout.RCFile)))
	}
	if req.TouchClaude {
		fmt.Fprintln(e.stdout, "Then run:  claude")
	}
	if req.TouchCodex {
		if req.TouchClaude {
			fmt.Fprintln(e.stdout, "Or run:    codex")
		} else {
			fmt.Fprintln(e.stdout, "Then run:  codex")
		}
	}
	if req.TouchGrok {
		fmt.Fprintln(e.stdout, "Then run:  grok")
	}
	if req.TouchClaude || req.TouchCodex || req.TouchDesktop {
		fmt.Fprintln(e.stdout, "Quit and reopen Codex and Claude Desktop. They do not read the shell startup file.")
	}
	if req.TouchGrok {
		fmt.Fprintln(e.stdout, "Start a new Grok Build session so it reads config.toml.")
	}
}
