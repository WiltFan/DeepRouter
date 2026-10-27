// Package cli is the deeprouter command line.
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
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter/internal/apply"
	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/backup"
	"github.com/zfg5118/deeprouter/internal/claudedesktop"
	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/gemini"
	"github.com/zfg5118/deeprouter/internal/paths"
	"github.com/zfg5118/deeprouter/internal/provider"
	"github.com/zfg5118/deeprouter/internal/shellrc"
	"github.com/zfg5118/deeprouter/internal/store"
	"github.com/zfg5118/deeprouter/internal/textutil"
	"github.com/zfg5118/deeprouter/internal/userenv"
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
		fmt.Fprintf(stderr, "deeprouter: %s\n", err.Error())
	}
	return code
}

func dispatch(args []string, version string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	if len(args) == 0 {
		if stdinIsTerminal(stdin) {
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
		fmt.Fprintf(stdout, "deeprouter %s\n", version)
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

const rootHelp = `deeprouter configures Claude Code and Codex / ChatGPT to use an
OpenAI- and Anthropic-compatible API gateway.

Usage:
  deeprouter                  Interactive setup
  deeprouter <command> [flags]

With no arguments, in a terminal, interactive setup asks which programs
to configure. The prompt is Programs [1]: and Enter selects Codex / ChatGPT.
It then asks for an API key and a model for each selected program.
Enter keeps a key already set. Enter on a model list uses that app's default model.
When stdin is not a terminal, the same no-argument run configures Codex / ChatGPT.
Image, video, audio, embedding, and moderation models are left out of
those lists. Gemini CLI uses the gateway's generateContent API.

Commands:
  setup       Write selected program configs (and, without --apps, the shell)
  test        Check connectivity, HTTP status, and latency
  models      List models from the gateway
  switch      Change the active profile or model
  profile     Manage named profiles
  status      Show the active configuration and CLI versions
  restore     Put config files back from a backup
  uninstall   Restore config files and optionally delete local state
  version     Print the version
  help        Show this help

The default gateway is https://deeprouter.top (DeepRouter). Other presets
are WolfAI (https://wolfai.top) and LinkMesh (https://www.linkmesh.org).
Pass --provider deeprouter|wolfai|linkmesh, or set DEEPROUTER_PROVIDER.
--base-url still overrides the URL. Any other OpenAI- or
Anthropic-compatible relay base URL works.

Run deeprouter <command> --help for flags.
`

type env struct {
	stdout  io.Writer
	stderr  io.Writer
	stdin   io.Reader
	home    string
	layout  paths.Layout
	stateP  string
	state   store.State
	userEnv bool
}

// useUserEnv reports whether shell variables go to the Windows user
// environment instead of a startup file. DEEPROUTER_USER_ENV=1 forces it
// (tests). DEEPROUTER_USER_ENV=0 forces a startup file.
func useUserEnv() bool {
	switch strings.TrimSpace(os.Getenv("DEEPROUTER_USER_ENV")) {
	case "1", "true", "TRUE":
		return true
	case "0", "false", "FALSE":
		return false
	default:
		return runtime.GOOS == "windows"
	}
}

func loadEnv(stderr io.Writer, stdin io.Reader, stdout io.Writer) (env, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return env{}, fmt.Errorf("cannot find the home directory: %w", err)
	}
	shell := paths.DetectShell(os.Getenv("SHELL"), runtime.GOOS)
	layout := paths.Resolve(home, os.Getenv("CODEX_HOME"), shell, "")
	layout.DesktopLibrary = claudedesktop.Dir(home)
	if grokHome := strings.TrimSpace(os.Getenv("GROK_HOME")); grokHome != "" {
		layout.GrokPath = filepath.Join(grokHome, "config.toml")
	}
	layout.GeminiPath = gemini.SettingsPath(home, os.Getenv("GEMINI_CLI_HOME"))
	statePath := store.Path(layout.ConfigDir)
	st, err := store.Load(statePath)
	if err != nil {
		return env{}, err
	}
	e := env{
		stdout:  stdout,
		stderr:  stderr,
		stdin:   stdin,
		home:    home,
		layout:  layout,
		stateP:  statePath,
		state:   st,
		userEnv: useUserEnv(),
	}
	if e.userEnv {
		// Windows persists these variables in HKCU\Environment. A startup
		// file such as .zshrc is not read by new console processes.
		e.layout.Shell = "userenv"
		e.layout.RCFile = ""
	}
	return e, nil
}

func (e env) attachShell(req apply.Request) (apply.Request, error) {
	if !e.userEnv {
		return req, nil
	}
	st := userenv.Active()
	if st == nil {
		return req, fmt.Errorf("user environment variables are not available on this operating system")
	}
	req.UserEnv = st
	req.UserEnvRecord = userenv.RecordPath(e.layout.ConfigDir)
	req.Shell = "userenv"
	req.RCFile = ""
	return req, nil
}

func (e env) backups() backup.Store {
	return backup.Store{Dir: filepath.Join(e.layout.ConfigDir, "backups")}
}

func (e env) shellStatus(rc string) (wired bool, shown string) {
	if e.userEnv {
		rec, err := userenv.Load(userenv.RecordPath(e.layout.ConfigDir))
		if err != nil || len(rec.Values) == 0 {
			return false, userenv.Location
		}
		return true, userenv.Location
	}
	return shellrc.HasBlock(readText(rc)), rc
}

func (e env) restoreBackups(id string) ([]string, error) {
	var before userenv.Record
	beforeOK := false
	var st userenv.Store
	recordPath := ""
	if e.userEnv {
		st = userenv.Active()
		if st == nil {
			return nil, fmt.Errorf("user environment variables are not available on this operating system")
		}
		recordPath = userenv.RecordPath(e.layout.ConfigDir)
		rec, err := userenv.Load(recordPath)
		if err == nil {
			before = rec
			beforeOK = true
		} else if !os.IsNotExist(err) {
			return nil, err
		}
	}
	restored, err := e.backups().Restore(id)
	if err != nil {
		return restored, err
	}
	if !e.userEnv {
		return restored, nil
	}
	after, loadErr := userenv.Load(recordPath)
	afterOK := loadErr == nil
	if loadErr != nil && !os.IsNotExist(loadErr) {
		return restored, loadErr
	}
	if err := userenv.Reconcile(st, before, beforeOK, after, afterOK); err != nil {
		return restored, err
	}
	return restored, nil
}

func (e env) show(path string) string {
	return textutil.DisplayPath(e.home, path)
}

func cmdSetup(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("setup", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, setupHelp) }
	base := fs.String("base-url", gateway.DefaultBaseURL, "gateway base URL; any OpenAI- or Anthropic-compatible relay")
	providerFlag := fs.String("provider", "", "gateway preset: deeprouter, wolfai, or linkmesh")
	apiKey := fs.String("api-key", "", "API key (prefer DEEPROUTER_API_KEY or --api-key-stdin)")
	keyStdin := fs.Bool("api-key-stdin", false, "read the API key from stdin")
	profile := fs.String("profile", "", "profile name (default: active profile, or default)")
	label := fs.String("label", "", "profile label shown in config")
	claudeModel := fs.String("claude-model", "", "model id for Claude Code")
	codexModel := fs.String("codex-model", "", "model id for Codex")
	desktopModel := fs.String("claude-desktop-model", "", "model id for Claude Desktop")
	grokModel := fs.String("grok-model", "", "model id for Grok Build")
	geminiModel := fs.String("gemini-model", "", "model id for Gemini CLI")
	wire := fs.String("codex-wire-api", "", "Codex provider protocol: responses or chat")
	appsFlag := fs.String("apps", "", "programs to configure: claude-code,claude-desktop,codex,grok,gemini")
	claudeCodeKey := fs.String("claude-code-key", "", "API key for Claude Code; defaults to --api-key")
	desktopKey := fs.String("claude-desktop-key", "", "API key for Claude Desktop; defaults to --api-key")
	codexKey := fs.String("codex-key", "", "API key for Codex; defaults to --api-key")
	grokKey := fs.String("grok-key", "", "API key for Grok Build; defaults to --api-key")
	geminiKey := fs.String("gemini-key", "", "API key for Gemini CLI; defaults to --api-key")
	claudeOnly := fs.Bool("claude-only", false, "configure Claude Code only")
	codexOnly := fs.Bool("codex-only", false, "configure Codex only")
	noShell := fs.Bool("no-shell", false, "do not edit the shell startup file")
	installCLIs := fs.Bool("install-clis", false, "install missing npm CLIs for the programs being configured")
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
	if setApps := strings.TrimSpace(*appsFlag) != ""; setApps && (*claudeOnly || *codexOnly) {
		return 2, fmt.Errorf("pass --apps or --claude-only/--codex-only, not both")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	set := visited(fs)
	if strings.TrimSpace(*apiKey) == "" && !set["api-key"] {
		for _, candidate := range []string{*claudeCodeKey, *desktopKey, *codexKey, *grokKey, *geminiKey} {
			if strings.TrimSpace(candidate) != "" {
				*apiKey = candidate
				set["api-key"] = true
				break
			}
		}
	}
	if *rcFile != "" {
		// An explicit startup file opts out of the Windows user environment.
		e.userEnv = false
		e.layout.RCFile = *rcFile
		if *shellFlag != "" {
			e.layout.Shell = paths.DetectShell(*shellFlag, runtime.GOOS)
		} else {
			e.layout.Shell = paths.DetectShell(os.Getenv("SHELL"), runtime.GOOS)
		}
	} else if *shellFlag != "" && !e.userEnv {
		e.layout.Shell = paths.DetectShell(*shellFlag, runtime.GOOS)
		e.layout.RCFile = paths.DefaultRC(e.home, e.layout.Shell)
	}

	name, labelValue, err := chooseIdentity(e.state, *profile, *label, set["profile"], set["label"])
	if err != nil {
		return 2, err
	}
	existing, _ := e.state.Find(name)

	key, err := resolveKey(*apiKey, *keyStdin, set["api-key"], stdin, stderr, existing.APIKey, true)
	if err != nil {
		return 2, err
	}
	providerID, baseURL, providerExplicit, err := resolveGateway(*providerFlag, set["provider"], set["base-url"], *base, existing.Provider, existing.BaseURL)
	if err != nil {
		return 2, err
	}
	if providerExplicit {
		labelValue = providerLabel(labelValue, providerID)
	}
	if _, err := gateway.ParseBase(baseURL); err != nil {
		return 2, err
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
	claude, clearClaude, claudeExplicit := takeModel(*claudeModel, set["claude-model"])
	if !claudeExplicit {
		claude = existing.ClaudeModel
	}
	codexModelValue, clearCodex, codexExplicit := takeModel(*codexModel, set["codex-model"])
	if !codexExplicit {
		codexModelValue = existing.CodexModel
	}
	desktopModelValue, clearDesktop, desktopExplicit := takeModel(*desktopModel, set["claude-desktop-model"])
	if !desktopExplicit && set["claude-model"] {
		desktopModelValue, clearDesktop, desktopExplicit = claude, clearClaude, true
	} else if !desktopExplicit {
		desktopModelValue = existing.DesktopModel
	}
	grokModelValue, clearGrok, grokExplicit := takeModel(*grokModel, set["grok-model"])
	if !grokExplicit {
		grokModelValue = existing.GrokModel
	}
	geminiModelValue, clearGemini, geminiExplicit := takeModel(*geminiModel, set["gemini-model"])
	if !geminiExplicit {
		geminiModelValue = existing.GeminiModel
	}

	var selection []apps.ID
	appsExplicit := strings.TrimSpace(*appsFlag) != ""
	if appsExplicit {
		selection, err = apps.ParseSelection(*appsFlag, false)
		if err != nil {
			return 2, err
		}
		noticePrograms(stderr, selection)
	}

	touchClaude := !*codexOnly
	touchDesktop := !*codexOnly
	touchCodex := !*claudeOnly
	touchGrok := false
	touchGemini := false
	touchShell := !*noShell
	if appsExplicit {
		touchClaude = apps.Contains(selection, apps.ClaudeCode)
		touchDesktop = apps.Contains(selection, apps.ClaudeDesktop)
		touchCodex = apps.Contains(selection, apps.Codex)
		touchGrok = apps.Contains(selection, apps.Grok)
		touchGemini = apps.Contains(selection, apps.Gemini)
		touchShell = false
	}
	if !appsExplicit {
		var touched []apps.ID
		if touchClaude {
			touched = append(touched, apps.ClaudeCode)
		}
		if touchDesktop {
			touched = append(touched, apps.ClaudeDesktop)
		}
		if touchCodex {
			touched = append(touched, apps.Codex)
		}
		noticePrograms(stderr, touched)
	}
	claudeKey, err := overrideKey(key, *claudeCodeKey, set["claude-code-key"])
	if err != nil {
		return 2, err
	}
	desktopKeyValue, err := overrideKey(key, *desktopKey, set["claude-desktop-key"])
	if err != nil {
		return 2, err
	}
	codexKeyValue, err := overrideKey(key, *codexKey, set["codex-key"])
	if err != nil {
		return 2, err
	}
	grokKeyValue, err := overrideKey(key, *grokKey, set["grok-key"])
	if err != nil {
		return 2, err
	}
	geminiKeyValue, err := overrideKey(key, *geminiKey, set["gemini-key"])
	if err != nil {
		return 2, err
	}
	claude, setClaude, codexModelValue, setCodex, desktopModelValue, setDesktop, grokModelValue, setGrok, geminiModelValue, setGemini := e.autopickModels(
		baseURL, key, touchClaude, touchCodex, touchDesktop, touchGrok, touchGemini,
		claude, claudeExplicit, codexModelValue, codexExplicit,
		desktopModelValue, desktopExplicit,
		grokModelValue, grokExplicit,
		geminiModelValue, geminiExplicit,
	)
	if clearClaude {
		claude = ""
		setClaude = false
	}
	if clearCodex {
		codexModelValue = ""
		setCodex = false
	}
	if clearDesktop {
		desktopModelValue = ""
		setDesktop = false
	}
	if clearGrok {
		grokModelValue = ""
		setGrok = false
	}
	if clearGemini {
		geminiModelValue = ""
		setGemini = false
	}
	if touchGrok && grokModelValue == "" && !clearGrok && !grokExplicit {
		return 2, fmt.Errorf("Grok Build needs a model id (none containing \"grok\" were listed). Pass --grok-model, or --grok-model default to use the app default")
	}
	prof := store.Profile{
		Name:         name,
		Label:        labelValue,
		BaseURL:      baseURL,
		Provider:     providerID,
		APIKey:       key,
		ClaudeModel:  claude,
		CodexModel:   codexModelValue,
		DesktopModel: desktopModelValue,
		GrokModel:    grokModelValue,
		GeminiModel:  geminiModelValue,
		WireAPI:      wireAPI,
	}
	if claudeKey != key {
		prof.ClaudeKey = claudeKey
	}
	if desktopKeyValue != key {
		prof.DesktopKey = desktopKeyValue
	}
	if codexKeyValue != key {
		prof.CodexKey = codexKeyValue
	}
	if grokKeyValue != key {
		prof.GrokKey = grokKeyValue
	}
	if geminiKeyValue != key {
		prof.GeminiKey = geminiKeyValue
	}
	req := apply.Request{
		TouchClaude:       touchClaude,
		TouchDesktop:      touchDesktop,
		TouchCodex:        touchCodex,
		TouchGrok:         touchGrok,
		TouchGemini:       touchGemini,
		TouchShell:        touchShell,
		AppsExplicit:      appsExplicit,
		SetClaudeModel:    setClaude,
		SetDesktopModel:   setDesktop,
		SetCodexModel:     setCodex,
		SetGrokModel:      setGrok,
		SetGeminiModel:    setGemini,
		ClearClaudeModel:  clearClaude,
		ClearDesktopModel: clearDesktop,
		ClearCodexModel:   clearCodex,
		ClearGrokModel:    clearGrok,
		ClearGeminiModel:  clearGemini,
		ClaudeKey:         claudeKey,
		DesktopKey:        desktopKeyValue,
		CodexKey:          codexKeyValue,
		GrokKey:           grokKeyValue,
		GeminiKey:         geminiKeyValue,
	}
	report, err := e.saveProfile(prof, req)
	if err != nil {
		return 1, err
	}
	printSetup(e, prof, report, touchClaude, touchCodex, touchShell || touchGemini, true)
	var installErr error
	if *installCLIs {
		installErr = installMissingCLIs(e, false, nil, true, selectedProgramIDs(req))
	}
	var keyErr error
	if touchCodex {
		keyErr = e.confirmCodexKey(e.layout.CodexPath, prof.KeyFor(string(apps.Codex)))
	}
	if installErr != nil || keyErr != nil {
		return 1, nil
	}
	return 0, nil
}

func (e env) autopickModels(baseURL, key string, touchClaude, touchCodex, touchDesktop, touchGrok, touchGemini bool, claude string, claudeExplicit bool, codex string, codexExplicit bool, desktop string, desktopExplicit bool, grokModel string, grokExplicit bool, geminiModel string, geminiExplicit bool) (string, bool, string, bool, string, bool, string, bool, string, bool) {
	setClaude := claude != ""
	setCodex := codex != ""
	setDesktop := desktop != ""
	setGrok := grokModel != ""
	setGemini := geminiModel != ""
	needClaude := touchClaude && !claudeExplicit && claude == ""
	needCodex := touchCodex && !codexExplicit && codex == ""
	needDesktop := touchDesktop && !desktopExplicit && desktop == ""
	needGrok := touchGrok && !grokExplicit && grokModel == ""
	needGemini := touchGemini && !geminiExplicit && geminiModel == ""
	if !needClaude && !needCodex && !needDesktop && !needGrok && !needGemini {
		return claude, setClaude, codex, setCodex, desktop, setDesktop, grokModel, setGrok, geminiModel, setGemini
	}
	fmt.Fprintf(e.stderr, "Fetching models from %s to choose defaults...\n", baseURL)
	catalog, err := fetchModels(baseURL, key, 30*time.Second)
	if err != nil {
		fmt.Fprintf(e.stderr, "Could not list models: %s\n", err.Error())
		fmt.Fprintln(e.stderr, "Continuing without a default model.")
		return claude, setClaude, codex, setCodex, desktop, setDesktop, grokModel, setGrok, geminiModel, setGemini
	}
	if needClaude {
		claude = gateway.PickDefaultModels(catalog, "claude")
		setClaude = claude != ""
		if claude == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Claude model id.")
		} else {
			fmt.Fprintf(e.stderr, "Claude model  %s\n", claude)
		}
	}
	if needDesktop {
		if claude != "" && (needClaude || touchClaude) {
			desktop = claude
		} else {
			desktop = gateway.PickDefaultModels(catalog, "claude")
		}
		setDesktop = desktop != ""
		if desktop == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Claude model id.")
		} else if desktop != claude || !touchClaude {
			fmt.Fprintf(e.stderr, "Desktop model %s\n", desktop)
		}
	}
	if needCodex {
		codex = gateway.PickDefaultModels(catalog, "gpt")
		setCodex = codex != ""
		if codex == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Codex model id.")
		} else {
			fmt.Fprintf(e.stderr, "Codex model   %s\n", codex)
		}
	}
	if needGrok {
		grokModel = gateway.PickDefaultModels(catalog, "grok")
		setGrok = grokModel != ""
		if grokModel == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Grok model id.")
		} else {
			fmt.Fprintf(e.stderr, "Grok model    %s\n", grokModel)
		}
	}
	if needGemini {
		geminiModel = gateway.PickDefaultModels(catalog, "gemini")
		setGemini = geminiModel != ""
		if geminiModel == "" {
			fmt.Fprintln(e.stderr, "The gateway list has no Gemini model id. Gemini CLI will use its own default.")
		} else {
			fmt.Fprintf(e.stderr, "Gemini model  %s\n", geminiModel)
		}
	}
	return claude, setClaude, codex, setCodex, desktop, setDesktop, grokModel, setGrok, geminiModel, setGemini
}

func overrideKey(shared, specific string, explicit bool) (string, error) {
	if !explicit || strings.TrimSpace(specific) == "" {
		return shared, nil
	}
	return textutil.ValidateKey(specific)
}

func fetchModels(baseURL, key string, timeout time.Duration) ([]gateway.Model, error) {
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	models, check, err := gateway.ListCatalog(ctx, gateway.ProbeOptions{
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
	return models, nil
}

func probeTarget(t target, timeout time.Duration) (gateway.Endpoints, gateway.Report, error) {
	ep, err := gateway.ParseBase(t.baseURL)
	if err != nil {
		return gateway.Endpoints{}, gateway.Report{}, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	report := gateway.Probe(ctx, gateway.ProbeOptions{
		Endpoints:    ep,
		APIKey:       t.key,
		ClaudeModel:  t.claudeModel,
		CodexModel:   t.codexModel,
		DesktopModel: t.desktopModel,
		GrokModel:    t.grokModel,
		GeminiModel:  t.geminiModel,
		CodexKey:     t.codexKey,
		ClaudeKey:    t.claudeKey,
		DesktopKey:   t.desktopKey,
		GrokKey:      t.grokKey,
		GeminiKey:    t.geminiKey,
		Scoped:       t.scoped,
		ProbeModels:  t.probeModels,
		ProbeChat:    t.probeChat,
		ProbeGrok:    t.probeGrok,
		ProbeClaude:  t.probeClaude,
		ProbeDesktop: t.probeDesktop,
		ProbeGemini:  t.probeGemini,
		Timeout:      timeout,
	})
	return ep, report, nil
}

// reopenHint tells the user to restart the GUI apps that were configured.
func reopenHint(codex, desktop bool) string {
	switch {
	case codex && desktop:
		return "Quit and reopen Codex / ChatGPT and Claude Desktop. They do not read the shell startup file."
	case codex:
		return "Quit and reopen Codex / ChatGPT. It does not read the shell startup file."
	case desktop:
		return "Quit and reopen Claude Desktop. It does not read the shell startup file."
	default:
		return ""
	}
}

func selectedProgramIDs(req apply.Request) []apps.ID {
	var ids []apps.ID
	if req.TouchCodex {
		ids = append(ids, apps.Codex)
	}
	if req.TouchClaude {
		ids = append(ids, apps.ClaudeCode)
	}
	if req.TouchDesktop {
		ids = append(ids, apps.ClaudeDesktop)
	}
	if req.TouchGrok {
		ids = append(ids, apps.Grok)
	}
	if req.TouchGemini {
		ids = append(ids, apps.Gemini)
	}
	return ids
}

func configuredSummary(claude, codex bool, report apply.Report) string {
	var names []string
	if claude {
		names = append(names, "Claude Code")
	}
	if report.DesktopProfile != "" {
		names = append(names, "Claude Desktop")
	}
	if codex {
		names = append(names, "Codex / ChatGPT")
	}
	if report.GrokPath != "" {
		names = append(names, "Grok Build")
	}
	if report.GeminiPath != "" {
		names = append(names, "Gemini CLI")
	}
	return strings.Join(names, ", ")
}

func touchedIDs(claude, codex bool, report apply.Report) []apps.ID {
	var ids []apps.ID
	if claude {
		ids = append(ids, apps.ClaudeCode)
	}
	if report.DesktopProfile != "" {
		ids = append(ids, apps.ClaudeDesktop)
	}
	if codex {
		ids = append(ids, apps.Codex)
	}
	if report.GrokPath != "" {
		ids = append(ids, apps.Grok)
	}
	if report.GeminiPath != "" {
		ids = append(ids, apps.Gemini)
	}
	return ids
}

func (e env) saveProfile(prof store.Profile, req apply.Request) (apply.Report, error) {
	req.ClaudePath = e.layout.ClaudePath
	req.CodexPath = e.layout.CodexPath
	req.GrokPath = e.layout.GrokPath
	req.GeminiPath = e.layout.GeminiPath
	req.RCFile = e.layout.RCFile
	req.Shell = e.layout.Shell
	prof.Provider = provider.NormalizeID(prof.Provider)
	req.ProviderID = prof.Provider
	req.BaseURL = prof.BaseURL
	req.APIKey = prof.APIKey
	req.Label = prof.Label
	req.ClaudeModel = prof.ClaudeModel
	req.CodexModel = prof.CodexModel
	req.DesktopModel = prof.DesktopModel
	req.GrokModel = prof.GrokModel
	req.GeminiModel = prof.GeminiModel
	req.WireAPI = prof.WireAPI
	req.DesktopDir = e.layout.DesktopLibrary
	req.Backups = e.backups()
	req, err := e.attachShell(req)
	if err != nil {
		return apply.Report{}, err
	}
	if prof.ClaudeKey != "" {
		req.ClaudeKey = prof.ClaudeKey
	}
	if prof.DesktopKey != "" {
		req.DesktopKey = prof.DesktopKey
	}
	if prof.CodexKey != "" {
		req.CodexKey = prof.CodexKey
	}
	if prof.GrokKey != "" {
		req.GrokKey = prof.GrokKey
	}
	if prof.GeminiKey != "" {
		req.GeminiKey = prof.GeminiKey
	}
	report, err := apply.Apply(req)
	if err != nil {
		return report, err
	}
	e.state.Upsert(prof)
	e.state.Active = prof.Name
	e.state.Apply.Selective = req.AppsExplicit
	e.state.Apply.Claude = req.TouchClaude
	e.state.Apply.Desktop = req.TouchDesktop || (!req.AppsExplicit && req.TouchClaude)
	e.state.Apply.Codex = req.TouchCodex
	e.state.Apply.Grok = req.TouchGrok
	e.state.Apply.Gemini = req.TouchGemini
	e.state.Apply.Shell = req.TouchShell
	e.state.Apply.ClaudePath = e.layout.ClaudePath
	e.state.Apply.CodexPath = e.layout.CodexPath
	e.state.Apply.GrokPath = e.layout.GrokPath
	e.state.Apply.GeminiPath = e.layout.GeminiPath
	if e.userEnv {
		e.state.Apply.ShellKind = "userenv"
		e.state.Apply.RCFile = ""
		e.layout.Shell = "userenv"
		e.layout.RCFile = ""
	} else {
		e.state.Apply.ShellKind = e.layout.Shell
		e.state.Apply.RCFile = e.layout.RCFile
	}
	if err := store.Save(e.stateP, e.state); err != nil {
		return report, err
	}
	return report, nil
}

func printSetup(e env, prof store.Profile, report apply.Report, claude, codex, shell, hints bool) {
	if summary := configuredSummary(claude, codex, report); summary != "" {
		fmt.Fprintf(e.stdout, "Configured %s.\n", summary)
		if prof.Name != "" {
			fmt.Fprintf(e.stdout, "Profile   %s (%s)\n", prof.Name, prof.Label)
		}
	} else {
		fmt.Fprintf(e.stdout, "Configured %s (%s).\n", prof.Name, prof.Label)
	}
	fmt.Fprintf(e.stdout, "Key       %s\n", textutil.Mask(prof.APIKey))
	fmt.Fprintf(e.stdout, "Gateway   %s\n", report.AnthropicBase)
	if codex {
		fmt.Fprintf(e.stdout, "Codex URL %s\n", report.OpenAIBase)
	}
	fmt.Fprintln(e.stdout)
	if claude {
		fmt.Fprintf(e.stdout, "  Claude Code  %s   model %s\n", e.show(e.layout.ClaudePath), modelLabel(prof.ClaudeModel))
	}
	if codex {
		fmt.Fprintf(e.stdout, "  Codex / ChatGPT %s   model %s\n", e.show(e.layout.CodexPath), modelLabel(prof.CodexModel))
	}
	if shell {
		if e.userEnv {
			fmt.Fprintf(e.stdout, "  User environment  %s\n", userenv.Location)
		} else {
			fmt.Fprintf(e.stdout, "  Shell        %s\n", e.show(e.layout.RCFile))
		}
	}
	if report.DesktopProfile != "" {
		model := prof.DesktopModel
		if model == "" {
			model = prof.ClaudeModel
		}
		fmt.Fprintf(e.stdout, "  Claude Desktop %s   model %s\n", e.show(report.DesktopProfile), modelLabel(model))
	}
	if report.GrokPath != "" {
		fmt.Fprintf(e.stdout, "  Grok Build   %s   model %s\n", e.show(report.GrokPath), modelLabel(prof.GrokModel))
	}
	if report.GeminiPath != "" {
		fmt.Fprintf(e.stdout, "  Gemini CLI   %s   model %s\n", e.show(report.GeminiPath), modelLabel(prof.GeminiModel))
	}
	if len(report.Changed) == 0 {
		fmt.Fprintln(e.stdout, "\nAlready up to date.")
	}
	if !hints {
		return
	}
	noticePrograms(e.stdout, touchedIDs(claude, codex, report))
	if shell {
		if e.userEnv {
			fmt.Fprintln(e.stdout, "\nOpen a new terminal to pick up the user environment variables.")
		} else {
			fmt.Fprintf(e.stdout, "\n%s\n", sourceHint(e.layout.Shell, e.show(e.layout.RCFile)))
		}
	}
	if line := reopenHint(codex, report.DesktopProfile != ""); line != "" {
		fmt.Fprintln(e.stdout, line)
	}
	if report.GrokPath != "" {
		fmt.Fprintln(e.stdout, "Start a new Grok Build session so it reads config.toml.")
	}
	if report.GeminiPath != "" {
		fmt.Fprintln(e.stdout, "Open a new terminal, then run gemini. It reads GEMINI_API_KEY and GOOGLE_GEMINI_BASE_URL from the environment.")
	}
	fmt.Fprintln(e.stdout, "\nThen check the gateway:")
	fmt.Fprintln(e.stdout, "  deeprouter test")
	fmt.Fprintln(e.stdout, "  deeprouter models")
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
	profile := fs.String("profile", "", "profile name to activate")
	claudeModel := fs.String("claude-model", "", "Claude Code model id")
	desktopModel := fs.String("claude-desktop-model", "", "Claude Desktop model id")
	codexModel := fs.String("codex-model", "", "Codex model id")
	grokModel := fs.String("grok-model", "", "Grok Build model id")
	geminiModel := fs.String("gemini-model", "", "Gemini CLI model id")
	wire := fs.String("codex-wire-api", "", "Codex provider protocol: responses or chat")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	set := visited(fs)
	if !set["profile"] && !set["claude-model"] && !set["claude-desktop-model"] && !set["codex-model"] && !set["grok-model"] && !set["gemini-model"] && !set["codex-wire-api"] {
		fmt.Fprint(stderr, switchHelp)
		return 2, fmt.Errorf("pass --profile, a model flag, or --codex-wire-api")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	name := e.state.Active
	if set["profile"] {
		name = *profile
	}
	clearClaude, clearDesktop, clearCodex, clearGrok, clearGemini := false, false, false, false, false
	if name == "" {
		return 2, fmt.Errorf("no active profile. Run deeprouter setup first")
	}
	prof, ok := e.state.Find(name)
	if !ok {
		return 2, fmt.Errorf("no profile %q. Create it with: deeprouter profile add %s --api-key \"$DEEPROUTER_API_KEY\"", name, name)
	}
	if set["claude-model"] {
		prof.ClaudeModel, clearClaude, _ = takeModel(*claudeModel, true)
	}
	if set["claude-desktop-model"] {
		prof.DesktopModel, clearDesktop, _ = takeModel(*desktopModel, true)
	} else if clearClaude {
		prof.DesktopModel = ""
		clearDesktop = true
	}
	if set["codex-model"] {
		prof.CodexModel, clearCodex, _ = takeModel(*codexModel, true)
	}
	if set["grok-model"] {
		prof.GrokModel, clearGrok, _ = takeModel(*grokModel, true)
	}
	if set["gemini-model"] {
		prof.GeminiModel, clearGemini, _ = takeModel(*geminiModel, true)
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
	return e.applyProfile(prof, true, modelClears{clearClaude, clearDesktop, clearCodex, clearGrok, clearGemini})
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
	providerFlag := fs.String("provider", "", "gateway preset: deeprouter, wolfai, or linkmesh")
	apiKey := fs.String("api-key", "", "API key")
	keyStdin := fs.Bool("api-key-stdin", false, "read the API key from stdin")
	label := fs.String("label", "", "profile label")
	claudeModel := fs.String("claude-model", "", "Claude Code model id")
	codexModel := fs.String("codex-model", "", "Codex model id")
	wire := fs.String("codex-wire-api", "", "Codex provider protocol: responses or chat")
	use := fs.Bool("use", false, "make this the active profile and write tool config")
	positional, flagArgs, err := splitFlags(args, map[string]bool{
		"base-url":       true,
		"provider":       true,
		"api-key":        true,
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
		return 2, fmt.Errorf("usage: deeprouter profile add <name>")
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
	}
	if labelValue == "" {
		labelValue = name
	}
	providerID, baseURL, providerExplicit, err := resolveGateway(*providerFlag, set["provider"], set["base-url"], *base, existing.Provider, existing.BaseURL)
	if err != nil {
		return 2, err
	}
	if providerExplicit {
		labelValue = providerLabel(labelValue, providerID)
	}
	key, err := resolveKey(*apiKey, *keyStdin, set["api-key"], stdin, stderr, existing.APIKey, true)
	if err != nil {
		return 2, err
	}
	if _, err := gateway.ParseBase(baseURL); err != nil {
		return 2, err
	}
	claude := existing.ClaudeModel
	clearClaude := false
	if set["claude-model"] {
		claude, clearClaude, _ = takeModel(*claudeModel, true)
	}
	codexModelValue := existing.CodexModel
	clearCodex := false
	if set["codex-model"] {
		codexModelValue, clearCodex, _ = takeModel(*codexModel, true)
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
		Provider:    providerID,
		APIKey:      key,
		ClaudeModel: claude,
		CodexModel:  codexModelValue,
		WireAPI:     wireAPI,
	}
	e.state.Upsert(prof)
	if *use {
		return e.applyProfile(prof, true, modelClears{clearClaude: clearClaude, clearCodex: clearCodex})
	}
	if err := store.Save(e.stateP, e.state); err != nil {
		return 1, err
	}
	fmt.Fprintf(stdout, "Saved profile %s (%s). Key %s.\n", prof.Name, prof.Label, textutil.Mask(prof.APIKey))
	fmt.Fprintf(stdout, "Activate it with:  deeprouter profile use %s\n", prof.Name)
	return 0, nil
}

func cmdProfileUse(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("profile use", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, "Usage: deeprouter profile use <name>\n") }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if fs.NArg() != 1 {
		return 2, fmt.Errorf("usage: deeprouter profile use <name>")
	}
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	prof, ok := e.state.Find(fs.Arg(0))
	if !ok {
		return 2, fmt.Errorf("no profile %q", fs.Arg(0))
	}
	return e.applyProfile(prof, true, modelClears{})
}

func cmdProfileRemove(args []string, stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	fs := flag.NewFlagSet("profile remove", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprint(stderr, "Usage: deeprouter profile remove <name>\n") }
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0, nil
		}
		return 2, nil
	}
	if fs.NArg() != 1 {
		return 2, fmt.Errorf("usage: deeprouter profile remove <name>")
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

type modelClears struct {
	clearClaude  bool
	clearDesktop bool
	clearCodex   bool
	clearGrok    bool
	clearGemini  bool
}

func (e env) applyProfile(prof store.Profile, requirePrior bool, clears modelClears) (int, error) {
	if prof.WireAPI == "" {
		prof.WireAPI = "responses"
	}
	prof.Provider = provider.NormalizeID(prof.Provider)
	wire, err := normalizeWire(prof.WireAPI)
	if err != nil {
		return 2, err
	}
	prof.WireAPI = wire
	touchClaude := e.state.Apply.Claude
	touchCodex := e.state.Apply.Codex
	touchShell := e.state.Apply.Shell
	touchDesktop := e.state.Apply.Desktop
	touchGrok := e.state.Apply.Grok
	touchGemini := e.state.Apply.Gemini
	if !touchClaude && !touchCodex && !touchShell && !touchDesktop && !touchGrok && !touchGemini {
		if requirePrior {
			touchClaude, touchCodex, touchShell = true, true, true
			touchDesktop = true
		}
	}
	if !e.state.Apply.Selective {
		touchDesktop = touchClaude
		touchGrok = false
		touchGemini = false
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
	grokPath := e.layout.GrokPath
	if e.state.Apply.GrokPath != "" {
		grokPath = e.state.Apply.GrokPath
	}
	geminiPath := e.layout.GeminiPath
	if e.state.Apply.GeminiPath != "" {
		geminiPath = e.state.Apply.GeminiPath
	}
	req := apply.Request{
		ClaudePath:        claudePath,
		CodexPath:         codexPath,
		GrokPath:          grokPath,
		GeminiPath:        geminiPath,
		RCFile:            rc,
		Shell:             shell,
		BaseURL:           prof.BaseURL,
		APIKey:            prof.APIKey,
		Label:             prof.Label,
		ProviderID:        provider.NormalizeID(prof.Provider),
		ClaudeModel:       prof.ClaudeModel,
		CodexModel:        prof.CodexModel,
		DesktopModel:      prof.DesktopModel,
		GrokModel:         prof.GrokModel,
		GeminiModel:       prof.GeminiModel,
		WireAPI:           prof.WireAPI,
		ClaudeKey:         prof.ClaudeKey,
		DesktopKey:        prof.DesktopKey,
		CodexKey:          prof.CodexKey,
		GrokKey:           prof.GrokKey,
		GeminiKey:         prof.GeminiKey,
		TouchClaude:       touchClaude,
		TouchDesktop:      touchDesktop,
		TouchCodex:        touchCodex,
		TouchGrok:         touchGrok,
		TouchGemini:       touchGemini,
		TouchShell:        touchShell,
		AppsExplicit:      e.state.Apply.Selective,
		SetClaudeModel:    prof.ClaudeModel != "" && !clears.clearClaude,
		SetDesktopModel:   prof.DesktopModel != "" && !clears.clearDesktop,
		SetCodexModel:     prof.CodexModel != "" && !clears.clearCodex,
		SetGrokModel:      prof.GrokModel != "" && !clears.clearGrok,
		SetGeminiModel:    prof.GeminiModel != "" && !clears.clearGemini,
		ClearClaudeModel:  clears.clearClaude,
		ClearDesktopModel: clears.clearDesktop,
		ClearCodexModel:   clears.clearCodex,
		ClearGrokModel:    clears.clearGrok,
		ClearGeminiModel:  clears.clearGemini,
		DesktopDir:        e.layout.DesktopLibrary,
		Backups:           e.backups(),
	}
	req, err = e.attachShell(req)
	if err != nil {
		return 1, err
	}
	report, err := apply.Apply(req)
	if err != nil {
		return 1, err
	}
	e.state.Upsert(prof)
	e.state.Active = prof.Name
	e.state.Apply.Claude = touchClaude
	e.state.Apply.Desktop = touchDesktop
	e.state.Apply.Codex = touchCodex
	e.state.Apply.Grok = touchGrok
	e.state.Apply.Gemini = touchGemini
	e.state.Apply.Shell = touchShell
	e.state.Apply.ClaudePath = claudePath
	e.state.Apply.CodexPath = codexPath
	e.state.Apply.GrokPath = grokPath
	e.state.Apply.GeminiPath = geminiPath
	if e.userEnv {
		e.state.Apply.RCFile = ""
		e.state.Apply.ShellKind = "userenv"
		rc = ""
		shell = "userenv"
	} else {
		e.state.Apply.RCFile = rc
		e.state.Apply.ShellKind = shell
	}
	if err := store.Save(e.stateP, e.state); err != nil {
		return 1, err
	}
	e.layout.ClaudePath = claudePath
	e.layout.CodexPath = codexPath
	e.layout.RCFile = rc
	e.layout.Shell = shell
	printSetup(e, prof, report, touchClaude, touchCodex, touchShell || touchGemini, true)
	return 0, nil
}

func printProfiles(e env) int {
	if len(e.state.Profiles) == 0 {
		fmt.Fprintln(e.stdout, "No profiles yet.")
		fmt.Fprintln(e.stdout, "Create one with:  deeprouter setup --api-key \"$DEEPROUTER_API_KEY\"")
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

func cmdStatus(args []string, stdout, stderr io.Writer) (int, error) {
	fs := flag.NewFlagSet("status", flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { fmt.Fprintln(stderr, "Usage: deeprouter status") }
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
		if *asJSON {
			payload := map[string]any{
				"profile": "",
				"clis":    cliStatusPayload(),
			}
			enc := json.NewEncoder(stdout)
			enc.SetIndent("", "  ")
			if err := enc.Encode(payload); err != nil {
				return 1, err
			}
			return 0, nil
		}
		fmt.Fprintln(stdout, "No active profile. Run deeprouter setup.")
		fmt.Fprintln(stdout)
		printCLIStatus(stdout)
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
	shellWired, shellShown := e.shellStatus(rc)
	grokText := readText(e.layout.GrokPath)
	grokWired := strings.Contains(grokText, ep.OpenAIBase) && strings.Contains(grokText, "[model.deeprouter]")
	geminiPath := e.layout.GeminiPath
	if e.state.Apply.GeminiPath != "" {
		geminiPath = e.state.Apply.GeminiPath
	}
	geminiText := readText(geminiPath)
	geminiWired := strings.Contains(geminiText, gemini.SelectedAuth)
	desktopPath, desktopWired := claudedesktop.Applied(e.layout.DesktopLibrary, ep.AnthropicBase)
	if *asJSON {
		payload := map[string]any{
			"profile":       prof.Name,
			"label":         prof.Label,
			"base_url":      ep.AnthropicBase,
			"openai_base":   ep.OpenAIBase,
			"key":           textutil.Mask(prof.APIKey),
			"claude_model":  prof.ClaudeModel,
			"codex_model":   prof.CodexModel,
			"wire_api":      prof.WireAPI,
			"claude_path":   claudePath,
			"codex_path":    codexPath,
			"rc_file":       shellShown,
			"desktop_path":  desktopPath,
			"grok_path":     e.layout.GrokPath,
			"gemini_path":   geminiPath,
			"gemini_model":  prof.GeminiModel,
			"claude_wired":  strings.Contains(claudeText, ep.AnthropicBase),
			"codex_wired":   codexWired(codexText),
			"shell_wired":   shellWired,
			"desktop_wired": desktopWired,
			"grok_wired":    grokWired,
			"gemini_wired":  geminiWired,
			"clis":          cliStatusPayload(),
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
	fmt.Fprintf(stdout, "Codex / ChatGPT %s\n", e.show(codexPath))
	fmt.Fprintf(stdout, "  model      %s\n", emptyAs(prof.CodexModel, "not set"))
	fmt.Fprintf(stdout, "  provider   %s\n", yesNo(codexWired(codexText)))
	fmt.Fprintf(stdout, "Shell        %s\n", e.show(shellShown))
	fmt.Fprintf(stdout, "  managed    %s\n", yesNo(shellWired))
	fmt.Fprintf(stdout, "Claude Desktop %s\n", e.show(desktopPath))
	fmt.Fprintf(stdout, "  gateway    %s\n", yesNo(desktopWired))
	fmt.Fprintf(stdout, "Grok Build   %s\n", e.show(e.layout.GrokPath))
	fmt.Fprintf(stdout, "  provider   %s\n", yesNo(grokWired))
	fmt.Fprintf(stdout, "Gemini CLI   %s\n", e.show(geminiPath))
	fmt.Fprintf(stdout, "  model      %s\n", emptyAs(prof.GeminiModel, "app default"))
	fmt.Fprintf(stdout, "  api key    %s\n", yesNo(geminiWired))
	fmt.Fprintln(stdout)
	printCLIStatus(stdout)
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
	restored, err := e.restoreBackups(*id)
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
	fmt.Fprintln(stdout, "Profiles were kept. Run deeprouter setup to apply one again.")
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
	restored, err := e.restoreBackups("original")
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
		fmt.Fprintln(stdout, "Delete them with:  deeprouter uninstall --purge")
	}
	return 0, nil
}

type target struct {
	name         string
	label        string
	baseURL      string
	key          string
	claudeModel  string
	codexModel   string
	desktopModel string
	grokModel    string
	geminiModel  string
	claudeKey    string
	codexKey     string
	desktopKey   string
	grokKey      string
	geminiKey    string
	scoped       bool
	probeModels  bool
	probeChat    bool
	probeGrok    bool
	probeClaude  bool
	probeDesktop bool
	probeGemini  bool
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
	scoped, probeModels, probeChat, probeGrok, probeClaude, probeDesktop, probeGemini := scopeFromApply(e.state.Apply)
	return target{
		name:         outName,
		label:        label,
		baseURL:      baseURL,
		key:          key,
		claudeModel:  claudeModel,
		codexModel:   codexModel,
		desktopModel: prof.DesktopModel,
		grokModel:    prof.GrokModel,
		geminiModel:  prof.GeminiModel,
		claudeKey:    prof.ClaudeKey,
		codexKey:     prof.CodexKey,
		desktopKey:   prof.DesktopKey,
		grokKey:      prof.GrokKey,
		geminiKey:    prof.GeminiKey,
		scoped:       scoped,
		probeModels:  probeModels,
		probeChat:    probeChat,
		probeGrok:    probeGrok,
		probeClaude:  probeClaude,
		probeDesktop: probeDesktop,
		probeGemini:  probeGemini,
	}, nil
}

// scopeFromApply chooses connectivity checks for `deeprouter test`.
// A profile with no saved programs keeps the historical models, chat, and
// messages checks. Otherwise only the configured programs are tested.
func scopeFromApply(a store.Apply) (scoped, models, chat, grok, claude, desktop, gemini bool) {
	configured := a.Claude || a.Desktop || a.Codex || a.Grok || a.Gemini
	if !configured {
		return false, false, false, false, false, false, false
	}
	return true, a.Codex, a.Codex, a.Grok, a.Claude, a.Desktop, a.Gemini
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
	// setup, test, and profile add stay non-interactive when a key is already
	// known. The no-argument wizard is what asks to keep or replace it.
	if existing != "" {
		return existing, nil
	}
	if envKey := strings.TrimSpace(os.Getenv("DEEPROUTER_API_KEY")); envKey != "" {
		return textutil.ValidateKey(envKey)
	}
	if allowPrompt && stdinIsTerminal(stdin) {
		key, err := promptKey(stdin, stderr)
		if err != nil {
			return "", err
		}
		return textutil.ValidateKey(key)
	}
	return "", fmt.Errorf("API key required. Pass --api-key, set DEEPROUTER_API_KEY, or use --api-key-stdin")
}

func chooseIdentity(st store.State, profile, label string, profileSet, labelSet bool) (string, string, error) {
	name := ""
	switch {
	case profileSet:
		name = profile
	case st.Active != "":
		name = st.Active
	default:
		name = "default"
	}
	if err := store.ValidName(name); err != nil {
		return "", "", err
	}
	existing, _ := st.Find(name)
	if labelSet {
		if strings.TrimSpace(label) == "" {
			return "", "", fmt.Errorf("label is empty")
		}
		return name, label, nil
	}
	if existing.Label != "" {
		return name, existing.Label, nil
	}
	if name == "default" {
		return name, "DeepRouter", nil
	}
	return name, name, nil
}

// takeModel interprets a model flag. "default" clears the model field.
func takeModel(raw string, explicit bool) (value string, clear bool, set bool) {
	if !explicit {
		return "", false, false
	}
	if strings.EqualFold(strings.TrimSpace(raw), "default") {
		return "", true, true
	}
	return strings.TrimSpace(raw), false, true
}

func modelLabel(model string) string {
	if strings.TrimSpace(model) == "" {
		return "app default"
	}
	return model
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
		}
		if !check.OK {
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
	if check.Skipped {
		return "skipped"
	}
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

// stdinIsTerminal reports whether stdin can take a masked prompt.
// Tests replace it; a pipe or a non-file reader is not a terminal.
// Windows uses the console API (see term_windows.go). There is no /dev/tty.
var stdinIsTerminal = isTerminal

// echoOff turns off terminal echo while a secret is typed.
// The restore func turns echo back on. ok is false when echo cannot be changed.
var echoOff = disableTerminalEcho

func promptKey(stdin io.Reader, stderr io.Writer) (string, error) {
	return promptSecretLine(stdin, stderr, bufio.NewReader(stdin), "API key: ")
}

// promptSecretLine prints prompt and reads one line. On a terminal the typed
// characters are not echoed. in must read from stdin so later prompts see the
// rest of the input.
func promptSecretLine(stdin io.Reader, stderr io.Writer, in *bufio.Reader, prompt string) (string, error) {
	fmt.Fprint(stderr, prompt)
	if f, ok := stdin.(*os.File); ok && stdinIsTerminal(f) {
		line, err := readMasked(f, stderr)
		if err == nil || !errors.Is(err, errMaskedUnavailable) {
			line = strings.TrimSpace(line)
			if err == nil && line != "" {
				fmt.Fprintf(stderr, "Entered (%s)\n", textutil.MaskConfirm(line))
			}
			return line, err
		}
	}
	line, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	line = strings.TrimSpace(line)
	if line != "" && stdinIsTerminal(stdin) {
		fmt.Fprintf(stderr, "Entered (%s)\n", textutil.MaskConfirm(line))
	}
	return line, nil
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
	for _, id := range provider.ManagedIDs() {
		if strings.Contains(text, "[model_providers."+id+"]") ||
			strings.Contains(text, "model_providers.\""+id+"\"") ||
			strings.Contains(text, "model_provider = \""+id+"\"") {
			return true
		}
	}
	return false
}

func resolveGateway(providerArg string, providerSet, baseSet bool, baseArg, existingProvider, existingBase string) (id, baseURL string, explicit bool, err error) {
	id = provider.NormalizeID(existingProvider)
	if providerSet {
		p, err := provider.Parse(providerArg)
		if err != nil {
			return "", "", false, err
		}
		id = p.ID
		explicit = true
	} else if env := strings.TrimSpace(os.Getenv(provider.EnvProvider)); env != "" {
		p, err := provider.Parse(env)
		if err != nil {
			return "", "", false, err
		}
		id = p.ID
		explicit = true
	}
	switch {
	case baseSet:
		baseURL = baseArg
	case explicit:
		if p, ok := provider.ByID(id); ok {
			baseURL = p.BaseURL
		} else if strings.TrimSpace(existingBase) != "" {
			baseURL = existingBase
		} else {
			baseURL = provider.Default().BaseURL
		}
	case strings.TrimSpace(existingBase) != "":
		baseURL = existingBase
	default:
		if p, ok := provider.ByID(id); ok {
			baseURL = p.BaseURL
		} else {
			baseURL = provider.Default().BaseURL
		}
	}
	return id, baseURL, explicit, nil
}

func providerLabel(current, id string) string {
	if provider.KnownLabel(current) {
		return provider.DisplayName(id)
	}
	return current
}

const setupHelp = `Usage: deeprouter setup [flags]

Write Claude Code, Codex / ChatGPT, Claude Desktop, and shell configuration for one gateway profile.
Running setup again updates the same files in place.

Flags:
  --provider NAME        deeprouter (default), wolfai, or linkmesh.
                         DEEPROUTER_PROVIDER selects the same preset when
                         this flag is omitted. --base-url still overrides
                         the URL and keeps the chosen provider id.
  --base-url URL         Gateway base URL (default https://deeprouter.top,
                         or the selected provider's URL).
                         Any OpenAI- or Anthropic-compatible relay URL works.
  --api-key KEY          API key. Visible in the process list; prefer the env var.
  --api-key-stdin        Read the API key from stdin
  --profile NAME         Profile name (default: active profile, or "default")
  --label TEXT           Label stored with the profile
  --apps LIST            claude-code, claude-desktop, codex, grok, gemini.
                         Only those programs are written. Without --apps, setup
                         still writes Claude Code, Claude Desktop, Codex / ChatGPT, and
                         the shell startup file. Gemini is not included unless
                         it is listed or chosen in the interactive picker.
  --claude-model ID      ANTHROPIC_MODEL for Claude Code.
                         "default" removes it and uses the app default.
  --claude-desktop-model ID
                         Model for Claude Desktop. Defaults to --claude-model.
                         "default" removes the model and uses the app default.
  --codex-model ID       model for Codex / ChatGPT. "default" removes it.
  --grok-model ID        model id sent by Grok Build. "default" removes it.
                         When a model flag is omitted, setup picks one from
                         GET /v1/models (claude, gpt, grok, or gemini in the id).
  --gemini-model ID      model.name in Gemini CLI settings. "default" removes it
                         so Gemini CLI uses its own default model.
  --claude-code-key KEY  API key for Claude Code. Defaults to --api-key.
  --claude-desktop-key KEY
  --codex-key KEY
  --grok-key KEY
  --gemini-key KEY       API key stored as GEMINI_API_KEY. Defaults to --api-key.
  --codex-wire-api KIND  responses (default) or chat
  --claude-only          Do not edit Codex / ChatGPT
  --codex-only           Do not edit Claude Code or Claude Desktop
  --no-shell             Do not edit the shell startup file. --apps never edits it.
  --install-clis         Install missing npm CLIs for the programs being configured
                         (claude, codex, gemini). Grok Build and Claude Desktop
                         keep their own installers. Nothing is installed unless
                         this flag is set.
  --shell KIND           bash, zsh, fish, or powershell
  --rc-file PATH         Startup file to edit. On Windows this opts out of
                         the user environment (HKCU\Environment).

The API key is read from --api-key, --api-key-stdin, DEEPROUTER_API_KEY,
an existing profile, or an interactive prompt.

Claude Code receives ANTHROPIC_BASE_URL without a trailing /v1.
Codex / ChatGPT receives base_url with /v1 and experimental_bearer_token in
config.toml (mode 0600), so Codex / ChatGPT does not need
DEEPROUTER_API_KEY in the process environment. The shell startup file still
exports that variable for terminals. Claude Desktop (Chat, Cowork, and the
Code tab) reads its own local third-party configuration, not the shell and
not settings.json, for the gateway. A signed-in Claude.ai chat does not.

Grok Build reads ~/.grok/config.toml ($GROK_HOME/config.toml). Setup writes
[model.<provider>] with base_url including /v1, api_key, and
api_backend = chat_completions. The section id matches --provider
(deeprouter by default). Codex / ChatGPT's CLI and desktop app share
config.toml, so the desktop app is not a separate file.

Gemini CLI reads GEMINI_API_KEY and GOOGLE_GEMINI_BASE_URL from the
environment (the shell startup file, or HKCU\Environment on Windows).
The base URL is the gateway origin. @google/genai appends
/v1beta/models/{model}:generateContent, so the value is https://deeprouter.top
with no /v1 suffix. ~/.gemini/settings.json ($GEMINI_CLI_HOME/.gemini/settings.json)
gets security.auth.selectedType = gemini-api-key, and model.name when a model
is chosen. Other settings in that file are left in place. Gemini CLI requires
HTTPS except for localhost, 127.0.0.1, and [::1].
`

const testHelp = `Usage: deeprouter test [flags]

Call GET /v1/models, a one-token chat completion, and a one-token
Anthropic messages request. When Gemini CLI is configured, also call
POST /v1beta/models/{model}:generateContent with x-goog-api-key.
Prints HTTP status, time to first byte, and total time.

Flags:
  --profile NAME
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --claude-model ID
  --codex-model ID
  --timeout DURATION   (default 30s; TLS handshake at least 20s)
  --json
`

const modelsHelp = `Usage: deeprouter models [flags]

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

const switchHelp = `Usage: deeprouter switch [flags]

Activate a profile, change a model, or both. Tool config files are updated
to match.

Flags:
  --profile NAME
  --claude-model ID          or "default" to use the app default
  --claude-desktop-model ID  or "default"
  --codex-model ID           or "default"
  --grok-model ID            or "default"
  --gemini-model ID          or "default"
  --codex-wire-api KIND
`

const profileHelp = `Usage: deeprouter profile <command>

  list              Show saved profiles
  add <name>        Save a profile without wiring tools (pass --use to wire them)
  use <name>        Make a profile active and rewrite tool config
  remove <name>     Delete a profile that is not active

A profile stores a base URL, an API key, and optional model ids.
`

const profileAddHelp = `Usage: deeprouter profile add <name> [flags]

Flags:
  --provider NAME       deeprouter, wolfai, or linkmesh
  --base-url URL
  --api-key KEY
  --api-key-stdin
  --label TEXT
  --claude-model ID
  --codex-model ID
  --codex-wire-api KIND
  --use                 Activate the profile after saving it
`

const restoreHelp = `Usage: deeprouter restore [flags]

Put every file deeprouter changed back to the copy taken before the
first edit. Later snapshots undo only the most recent edit.

Flags:
  --list
  --id original|latest|BACKUP
`

const uninstallHelp = `Usage: deeprouter uninstall [flags]

Restore config files to the pre-setup copies.
With --purge, also delete ~/.deeprouter (profiles and backups).

Flags:
  --purge
`
