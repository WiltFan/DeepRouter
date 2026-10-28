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
	zh := promptLocaleZH()
	if !stdinIsTerminal(e.stdin) {
		if zh {
			fmt.Fprintln(stderr, "请在终端中运行 deeprouter。")
			fmt.Fprintln(stderr, "非交互配置请设置环境变量 DEEPROUTER_API_KEY，或使用：deeprouter setup --api-key-stdin --base-url <网关>")
		} else {
			fmt.Fprintln(stderr, "Run deeprouter in a terminal.")
			fmt.Fprintln(stderr, "For a non-interactive setup, set DEEPROUTER_API_KEY, or use: deeprouter setup --api-key-stdin --base-url <gateway>")
		}
		return 2, nil
	}
	guard := e.guardInterrupts()
	defer guard.stop()
	fmt.Fprintln(stderr, chooseProgramsIntro(zh))
	fmt.Fprintln(stderr, chooseProgramsExample(zh))
	selection, err := promptPrograms(in, stderr, zh)
	if err != nil {
		return e.cancelled(err)
	}
	fmt.Fprintln(stderr)
	providerID, custom, err := promptPlatform(in, stderr, zh, selection)
	if err != nil {
		return e.cancelled(err)
	}
	selection, err = limitPrograms(providerID, selection, stderr, zh)
	if err != nil {
		return 2, err
	}
	noticePrograms(stderr, selection)
	fmt.Fprintln(stderr)
	s := &session{
		e:        e,
		in:       in,
		zh:       zh,
		provider: providerID,
		custom:   custom,
		wire:     codexWireFor(providerID),
		catalogs: map[string][]gateway.Model{},
		probes:   map[string]familyProbe{},
	}
	if custom {
		s.baseURL, err = promptGateway(in, stderr, true, gateway.DefaultBaseURL)
		if err != nil {
			return e.cancelled(err)
		}
	} else {
		chosen, _ := provider.ByID(providerID)
		s.baseURL = chosen.BaseURL
		fmt.Fprintln(stderr, ui("Gateway "+s.baseURL, "网关 "+s.baseURL))
	}

	var choices []choice
	sessionKey := ""
	for _, id := range selection {
		info, _ := apps.ByID(id)
		existing := existingAppKey(e, id, providerID, s.baseURL)
		reuse := ""
		if existing == "" {
			reuse = sessionKey
		}
		key, probe, err := s.askKey(info, existing, reuse)
		if err != nil {
			if errors.Is(err, errTokenAbandoned) {
				fmt.Fprintln(stderr, ui(
					"The token was rejected. Nothing was written.",
					"令牌被拒绝。没有写入配置。",
				))
				return 1, nil
			}
			if errors.Is(err, io.EOF) || errors.Is(err, errInterrupted) {
				return e.cancelled(err)
			}
			return 1, err
		}
		sessionKey = key
		var model string
		clear := true
		if provider.IsOfficial(s.provider) {
			fmt.Fprintln(stderr, officialModelLine(e, info))
		} else {
			kept := ""
			if info.ID == apps.Codex {
				kept = displayedCodexModel(e, "")
			}
			model, clear, err = pickModel(in, stderr, info.Title, info.ModelKind, s.catalogs[key], zh, s.baseURL, key, s.wire, probe, kept)
			if err != nil {
				return e.cancelled(err)
			}
		}
		choices = append(choices, choice{id: id, key: key, model: model, clear: clear})
	}
	if len(choices) == 0 {
		return 2, fmt.Errorf("%s", ui("no programs selected", "没有选择程序"))
	}

	prof, req := s.profile(choices)
	announceWrite(stderr, e, prof, req)
	snap := e.snapshotWrite(prof, req)
	var report apply.Report
	err = guard.write(snap, func() error {
		var saveErr error
		report, saveErr = e.saveProfile(prof, req)
		return saveErr
	})
	if err != nil {
		guard.rollback()
		fmt.Fprintln(stderr, rolledBackLine())
		return 1, err
	}
	printSetup(e, prof, report, req.TouchClaude, req.TouchCodex, req.TouchShell || req.TouchGemini, false)

	if !provider.IsOfficial(providerID) {
		fmt.Fprintln(stdout)
		ep, probeReport, err := probeTarget(wizardProbeTarget(prof, req), 30*time.Second)
		if err != nil {
			guard.rollback()
			fmt.Fprintln(stderr, rolledBackLine())
			return 1, err
		}
		printReport(stdout, target{
			name:  prof.Name,
			label: prof.Label,
			key:   prof.APIKey,
		}, ep, probeReport)
		if !probeReport.OK {
			probeReport, err = s.recover(guard, snap, &prof, &req, probeReport)
			if err != nil || !probeReport.OK {
				guard.rollback()
				fmt.Fprintln(stderr, rolledBackLine())
				if err != nil && !errors.Is(err, io.EOF) {
					return 1, err
				}
				return 1, nil
			}
		}
	}
	guard.settle()
	var keyErr error
	if req.TouchCodex {
		keyErr = e.confirmCodexKey(e.layout.CodexPath, prof.KeyFor(string(apps.Codex)))
	}
	cliErr := installMissingCLIs(e, true, in, false, selectedProgramIDs(req))
	printNextSteps(e, req)
	if cliErr != nil || keyErr != nil {
		return 1, nil
	}
	return 0, nil
}

// session holds one wizard run's gateway, and the model lists and probe
// results already fetched for each token, so a token is checked once.
type session struct {
	e          env
	in         *bufio.Reader
	zh         bool
	provider   string
	custom     bool
	baseURL    string
	wire       string
	catalogs   map[string][]gateway.Model
	probes     map[string]familyProbe
	officialOK map[string]bool
}

type choice struct {
	id    apps.ID
	key   string
	model string
	clear bool
}

// familyProbe is the result of probing one model family with one token.
// check decided the probe: a success, a 401 or 403, or the last failure.
// passed lists the ids that answered, in candidate order.
type familyProbe struct {
	ran     bool
	check   gateway.Check
	skipped []gateway.Skip
	passed  []string
}

// errTokenAbandoned is end of input on the token prompt after that token
// was already rejected. The wizard exits 1 and does not print the cancel
// line. Ctrl-D on any earlier question, including the first token prompt,
// still exits 2.
var errTokenAbandoned = errors.New("token prompt ended after a rejection")

// askKey asks for one program's token until the gateway accepts it: the
// model list loads, and a tiny request does not refuse the key.
// A refused token is not offered again as the Enter default.
// Ctrl-D on the re-prompt after a refusal returns errTokenAbandoned.
func (s *session) askKey(info apps.Info, existing, reuse string) (string, familyProbe, error) {
	stderr := s.e.stderr
	rejected := false
	for {
		line, err := promptSecretLine(s.e.stdin, stderr, s.in, tokenPrompt(info.Title, s.provider, s.baseURL, existing, reuse))
		if err != nil {
			if rejected && errors.Is(err, io.EOF) {
				return "", familyProbe{}, errTokenAbandoned
			}
			return "", familyProbe{}, err
		}
		var key string
		switch {
		case line == "" && existing != "":
			key = existing
			fmt.Fprintln(stderr, keyNote(ui("Kept", "已保留"), existing))
		case line == "" && reuse != "":
			key = reuse
			fmt.Fprintln(stderr, keyNote(ui("Reused", "已沿用"), reuse))
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
		if provider.IsOfficial(s.provider) {
			if err := s.confirmOfficial(key); err != nil {
				if errors.Is(err, errOfficialRejected) {
					fmt.Fprintln(stderr, ui("Enter the token again.", "请重新输入令牌。"))
					if existing == key {
						existing = ""
					}
					if reuse == key {
						reuse = ""
					}
					rejected = true
					continue
				}
				return "", familyProbe{}, err
			}
			return key, familyProbe{}, nil
		}
		check, err := s.loadModels(key)
		if err != nil {
			return "", familyProbe{}, err
		}
		probe := familyProbe{}
		if !check.Rejected() {
			probe = s.probe(key, info.ModelKind)
			check = probe.check
		}
		if check.Rejected() {
			fmt.Fprintln(stderr, rejectionLine(check))
			fmt.Fprintln(stderr, ui("Enter the token again.", "请重新输入令牌。"))
			if existing == key {
				existing = ""
			}
			if reuse == key {
				reuse = ""
			}
			rejected = true
			continue
		}
		return key, probe, nil
	}
}

// loadModels fetches the model list for key once per session. A 401 or 403
// is returned as the check. Any other failure ends the wizard, except that
// a custom gateway URL can be typed again before the first list loads.
func (s *session) loadModels(key string) (gateway.Check, error) {
	for {
		if _, ok := s.catalogs[key]; ok {
			return gateway.Check{OK: true}, nil
		}
		fmt.Fprintln(s.e.stderr, ui(
			fmt.Sprintf("Fetching models from %s ...", s.baseURL),
			fmt.Sprintf("正在从 %s 获取模型列表……", s.baseURL),
		))
		models, check, err := fetchModels(s.baseURL, key, 30*time.Second)
		if err == nil {
			s.catalogs[key] = models
			fmt.Fprintln(s.e.stderr, ui(fmt.Sprintf("%d models.", len(models)), fmt.Sprintf("共 %d 个模型。", len(models))))
			fmt.Fprintln(s.e.stderr)
			return check, nil
		}
		if check.Rejected() {
			return check, nil
		}
		fmt.Fprintln(s.e.stderr, ui("Could not list models: ", "无法获取模型列表：")+localHint(check))
		if !s.custom || len(s.catalogs) > 0 {
			return check, fmt.Errorf("%s", ui(
				"could not list models from "+s.baseURL+". Nothing was written.",
				"无法从 "+s.baseURL+" 获取模型列表，没有写入任何文件。",
			))
		}
		base, err := s.reenterGateway()
		if err != nil {
			return check, err
		}
		s.baseURL = base
	}
}

func (s *session) reenterGateway() (string, error) {
	prompt := ui(
		fmt.Sprintf("Gateway URL (Enter to try %s again): ", s.baseURL),
		fmt.Sprintf("网关地址（直接回车重试 %s）：", s.baseURL),
	)
	for {
		line, err := readLine(s.in, s.e.stderr, prompt)
		if err != nil {
			return "", err
		}
		if line == "" {
			return s.baseURL, nil
		}
		if _, err := gateway.ParseBase(line); err != nil {
			fmt.Fprintln(s.e.stderr, baseURLError(err))
			continue
		}
		return line, nil
	}
}

// probe checks which model of family answers for key, once per session.
func (s *session) probe(key, family string) familyProbe {
	id := key + "\x00" + family
	if p, ok := s.probes[id]; ok {
		return p
	}
	models := s.catalogs[key]
	if len(gateway.FamilyIDs(models, family)) == 0 {
		return familyProbe{}
	}
	fmt.Fprintln(s.e.stderr, ui("Checking which models answer...", "正在确认哪些模型有应答……"))
	p := probeFamily(s.baseURL, key, family, s.wire, models)
	if !p.check.Rejected() {
		s.probes[id] = p
	}
	return p
}

// profile turns the answers into the profile and write request.
func (s *session) profile(choices []choice) (store.Profile, apply.Request) {
	e := s.e
	name := "default"
	label := "DeepRouter"
	if e.state.Active != "" {
		name = e.state.Active
		if existing, ok := e.state.Find(name); ok && existing.Label != "" {
			label = existing.Label
		}
	}
	label = providerLabel(label, s.provider)
	prof := store.Profile{
		Name:     name,
		Label:    label,
		BaseURL:  s.baseURL,
		Provider: s.provider,
		APIKey:   choices[0].key,
		WireAPI:  s.wire,
	}
	req := apply.Request{AppsExplicit: true}
	for _, c := range choices {
		set := c.model != "" && !c.clear
		switch c.id {
		case apps.ClaudeCode:
			req.TouchClaude = true
			req.SetClaudeModel, req.ClearClaudeModel = set, c.clear
			prof.ClaudeModel = c.model
			req.ClaudeKey = c.key
			if c.key != prof.APIKey {
				prof.ClaudeKey = c.key
			}
		case apps.ClaudeDesktop:
			req.TouchDesktop = true
			req.SetDesktopModel, req.ClearDesktopModel = set, c.clear
			prof.DesktopModel = c.model
			req.DesktopKey = c.key
			if c.key != prof.APIKey {
				prof.DesktopKey = c.key
			}
		case apps.Codex:
			req.TouchCodex = true
			// Option 1 leaves an existing root model line in place.
			if c.clear || c.model == "" {
				req.SetCodexModel, req.ClearCodexModel = false, false
				prof.CodexModel = ""
			} else {
				req.SetCodexModel, req.ClearCodexModel = true, false
				prof.CodexModel = c.model
			}
			req.CodexKey = c.key
			if c.key != prof.APIKey {
				prof.CodexKey = c.key
			}
		case apps.Grok:
			req.TouchGrok = true
			req.SetGrokModel, req.ClearGrokModel = set, c.clear
			prof.GrokModel = c.model
			req.GrokKey = c.key
			if c.key != prof.APIKey {
				prof.GrokKey = c.key
			}
		case apps.Gemini:
			req.TouchGemini = true
			req.SetGeminiModel, req.ClearGeminiModel = set, c.clear
			prof.GeminiModel = c.model
			req.GeminiKey = c.key
			if c.key != prof.APIKey {
				prof.GeminiKey = c.key
			}
		}
	}
	return prof, req
}

// cancelled ends the wizard after a prompt returned err. Nothing has been
// written yet. End of input (Ctrl-D) exits 2 and Ctrl-C exits 130.
// Ctrl-D on the token re-prompt after a rejection is handled earlier
// and exits 1.
func (e env) cancelled(err error) (int, error) {
	switch {
	case errors.Is(err, errInterrupted):
		fmt.Fprintln(e.stderr, cancelledLine())
		return 130, nil
	case errors.Is(err, io.EOF):
		fmt.Fprintln(e.stderr, cancelledLine())
		return 2, nil
	}
	return 2, err
}

func rolledBackLine() string {
	return ui("Rolled back the files just written.", "已撤销刚才的写入。")
}

// keyNote is the line after a token is kept or reused: Kept (sk-****abcd, 51 chars).
func keyNote(verb, key string) string {
	if promptLocaleZH() {
		return verb + "（" + maskConfirm(key) + "）"
	}
	return verb + " (" + maskConfirm(key) + ")"
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
	offered := writablePrograms()
	for {
		for i, info := range offered {
			fmt.Fprintln(stderr, formatProgramLine(i+1, info))
		}
		line, err := readLine(in, stderr, programsPrompt(zh))
		if err != nil {
			return nil, err
		}
		selection, err := apps.ParseIndexed(line, offered, nil)
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

func writablePrograms() []apps.Info {
	var out []apps.Info
	for _, info := range apps.Catalog() {
		if info.Writable {
			out = append(out, info)
		}
	}
	if len(out) == 0 {
		return apps.Catalog()
	}
	return out
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
		return "多选示例：1,2（英文逗号、中文逗号、顿号或空格都可以分隔）"
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
// A custom gateway shows the URL the token is for.
// reuse is a key chosen earlier in this session. It is offered only when
// this program has no saved key of its own.
func tokenPrompt(program, providerID, baseURL, existing, reuse string) string {
	if existing == "" && reuse != "" {
		if promptLocaleZH() {
			return fmt.Sprintf("%s 令牌（直接回车沿用 %s）：", program, textutil.Mask(reuse))
		}
		return fmt.Sprintf("%s token (Enter to reuse %s): ", program, textutil.Mask(reuse))
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
			return fmt.Sprintf("Enter the gateway token (%s)%s: ", shown, mask)
		}
		return fmt.Sprintf("Enter the gateway token (%s) for %s%s: ", shown, program, mask)
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

const clientDefaultModelEN = "Do not set a model (use the client default)"
const clientDefaultModelZH = "不指定模型（使用客户端默认）"

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
			fmt.Fprintln(stderr, baseURLError(err))
			if !terminal {
				return "", err
			}
			continue
		}
		return base, nil
	}
}

func clientDefaultModelLabel(zh bool) string {
	if zh {
		return clientDefaultModelZH
	}
	return clientDefaultModelEN
}

// modelChoiceLabel is option 1. kept names a Codex model line that Enter
// leaves in place. An empty kept is the client default.
func modelChoiceLabel(zh bool, kept string) string {
	if strings.TrimSpace(kept) == "" {
		return clientDefaultModelLabel(zh)
	}
	if zh {
		return "保留已有模型 " + kept
	}
	return "Keep the existing model " + kept
}

// promptModel probes the family with key, then asks for a model.
// The recovery menu uses it; the main flow already has a probe.
func promptModel(in *bufio.Reader, stderr io.Writer, label, kind string, models []gateway.Model, zh bool, baseURL, key, wire string) (string, bool, error) {
	var probe familyProbe
	if strings.TrimSpace(baseURL) != "" && strings.TrimSpace(key) != "" && len(gateway.FamilyIDs(models, kind)) > 0 {
		fmt.Fprintln(stderr, ui("Checking which models answer...", "正在确认哪些模型有应答……"))
		probe = probeFamily(baseURL, key, kind, wire, models)
		if probe.check.Rejected() {
			fmt.Fprintln(stderr, rejectionLine(probe.check))
		}
	}
	return pickModel(in, stderr, label, kind, models, zh, baseURL, key, wire, probe, "")
}

// pickModel lists option 1, then only the ids that answered the probe.
// kept is an existing Codex model line that option 1 leaves in place.
// An empty kept means option 1 uses the client default. The recovery menu
// passes an empty kept. When the probe did not run, the list is the
// family's chat ids so a caller without a gateway can still choose.
// A typed id that was not in the verified list is rejected when that list
// is non-empty. When nothing answered, a typed id is checked once.
func pickModel(in *bufio.Reader, stderr io.Writer, label, kind string, models []gateway.Model, zh bool, baseURL, key, wire string, probe familyProbe, kept string) (string, bool, error) {
	ids := gateway.FamilyIDs(models, kind)
	verified := map[string]bool{}
	for _, id := range probe.passed {
		verified[id] = true
	}
	failed := map[string]bool{}
	for _, skip := range probe.skipped {
		failed[skip.ID] = true
		line := skipReason(skip.Check)
		fmt.Fprintln(stderr, ui(
			fmt.Sprintf("Skipped %s: %s", skip.ID, line),
			fmt.Sprintf("已跳过 %s：%s", skip.ID, line),
		))
	}
	var matched []string
	if probe.ran {
		matched = append(matched, probe.passed...)
	} else {
		matched = orderedModelIDs(ids, "", failed)
	}
	switch {
	case len(ids) == 0 && zh && kept != "":
		fmt.Fprintf(stderr, "网关没有列出 %s 可用的 %s 模型。直接回车保留已有模型 %s，也可以输入模型 id。\n", label, kind, kept)
	case len(ids) == 0 && kept != "":
		fmt.Fprintf(stderr, "No %s model id containing %q was listed. Press Enter to keep the existing model %s, or type an id.\n", label, kind, kept)
	case len(ids) == 0 && zh:
		fmt.Fprintf(stderr, "网关没有列出 %s 可用的 %s 模型。直接回车不指定模型，也可以输入模型 id。\n", label, kind)
	case len(ids) == 0:
		fmt.Fprintf(stderr, "No %s model id containing %q was listed. Press Enter to leave the model unset, or type an id.\n", label, kind)
	case len(matched) == 0 && zh && kept != "":
		fmt.Fprintf(stderr, "%s 的模型都没有通过测试请求。直接回车保留已有模型 %s，也可以输入模型 id。\n", label, kept)
	case len(matched) == 0 && kept != "":
		fmt.Fprintf(stderr, "No %s model answered a test request. Press Enter to keep the existing model %s, or type an id.\n", label, kept)
	case len(matched) == 0 && zh:
		fmt.Fprintf(stderr, "%s 的模型都没有通过测试请求。直接回车不指定模型，也可以输入模型 id。\n", label)
	case len(matched) == 0:
		fmt.Fprintf(stderr, "No %s model answered a test request. Press Enter to leave the model unset, or type an id.\n", label)
	case zh && kept != "":
		fmt.Fprintf(stderr, "%s 模型（直接回车保留已有模型 %s）：\n", label, kept)
	case kept != "":
		fmt.Fprintf(stderr, "%s model (Enter keeps the existing model %s):\n", label, kept)
	case zh:
		fmt.Fprintf(stderr, "%s 模型（直接回车不指定模型，使用客户端默认）：\n", label)
	default:
		fmt.Fprintf(stderr, "%s model (Enter does not set a model; the client keeps its own default):\n", label)
	}
	fmt.Fprintf(stderr, "  1) %s\n", modelChoiceLabel(zh, kept))
	for i, id := range matched {
		mark := ""
		if verified[id] {
			mark = ui("  (answered a test request)", "（已通过测试）")
		}
		fmt.Fprintf(stderr, "  %d) %s%s\n", i+2, id, mark)
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
		model, clear, err := chooseModel(line, matched, failed)
		if err != nil {
			fmt.Fprintln(stderr, modelRetryMessage(err))
			continue
		}
		if model != "" && !verified[model] && strings.TrimSpace(baseURL) != "" && strings.TrimSpace(key) != "" {
			fmt.Fprintln(stderr, ui(fmt.Sprintf("Testing %s ...", model), fmt.Sprintf("正在测试 %s……", model)))
			check := probeOne(baseURL, key, kind, wire, model)
			if !check.OK {
				fmt.Fprintln(stderr, ui(
					fmt.Sprintf("%s did not answer a test request. Choose another model.", model),
					fmt.Sprintf("%s 没有通过测试请求，请另选一个模型。", model),
				))
				if hint := localHint(check); hint != "" {
					fmt.Fprintln(stderr, "  "+hint)
				}
				continue
			}
		}
		return model, clear, nil
	}
}

// orderedModelIDs lists probe-verified ids first and drops ids that failed a probe.
func orderedModelIDs(ids []string, verified string, failed map[string]bool) []string {
	var out []string
	if verified != "" && !failed[verified] {
		out = append(out, verified)
	}
	for _, id := range ids {
		if id == "" || id == verified || failed[id] {
			continue
		}
		out = append(out, id)
	}
	return out
}

// chooseModel returns the selected id.
// Option 1, Enter, and "default" leave the model unset so the client keeps
// its own default. Option 2 is ids[0]. A typed id must be listed when ids
// is non-empty. An id in failed already failed a probe.
func chooseModel(answer string, ids []string, failed map[string]bool) (string, bool, error) {
	answer = strings.TrimSpace(answer)
	if answer == "" || answer == "1" || strings.EqualFold(answer, "default") {
		return "", true, nil
	}
	if n, err := strconv.Atoi(answer); err == nil {
		if n < 2 || n > len(ids)+1 {
			return "", false, fmt.Errorf("model %d is out of range", n)
		}
		return ids[n-2], false, nil
	}
	for _, id := range ids {
		if answer == id {
			return answer, false, nil
		}
	}
	if failed[answer] {
		return "", false, errModelFailed
	}
	if len(ids) == 0 && plausibleModelID(answer) {
		return answer, false, nil
	}
	return "", false, fmt.Errorf("not a model id")
}

var errModelFailed = errors.New("that model did not answer a test request")

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

// announceWrite lists the files and says a backup can undo the write.
// It does not ask. The wizard and setup both write after this line.
func announceWrite(stderr io.Writer, e env, prof store.Profile, req apply.Request) {
	fmt.Fprintln(stderr, ui("These files will be modified:", "将修改这些文件："))
	for _, path := range filesToTouch(e, prof, req) {
		fmt.Fprintf(stderr, "  %s\n", path)
	}
	fmt.Fprintln(stderr, ui(
		"A backup is saved first. Undo with: deeprouter restore",
		"修改前会留下备份。撤销请运行：deeprouter restore",
	))
}

// filesToTouch is writePaths for display. The Windows user environment is
// shown as HKCU\Environment rather than deeprouter's record of it.
func filesToTouch(e env, prof store.Profile, req apply.Request) []string {
	record := ""
	if e.userEnv {
		record = userenv.RecordPath(e.layout.ConfigDir)
	}
	var out []string
	for _, path := range e.writePaths(prof, req) {
		if record != "" && path == record {
			out = append(out, userenv.Location)
			continue
		}
		out = append(out, e.show(path))
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// zhUI reports whether output is Simplified Chinese; see promptLocaleZH.
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

func platformDefault(list []provider.Preset) (int, error) {
	p, explicit, err := providerFromEnv()
	if err != nil || !explicit {
		return 1, err
	}
	for i, item := range list {
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

func promptPlatform(in *bufio.Reader, stderr io.Writer, zh bool, clients []apps.ID) (string, bool, error) {
	list := provider.ForClients(clients)
	def, err := platformDefault(list)
	if err != nil {
		return "", false, err
	}
	last := len(list) + 1
	intro := "Choose a platform for the selected programs. Press Enter for the default."
	custom := "Custom base URL"
	word := "Platform"
	defMark := " (default)"
	outOfRange := "platform %d is out of range"
	badChoice := fmt.Sprintf("Choose a number from 1 to %d.", last)
	if zh {
		intro = "选择这些程序使用的平台。直接回车使用默认项。"
		custom = "自定义网关地址"
		word = "平台"
		defMark = "（默认）"
		outOfRange = "平台 %d 超出范围"
		badChoice = fmt.Sprintf("请选择 1 到 %d。", last)
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

// readLine prints label and reads one answer. End of input with nothing
// typed, which is Ctrl-D in a terminal, returns io.EOF.
func readLine(in *bufio.Reader, stderr io.Writer, label string) (string, error) {
	fmt.Fprint(stderr, label)
	s, err := in.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", err
	}
	s = strings.TrimSpace(s)
	if err != nil && s == "" {
		fmt.Fprintln(stderr)
		return "", io.EOF
	}
	return s, nil
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
		fmt.Fprintln(e.stdout, cliRunLine(false, "claude"))
	}
	if req.TouchCodex {
		fmt.Fprintln(e.stdout, cliRunLine(req.TouchClaude, "codex"))
	}
	if req.TouchGrok {
		fmt.Fprintln(e.stdout, cliRunLine(false, "grok"))
	}
	if req.TouchGemini {
		fmt.Fprintln(e.stdout, cliRunLine(false, "gemini"))
	}
	if line := reopenHint(req.TouchCodex, req.TouchDesktop, req.TouchShell || req.TouchGemini); line != "" {
		fmt.Fprintln(e.stdout, line)
	}
	if req.TouchGrok {
		fmt.Fprintln(e.stdout, ui("Start a new Grok Build session so it reads config.toml.", "新开一个 Grok Build 会话，以便读取 config.toml。"))
	}
}

// probeFamily finds a model of family that answers a tiny request with key.
func probeFamily(baseURL, key, family, wire string, models []gateway.Model) familyProbe {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return familyProbe{ran: true, check: gateway.Check{Hint: err.Error()}}
	}
	result := gateway.VerifiedModels(ctx, gateway.ProbeOptions{
		Endpoints: ep,
		APIKey:    key,
		CodexWire: wire,
		Timeout:   gateway.VerifyProbeTimeout,
	}, models, family, wire)
	return familyProbe{ran: true, check: result.Check, skipped: result.Skipped, passed: result.Passed}
}

func probeOne(baseURL, key, family, wire, id string) gateway.Check {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	ep, err := gateway.ParseBase(baseURL)
	if err != nil {
		return gateway.Check{Hint: err.Error(), Detail: id}
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

// recover runs after the connectivity test failed. The user can pick other
// models, which are written and tested again, or roll back. An error or a
// report that still fails means the caller rolls back.
func (s *session) recover(guard *interruptGuard, snap writeSnapshot, prof *store.Profile, req *apply.Request, report gateway.Report) (gateway.Report, error) {
	e := s.e
	for round := 0; round < 4 && !report.OK; round++ {
		fmt.Fprintln(e.stderr, ui(
			"The connectivity test failed. The files above were already written.",
			"连通性测试没有通过。上面的文件已经写入。",
		))
		for _, check := range report.Checks {
			if check.OK || check.Skipped {
				continue
			}
			fmt.Fprintf(e.stderr, "  %s %s  %s\n", check.Method, check.Path, localHint(check))
		}
		fmt.Fprintln(e.stderr, ui("1) Pick another model", "1) 换一个模型"))
		fmt.Fprintln(e.stderr, ui("2) Roll back the files just written", "2) 撤销刚才的写入"))
		var answer string
		for {
			line, err := readLine(s.in, e.stderr, ui("Choice [2]: ", "选择 [2]: "))
			if err != nil {
				return report, err
			}
			answer = strings.ToLower(strings.TrimSpace(line))
			if answer == "" || answer == "1" || answer == "2" || answer == "rollback" {
				break
			}
			fmt.Fprintln(e.stderr, ui("Enter 1 or 2.", "请输入 1 或 2。"))
		}
		if answer != "1" {
			return report, nil
		}
		changed, err := s.repromptFailed(prof, req, report)
		if err != nil {
			return report, err
		}
		if err := guard.write(snap, func() error {
			_, saveErr := e.saveProfile(*prof, *req)
			return saveErr
		}); err != nil {
			return report, err
		}
		for _, line := range changed {
			fmt.Fprintln(e.stderr, line)
		}
		var ep gateway.Endpoints
		ep, report, err = probeTarget(wizardProbeTarget(*prof, *req), 30*time.Second)
		if err != nil {
			return report, err
		}
		fmt.Fprintln(e.stdout)
		printReport(e.stdout, target{name: prof.Name, label: prof.Label, key: prof.APIKey}, ep, report)
	}
	return report, nil
}

// repromptFailed asks again for the model of each program whose check
// failed, and returns one line per program saying what will be written.
func (s *session) repromptFailed(prof *store.Profile, req *apply.Request, report gateway.Report) ([]string, error) {
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
	var changed []string
	for _, id := range ids {
		info, ok := apps.ByID(id)
		if !ok {
			continue
		}
		key := prof.KeyFor(string(id))
		model, clear, err := promptModel(s.in, s.e.stderr, info.Title, info.ModelKind, s.catalogs[key], s.zh, prof.BaseURL, key, prof.WireAPI)
		if err != nil {
			return nil, err
		}
		set := model != "" && !clear
		switch id {
		case apps.ClaudeCode:
			prof.ClaudeModel = model
			req.SetClaudeModel, req.ClearClaudeModel = set, clear
		case apps.ClaudeDesktop:
			prof.DesktopModel = model
			req.SetDesktopModel, req.ClearDesktopModel = set, clear
		case apps.Codex:
			// Option 1 removes a model this run just wrote. It does not
			// delete a line the first choice left untouched.
			if clear && !req.SetCodexModel {
				prof.CodexModel = ""
				req.SetCodexModel, req.ClearCodexModel = false, false
				break
			}
			prof.CodexModel = model
			req.SetCodexModel, req.ClearCodexModel = set, clear
		case apps.Grok:
			prof.GrokModel = model
			req.SetGrokModel, req.ClearGrokModel = set, clear
		case apps.Gemini:
			prof.GeminiModel = model
			req.SetGeminiModel, req.ClearGeminiModel = set, clear
		}
		if model == "" {
			changed = append(changed, ui(
				fmt.Sprintf("%s: no model set; the client uses its own default.", info.Title),
				fmt.Sprintf("%s 不指定模型，使用客户端默认。", info.Title),
			))
			continue
		}
		changed = append(changed, ui(
			fmt.Sprintf("Saved %s model: %s", info.Title, model),
			fmt.Sprintf("已写入 %s 模型：%s", info.Title, model),
		))
	}
	return changed, nil
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
	var n int
	outOfRange := false
	if _, scanErr := fmt.Sscanf(msg, "model %d is out of range", &n); scanErr == nil && strings.Contains(msg, "out of range") {
		outOfRange = true
	}
	switch {
	case errors.Is(err, errModelFailed):
		return ui("That model did not answer a test request. Choose another one, or press Enter.", "这个模型没有通过测试请求。请另选一个，或直接回车。")
	case outOfRange:
		return ui(fmt.Sprintf("Model %d is out of range. Enter a listed number, a listed id, or press Enter.", n), fmt.Sprintf("模型 %d 超出范围。请输入列表中的编号或 id，或直接回车。", n))
	default:
		return ui("That is not a model id. Enter a listed number, a listed id, or press Enter.", "这不是模型 id。请输入列表中的编号或 id，或直接回车。")
	}
}

func keyRetryMessage(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "empty"):
		return ui("The token is empty. Enter it again.", "令牌不能为空，请重新输入。")
	case strings.Contains(msg, "single line"):
		return ui("The token must be a single line. Enter it again.", "令牌必须是一行，请重新输入。")
	case strings.Contains(msg, "UTF-8"):
		return ui("The token must be valid UTF-8. Enter it again.", "令牌必须是有效的 UTF-8，请重新输入。")
	case strings.Contains(msg, "too long"):
		return ui("The token is too long. Enter it again.", "令牌太长，请重新输入。")
	default:
		return ui("That token is not valid. Enter it again.", "令牌无效，请重新输入。")
	}
}

// maskConfirm is "sk-****abcd, 51 chars", or "sk-****abcd，51 个字符" in Chinese.
func maskConfirm(secret string) string {
	s := textutil.MaskConfirm(secret)
	if promptLocaleZH() {
		s = strings.Replace(s, ", ", "，", 1)
		s = strings.Replace(s, " chars", " 个字符", 1)
	}
	return s
}
