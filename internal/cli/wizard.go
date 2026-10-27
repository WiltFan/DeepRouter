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
		fmt.Fprintln(stderr, chooseProgramsIntro(zh))
		fmt.Fprintln(stderr, chooseProgramsExample(zh))
		selection, err = promptPrograms(in, stderr, zh)
		if err != nil {
			return 2, err
		}
		selection, err = limitPrograms(providerID, selection, stderr, zh)
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
		selection, err = limitPrograms(providerID, selection, stderr, false)
		if err != nil {
			return 2, err
		}
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
		key, err := wizardKey(e, in, providerID, baseURL)
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
			model, clear, err := promptModel(in, stderr, info.Title, info.ModelKind, catalog, false)
			if err != nil {
				return 2, err
			}
			choices = append(choices, chosen{id: id, key: key, model: model, clear: clear})
		}
	} else {
		var cachedKey string
		var cachedModels []gateway.Model
		var haveIDs bool
		var sessionKey string
		for _, id := range selection {
			info, _ := apps.ByID(id)
			existing := existingAppKey(e, id, providerID, baseURL)
			reuse := ""
			if existing == "" {
				reuse = sessionKey
			}
			line, err := promptSecretLine(e.stdin, e.stderr, in, tokenPrompt(info.Title, providerID, baseURL, existing, reuse))
			if err != nil {
				return 2, err
			}
			var key string
			if line == "" {
				switch {
				case existing != "":
					key = existing
					fmt.Fprintf(stderr, "Kept (%s)\n", textutil.MaskConfirm(existing))
				case reuse != "":
					key = reuse
					fmt.Fprintf(stderr, "Reused (%s)\n", textutil.MaskConfirm(reuse))
				default:
					return 2, fmt.Errorf("API key is empty")
				}
			} else {
				key, err = textutil.ValidateKey(line)
				if err != nil {
					return 2, err
				}
			}
			sessionKey = key
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
			model, clear, err := promptModel(in, stderr, info.Title, info.ModelKind, cachedModels, zh)
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
		WireAPI:  codexWireFor(providerID),
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
	if err := confirmWrite(in, stderr, e, prof, req, zh); err != nil {
		return 2, err
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

// wizardKey asks for a token when stdin is a terminal.
// A key already saved for this same provider is the default: Enter keeps it,
// and a typed value replaces it. Input is masked. When stdin is not a
// terminal, that saved key is used with no prompt. DEEPROUTER_API_KEY is the
// DeepRouter token only.
func wizardKey(e env, in *bufio.Reader, providerID, baseURL string) (string, error) {
	existing, fromEnv, err := detectKey(e, providerID, baseURL)
	if err != nil {
		return "", err
	}
	if stdinIsTerminal(e.stdin) {
		line, err := promptSecretLine(e.stdin, e.stderr, in, tokenPrompt("", providerID, baseURL, existing, ""))
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
			fmt.Fprintf(e.stderr, "Using saved %s token (%s).\n", provider.DisplayName(providerID), textutil.Mask(existing))
		}
		return existing, nil
	}
	return "", fmt.Errorf("API key required. Pass --api-key, set DEEPROUTER_API_KEY, or use --api-key-stdin")
}

// detectKey returns a key already saved for providerID.
// DEEPROUTER_API_KEY wins for DeepRouter, because a sourced shell startup
// file puts it in the environment. It is not a token for WolfAI, LinkMesh,
// or a custom URL. An invalid environment value is an error only when that
// variable is used. An invalid saved value is skipped.
func detectKey(e env, providerID, baseURL string) (key string, fromEnv bool, err error) {
	id := provider.NormalizeID(providerID)
	if id == provider.Default().ID {
		if raw := strings.TrimSpace(os.Getenv("DEEPROUTER_API_KEY")); raw != "" {
			key, err = textutil.ValidateKey(raw)
			if err != nil {
				return "", false, err
			}
			return key, true, nil
		}
	}
	for _, candidate := range savedKeyCandidates(e, id, baseURL) {
		key, err = textutil.ValidateKey(candidate)
		if err == nil {
			return key, false, nil
		}
	}
	return "", false, nil
}

// savedKeyCandidates lists tokens that belong to providerID.
// Claude settings and the shell rc do not record a provider, so they are
// the DeepRouter token only. DEEPROUTER_API_KEY is handled by detectKey.
func savedKeyCandidates(e env, providerID, baseURL string) []string {
	var out []string
	if providerID == provider.Default().ID {
		out = append(out, readClaudeKey(e.layout.ClaudePath))
	}
	out = append(out, readCodexKeyFor(e.layout.CodexPath, providerID, baseURL))
	if p, ok := e.activeProfile(); ok && provider.SameKeySource(p.Provider, p.BaseURL, providerID, baseURL) {
		out = append(out, p.APIKey)
	}
	if providerID == provider.Default().ID {
		out = append(out, readShellKey(e))
	}
	return out
}

func (e env) activeProfile() (store.Profile, bool) {
	if e.state.Active == "" {
		return store.Profile{}, false
	}
	return e.state.Find(e.state.Active)
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

func readCodexKeyFor(path, providerID, baseURL string) string {
	text := readText(path)
	id := provider.NormalizeID(providerID)
	if codex.ActiveProvider(text) != id {
		return ""
	}
	auth := codex.ReadAuth(text)
	if id == provider.CustomID && !provider.SameKeySource(id, auth.BaseURL, id, baseURL) {
		return ""
	}
	return auth.BearerToken
}

func readGrokKeyFor(path, providerID, baseURL string) string {
	text := readText(path)
	id := provider.NormalizeID(providerID)
	if grok.ActiveSection(text) != id {
		return ""
	}
	if id == provider.CustomID && !provider.SameKeySource(id, grok.ReadBaseURL(text), id, baseURL) {
		return ""
	}
	return grok.ReadAPIKey(text)
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

// limitPrograms drops apps whose route this provider does not expose.
// A preset with no route table, including a custom URL, keeps every app.
func limitPrograms(providerID string, ids []apps.ID, w io.Writer, zh bool) ([]apps.ID, error) {
	var kept []apps.ID
	for _, id := range ids {
		prog, ok := programExposed(providerID, id)
		if ok && !prog.Exposed {
			info, _ := apps.ByID(id)
			fmt.Fprintln(w, skipProgramLine(zh, provider.DisplayName(providerID), info.Title, prog.Route))
			continue
		}
		kept = append(kept, id)
	}
	if len(ids) > 0 && len(kept) == 0 {
		return nil, fmt.Errorf("no programs left to configure")
	}
	return kept, nil
}

func skipProgramLine(zh bool, site, title, route string) string {
	if zh {
		return fmt.Sprintf("%s 不提供 %s，已跳过 %s。", site, route, title)
	}
	return fmt.Sprintf("%s does not expose %s, so %s is skipped.", site, route, title)
}

// programExposed is replaced in tests that simulate a missing route.
var programExposed = provider.ProgramFor

func codexWireFor(providerID string) string {
	if wire, force := provider.CodexWire(providerID); force {
		return wire
	}
	return "responses"
}

func promptPrograms(in *bufio.Reader, stderr io.Writer, zh bool) ([]apps.ID, error) {
	for {
		for i, info := range apps.Catalog() {
			fmt.Fprintln(stderr, apps.FormatPickerLine(i+1, info))
		}
		line, err := readLine(in, stderr, programsPrompt(zh))
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
			if zh {
				fmt.Fprintln(stderr, "这次选择里没有可配置的程序。请至少选一个。")
			} else {
				fmt.Fprintln(stderr, "No configurable programs in that selection. Choose at least one.")
			}
			continue
		}
		return writable, nil
	}
}

func programsPrompt(zh bool) string {
	if zh {
		return "程序 [1]: "
	}
	return "Programs [1]: "
}

func chooseProgramsIntro(zh bool) string {
	if zh {
		return "选择要配置的程序。直接回车只配置 Codex / ChatGPT。"
	}
	return "Choose which programs to configure. Press Enter for Codex / ChatGPT."
}

func chooseProgramsExample(zh bool) string {
	if zh {
		return "多选示例：1,2（逗号、空格、，、、都可以）"
	}
	return "Example: 1,2"
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

// tokenPrompt names the site where the token is created.
// A preset uses its official host, not an overridden base URL.
// Chinese is used when the interactive locale starts with zh.
// reuse is a key chosen earlier in this session. It is offered only when
// this program has no saved key of its own.
func tokenPrompt(program, providerID, baseURL, existing, reuse string) string {
	if existing == "" && reuse != "" {
		if promptLocaleZH() {
			return fmt.Sprintf("%s API key（直接回车沿用 %s）：", program, textutil.Mask(reuse))
		}
		return fmt.Sprintf("%s API key (Enter to reuse %s): ", program, textutil.Mask(reuse))
	}
	host := provider.TokenHost(providerID, baseURL)
	mask := ""
	if existing != "" {
		mask = " [" + textutil.Mask(existing) + "]"
	}
	if promptLocaleZH() {
		lead := zhTokenLead(provider.PromptName(providerID, true))
		if program == "" {
			return fmt.Sprintf("%s（在 %s 创建）%s：", lead, host, mask)
		}
		return fmt.Sprintf("%s（在 %s 创建），用于 %s%s：", lead, host, program, mask)
	}
	if provider.NormalizeID(providerID) == provider.CustomID {
		if program == "" {
			return fmt.Sprintf("Enter the token for this gateway (create one at %s)%s: ", host, mask)
		}
		return fmt.Sprintf("Enter the token for this gateway (create one at %s) for %s%s: ", host, program, mask)
	}
	name := provider.DisplayName(providerID)
	if program == "" {
		return fmt.Sprintf("Enter the %s token (create one at %s)%s: ", name, host, mask)
	}
	return fmt.Sprintf("Enter the %s token (create one at %s) for %s%s: ", name, host, program, mask)
}

func zhTokenLead(name string) string {
	if name == "自定义网关" {
		return "请输入" + name + "的令牌"
	}
	return "请输入 " + name + " 的令牌"
}

func keyPrompt(existing string) string {
	return tokenPrompt("", provider.Default().ID, provider.Default().BaseURL, existing, "")
}

func existingAppKey(e env, id apps.ID, providerID, baseURL string) string {
	prov := provider.NormalizeID(providerID)
	var candidates []string
	switch id {
	case apps.ClaudeCode:
		if prov == provider.Default().ID {
			candidates = append(candidates, readClaudeKey(e.layout.ClaudePath))
		}
	case apps.ClaudeDesktop:
		if prov == provider.Default().ID {
			candidates = append(candidates, claudedesktop.ReadAPIKey(e.layout.DesktopLibrary))
		}
	case apps.Codex:
		candidates = append(candidates, readCodexKeyFor(e.layout.CodexPath, prov, baseURL))
	case apps.Grok:
		candidates = append(candidates, readGrokKeyFor(e.layout.GrokPath, prov, baseURL))
	case apps.Gemini:
		if prov == provider.Default().ID {
			candidates = append(candidates, readGeminiKey(e))
		}
	}
	if p, ok := e.activeProfile(); ok && provider.SameKeySource(p.Provider, p.BaseURL, prov, baseURL) {
		candidates = append(candidates, p.KeyFor(string(id)))
	}
	if prov == provider.Default().ID {
		candidates = append(candidates, readShellKey(e), os.Getenv("DEEPROUTER_API_KEY"))
	}
	for _, candidate := range candidates {
		key, err := textutil.ValidateKey(candidate)
		if err == nil {
			return key
		}
	}
	return ""
}

// gatewayPrompt is the interactive base-URL question.
const gatewayPrompt = "Gateway URL (Enter for https://deeprouter.top, or any compatible endpoint):"

const gatewayPromptZH = "网关地址（直接回车使用 https://deeprouter.top，或填写其他兼容接口）："

const appDefaultModelLabel = "Use the app default (do not set a model)"

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

func promptModel(in *bufio.Reader, stderr io.Writer, label, kind string, models []gateway.Model, zh bool) (string, bool, error) {
	matched := recommendFirst(gateway.FamilyIDs(models, kind), gateway.PickDefaultModels(models, kind))
	if len(matched) == 0 {
		if zh {
			fmt.Fprintf(stderr, "网关没有列出包含 %q 的 %s 模型。输入一个 id，或直接回车使用程序自己的默认模型。\n", kind, label)
			fmt.Fprintf(stderr, "  1) %s\n", "使用程序自己的默认模型（不写入模型）")
		} else {
			fmt.Fprintf(stderr, "No %s model id containing %q was listed. Type an id, or press Enter to use the app default.\n", label, kind)
			fmt.Fprintf(stderr, "  1) %s\n", appDefaultModelLabel)
		}
	} else if zh {
		fmt.Fprintf(stderr, "%s 模型（直接回车使用网关推荐的 %s）：\n", label, matched[0])
		for i, id := range matched {
			if i == 0 {
				fmt.Fprintf(stderr, "  1) %s  （推荐）\n", id)
				continue
			}
			fmt.Fprintf(stderr, "  %d) %s\n", i+1, id)
		}
		fmt.Fprintf(stderr, "  %d) %s\n", len(matched)+1, "使用程序自己的默认模型（不写入模型）")
	} else {
		fmt.Fprintf(stderr, "%s model (Enter for %s, recommended by the gateway):\n", label, matched[0])
		for i, id := range matched {
			if i == 0 {
				fmt.Fprintf(stderr, "  1) %s  (recommended)\n", id)
				continue
			}
			fmt.Fprintf(stderr, "  %d) %s\n", i+1, id)
		}
		fmt.Fprintf(stderr, "  %d) %s\n", len(matched)+1, appDefaultModelLabel)
	}
	ask := fmt.Sprintf("%s model [1]: ", label)
	if zh {
		ask = fmt.Sprintf("%s 模型 [1]: ", label)
	}
	line, err := readLine(in, stderr, ask)
	if err != nil {
		return "", false, err
	}
	return chooseModel(line, matched)
}

// recommendFirst moves the gateway-recommended id to option 1.
// The remaining ids keep their original order.
func recommendFirst(ids []string, recommended string) []string {
	if recommended == "" || len(ids) == 0 {
		return ids
	}
	out := make([]string, 0, len(ids))
	out = append(out, recommended)
	for _, id := range ids {
		if id != recommended {
			out = append(out, id)
		}
	}
	return out
}

// chooseModel returns the selected id.
// When ids is non-empty, Enter and 1 select ids[0] (the recommended model).
// The last number, or the word "default", clears the model so the app uses its own.
// When ids is empty, Enter and 1 clear the model.
func chooseModel(answer string, ids []string) (string, bool, error) {
	answer = strings.TrimSpace(answer)
	if strings.EqualFold(answer, "default") {
		return "", true, nil
	}
	if len(ids) == 0 {
		if answer == "" || answer == "1" {
			return "", true, nil
		}
		if n, err := strconv.Atoi(answer); err == nil {
			return "", false, fmt.Errorf("model %d is out of range", n)
		}
		return answer, false, nil
	}
	if answer == "" || answer == "1" {
		return ids[0], false, nil
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n == len(ids)+1 {
			return "", true, nil
		}
		if n < 1 || n > len(ids)+1 {
			return "", false, fmt.Errorf("model %d is out of range", n)
		}
		return ids[n-1], false, nil
	}
	return answer, false, nil
}

func confirmWrite(in *bufio.Reader, stderr io.Writer, e env, prof store.Profile, req apply.Request, zh bool) error {
	paths := filesToTouch(e, prof, req)
	if zh {
		fmt.Fprintln(stderr, "将修改这些文件：")
	} else {
		fmt.Fprintln(stderr, "These files will be modified:")
	}
	for _, path := range paths {
		fmt.Fprintf(stderr, "  %s\n", path)
	}
	label := "Continue? [Y/n] "
	if zh {
		label = "继续写入？[Y/n] "
	}
	answer, err := readLine(in, stderr, label)
	if err != nil {
		return err
	}
	if !defaultYes(answer) {
		return fmt.Errorf("setup cancelled")
	}
	return nil
}

func filesToTouch(e env, prof store.Profile, req apply.Request) []string {
	var paths []string
	add := func(path string) {
		if path != "" {
			paths = append(paths, e.show(path))
		}
	}
	if req.TouchClaude {
		add(e.layout.ClaudePath)
	}
	if req.TouchCodex {
		add(e.layout.CodexPath)
	}
	if req.TouchGrok {
		add(e.layout.GrokPath)
	}
	if req.TouchGemini {
		add(e.layout.GeminiPath)
	}
	if req.TouchDesktop {
		files, err := claudedesktop.Plan(e.layout.DesktopLibrary, claudedesktop.Update{
			BaseURL:    prof.BaseURL,
			APIKey:     firstNonEmpty(prof.DesktopKey, prof.APIKey),
			Model:      prof.DesktopModel,
			SetModel:   req.SetDesktopModel,
			ClearModel: req.ClearDesktopModel,
		})
		if err == nil {
			add(files.MetaPath)
			add(files.ProfilePath)
		} else {
			add(e.layout.DesktopLibrary)
		}
	}
	if req.TouchShell || req.TouchGemini {
		if e.userEnv {
			paths = append(paths, userenv.Location)
		} else {
			add(e.layout.RCFile)
		}
	}
	add(e.statePath())
	return paths
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

func (e env) statePath() string {
	if e.stateP != "" {
		return e.stateP
	}
	return ""
}

// zhUI reports whether interactive prompts should be Simplified Chinese.
// LC_ALL wins when it is set. A value that starts with zh (zh_CN, zh-CN, zh_TW)
// selects Chinese. Non-interactive output does not use this.
func zhUI() bool {
	return promptLocaleZH()
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
