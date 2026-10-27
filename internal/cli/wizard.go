package cli

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"runtime"
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
	zh := promptLocaleZH()
	if !terminal {
		if zh {
			fmt.Fprintln(stderr, "请在终端中运行 deeprouter。")
			fmt.Fprintln(stderr, "非交互配置请使用：deeprouter setup --api-key <令牌> --base-url <网关>")
		} else {
			fmt.Fprintln(stderr, "Run deeprouter in a terminal.")
			fmt.Fprintln(stderr, "For a non-interactive setup, use: deeprouter setup --api-key <token> --base-url <gateway>")
		}
		return 2, nil
	}
	providerID := provider.Default().ID
	emptyDefault := gateway.DefaultBaseURL
	if p, explicit, err := providerFromEnv(); err != nil {
		return 2, err
	} else if explicit {
		providerID = p.ID
		emptyDefault = p.BaseURL
	}
	id, custom, err := promptPlatform(in, stderr, zh)
	if err != nil {
		return 2, err
	}
	providerID = id
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, chooseProgramsIntro(zh))
	fmt.Fprintln(stderr, chooseProgramsExample(zh))
	selection, err := promptPrograms(in, stderr, zh)
	if err != nil {
		return 2, err
	}
	selection, err = limitPrograms(providerID, selection, stderr, zh)
	if err != nil {
		return 2, err
	}
	noticePrograms(stderr, selection)
	fmt.Fprintln(stderr)
	var baseURL string
	if custom {
		baseURL, err = promptGateway(in, stderr, true, gateway.DefaultBaseURL)
		if err != nil {
			return 2, err
		}
	} else {
		chosen, _ := provider.ByID(id)
		baseURL = chosen.BaseURL
		fmt.Fprintf(stderr, "%s\n", ui(fmt.Sprintf("Gateway %s", baseURL), fmt.Sprintf("网关 %s", baseURL)))
	}
	_ = emptyDefault

	type chosen struct {
		id    apps.ID
		key   string
		model string
		clear bool
	}
	var choices []chosen
	var catalog []gateway.Model
	{
		var cachedModels []gateway.Model
		var sessionKey string
		for _, id := range selection {
			info, _ := apps.ByID(id)
			existing := existingAppKey(e, id, providerID, baseURL)
			reuse := ""
			if existing == "" {
				reuse = sessionKey
			}
			var key string
			for {
				line, err := promptSecretLine(e.stdin, e.stderr, in, tokenPrompt(info.Title, providerID, baseURL, existing, reuse))
				if errors.Is(err, io.EOF) {
					fmt.Fprintln(stderr, ui("The token is empty. Enter it again.", "令牌不能为空，请重新输入。"))
					return 2, fmt.Errorf("%s", ui("The token is empty. Enter it again.", "令牌不能为空，请重新输入。"))
				}
				if err != nil {
					return 2, err
				}
				switch {
				case line == "" && existing != "":
					key = existing
					fmt.Fprintf(stderr, "%s (%s)\n", ui("Kept", "已保留"), maskConfirm(existing))
				case line == "" && reuse != "":
					key = reuse
					fmt.Fprintf(stderr, "%s (%s)\n", ui("Reused", "已沿用"), maskConfirm(reuse))
				case line == "":
					fmt.Fprintln(stderr, ui("The token is empty. Enter it again.", "令牌不能为空，请重新输入。"))
					continue
				default:
					key, err = textutil.ValidateKey(line)
					if err != nil {
						fmt.Fprintln(stderr, keyRetryMessage(err))
						continue
					}
				}
				fmt.Fprintf(stderr, "%s\n", ui(fmt.Sprintf("Fetching models from %s ...", baseURL), fmt.Sprintf("正在从 %s 获取模型列表……", baseURL)))
				cachedModels, err = fetchModels(baseURL, key, 30*time.Second)
				if err != nil {
					if authFailure(err) {
						fmt.Fprintln(stderr, ui("That token was rejected. Enter it again.", "令牌无效，请重新输入。"))
						fmt.Fprintln(stderr, err.Error())
						continue
					}
					return 1, fmt.Errorf("%s", ui("could not list models: "+err.Error(), "无法获取模型列表："+err.Error()))
				}
				fmt.Fprintf(stderr, "%s\n\n", ui(fmt.Sprintf("%d models.", len(cachedModels)), fmt.Sprintf("%d 个模型。", len(cachedModels))))
				break
			}
			sessionKey = key
			model, clear, err := promptModel(in, stderr, info.Title, info.ModelKind, cachedModels, zh, baseURL, key, codexWireFor(providerID))
			if err != nil {
				return 2, err
			}
			choices = append(choices, chosen{id: id, key: key, model: model, clear: clear})
			catalog = cachedModels
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
	stateSnap := snapshotBytes(e.stateP)
	report, err := e.saveProfile(prof, req)
	if err != nil {
		return 1, err
	}
	printSetup(e, prof, report, req.TouchClaude, req.TouchCodex, req.TouchShell || req.TouchGemini, false)

	fmt.Fprintln(stdout)
	ep, probeReport, err := probeTarget(wizardProbeTarget(prof, req), 30*time.Second)
	if err != nil {
		e.rollbackSetup(stateSnap)
		return 1, err
	}
	printReport(stdout, target{
		name:  prof.Name,
		label: prof.Label,
		key:   prof.APIKey,
	}, ep, probeReport)
	if !probeReport.OK {
		probeReport, err = e.recoverFailedProbe(in, &prof, &req, probeReport, catalog)
		if err != nil || !probeReport.OK {
			e.rollbackSetup(stateSnap)
			fmt.Fprintln(stderr, ui("Rolled back the files just written.", "已撤销刚才的写入。"))
			return 1, nil
		}
	}
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
			fmt.Fprintln(stderr, formatProgramLine(i+1, info))
		}
		line, err := readLine(in, stderr, programsPrompt(zh))
		if err != nil {
			return nil, err
		}
		selection, err := apps.ParseSelection(line, true)
		if err != nil {
			fmt.Fprintln(stderr, programSelectError(err))
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
			return fmt.Sprintf("%s 令牌（直接回车沿用 %s）：", program, textutil.Mask(reuse))
		}
		return fmt.Sprintf("%s API key (Enter to reuse %s): ", program, textutil.Mask(reuse))
	}
	host := provider.TokenHost(providerID, baseURL)
	mask := ""
	if existing != "" {
		mask = " [" + textutil.Mask(existing) + "]"
	}
	if provider.NormalizeID(providerID) == provider.CustomID {
		shown := customGatewayLabel(baseURL)
		if promptLocaleZH() {
			if program == "" {
				return fmt.Sprintf("请输入该网关的令牌（%s）%s：", shown, mask)
			}
			return fmt.Sprintf("请输入该网关的令牌（%s），用于 %s%s：", shown, program, mask)
		}
		if program == "" {
			return fmt.Sprintf("Enter the token for %s%s: ", shown, mask)
		}
		return fmt.Sprintf("Enter the token for %s for %s%s: ", shown, program, mask)
	}
	if promptLocaleZH() {
		lead := zhTokenLead(provider.PromptName(providerID, true))
		if program == "" {
			return fmt.Sprintf("%s（在 %s 创建）%s：", lead, host, mask)
		}
		return fmt.Sprintf("%s（在 %s 创建），用于 %s%s：", lead, host, program, mask)
	}
	name := provider.DisplayName(providerID)
	if program == "" {
		return fmt.Sprintf("Enter the %s token (create one at %s)%s: ", name, host, mask)
	}
	return fmt.Sprintf("Enter the %s token (create one at %s) for %s%s: ", name, host, program, mask)
}

// customGatewayLabel is the URL the user will call, without a trailing slash.
// An IP and port stay intact so the hint is not "create a token at 127.0.0.1".
func customGatewayLabel(baseURL string) string {
	baseURL = strings.TrimSpace(baseURL)
	baseURL = strings.TrimRight(baseURL, "/")
	if baseURL == "" {
		return gateway.DefaultBaseURL
	}
	return baseURL
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

func promptModel(in *bufio.Reader, stderr io.Writer, label, kind string, models []gateway.Model, zh bool, baseURL, key, wire string) (string, bool, error) {
	ids := gateway.FamilyIDs(models, kind)
	recommended := ""
	probed := false
	if strings.TrimSpace(baseURL) != "" && strings.TrimSpace(key) != "" && len(ids) > 0 {
		probed = true
		fmt.Fprintln(stderr, ui("Checking the recommended model...", "正在确认推荐模型……"))
		working, auth, hint, skipped := probeFamily(baseURL, key, kind, wire, models)
		if auth {
			return "", false, fmt.Errorf("%s", ui("That token was rejected. Enter it again. "+hint, "令牌无效，请重新输入。"+hint))
		}
		for _, skip := range skipped {
			line := skip.Hint
			if line == "" {
				line = ui("it did not answer", "没有应答")
			}
			fmt.Fprintf(stderr, "%s\n", ui(
				fmt.Sprintf("Skipped %s: %s", skip.ID, line),
				fmt.Sprintf("已跳过 %s：%s", skip.ID, line),
			))
		}
		recommended = working
		if recommended == "" {
			fmt.Fprintln(stderr, ui(
				"None of the listed models answered a test request. Type an id, or press Enter to use the app default.",
				"列出的模型都没有通过测试请求。请输入一个 id，或直接回车使用程序自己的默认模型。",
			))
		}
	}
	if !probed {
		recommended = gateway.PickDefaultModels(models, kind)
	}
	matched := recommendFirst(ids, recommended)
	// Option 1 stays the model Enter selects. A live probe only puts an id
	// there after a tiny request succeeded, so a listed model that returns
	// 503 is not the default. Enter still selects that option.
	showRecommended := recommended != "" && !probed || (probed && recommended != "")
	if probed && recommended == "" {
		showRecommended = false
	}
	if len(matched) == 0 {
		if zh {
			fmt.Fprintf(stderr, "网关没有列出包含 %q 的 %s 模型。输入一个 id，或直接回车使用程序自己的默认模型。\n", kind, label)
			fmt.Fprintf(stderr, "  1) %s\n", "使用程序自己的默认模型（不写入模型）")
		} else {
			fmt.Fprintf(stderr, "No %s model id containing %q was listed. Type an id, or press Enter to use the app default.\n", label, kind)
			fmt.Fprintf(stderr, "  1) %s\n", appDefaultModelLabel)
		}
	} else if showRecommended && zh {
		fmt.Fprintf(stderr, "%s 模型（直接回车使用网关推荐的 %s）：\n", label, matched[0])
		printModelChoices(stderr, matched, true, true)
	} else if showRecommended {
		fmt.Fprintf(stderr, "%s model (Enter for %s, recommended by the gateway):\n", label, matched[0])
		printModelChoices(stderr, matched, false, true)
	} else if zh {
		fmt.Fprintf(stderr, "%s 模型（直接回车使用程序自己的默认模型，或输入一个 id）：\n", label)
		printModelChoices(stderr, matched, true, false)
	} else {
		fmt.Fprintf(stderr, "%s model (Enter to use the app default, or type an id):\n", label)
		printModelChoices(stderr, matched, false, false)
	}
	ask := fmt.Sprintf("%s model [1]: ", label)
	if zh {
		ask = fmt.Sprintf("%s 模型 [1]: ", label)
	}
	for {
		line, err := readLine(in, stderr, ask)
		if err != nil {
			return "", false, err
		}
		var model string
		var clear bool
		if probed && recommended == "" {
			model, clear, err = chooseUnverified(line, matched)
		} else {
			model, clear, err = chooseModel(line, matched)
		}
		if err != nil {
			fmt.Fprintln(stderr, modelRetryMessage(err))
			continue
		}
		if model != "" && strings.TrimSpace(baseURL) != "" && strings.TrimSpace(key) != "" {
			ok, auth, hint := probeOne(baseURL, key, kind, wire, model)
			if auth {
				return "", false, fmt.Errorf("%s", ui("That token was rejected. Enter it again. "+hint, "令牌无效，请重新输入。"+hint))
			}
			if !ok {
				fmt.Fprintln(stderr, ui(
					fmt.Sprintf("%s did not answer a test request. Choose another model.", model),
					fmt.Sprintf("%s 没有通过测试请求，请另选一个模型。", model),
				))
				if hint != "" {
					fmt.Fprintln(stderr, hint)
				}
				continue
			}
		}
		return model, clear, nil
	}
}

func printModelChoices(stderr io.Writer, matched []string, zh, markFirst bool) {
	mark := ""
	fallback := appDefaultModelLabel
	if markFirst {
		mark = "  (recommended)"
	}
	if zh {
		if markFirst {
			mark = "  （推荐）"
		}
		fallback = "使用程序自己的默认模型（不写入模型）"
	}
	for i, id := range matched {
		if i == 0 && mark != "" {
			fmt.Fprintf(stderr, "  1) %s%s\n", id, mark)
			continue
		}
		fmt.Fprintf(stderr, "  %d) %s\n", i+1, id)
	}
	fmt.Fprintf(stderr, "  %d) %s\n", len(matched)+1, fallback)
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
		if !plausibleModelID(answer) {
			return "", false, fmt.Errorf("not a model id")
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
	for _, id := range ids {
		if answer == id {
			return answer, false, nil
		}
	}
	return "", false, fmt.Errorf("not a model id")
}

// chooseUnverified is the menu after every recommendation probe failed.
// Enter, 1 when the list is empty, the last number, and "default" use the
// app default. A number still selects that listed id so it can be probed again.
func chooseUnverified(answer string, ids []string) (string, bool, error) {
	answer = strings.TrimSpace(answer)
	if strings.EqualFold(answer, "default") || answer == "" {
		return "", true, nil
	}
	if len(ids) == 0 {
		return chooseModel(answer, nil)
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
	for _, id := range ids {
		if answer == id {
			return answer, false, nil
		}
	}
	if !plausibleModelID(answer) {
		return "", false, fmt.Errorf("not a model id")
	}
	return answer, false, nil
}

func plausibleModelID(s string) bool {
	switch strings.ToLower(s) {
	case "n", "y", "no", "yes", "q", "quit":
		return false
	}
	if len([]rune(s)) >= 4 {
		return true
	}
	for _, r := range s {
		if (r >= '0' && r <= '9') || r == '-' || r == '.' || r == '_' {
			return true
		}
	}
	return false
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
		return fmt.Errorf("%s", ui("setup cancelled", "已取消配置"))
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

// uiLang is set by --lang or DEEPROUTER_LANG. Empty follows the locale,
// and the default UI is Simplified Chinese.
var uiLang string

func setUILang(v string) error {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "zh", "zh-cn", "zh_cn", "zh-hans":
		uiLang = "zh"
		return nil
	case "en", "en-us", "en_us", "en-gb", "en_gb":
		uiLang = "en"
		return nil
	default:
		return fmt.Errorf("language must be zh or en")
	}
}

func ui(en, zh string) string {
	if promptLocaleZH() {
		return zh
	}
	return en
}

// promptLocaleZH is Simplified Chinese unless the locale is clearly English
// or --lang en / DEEPROUTER_LANG=en is set. LC_ALL wins over LANG.
// C, POSIX, and an empty locale are not English, so they stay Chinese.
func promptLocaleZH() bool {
	switch uiLang {
	case "en":
		return false
	case "zh":
		return true
	}
	loc := strings.TrimSpace(os.Getenv("LC_ALL"))
	if loc == "" {
		loc = os.Getenv("LANG")
	}
	return !localeIsEnglish(loc)
}

func localeIsEnglish(loc string) bool {
	loc = strings.ToLower(strings.TrimSpace(loc))
	if i := strings.IndexAny(loc, ".@"); i >= 0 {
		loc = loc[:i]
	}
	return loc == "en" || strings.HasPrefix(loc, "en_") || strings.HasPrefix(loc, "en-")
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
	fmt.Fprintln(e.stdout, ui("\nNext:", "\n接下来："))
	if req.TouchShell || req.TouchGemini {
		if e.userEnv {
			fmt.Fprintln(e.stdout, ui("Open a new terminal to pick up the user environment variables.", "打开一个新终端以使用用户环境变量。"))
		} else {
			fmt.Fprintln(e.stdout, sourceHint(e.layout.Shell, e.show(e.layout.RCFile)))
		}
	}
	if req.TouchClaude {
		noticeIfMissing(e.stdout, apps.ClaudeCode)
		fmt.Fprintln(e.stdout, ui("Then run:  claude", "然后运行：  claude"))
	}
	if req.TouchCodex {
		noticeIfMissing(e.stdout, apps.Codex)
		if req.TouchClaude {
			fmt.Fprintln(e.stdout, ui("Or run:    codex", "或者运行：    codex"))
		} else {
			fmt.Fprintln(e.stdout, ui("Then run:  codex", "然后运行：  codex"))
		}
	}
	if req.TouchGrok {
		noticeIfMissing(e.stdout, apps.Grok)
		fmt.Fprintln(e.stdout, ui("Then run:  grok", "然后运行：  grok"))
	}
	if req.TouchGemini {
		noticeIfMissing(e.stdout, apps.Gemini)
		fmt.Fprintln(e.stdout, ui("Then run:  gemini", "然后运行：  gemini"))
	}
	if req.TouchDesktop {
		noticeIfMissing(e.stdout, apps.ClaudeDesktop)
	}
	if line := reopenHint(req.TouchCodex, req.TouchDesktop); line != "" {
		fmt.Fprintln(e.stdout, line)
	}
	if req.TouchGrok {
		fmt.Fprintln(e.stdout, ui("Start a new Grok Build session so it reads config.toml.", "新开一个 Grok Build 会话，以便读取 config.toml。"))
	}
}

func probeFamily(baseURL, key, family, wire string, models []gateway.Model) (string, bool, string, []gateway.Skip) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return "", false, err.Error(), nil
	}
	return gateway.FirstWorking(ctx, gateway.ProbeOptions{
		Endpoints: ep,
		APIKey:    key,
		CodexWire: wire,
		Timeout:   20 * time.Second,
	}, models, family, wire)
}

func probeOne(baseURL, key, family, wire, id string) (bool, bool, string) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return false, false, err.Error()
	}
	return gateway.ProbeID(ctx, gateway.ProbeOptions{
		Endpoints: ep,
		APIKey:    key,
		CodexWire: wire,
		Timeout:   20 * time.Second,
	}, family, wire, id)
}

func wizardProbeTarget(prof store.Profile, req apply.Request) target {
	return target{
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
		codexWire:    prof.WireAPI,
		scoped:       true,
		probeModels:  req.TouchCodex,
		probeChat:    req.TouchCodex,
		probeGrok:    req.TouchGrok,
		probeClaude:  req.TouchClaude,
		probeDesktop: req.TouchDesktop,
		probeGemini:  req.TouchGemini,
	}
}

func (e env) recoverFailedProbe(in *bufio.Reader, prof *store.Profile, req *apply.Request, report gateway.Report, models []gateway.Model) (gateway.Report, error) {
	for round := 0; round < 4 && !report.OK; round++ {
		fmt.Fprintln(e.stderr, ui(
			"The connectivity test failed. The files above were already written.",
			"连通性测试没有通过。上面的文件已经写入。",
		))
		for _, check := range report.Checks {
			if check.OK || check.Skipped {
				continue
			}
			fmt.Fprintf(e.stderr, "  %s %s  %s\n", check.Method, check.Path, check.Hint)
		}
		fmt.Fprintln(e.stderr, ui("1) Pick another model", "1) 换一个模型"))
		fmt.Fprintln(e.stderr, ui("2) Roll back the files just written", "2) 撤销刚才的写入"))
		answer, err := readLine(in, e.stderr, ui("Choice [2]: ", "选择 [2]: "))
		if err != nil {
			return report, err
		}
		answer = strings.TrimSpace(answer)
		if answer == "" || answer == "2" || strings.EqualFold(answer, "rollback") {
			return report, nil
		}
		if answer != "1" {
			fmt.Fprintln(e.stderr, ui("Enter 1 or 2.", "请输入 1 或 2。"))
			continue
		}
		if err := e.repromptFailed(in, prof, req, report, models); err != nil {
			return report, err
		}
		if _, err := e.saveProfile(*prof, *req); err != nil {
			return report, err
		}
		_, report, err = probeTarget(wizardProbeTarget(*prof, *req), 30*time.Second)
		if err != nil {
			return report, err
		}
		fmt.Fprintln(e.stdout)
		printReport(e.stdout, target{name: prof.Name, label: prof.Label, key: prof.APIKey}, mustEndpoints(prof.BaseURL), report)
	}
	return report, nil
}

func mustEndpoints(baseURL string) gateway.Endpoints {
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return gateway.Endpoints{}
	}
	return ep
}

func (e env) repromptFailed(in *bufio.Reader, prof *store.Profile, req *apply.Request, report gateway.Report, models []gateway.Model) error {
	zh := promptLocaleZH()
	wire := prof.WireAPI
	seen := map[apps.ID]bool{}
	var ids []apps.ID
	add := func(id apps.ID, on bool) {
		if on && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, check := range report.Checks {
		if check.OK || check.Skipped {
			continue
		}
		switch check.Name {
		case "chat":
			add(apps.Codex, req.TouchCodex)
		case "grok":
			add(apps.Grok, req.TouchGrok)
		case "generateContent":
			add(apps.Gemini, req.TouchGemini)
		case "messages":
			add(apps.ClaudeCode, req.TouchClaude)
			add(apps.ClaudeDesktop, req.TouchDesktop)
		default:
			add(apps.Codex, req.TouchCodex)
			add(apps.ClaudeCode, req.TouchClaude)
			add(apps.ClaudeDesktop, req.TouchDesktop)
			add(apps.Grok, req.TouchGrok)
			add(apps.Gemini, req.TouchGemini)
		}
	}
	for _, id := range ids {
		info, ok := apps.ByID(id)
		if !ok {
			continue
		}
		key := prof.KeyFor(string(id))
		model, clear, err := promptModel(in, e.stderr, info.Title, info.ModelKind, models, zh, prof.BaseURL, key, wire)
		if err != nil {
			return err
		}
		switch id {
		case apps.ClaudeCode:
			prof.ClaudeModel = model
			req.SetClaudeModel = model != "" && !clear
			req.ClearClaudeModel = clear
		case apps.ClaudeDesktop:
			prof.DesktopModel = model
			req.SetDesktopModel = model != "" && !clear
			req.ClearDesktopModel = clear
		case apps.Codex:
			prof.CodexModel = model
			req.SetCodexModel = model != "" && !clear
			req.ClearCodexModel = clear
		case apps.Grok:
			prof.GrokModel = model
			req.SetGrokModel = model != "" && !clear
			req.ClearGrokModel = clear
		case apps.Gemini:
			prof.GeminiModel = model
			req.SetGeminiModel = model != "" && !clear
			req.ClearGeminiModel = clear
		}
	}
	return nil
}

type fileSnap struct {
	data    []byte
	existed bool
}

func snapshotBytes(path string) fileSnap {
	b, err := os.ReadFile(path)
	if err != nil {
		return fileSnap{}
	}
	return fileSnap{data: append([]byte(nil), b...), existed: true}
}

func (e env) rollbackSetup(snap fileSnap) {
	if _, err := e.backups().Restore("original"); err != nil && !strings.Contains(err.Error(), "no original backup") {
		fmt.Fprintln(e.stderr, err.Error())
	}
	if e.stateP == "" {
		return
	}
	if !snap.existed {
		_ = os.Remove(e.stateP)
		return
	}
	_ = os.WriteFile(e.stateP, snap.data, 0o600)
}

func formatProgramLine(index int, info apps.Info) string {
	note := info.Note
	if promptLocaleZH() {
		note = zhProgramNote(info)
	}
	return fmt.Sprintf("%d) %-*s %s", index, 17, info.Title, note)
}

func zhProgramNote(info apps.Info) string {
	switch info.ID {
	case apps.Codex:
		if runtime.GOOS == "windows" {
			return `Codex CLI 和 ChatGPT 桌面版共用 %USERPROFILE%\.codex\config.toml`
		}
		return "Codex CLI 和 ChatGPT 桌面版共用 ~/.codex/config.toml"
	case apps.ClaudeDesktop:
		if runtime.GOOS == "windows" {
			return info.Note
		}
		return "聊天、Cowork 和 Code 标签（configLibrary）"
	default:
		return info.Note
	}
}

func programSelectError(err error) string {
	msg := err.Error()
	if !promptLocaleZH() {
		return msg
	}
	var n int
	if _, scanErr := fmt.Sscanf(msg, "program %d is out of range", &n); scanErr == nil && strings.Contains(msg, "out of range") {
		return fmt.Sprintf("程序 %d 超出范围", n)
	}
	if strings.HasPrefix(msg, "unknown program ") {
		return "未知程序 " + strings.TrimPrefix(msg, "unknown program ")
	}
	if msg == "no programs selected" {
		return "没有选择程序"
	}
	return msg
}

func modelRetryMessage(err error) string {
	msg := err.Error()
	if !promptLocaleZH() {
		if msg == "not a model id" {
			return "That is not a model id. Enter a listed number, a listed id, or press Enter."
		}
		return msg
	}
	var n int
	if _, scanErr := fmt.Sscanf(msg, "model %d is out of range", &n); scanErr == nil && strings.Contains(msg, "out of range") {
		return fmt.Sprintf("模型 %d 超出范围", n)
	}
	return "这不是模型 id。请输入列表中的编号或 id。"
}

func keyRetryMessage(err error) string {
	msg := err.Error()
	if !promptLocaleZH() {
		return msg + " Enter it again."
	}
	switch {
	case strings.Contains(msg, "empty"):
		return "令牌不能为空，请重新输入。"
	case strings.Contains(msg, "single line"):
		return "令牌必须是一行，请重新输入。"
	case strings.Contains(msg, "UTF-8"):
		return "令牌必须是有效的 UTF-8，请重新输入。"
	case strings.Contains(msg, "too long"):
		return "令牌太长，请重新输入。"
	default:
		return "令牌无效，请重新输入。"
	}
}

func authFailure(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.HasPrefix(msg, "401") || strings.HasPrefix(msg, "403")
}

func maskConfirm(secret string) string {
	s := textutil.MaskConfirm(secret)
	if promptLocaleZH() {
		s = strings.Replace(s, " chars", " 个字符", 1)
	}
	return s
}
