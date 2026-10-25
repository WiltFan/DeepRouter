// Package cli is the deeprouter-setup command line.
package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter-setup/internal/apply"
	"github.com/zfg5118/deeprouter-setup/internal/backup"
	"github.com/zfg5118/deeprouter-setup/internal/gateway"
	"github.com/zfg5118/deeprouter-setup/internal/groups"
	"github.com/zfg5118/deeprouter-setup/internal/paths"
	"github.com/zfg5118/deeprouter-setup/internal/shellrc"
	"github.com/zfg5118/deeprouter-setup/internal/store"
	"github.com/zfg5118/deeprouter-setup/internal/textutil"
)

// Main runs the CLI and returns an exit code.
func Main(args []string, version string) int {
	return mainIO(args, version, os.Stdout, os.Stderr, os.Stdin)
}

func mainIO(args []string, version string, stdout, stderr io.Writer, stdin io.Reader) int {
	code, err := dispatch(args, version, stdout, stderr, stdin)
	if err != nil {
		if code == 0 {
			code = 1
		}
		fmt.Fprintf(stderr, "deeprouter-setup: %s\n", err.Error())
	}
	return code
}

func dispatch(args []string, version string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	if len(args) == 0 {
		if isTerminal(stdin) {
			return cmdWizard(stdout, stderr, stdin)
		}
		fmt.Fprint(stdout, rootHelp)
		return 0, nil
	}
	if args[0] == "help" || args[0] == "--help" || args[0] == "-h" {
		fmt.Fprint(stdout, rootHelp)
		return 0, nil
	}
	if args[0] == "version" || args[0] == "--version" || args[0] == "-v" {
		fmt.Fprintf(stdout, "deeprouter-setup %s\n", version)
		return 0, nil
	}
	switch args[0] {
	case "setup":
		return cmdSetup(args[1:], stdout, stderr, stdin)
	case "test":
		return cmdTest(args[1:], stdout, stderr, stdin)
	case "models":
		return cmdModels(args[1:], stdout, stderr, stdin)
	case "switch":
		return cmdSwitch(args[1:], stdout, stderr, stdin)
	case "profile":
		return cmdProfile(args[1:], stdout, stderr, stdin)
	case "groups":
		return cmdGroups(stdout)
	case "status":
		return cmdStatus(args[1:], stdout, stderr)
	case "restore":
		return cmdRestore(args[1:], stdout, stderr)
	case "uninstall":
		return cmdUninstall(args[1:], stdout, stderr)
	default:
		fmt.Fprint(stderr, rootHelp)
		return 2, fmt.Errorf("unknown command %q", args[0])
	}
}

const rootHelp = `deeprouter-setup configures Claude Code and Codex CLI to use an
OpenAI- and Anthropic-compatible API gateway.

Usage:
  deeprouter-setup                  Interactive setup
  deeprouter-setup <command> [flags]

With no arguments, in a terminal, deeprouter-setup asks for an API key,
a group, and models, writes the config, and runs the connectivity test.

Commands:
  setup       Write Claude Code, Codex, and shell configuration
  test        Check connectivity, HTTP status, and latency
  models      List models from the gateway
  switch      Change the active profile (group) or model
  profile     Manage named profiles, one API key per group
  groups      Show recommended official-key groups
  status      Show the active configuration
  restore     Put config files back from a backup
  uninstall   Restore config files and optionally delete local state
  version     Print the version
  help        Show this help

The default gateway is https://deeprouter.top. Any base URL that speaks
OpenAI /v1 and Anthropic /v1/messages works. Pass --base-url to use another.

A group is selected by the API key on a profile. Recommended official-key
groups: OpenAI, Azure, AWS, Vertex, Officially.

Run deeprouter-setup <command> --help for flags.
`

type env struct {
	stdout io.Writer
	stderr io.Writer
	stdin  io.Reader
	home   string
	layout paths.Layout
	stateP string
	state  store.State
}

func loadEnv(stderr io.Writer, stdin io.Reader, stdout io.Writer) (env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return env{}, fmt.Errorf("cannot find the home directory: %w", err)
	}
	shell := paths.DetectShell(os.Getenv("SHELL"), runtime.GOOS)
	layout := paths.Resolve(home, os.Getenv("CODEX_HOME"), shell, "")
	statePath := store.Path(layout.ConfigDir)
	st, err := store.Load(statePath)
	if err != nil {
		return env{}, err
	}
	return env{
		stdout: stdout,
		stderr: stderr,
		stdin:  stdin,
		home:   home,
		layout: layout,
		stateP: statePath,
		state:  st,
	}, nil
}

func (e env) backups() backup.Store {
	return backup.Store{Dir: filepath.Join(e.layout.ConfigDir, "backups")}
}

func (e env) show(path string) string {
	return textutil.DisplayPath(e.home, path)
}

func cmdSetup(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, setupHelp) }
	base := fs.String("base-url", gateway.DefaultBaseURL, "gateway base URL")
	apiKey := fs.String("api-key", "", "API key (prefer DEEPROUTER_API_KEY or --api-key-stdin)")
	keyStdin := fs.Bool("api-key-stdin", false, "read the API key from stdin")
	group := fs.String("group", "", "official-key group: openai, azure, aws, vertex, officially")
	profile := fs.String("profile", "", "profile name (default: the group, or default)")
	label := fs.String("label", "", "profile label shown in config")
	claudeModel := fs.String("claude-model", "", "model id for Claude Code")
	codexModel := fs.String("codex-model", "", "model id for Codex")
	wire := fs.String("codex-wire-api", "", "Codex provider protocol: responses or chat")
	claudeOnly := fs.Bool("claude-only", false, "configure Claude Code only")
	codexOnly := fs.Bool("codex-only", false, "configure Codex only")
	noShell := fs.Bool("no-shell", false, "do not edit the shell startup file")
	shellFlag := fs.String("shell", "", "shell syntax: bash, zsh, fish, or powershell")
	rcFile := fs.String("rc-file", "", "shell startup file to edit")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if fs.NArg() != 0 {
		fmt.Fprint(stderr, setupHelp)
		return 2, fmt.Errorf("unexpected argument %q", fs.Arg(0))
	}
	if *claudeOnly && *codexOnly {
		return 2, fmt.Errorf("pass only one of --claude-only and --codex-only")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	set := visited(fs)
	if *shellFlag != "" {
		e.layout.Shell = paths.DetectShell(*shellFlag, runtime.GOOS)
	}
	if *rcFile != "" {
		e.layout.RCFile = *rcFile
	} else if *shellFlag != "" {
		e.layout.RCFile = paths.DefaultRC(e.home, e.layout.Shell)
	}

	name, labelValue, err := chooseIdentity(e.state, *profile, *group, *label, set["profile"], set["group"], set["label"])
	if err != nil {
		return 2, err
	}
	existing, _ := e.state.Find(name)

	key, err := resolveKey(*apiKey, *keyStdin, set["api-key"], stdin, stderr, existing.APIKey, true)
	if err != nil {
		return 2, err
	}
	baseURL := *base
	if !set["base-url"] && existing.BaseURL != "" {
		baseURL = existing.BaseURL
	}
	wireAPI := existing.WireAPI
	if wireAPI == "" {
		wireAPI = "responses"
	}
	if set["codex-wire-api"] {
		wireAPI = *wire
	}
	wireAPI, err = normalizeWire(wireAPI)
	if err != nil {
		return 2, err
	}
	claude := existing.ClaudeModel
	if set["claude-model"] {
		claude = *claudeModel
	}
	codexModelValue := existing.CodexModel
	if set["codex-model"] {
		codexModelValue = *codexModel
	}

	touchClaude := !*codexOnly
	touchCodex := !*claudeOnly
	touchShell := !*noShell
	claude, setClaude, codexModelValue, setCodex := e.autopickModels(
		baseURL, key, touchClaude, touchCodex,
		claude, set["claude-model"], codexModelValue, set["codex-model"],
	)
	prof := store.Profile{
		Name:        name,
		Label:       labelValue,
		BaseURL:     baseURL,
		APIKey:      key,
		ClaudeModel: claude,
		CodexModel:  codexModelValue,
		WireAPI:     wireAPI,
	}
	report, err := e.saveProfile(prof, touchClaude, touchCodex, touchShell, setClaude, setCodex)
	if err != nil {
		return 1, err
	}
	printSetup(e, prof, report, touchClaude, touchCodex, touchShell, true)
	return 0, nil
}

func (e env) autopickModels(baseURL, key string, touchClaude, touchCodex bool, claude string, claudeExplicit bool, codex string, codexExplicit bool) (string, bool, string, bool) {
	setClaude := claude != ""
	setCodex := codex != ""
	needClaude := touchClaude && !claudeExplicit && claude == ""
	needCodex := touchCodex && !codexExplicit && codex == ""
	if !needClaude && !needCodex {
		return claude, setClaude, codex, setCodex
	}
	fmt.Fprintf(e.stderr, "Fetching models from %s to choose defaults...\n", baseURL)
	ids, err := fetchModelIDs(baseURL, key, 30*time.Second)
	if err != nil {
		fmt.Fprintf(e.stderr, "Could not list models: %s\n", err.Error())
		fmt.Fprintln(e.stderr, "Continuing without a default model.")
		return claude, setClaude, codex, setCodex
	}
	if needClaude {
		claude = gateway.PickDefault(ids, "claude")
		setClaude = claude != ""
		if claude == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Claude model id.")
		} else {
			fmt.Fprintf(e.stderr, "Claude model  %s\n", claude)
		}
	}
	if needCodex {
		codex = gateway.PickDefault(ids, "gpt")
		setCodex = codex != ""
		if codex == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Codex model id.")
		} else {
			fmt.Fprintf(e.stderr, "Codex model   %s\n", codex)
		}
	}
	return claude, setClaude, codex, setCodex
}

func fetchModelIDs(baseURL, key string, timeout time.Duration) ([]string, error) {
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	ids, check, err := gateway.ListModels(ctx, gateway.ProbeOptions{
		Endpoints: ep,
		APIKey:    key,
		Timeout:   timeout,
	})
	if err != nil {
		if check.Hint != "" {
			return nil, errors.New(check.Hint)
		}
		return nil, err
	}
	return ids, nil
}

func probeTarget(t target, timeout time.Duration) (gateway.Endpoints, gateway.Report, error) {
	ep, err := gateway.ParseBase(t.baseURL)
	if err != nil {
		return gateway.Endpoints{}, gateway.Report{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	report := gateway.Probe(ctx, gateway.ProbeOptions{
		Endpoints:   ep,
		APIKey:      t.key,
		ClaudeModel: t.claudeModel,
		CodexModel:  t.codexModel,
		Timeout:     timeout,
	})
	return ep, report, nil
}

func (e env) saveProfile(prof store.Profile, touchClaude, touchCodex, touchShell, setClaude, setCodex bool) (apply.Report, error) {
	req := apply.Request{
		ClaudePath:     e.layout.ClaudePath,
		CodexPath:      e.layout.CodexPath,
		RCFile:         e.layout.RCFile,
		Shell:          e.layout.Shell,
		BaseURL:        prof.BaseURL,
		APIKey:         prof.APIKey,
		Label:          prof.Label,
		ClaudeModel:    prof.ClaudeModel,
		CodexModel:     prof.CodexModel,
		WireAPI:        prof.WireAPI,
		TouchClaude:    touchClaude,
		TouchCodex:     touchCodex,
		TouchShell:     touchShell,
		SetClaudeModel: setClaude,
		SetCodexModel:  setCodex,
		Backups:        e.backups(),
	}
	// Switch reuses the paths recorded here, including a custom rc file.
	if e.state.Apply.Claude && e.state.Apply.ClaudePath != "" && !touchClaude {
		req.ClaudePath = e.state.Apply.ClaudePath
	}
	report, err := apply.Apply(req)
	if err != nil {
		return report, err
	}
	e.state.Upsert(prof)
	e.state.Active = prof.Name
	if touchClaude {
		e.state.Apply.Claude = true
		e.state.Apply.ClaudePath = e.layout.ClaudePath
	}
	if touchCodex {
		e.state.Apply.Codex = true
		e.state.Apply.CodexPath = e.layout.CodexPath
	}
	if touchShell {
		e.state.Apply.Shell = true
		e.state.Apply.ShellKind = e.layout.Shell
		e.state.Apply.RCFile = e.layout.RCFile
	}
	if err := store.Save(e.stateP, e.state); err != nil {
		return report, err
	}
	return report, nil
}

func printSetup(e env, prof store.Profile, report apply.Report, claude, codex, shell, hints bool) {
	fmt.Fprintf(e.stdout, "Configured %s (%s).\n", prof.Name, prof.Label)
	fmt.Fprintf(e.stdout, "Key       %s\n", textutil.Mask(prof.APIKey))
	fmt.Fprintf(e.stdout, "Gateway   %s\n", report.AnthropicBase)
	if codex {
		fmt.Fprintf(e.stdout, "Codex URL %s\n", report.OpenAIBase)
	}
	fmt.Fprintln(e.stdout)
	if claude {
		model := prof.ClaudeModel
		if model == "" {
			model = "not set"
		}
		fmt.Fprintf(e.stdout, "  Claude Code  %s   model %s\n", e.show(e.layout.ClaudePath), model)
	}
	if codex {
		model := prof.CodexModel
		if model == "" {
			model = "not set"
		}
		fmt.Fprintf(e.stdout, "  Codex        %s   model %s\n", e.show(e.layout.CodexPath), model)
	}
	if shell {
		fmt.Fprintf(e.stdout, "  Shell        %s\n", e.show(e.layout.RCFile))
	}
	if len(report.Changed) == 0 {
		fmt.Fprintln(e.stdout, "\nAlready up to date.")
	}
	if !hints {
		return
	}
	if shell {
		fmt.Fprintf(e.stdout, "\nOpen a new terminal, or run:  %s\n", sourceHint(e.layout.Shell, e.show(e.layout.RCFile)))
	}
	fmt.Fprintln(e.stdout, "\nThen check the gateway:")
	fmt.Fprintln(e.stdout, "  deeprouter-setup test")
	fmt.Fprintln(e.stdout, "  deeprouter-setup models")
}

func cmdTest(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, testHelp) }
	base := fs.String("base-url", "", "gateway base URL (default: active profile)")
	apiKey := fs.String("api-key", "", "API key")
	keyStdin := fs.Bool("api-key-stdin", false, "read the API key from stdin")
	profile := fs.String("profile", "", "profile to test")
	claudeModel := fs.String("claude-model", "", "model id for the messages request")
	codexModel := fs.String("codex-model", "", "model id for the chat completion")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	set := visited(fs)
	target, err := e.resolveTarget(set, *profile, *base, *apiKey, *keyStdin, *claudeModel, *codexModel, false)
	if err != nil {
		return 2, err
	}
	ep, report, err := probeTarget(target, *timeout)
	if err != nil {
		return 2, err
	}
	if *asJSON {
		payload := map[string]any{
			"profile":  target.name,
			"label":    target.label,
			"base_url": ep.AnthropicBase,
			"ok":       report.OK,
			"models":   report.Models,
			"checks":   report.Checks,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			return 1, err
		}
	} else {
		printReport(stdout, target, ep, report)
	}
	if !report.OK {
		return 1, nil
	}
	return 0, nil
}

func cmdModels(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("models", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, modelsHelp) }
	base := fs.String("base-url", "", "gateway base URL")
	apiKey := fs.String("api-key", "", "API key")
	keyStdin := fs.Bool("api-key-stdin", false, "read the API key from stdin")
	profile := fs.String("profile", "", "profile to query")
	timeout := fs.Duration("timeout", 30*time.Second, "request timeout")
	asJSON := fs.Bool("json", false, "print a JSON array")
	plain := fs.Bool("plain", false, "print one model id per line")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	set := visited(fs)
	target, err := e.resolveTarget(set, *profile, *base, *apiKey, *keyStdin, "", "", false)
	if err != nil {
		return 2, err
	}
	ep, err := gateway.ParseBase(target.baseURL)
	if err != nil {
		return 2, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()
	ids, check, err := gateway.ListModels(ctx, gateway.ProbeOptions{
		Endpoints: ep,
		APIKey:    target.key,
		Timeout:   *timeout,
	})
	if err != nil {
		fmt.Fprintf(stderr, "GET /v1/models\n  %s\n", describeCheck(check))
		if check.Hint != "" {
			fmt.Fprintf(stderr, "  %s\n", check.Hint)
		}
		return 1, nil
	}
	if *asJSON {
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(ids); err != nil {
			return 1, err
		}
		return 0, nil
	}
	if *plain {
		for _, id := range ids {
			fmt.Fprintln(stdout, id)
		}
		return 0, nil
	}
	fmt.Fprintf(stdout, "%d models at %s\n", len(ids), ep.AnthropicBase)
	if len(ids) == 0 {
		fmt.Fprintln(stdout, "\nThe gateway returned an empty model list.")
		return 0, nil
	}
	fmt.Fprintln(stdout)
	for _, id := range ids {
		fmt.Fprintf(stdout, "  %s\n", id)
	}
	return 0, nil
}

func cmdSwitch(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("switch", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, switchHelp) }
	group := fs.String("group", "", "profile group to activate")
	profile := fs.String("profile", "", "profile name to activate")
	claudeModel := fs.String("claude-model", "", "Claude Code model id")
	codexModel := fs.String("codex-model", "", "Codex model id")
	wire := fs.String("codex-wire-api", "", "Codex provider protocol: responses or chat")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	set := visited(fs)
	if !set["group"] && !set["profile"] && !set["claude-model"] && !set["codex-model"] && !set["codex-wire-api"] {
		fmt.Fprint(stderr, switchHelp)
		return 2, fmt.Errorf("pass --profile, --group, --claude-model, or --codex-model")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	name := e.state.Active
	if set["profile"] {
		name = *profile
	} else if set["group"] {
		if g, ok := groups.Lookup(*group); ok {
			name = g.Name
		} else {
			name = *group
		}
	}
	if name == "" {
		return 2, fmt.Errorf("no active profile. Run deeprouter-setup setup first")
	}
	prof, ok := e.state.Find(name)
	if !ok {
		return 2, fmt.Errorf("no profile %q. Create it with: deeprouter-setup profile add %s --api-key \"$DEEPROUTER_API_KEY\"", name, name)
	}
	if set["claude-model"] {
		prof.ClaudeModel = *claudeModel
	}
	if set["codex-model"] {
		prof.CodexModel = *codexModel
	}
	if set["codex-wire-api"] {
		w, err := normalizeWire(*wire)
		if err != nil {
			return 2, err
		}
		prof.WireAPI = w
	}
	if prof.WireAPI == "" {
		prof.WireAPI = "responses"
	}
	return e.applyProfile(prof, true)
}

func cmdProfile(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	if len(args) == 0 || args[0] == "list" || args[0] == "--help" || args[0] == "-h" {
		if len(args) > 0 && (args[0] == "--help" || args[0] == "-h") {
			fmt.Fprint(stdout, profileHelp)
			return 0, nil
		}
		if len(args) > 0 && args[0] == "list" {
			args = args[1:]
		}
		fs := flag.NewFlagSet("profile list", flag.ContinueOnError)
		fs.SetOutput(stderr)
		fs.Usage = func() { fmt.Fprint(stderr, profileHelp) }
		if err := fs.Parse(args); err != nil {
			if errors.Is(err, flag.ErrHelp) {
				return 0, nil
			}
			return 2, nil
		}
		e, err := loadEnv(stderr, stdin, stdout)
		if err != nil {
			return 1, err
		}
		return printProfiles(e), nil
	}
	switch args[0] {
	case "add":
		return cmdProfileAdd(args[1:], stdout, stderr, stdin)
	case "use":
		return cmdProfileUse(args[1:], stdout, stderr, stdin)
	case "remove":
		return cmdProfileRemove(args[1:], stdout, stderr, stdin)
	default:
		fmt.Fprint(stderr, profileHelp)
		return 2, fmt.Errorf("unknown profile command %q", args[0])
	}
}

func cmdProfileAdd(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("profile add", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, profileAddHelp) }
	base := fs.String("base-url", gateway.DefaultBaseURL, "gateway base URL")
	apiKey := fs.String("api-key", "", "API key")
	keyStdin := fs.Bool("api-key-stdin", false, "read the API key from stdin")
	group := fs.String("group", "", "official-key group used for the default label")
	label := fs.String("label", "", "profile label")
	claudeModel := fs.String("claude-model", "", "Claude Code model id")
	codexModel := fs.String("codex-model", "", "Codex model id")
	wire := fs.String("codex-wire-api", "", "Codex provider protocol: responses or chat")
	use := fs.Bool("use", false, "make this the active profile and write tool config")
	positional, flagArgs, err := splitFlags(args, map[string]bool{
		"base-url":       true,
		"api-key":        true,
		"group":          true,
		"label":          true,
		"claude-model":   true,
		"codex-model":    true,
		"codex-wire-api": true,
	})
	if err != nil {
		return 2, err
	}
	if err := fs.Parse(flagArgs); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if len(positional) != 1 || fs.NArg() != 0 {
		fmt.Fprint(stderr, profileAddHelp)
		return 2, fmt.Errorf("usage: deeprouter-setup profile add <name>")
	}
	name := positional[0]
	if err := store.ValidName(name); err != nil {
		return 2, err
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	set := visited(fs)
	existing, _ := e.state.Find(name)
	labelValue := existing.Label
	if set["label"] {
		labelValue = *label
	} else if labelValue == "" && set["group"] {
		if g, ok := groups.Lookup(*group); ok {
			labelValue = g.Label
		}
	}
	if labelValue == "" {
		if g, ok := groups.Lookup(name); ok {
			labelValue = g.Label
		} else {
			labelValue = name
		}
	}
	key, err := resolveKey(*apiKey, *keyStdin, set["api-key"], stdin, stderr, existing.APIKey, true)
	if err != nil {
		return 2, err
	}
	baseURL := *base
	if !set["base-url"] && existing.BaseURL != "" {
		baseURL = existing.BaseURL
	}
	if _, err := gateway.ParseBase(baseURL); err != nil {
		return 2, err
	}
	claude := existing.ClaudeModel
	if set["claude-model"] {
		claude = *claudeModel
	}
	codexModelValue := existing.CodexModel
	if set["codex-model"] {
		codexModelValue = *codexModel
	}
	wireAPI := existing.WireAPI
	if set["codex-wire-api"] {
		wireAPI = *wire
	}
	if wireAPI == "" {
		wireAPI = "responses"
	}
	wireAPI, err = normalizeWire(wireAPI)
	if err != nil {
		return 2, err
	}
	prof := store.Profile{
		Name:        name,
		Label:       labelValue,
		BaseURL:     baseURL,
		APIKey:      key,
		ClaudeModel: claude,
		CodexModel:  codexModelValue,
		WireAPI:     wireAPI,
	}
	e.state.Upsert(prof)
	if *use {
		return e.applyProfile(prof, true)
	}
	if err := store.Save(e.stateP, e.state); err != nil {
		return 1, err
	}
	fmt.Fprintf(stdout, "Saved profile %s (%s). Key %s.\n", prof.Name, prof.Label, textutil.Mask(prof.APIKey))
	fmt.Fprintf(stdout, "Activate it with:  deeprouter-setup profile use %s\n", prof.Name)
	return 0, nil
}

func cmdProfileUse(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("profile use", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, "Usage: deeprouter-setup profile use <name>\n") }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if fs.NArg() != 1 {
		return 2, fmt.Errorf("usage: deeprouter-setup profile use <name>")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	prof, ok := e.state.Find(fs.Arg(0))
	if !ok {
		return 2, fmt.Errorf("no profile %q", fs.Arg(0))
	}
	return e.applyProfile(prof, true)
}

func cmdProfileRemove(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("profile remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, "Usage: deeprouter-setup profile remove <name>\n") }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if fs.NArg() != 1 {
		return 2, fmt.Errorf("usage: deeprouter-setup profile remove <name>")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	if err := e.state.Remove(fs.Arg(0)); err != nil {
		return 2, err
	}
	if err := store.Save(e.stateP, e.state); err != nil {
		return 1, err
	}
	fmt.Fprintf(stdout, "Removed profile %s.\n", fs.Arg(0))
	return 0, nil
}

func (e env) applyProfile(prof store.Profile, requirePrior bool) (int, error) {
	if prof.WireAPI == "" {
		prof.WireAPI = "responses"
	}
	wire, err := normalizeWire(prof.WireAPI)
	if err != nil {
		return 2, err
	}
	prof.WireAPI = wire
	touchClaude := e.state.Apply.Claude
	touchCodex := e.state.Apply.Codex
	touchShell := e.state.Apply.Shell
	if !touchClaude && !touchCodex && !touchShell {
		if requirePrior {
			touchClaude, touchCodex, touchShell = true, true, true
		}
	}
	claudePath := e.layout.ClaudePath
	codexPath := e.layout.CodexPath
	rc := e.layout.RCFile
	shell := e.layout.Shell
	if e.state.Apply.ClaudePath != "" {
		claudePath = e.state.Apply.ClaudePath
	}
	if e.state.Apply.CodexPath != "" {
		codexPath = e.state.Apply.CodexPath
	}
	if e.state.Apply.RCFile != "" {
		rc = e.state.Apply.RCFile
	}
	if e.state.Apply.ShellKind != "" {
		shell = e.state.Apply.ShellKind
	}
	report, err := apply.Apply(apply.Request{
		ClaudePath:     claudePath,
		CodexPath:      codexPath,
		RCFile:         rc,
		Shell:          shell,
		BaseURL:        prof.BaseURL,
		APIKey:         prof.APIKey,
		Label:          prof.Label,
		ClaudeModel:    prof.ClaudeModel,
		CodexModel:     prof.CodexModel,
		WireAPI:        prof.WireAPI,
		TouchClaude:    touchClaude,
		TouchCodex:     touchCodex,
		TouchShell:     touchShell,
		SetClaudeModel: prof.ClaudeModel != "",
		SetCodexModel:  prof.CodexModel != "",
		Backups:        e.backups(),
	})
	if err != nil {
		return 1, err
	}
	e.state.Upsert(prof)
	e.state.Active = prof.Name
	e.state.Apply.Claude = touchClaude
	e.state.Apply.Codex = touchCodex
	e.state.Apply.Shell = touchShell
	e.state.Apply.ClaudePath = claudePath
	e.state.Apply.CodexPath = codexPath
	e.state.Apply.RCFile = rc
	e.state.Apply.ShellKind = shell
	if err := store.Save(e.stateP, e.state); err != nil {
		return 1, err
	}
	e.layout.ClaudePath = claudePath
	e.layout.CodexPath = codexPath
	e.layout.RCFile = rc
	e.layout.Shell = shell
	printSetup(e, prof, report, touchClaude, touchCodex, touchShell, true)
	return 0, nil
}

func printProfiles(e env) int {
	if len(e.state.Profiles) == 0 {
		fmt.Fprintln(e.stdout, "No profiles yet.")
		fmt.Fprintln(e.stdout, "Create one with:  deeprouter-setup setup --group officially --api-key \"$DEEPROUTER_API_KEY\"")
		return 0
	}
	fmt.Fprintf(e.stdout, "%-14s %-14s %-32s %s\n", "NAME", "LABEL", "BASE URL", "ACTIVE")
	for _, p := range e.state.Profiles {
		active := ""
		if p.Name == e.state.Active {
			active = "yes"
		}
		fmt.Fprintf(e.stdout, "%-14s %-14s %-32s %s\n", p.Name, p.Label, p.BaseURL, active)
	}
	return 0
}

func cmdGroups(stdout io.Writer) (int, error) {
	fmt.Fprint(stdout, `Official-key groups

A group is selected by which API key you put on a profile. Create one profile
per key, then switch profiles when you want a different group.

`)
	fmt.Fprintf(stdout, "  %-12s %-12s %s\n", "NAME", "LABEL", "USE FOR")
	for _, g := range groups.Official {
		fmt.Fprintf(stdout, "  %-12s %-12s %s\n", g.Name, g.Label, g.Blurb)
	}
	fmt.Fprint(stdout, `
Examples:
  deeprouter-setup setup --group officially --api-key "$DEEPROUTER_API_KEY"
  deeprouter-setup profile add azure --label Azure --api-key "$AZURE_KEY"
  deeprouter-setup switch --group azure
`)
	return 0, nil
}

func cmdStatus(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "Usage: deeprouter-setup status") }
	asJSON := fs.Bool("json", false, "print JSON")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	e, err := loadEnv(stderr, nil, stdout)
	if err != nil {
		return 1, err
	}
	if e.state.Active == "" {
		fmt.Fprintln(stdout, "No active profile. Run deeprouter-setup setup.")
		return 0, nil
	}
	prof, ok := e.state.Find(e.state.Active)
	if !ok {
		return 1, fmt.Errorf("active profile %q is missing from the profile store", e.state.Active)
	}
	ep, err := gateway.ParseBase(prof.BaseURL)
	if err != nil {
		return 1, err
	}
	claudePath := e.layout.ClaudePath
	codexPath := e.layout.CodexPath
	rc := e.layout.RCFile
	if e.state.Apply.ClaudePath != "" {
		claudePath = e.state.Apply.ClaudePath
	}
	if e.state.Apply.CodexPath != "" {
		codexPath = e.state.Apply.CodexPath
	}
	if e.state.Apply.RCFile != "" {
		rc = e.state.Apply.RCFile
	}
	claudeText := readText(claudePath)
	codexText := readText(codexPath)
	rcText := readText(rc)
	if *asJSON {
		payload := map[string]any{
			"profile":      prof.Name,
			"label":        prof.Label,
			"base_url":     ep.AnthropicBase,
			"openai_base":  ep.OpenAIBase,
			"key":          textutil.Mask(prof.APIKey),
			"claude_model": prof.ClaudeModel,
			"codex_model":  prof.CodexModel,
			"wire_api":     prof.WireAPI,
			"claude_path":  claudePath,
			"codex_path":   codexPath,
			"rc_file":      rc,
			"claude_wired": strings.Contains(claudeText, ep.AnthropicBase),
			"codex_wired":  codexWired(codexText),
			"shell_wired":  shellrc.HasBlock(rcText),
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		if err := enc.Encode(payload); err != nil {
			return 1, err
		}
		return 0, nil
	}
	fmt.Fprintf(stdout, "Profile    %s (%s)\n", prof.Name, prof.Label)
	fmt.Fprintf(stdout, "Key        %s\n", textutil.Mask(prof.APIKey))
	fmt.Fprintf(stdout, "Gateway    %s\n", ep.AnthropicBase)
	fmt.Fprintf(stdout, "Codex URL  %s\n", ep.OpenAIBase)
	fmt.Fprintf(stdout, "Wire API   %s\n", emptyAs(prof.WireAPI, "responses"))
	fmt.Fprintln(stdout)
	fmt.Fprintf(stdout, "Claude Code  %s\n", e.show(claudePath))
	fmt.Fprintf(stdout, "  model      %s\n", emptyAs(prof.ClaudeModel, "not set"))
	fmt.Fprintf(stdout, "  wired      %s\n", yesNo(strings.Contains(claudeText, "ANTHROPIC_BASE_URL")))
	fmt.Fprintf(stdout, "Codex        %s\n", e.show(codexPath))
	fmt.Fprintf(stdout, "  model      %s\n", emptyAs(prof.CodexModel, "not set"))
	fmt.Fprintf(stdout, "  provider   %s\n", yesNo(codexWired(codexText)))
	fmt.Fprintf(stdout, "Shell        %s\n", e.show(rc))
	fmt.Fprintf(stdout, "  managed    %s\n", yesNo(shellrc.HasBlock(rcText)))
	return 0, nil
}

func cmdRestore(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, restoreHelp) }
	id := fs.String("id", "original", "backup id, original, or latest")
	list := fs.Bool("list", false, "list backups")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	e, err := loadEnv(stderr, nil, stdout)
	if err != nil {
		return 1, err
	}
	backups := e.backups()
	if *list {
		entries, err := backups.List()
		if err != nil {
			return 1, err
		}
		if len(entries) == 0 {
			fmt.Fprintln(stdout, "No backups yet.")
			return 0, nil
		}
		for _, entry := range entries {
			when := entry.At
			if when == "" {
				when = "before the first edit"
			}
			fmt.Fprintf(stdout, "%s  %s\n", entry.ID, when)
			for _, path := range entry.Paths {
				fmt.Fprintf(stdout, "  %s\n", e.show(path))
			}
		}
		return 0, nil
	}
	restored, err := backups.Restore(*id)
	if err != nil {
		if strings.Contains(err.Error(), "no original backup") {
			fmt.Fprintln(stdout, "No backup to restore.")
			return 0, nil
		}
		return 1, err
	}
	for _, path := range restored {
		fmt.Fprintf(stdout, "Restored %s\n", e.show(path))
	}
	fmt.Fprintln(stdout, "Profiles were kept. Run deeprouter-setup setup to apply one again.")
	return 0, nil
}

func cmdUninstall(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("uninstall", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, uninstallHelp) }
	purge := fs.Bool("purge", false, "delete ~/.deeprouter, including profiles and backups")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	e, err := loadEnv(stderr, nil, stdout)
	if err != nil {
		return 1, err
	}
	restored, err := e.backups().Restore("original")
	if err != nil {
		if strings.Contains(err.Error(), "no original backup") {
			fmt.Fprintln(stdout, "No backup to restore.")
		} else {
			return 1, err
		}
	} else {
		for _, path := range restored {
			fmt.Fprintf(stdout, "Restored %s\n", e.show(path))
		}
	}
	if *purge {
		if err := os.RemoveAll(e.layout.ConfigDir); err != nil {
			return 1, err
		}
		fmt.Fprintf(stdout, "Removed %s\n", e.show(e.layout.ConfigDir))
	} else {
		fmt.Fprintln(stdout, "Profiles and backups are still in "+e.show(e.layout.ConfigDir)+".")
		fmt.Fprintln(stdout, "Delete them with:  deeprouter-setup uninstall --purge")
	}
	return 0, nil
}

type target struct {
	name        string
	label       string
	baseURL     string
	key         string
	claudeModel string
	codexModel  string
}

func (e env) resolveTarget(set map[string]bool, profile, base, apiKey string, keyStdin bool, claudeModel, codexModel string, allowPrompt bool) (target, error) {
	var prof store.Profile
	var ok bool
	name := profile
	if name == "" {
		name = e.state.Active
	}
	if name != "" {
		prof, ok = e.state.Find(name)
	}
	if profile != "" && !ok {
		return target{}, fmt.Errorf("no profile %q", profile)
	}
	baseURL := base
	if baseURL == "" {
		baseURL = prof.BaseURL
	}
	if baseURL == "" {
		baseURL = gateway.DefaultBaseURL
	}
	key, err := resolveKey(apiKey, keyStdin, set["api-key"], e.stdin, e.stderr, prof.APIKey, allowPrompt)
	if err != nil {
		return target{}, err
	}
	if claudeModel == "" {
		claudeModel = prof.ClaudeModel
	}
	if codexModel == "" {
		codexModel = prof.CodexModel
	}
	label := prof.Label
	outName := prof.Name
	if !ok {
		outName = ""
	}
	return target{
		name:        outName,
		label:       label,
		baseURL:     baseURL,
		key:         key,
		claudeModel: claudeModel,
		codexModel:  codexModel,
	}, nil
}

func resolveKey(flagKey string, keyStdin, flagSet bool, stdin io.Reader, stderr io.Writer, existing string, allowPrompt bool) (string, error) {
	if flagSet && keyStdin {
		return "", fmt.Errorf("pass only one of --api-key and --api-key-stdin")
	}
	if flagSet {
		return textutil.ValidateKey(flagKey)
	}
	if keyStdin {
		raw, err := io.ReadAll(stdin)
		if err != nil {
			return "", fmt.Errorf("read API key from stdin: %w", err)
		}
		return textutil.ValidateKey(string(raw))
	}
	if existing != "" {
		return existing, nil
	}
	if envKey := strings.TrimSpace(os.Getenv("DEEPROUTER_API_KEY")); envKey != "" {
		return textutil.ValidateKey(envKey)
	}
	if allowPrompt && isTerminal(stdin) {
		key, err := promptKey(stdin, stderr)
		if err != nil {
			return "", err
		}
		return textutil.ValidateKey(key)
	}
	return "", fmt.Errorf("API key required. Pass --api-key, set DEEPROUTER_API_KEY, or use --api-key-stdin")
}

func chooseIdentity(st store.State, profile, group, label string, profileSet, groupSet, labelSet bool) (string, string, error) {
	name := ""
	switch {
	case profileSet:
		name = profile
	case groupSet:
		if g, ok := groups.Lookup(group); ok {
			name = g.Name
		} else {
			name = group
		}
	case st.Active != "":
		name = st.Active
	default:
		name = "default"
	}
	if err := store.ValidName(name); err != nil {
		return "", "", err
	}
	existing, _ := st.Find(name)
	switch {
	case labelSet:
		if strings.TrimSpace(label) == "" {
			return "", "", fmt.Errorf("label is empty")
		}
		return name, label, nil
	case groupSet:
		if g, ok := groups.Lookup(group); ok {
			return name, g.Label, nil
		}
	}
	if existing.Label != "" {
		return name, existing.Label, nil
	}
	if g, ok := groups.Lookup(name); ok {
		return name, g.Label, nil
	}
	if name == "default" {
		return name, "DeepRouter", nil
	}
	return name, name, nil
}

func printReport(w io.Writer, target target, ep gateway.Endpoints, report gateway.Report) {
	fmt.Fprintf(w, "Gateway  %s\n", ep.AnthropicBase)
	if target.name != "" {
		label := target.label
		if label == "" {
			label = target.name
		}
		fmt.Fprintf(w, "Profile  %s (%s)\n", target.name, label)
	}
	fmt.Fprintf(w, "Key      %s\n\n", textutil.Mask(target.key))
	failed := 0
	for _, check := range report.Checks {
		fmt.Fprintf(w, "%s %s", check.Method, check.Path)
		if check.Detail != "" && check.Name != "models" {
			fmt.Fprintf(w, "  %s", check.Detail)
		}
		fmt.Fprintln(w)
		fmt.Fprintf(w, "  %s\n", describeCheck(check))
		if check.Hint != "" {
			fmt.Fprintf(w, "  %s\n", check.Hint)
			failed++
		} else if !check.OK {
			failed++
		}
	}
	fmt.Fprintln(w)
	if report.OK {
		fmt.Fprintln(w, "Passed.")
		return
	}
	if failed == 1 {
		fmt.Fprintln(w, "1 check failed.")
		return
	}
	fmt.Fprintf(w, "%d checks failed.\n", failed)
}

func describeCheck(check gateway.Check) string {
	latency := fmt.Sprintf("ttfb %s   total %s", fmtMS(check.TTFB), fmtMS(check.Total))
	if check.Status == 0 {
		msg := "failed"
		if check.Err != "" {
			msg = "failed"
		}
		return fmt.Sprintf("%-28s %s", msg, latency)
	}
	text := check.StatusText
	if text == "" {
		text = "Error"
	}
	line := fmt.Sprintf("%d %-16s %s", check.Status, text, latency)
	if check.Detail != "" && check.Name == "models" {
		line += "   " + check.Detail
	}
	return line
}

func fmtMS(d time.Duration) string {
	if d <= 0 {
		return "0ms"
	}
	ms := d.Milliseconds()
	if ms == 0 {
		return "<1ms"
	}
	return fmt.Sprintf("%dms", ms)
}

func normalizeWire(s string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "", "responses":
		return "responses", nil
	case "chat":
		return "chat", nil
	default:
		return "", fmt.Errorf("codex wire api must be responses or chat")
	}
}

func sourceHint(shell, shown string) string {
	switch shell {
	case "fish":
		return "source " + shown
	case "powershell":
		return ". " + shown
	default:
		return "source " + shown
	}
}

// splitFlags pulls positional arguments out from among flags so
// `profile add azure --label Azure` and `profile add --label Azure azure` both work.
// valueFlags names flags that consume the following argument.
func splitFlags(args []string, valueFlags map[string]bool) (positional, flags []string, err error) {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if a == "-" || !strings.HasPrefix(a, "-") {
			positional = append(positional, a)
			continue
		}
		flags = append(flags, a)
		name := strings.TrimLeft(a, "-")
		if name == "" || strings.Contains(name, "=") {
			continue
		}
		if valueFlags[name] {
			if i+1 >= len(args) {
				return nil, nil, fmt.Errorf("flag --%s needs a value", name)
			}
			i++
			flags = append(flags, args[i])
		}
	}
	return positional, flags, nil
}

func visited(fs *flag.FlagSet) map[string]bool {
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	return set
}

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

func promptKey(stdin io.Reader, stderr io.Writer) (string, error) {
	f, ok := stdin.(*os.File)
	if !ok {
		return "", fmt.Errorf("cannot prompt for an API key")
	}
	fmt.Fprint(stderr, "API key: ")
	restore := func() {}
	if _, err := exec.LookPath("stty"); err == nil {
		off := exec.Command("stty", "-echo")
		off.Stdin = f
		if err := off.Run(); err == nil {
			restore = func() {
				on := exec.Command("stty", "echo")
				on.Stdin = f
				_ = on.Run()
				fmt.Fprintln(stderr)
			}
		}
	}
	defer restore()
	line, err := bufio.NewReader(f).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	return strings.TrimSpace(line), nil
}

func readText(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return string(b)
}

func yesNo(ok bool) string {
	if ok {
		return "yes"
	}
	return "no"
}

func emptyAs(s, fallback string) string {
	if strings.TrimSpace(s) == "" {
		return fallback
	}
	return s
}

func codexWired(text string) bool {
	return strings.Contains(text, "[model_providers.deeprouter]") ||
		strings.Contains(text, "model_providers.\"deeprouter\"") ||
		strings.Contains(text, "model_provider = \"deeprouter\"")
}

const setupHelp = `Usage: deeprouter-setup setup [flags]

Write Claude Code, Codex, and shell configuration for one gateway profile.
Running setup again updates the same files in place.

Flags:
  --base-url URL         Gateway base URL (default https://deeprouter.top)
  --api-key KEY          API key. Visible in the process list; prefer the env var.
  --api-key-stdin        Read the API key from stdin
  --group NAME           openai, azure, aws, vertex, or officially
  --profile NAME         Profile name (default: group name, or "default")
  --label TEXT           Label stored with the profile
  --claude-model ID      ANTHROPIC_MODEL for Claude Code
  --codex-model ID       model for Codex
                         When either model flag is omitted, setup picks one
                         from GET /v1/models (a Claude id and a GPT id).
  --codex-wire-api KIND  responses (default) or chat
  --claude-only          Do not edit Codex
  --codex-only           Do not edit Claude Code
  --no-shell             Do not edit the shell startup file
  --shell KIND           bash, zsh, fish, or powershell
  --rc-file PATH         Startup file to edit

The API key is read from --api-key, --api-key-stdin, DEEPROUTER_API_KEY,
an existing profile, or an interactive prompt.

Claude Code receives ANTHROPIC_BASE_URL without a trailing /v1.
Codex receives base_url with /v1 and reads the key from DEEPROUTER_API_KEY.
`

const testHelp = `Usage: deeprouter-setup test [flags]

Call GET /v1/models, a one-token chat completion, and a one-token
Anthropic messages request. Prints HTTP status, time to first byte,
and total time.

Flags:
  --profile NAME
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --claude-model ID
  --codex-model ID
  --timeout DURATION   (default 30s)
  --json
`

const modelsHelp = `Usage: deeprouter-setup models [flags]

List model ids from GET /v1/models.

Flags:
  --profile NAME
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --timeout DURATION
  --plain            one id per line
  --json             a JSON array
`

const switchHelp = `Usage: deeprouter-setup switch [flags]

Activate a profile, change a model, or both. Tool config files are updated
to match.

Flags:
  --profile NAME
  --group NAME
  --claude-model ID
  --codex-model ID
  --codex-wire-api KIND
`

const profileHelp = `Usage: deeprouter-setup profile <command>

  list              Show saved profiles
  add <name>        Save a profile without wiring tools (pass --use to wire them)
  use <name>        Make a profile active and rewrite tool config
  remove <name>     Delete a profile that is not active

A profile stores a base URL, an API key, and optional model ids.
The key is what selects a gateway group.
`

const profileAddHelp = `Usage: deeprouter-setup profile add <name> [flags]

Flags:
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --group NAME
  --label TEXT
  --claude-model ID
  --codex-model ID
  --codex-wire-api KIND
  --use                 Activate the profile after saving it
`

const restoreHelp = `Usage: deeprouter-setup restore [flags]

Put every file deeprouter-setup changed back to the copy taken before the
first edit. Later snapshots undo only the most recent edit.

Flags:
  --list
  --id original|latest|BACKUP
`

const uninstallHelp = `Usage: deeprouter-setup uninstall [flags]

Restore config files to the pre-setup copies.
With --purge, also delete ~/.deeprouter (profiles and backups).

Flags:
  --purge
`
