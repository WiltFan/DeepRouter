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

	"github.com/zfg5118/deeprouter/internal/apply"
	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/claudedesktop"
	"github.com/zfg5118/deeprouter/internal/codex"
	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/grok"
	"github.com/zfg5118/deeprouter/internal/provider"
	"github.com/zfg5118/deeprouter/internal/shellrc"
	"github.com/zfg5118/deeprouter/internal/store"
	"github.com/zfg5118/deeprouter/internal/textutil"
	"github.com/zfg5118/deeprouter/internal/userenv"
)

// cmdWizard is the no-argument setup flow used from a terminal.
func cmdWizard(stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	in := bufio.NewReader(stdin)
	terminal := stdinIsTerminal(e.stdin)
	zh := terminal && promptLocaleZH()
	providerID := provider.Default().ID
	emptyDefault := gateway.DefaultBaseURL
	if p, explicit, err := providerFromEnv(); err != nil {
		return 2, err
	} else if explicit {
		providerID = p.ID
		emptyDefault = p.BaseURL
	}
	var selection []apps.ID
	var baseURL string
	if terminal {
		id, custom, err := promptPlatform(in, stderr, zh)
		if err != nil {
			return 2, err
		}
		providerID = id
		fmt.Fprintln(stderr)
		fmt.Fprintln(stderr, programIntro(zh))
		selection, err = promptPrograms(in, stderr)
		if err != nil {
			return 2, err
		}
		noticePrograms(stderr, selection)
		fmt.Fprintln(stderr)
		if custom {
			baseURL, err = promptGateway(in, stderr, true, gateway.DefaultBaseURL)
			if err != nil {
				return 2, err
			}
		} else {
			chosen, _ := provider.ByID(id)
			baseURL = chosen.BaseURL
			fmt.Fprintf(stderr, "Gateway %s\n", baseURL)
		}
	} else {
		fmt.Fprintln(stderr, "Set up Codex / ChatGPT. Press Enter to accept a default.")
		selection = apps.DefaultIDs()
		noticePrograms(stderr, selection)
		fmt.Fprintln(stderr)
		baseURL, err = promptGateway(in, stderr, false, emptyDefault)
		if err != nil {
			return 2, err
		}
	}

	type chosen struct {
		id    apps.ID
		key   string
		model string
		clear bool
	}
	var choices []chosen
	if !terminal {
		key, err := wizardKey(e, in)
		if err != nil {
			return 2, err
		}
		fmt.Fprintf(stderr, "Fetching models from %s ...\n", baseURL)
		catalog, err := fetchModels(baseURL, key, 30*time.Second)
		if err != nil {
			return 1, fmt.Errorf("could not list models: %s", err.Error())
		}
		fmt.Fprintf(stderr, "%d models.\n\n", len(catalog))
		for _, id := range selection {
			info, _ := apps.ByID(id)
			model, clear, err := promptModel(in, stderr, info.Title, info.ModelKind, catalog)
			if err != nil {
				return 2, err
			}
			choices = append(choices, chosen{id: id, key: key, model: model, clear: clear})
		}
	} else {
		var cachedKey string
		var cachedModels []gateway.Model
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
				fmt.Fprintf(stderr, "Kept (%s)\n", textutil.MaskConfirm(existing))
			} else {
				key, err = textutil.ValidateKey(line)
				if err != nil {
					return 2, err
				}
			}
			if !haveIDs || key != cachedKey {
				fmt.Fprintf(stderr, "Fetching models from %s ...\n", baseURL)
				cachedModels, err = fetchModels(baseURL, key, 30*time.Second)
				if err != nil {
					return 1, fmt.Errorf("could not list models: %s", err.Error())
				}
				fmt.Fprintf(stderr, "%d models.\n\n", len(cachedModels))
				cachedKey = key
				haveIDs = true
			}
			model, clear, err := promptModel(in, stderr, info.Title, info.ModelKind, cachedModels)
			if err != nil {
				return 2, err
			}
			choices = append(choices, chosen{id: id, key: key, model: model, clear: clear})
		}
	}
	if len(choices) == 0 {
		return 2, fmt.Errorf("no programs selected")
	}

	name := "default"
	label := "DeepRouter"
	if e.state.Active != "" {
		name = e.state.Active
		if existing, ok := e.state.Find(name); ok && existing.Label != "" {
			label = existing.Label
		}
	}
	label = providerLabel(label, providerID)
	prof := store.Profile{
		Name:     name,
		Label:    label,
		BaseURL:  baseURL,
		Provider: providerID,
		APIKey:   choices[0].key,
		WireAPI:  "responses",
	}
	req := apply.Request{AppsExplicit: terminal, TouchShell: !terminal}
	for _, choice := range choices {
		switch choice.id {
		case apps.ClaudeCode:
			req.TouchClaude = true
			req.SetClaudeModel = choice.model != "" && !choice.clear
			req.ClearClaudeModel = choice.clear
			prof.ClaudeModel = choice.model
			req.ClaudeKey = choice.key
			if choice.key != prof.APIKey {
				prof.ClaudeKey = choice.key
			}
		case apps.ClaudeDesktop:
			req.TouchDesktop = true
			req.SetDesktopModel = choice.model != "" && !choice.clear
			req.ClearDesktopModel = choice.clear
			prof.DesktopModel = choice.model
			req.DesktopKey = choice.key
			if choice.key != prof.APIKey {
				prof.DesktopKey = choice.key
			}
		case apps.Codex:
			req.TouchCodex = true
			req.SetCodexModel = choice.model != "" && !choice.clear
			req.ClearCodexModel = choice.clear
			prof.CodexModel = choice.model
			req.CodexKey = choice.key
			if choice.key != prof.APIKey {
				prof.CodexKey = choice.key
			}
		case apps.Grok:
			req.TouchGrok = true
			req.SetGrokModel = choice.model != "" && !choice.clear
			req.ClearGrokModel = choice.clear
			prof.GrokModel = choice.model
			req.GrokKey = choice.key
			if choice.key != prof.APIKey {
				prof.GrokKey = choice.key
			}
		case apps.Gemini:
			req.TouchGemini = true
			req.SetGeminiModel = choice.model != "" && !choice.clear
			req.ClearGeminiModel = choice.clear
			prof.GeminiModel = choice.model
			req.GeminiKey = choice.key
			if choice.key != prof.APIKey {
				prof.GeminiKey = choice.key
			}
		}
	}
	report, err := e.saveProfile(prof, req)
	if err != nil {
		return 1, err
	}
	printSetup(e, prof, report, req.TouchClaude, req.TouchCodex, req.TouchShell || req.TouchGemini, false)

	fmt.Fprintln(stdout)
	ep, probeReport, err := probeTarget(target{
		name:         prof.Name,
		label:        prof.Label,
		baseURL:      prof.BaseURL,
		key:          prof.APIKey,
		claudeModel:  prof.ClaudeModel,
		codexModel:   prof.CodexModel,
		desktopModel: prof.DesktopModel,
		grokModel:    prof.GrokModel,
		geminiModel:  prof.GeminiModel,
		claudeKey:    prof.ClaudeKey,
		codexKey:     prof.CodexKey,
		desktopKey:   prof.DesktopKey,
		grokKey:      prof.GrokKey,
		geminiKey:    prof.GeminiKey,
		scoped:       true,
		probeModels:  req.TouchCodex,
		probeChat:    req.TouchCodex,
		probeGrok:    req.TouchGrok,
		probeClaude:  req.TouchClaude,
		probeDesktop: req.TouchDesktop,
		probeGemini:  req.TouchGemini,
	}, 30*time.Second)
	if err != nil {
		return 1, err
	}
	printReport(stdout, target{
		name:  prof.Name,
		label: prof.Label,
		key:   prof.APIKey,
	}, ep, probeReport)
	cliErr := installMissingCLIs(e, true, in, false, selectedProgramIDs(req))
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
			fmt.Fprintf(e.stderr, "Kept (%s)\n", textutil.MaskConfirm(existing))
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
		readShellKey(e),
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

func readShellKey(e env) string {
	if e.userEnv {
		st := userenv.Active()
		if st == nil {
			return ""
		}
		v, ok, err := st.Get("DEEPROUTER_API_KEY")
		if err != nil || !ok {
			return ""
		}
		return v
	}
	b, err := os.ReadFile(e.layout.RCFile)
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
	for {
		for i, info := range apps.Catalog() {
			fmt.Fprintln(stderr, apps.FormatPickerLine(i+1, info))
		}
		line, err := readLine(in, stderr, "Programs [1]: ")
		if err != nil {
			return nil, err
		}
		selection, err := apps.ParseSelection(line, true)
		if err != nil {
			fmt.Fprintln(stderr, err.Error())
			continue
		}
		writable := apps.WritableOnly(selection)
		if len(writable) == 0 {
			fmt.Fprintln(stderr, "No configurable programs in that selection. Choose at least one.")
			continue
		}
		return writable, nil
	}
}

func readGeminiKey(e env) string {
	if e.userEnv {
		st := userenv.Active()
		if st == nil {
			return ""
		}
		v, ok, err := st.Get("GEMINI_API_KEY")
		if err != nil || !ok {
			return ""
		}
		return v
	}
	b, err := os.ReadFile(e.layout.RCFile)
	if err != nil {
		return ""
	}
	v, ok := shellrc.Lookup(string(b), "GEMINI_API_KEY")
	if !ok {
		return ""
	}
	return v
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
	case apps.Gemini:
		fromFile = readGeminiKey(e)
	}
	for _, candidate := range []string{fromFile, e.profileKey(string(id)), readShellKey(e), os.Getenv("DEEPROUTER_API_KEY")} {
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

// gatewayPrompt is the interactive base-URL question.
const gatewayPrompt = "Gateway URL (Enter for https://deeprouter.top, or any compatible endpoint):"

const gatewayPromptZH = "网关地址（直接回车使用 https://deeprouter.top，或填写其他兼容接口）："

const appDefaultModelLabel = "Use the app default (do not set a model)"

func programIntro(zh bool) string {
	if zh {
		return "选择要配置的程序。直接回车使用默认项。"
	}
	return "Choose which programs to configure. Press Enter for the default."
}

func promptGateway(in *bufio.Reader, stderr io.Writer, terminal bool, emptyDefault string) (string, error) {
	prompt := gatewayPrompt
	if terminal && promptLocaleZH() {
		prompt = gatewayPromptZH
	}
	if strings.TrimSpace(emptyDefault) == "" {
		emptyDefault = gateway.DefaultBaseURL
	}
	for {
		line, err := readLine(in, stderr, prompt)
		if err != nil {
			return "", err
		}
		base := strings.TrimSpace(line)
		if base == "" {
			base = emptyDefault
		}
		if _, err := gateway.ParseBase(base); err != nil {
			fmt.Fprintln(stderr, err.Error())
			if !terminal {
				return "", err
			}
			continue
		}
		return base, nil
	}
}

func promptModel(in *bufio.Reader, stderr io.Writer, label, kind string, models []gateway.Model) (string, bool, error) {
	matched := gateway.FamilyIDs(models, kind)
	fmt.Fprintf(stderr, "%s model (Enter for the app default):\n", label)
	fmt.Fprintf(stderr, "  1) %s\n", appDefaultModelLabel)
	for i, id := range matched {
		fmt.Fprintf(stderr, "  %d) %s\n", i+2, id)
	}
	if len(matched) == 0 {
		fmt.Fprintf(stderr, "No %s model id containing %q was listed. Type an id, or press Enter to use the app default.\n", label, kind)
	}
	line, err := readLine(in, stderr, fmt.Sprintf("%s model [1]: ", label))
	if err != nil {
		return "", false, err
	}
	return chooseModel(line, matched)
}

// chooseModel returns the selected id. clear is true for option 1, Enter, or "default".
func chooseModel(answer string, ids []string) (string, bool, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" || answer == "1" || strings.EqualFold(answer, "default") {
		return "", true, nil
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n < 1 || n > len(ids)+1 {
			return "", false, fmt.Errorf("model %d is out of range", n)
		}
		return ids[n-2], false, nil
	}
	return answer, false, nil
}

func providerFromEnv() (provider.Preset, bool, error) {
	env := strings.TrimSpace(os.Getenv(provider.EnvProvider))
	if env == "" {
		return provider.Preset{}, false, nil
	}
	p, err := provider.Parse(env)
	if err != nil {
		return provider.Preset{}, false, err
	}
	return p, true, nil
}

func platformDefault() (int, error) {
	p, explicit, err := providerFromEnv()
	if err != nil || !explicit {
		return 1, err
	}
	for i, item := range provider.All() {
		if item.ID == p.ID {
			return i + 1, nil
		}
	}
	return 1, nil
}

// promptLocaleZH is true when the interactive locale starts with zh.
// LC_ALL wins when it is non-empty. Otherwise LANG is used.
// Flags, help, and a non-terminal session stay in English.
func promptLocaleZH() bool {
	loc := os.Getenv("LC_ALL")
	if strings.TrimSpace(loc) == "" {
		loc = os.Getenv("LANG")
	}
	loc = strings.ToLower(strings.TrimSpace(loc))
	if i := strings.IndexAny(loc, ".@"); i >= 0 {
		loc = loc[:i]
	}
	return strings.HasPrefix(loc, "zh")
}

func promptPlatform(in *bufio.Reader, stderr io.Writer, zh bool) (string, bool, error) {
	def, err := platformDefault()
	if err != nil {
		return "", false, err
	}
	list := provider.All()
	intro := "Choose a platform. Press Enter for the default."
	custom := "Custom base URL"
	word := "Platform"
	defMark := " (default)"
	outOfRange := "platform %d is out of range"
	badChoice := "Choose 1, 2, 3, or 4."
	if zh {
		intro = "选择平台。直接回车使用默认项。"
		custom = "自定义网关地址"
		word = "平台"
		defMark = "（默认）"
		outOfRange = "平台 %d 超出范围"
		badChoice = "请选择 1、2、3 或 4。"
	}
	for {
		fmt.Fprintln(stderr, intro)
		for i, p := range list {
			mark := ""
			if i+1 == def {
				mark = defMark
			}
			fmt.Fprintf(stderr, "  %d) %s  %s%s\n", i+1, p.Name, p.BaseURL, mark)
		}
		mark := ""
		if def == len(list)+1 {
			mark = defMark
		}
		fmt.Fprintf(stderr, "  %d) %s%s\n", len(list)+1, custom, mark)
		line, err := readLine(in, stderr, fmt.Sprintf("%s [%d]: ", word, def))
		if err != nil {
			return "", false, err
		}
		if line == "" {
			line = strconv.Itoa(def)
		}
		n, convErr := strconv.Atoi(line)
		if convErr != nil || n < 1 || n > len(list)+1 {
			if convErr != nil {
				fmt.Fprintln(stderr, badChoice)
			} else {
				fmt.Fprintf(stderr, outOfRange+"\n", n)
			}
			continue
		}
		if n == len(list)+1 {
			return provider.CustomID, true, nil
		}
		return list[n-1].ID, false, nil
	}
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

func noticeIfMissing(w io.Writer, id apps.ID) {
	command, _, _ := apps.InstallHelp(id)
	if programPresent(id, command) {
		return
	}
	noticePrograms(w, []apps.ID{id})
}

func printNextSteps(e env, req apply.Request) {
	fmt.Fprintln(e.stdout, "\nNext:")
	if req.TouchShell || req.TouchGemini {
		if e.userEnv {
			fmt.Fprintln(e.stdout, "Open a new terminal to pick up the user environment variables.")
		} else {
			fmt.Fprintln(e.stdout, sourceHint(e.layout.Shell, e.show(e.layout.RCFile)))
		}
	}
	if req.TouchClaude {
		noticeIfMissing(e.stdout, apps.ClaudeCode)
		fmt.Fprintln(e.stdout, "Then run:  claude")
	}
	if req.TouchCodex {
		noticeIfMissing(e.stdout, apps.Codex)
		if req.TouchClaude {
			fmt.Fprintln(e.stdout, "Or run:    codex")
		} else {
			fmt.Fprintln(e.stdout, "Then run:  codex")
		}
	}
	if req.TouchGrok {
		noticeIfMissing(e.stdout, apps.Grok)
		fmt.Fprintln(e.stdout, "Then run:  grok")
	}
	if req.TouchGemini {
		noticeIfMissing(e.stdout, apps.Gemini)
		fmt.Fprintln(e.stdout, "Then run:  gemini")
	}
	if req.TouchDesktop {
		noticeIfMissing(e.stdout, apps.ClaudeDesktop)
	}
	if line := reopenHint(req.TouchCodex, req.TouchDesktop); line != "" {
		fmt.Fprintln(e.stdout, line)
	}
	if req.TouchGrok {
		fmt.Fprintln(e.stdout, "Start a new Grok Build session so it reads config.toml.")
	}
}
