package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/imageconfig"
	"github.com/zfg5118/deeprouter/internal/imageskill"
	"github.com/zfg5118/deeprouter/internal/provider"
	"github.com/zfg5118/deeprouter/internal/textutil"
)

// cmdMainMenu is deeprouter with no arguments in a terminal: the program
// setup wizard, whose program list also offers the optional Image Skill
// row. Enter is Codex / ChatGPT only, as before, and installs no Skill.
func cmdMainMenu(stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	return toolWizard(stdout, stderr, stdin, nil, true)
}

// imageSkillEntry is the Image Skill row of the program list: the deeprouter
// skill install targets, then the image API address, token, and model.
// programs are the programs chosen in the same list; the
// Skill is installed for them, or the Skill program list is asked when
// there are none. Running it again updates the Skill if needed and replaces
// the address, token, and model.
func imageSkillEntry(e env, in *bufio.Reader, programs []apps.ID, draft *imageSetupDraft, priorWrite bool) (int, error) {
	stdout, stderr := e.stdout, e.stderr
	loc, err := skillLocations()
	if err != nil {
		return 1, err
	}
	var targets []apps.ID
	for _, id := range programs {
		if imageskill.Supported(id) {
			targets = append(targets, id)
		}
	}
	g := guardSkill(stderr, priorWrite)
	defer g.stop()
	fmt.Fprintln(stderr, ui("Image Skill", "生图Skill"))
	fmt.Fprintln(stderr, ui(
		"Sets the image API address, token, and model, then installs or updates the bundled image Skill for this user.",
		"设置生图接口地址、令牌和模型，然后为当前用户安装或更新内置的生图Skill。",
	))
	if len(draft.targets) > 0 && len(targets) == 0 {
		targets = draft.targets
	}
	askTargets := len(programs) == 0 || len(targets) == 0
	for {
		if askTargets {
			targets, err = promptSkillTargets(in, stderr, targets, false)
			if err != nil {
				return imageCancelled(stderr, err, priorWrite)
			}
		}
		plans, err := prepareSkillInstall(in, stderr, loc, targets)
		if errors.Is(err, errBack) {
			askTargets = true
			continue
		}
		if err != nil {
			return imageCancelled(stderr, err, priorWrite)
		}
		draft.targets = targets
		code, err := configureImageAPI(e, in, g, skillPlanApps(plans), draft, priorWrite, func() error {
			return saveSkillInstall(stdout, stderr, loc, g, plans)
		})
		if !errors.Is(err, errBack) {
			return code, err
		}
		askTargets = true
	}
}

type imageSetupDraft struct {
	targets    []apps.ID
	preset     string
	customBase string
	keys       map[[2]string]string
	visible    map[[2]string][]string
}

func skillPlanApps(plans []imageskill.Plan) []apps.ID {
	var ids []apps.ID
	for _, id := range imageskill.Targets() {
		for _, p := range plans {
			if apps.Contains(p.Apps, id) {
				ids = append(ids, id)
				break
			}
		}
	}
	return ids
}

// configureImageAPI asks for the image address, token, and model and saves
// them to ~/.deeprouter/image-skill.json only. targets are the programs the
// Skill was installed for; their saved tokens are the current token, as in
// the program wizard. The only request it makes is GET /v1/models to the
// chosen address, sent right after the token step, to list the models the
// token can see. Saving never depends on that request.
func configureImageAPI(e env, in *bufio.Reader, g *skillGuard, targets []apps.ID, draft *imageSetupDraft, priorWrite bool, install func() error) (int, error) {
	stderr := e.stderr
	zh := promptLocaleZH()
	path := imageconfig.Path(e.layout.ConfigDir)
	saved, savedOK, err := imageconfig.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, imageNotSavedLine(priorWrite))
		return 1, err
	}
	fmt.Fprintln(stderr)
	if savedOK && saved.Complete() {
		fmt.Fprintln(stderr, ui(
			fmt.Sprintf("Current image address: %s (token %s)", saved.BaseURL, textutil.Mask(saved.APIKey)),
			fmt.Sprintf("当前生图地址：%s（令牌 %s）", saved.BaseURL, textutil.Mask(saved.APIKey)),
		))
	}
	intro := ui(
		"Choose the image API address. Press Enter for the default. It is used only by the image Skill; other programs keep their settings.",
		"选择生图接口地址。直接回车使用默认项。这个地址只给生图Skill用，其他程序的配置不变。",
	)
	if draft.keys == nil {
		draft.keys = make(map[[2]string]string)
		draft.visible = make(map[[2]string][]string)
	}
	list := provider.Hosted()
addressStep:
	p, custom, err := choosePreset(in, stderr, zh, list, presetDefault(list, 1, draft.preset), intro, ui("Address", "地址"))
	if err != nil {
		return imageCancelled(stderr, err, priorWrite)
	}
	id := p.ID
	if custom {
		id = provider.CustomID
	}
	draft.preset = id
	var base string
customAddressStep:
	if custom {
		base, err = promptImageBase(in, stderr, draft.customBase)
		if errors.Is(err, errBack) {
			goto addressStep
		}
		if err != nil {
			return imageCancelled(stderr, err, priorWrite)
		}
		draft.customBase = base
	} else if base, err = imageconfig.ParseBase(p.BaseURL); err != nil {
		return 1, err
	}
	pick, key, err := promptImageDetails(e, in, targets, draft, saved, savedOK, id, base)
	if errors.Is(err, errBack) {
		if custom {
			goto customAddressStep
		}
		goto addressStep
	}
	if err != nil {
		return imageCancelled(stderr, err, priorWrite)
	}
	return saveImageAPI(e, g, id, base, key, pick, len(draft.visible), install)
}

func promptImageDetails(e env, in *bufio.Reader, targets []apps.ID, draft *imageSetupDraft, saved imageconfig.Config, savedOK bool, id, base string) (imageModelPick, string, error) {
	stderr := e.stderr
	fmt.Fprintln(stderr, ui(
		fmt.Sprintf("Image API address %s. The token is sent only to %s.", base, imageconfig.Host(base)),
		fmt.Sprintf("生图接口地址 %s。令牌只会发送到 %s。", base, imageconfig.Host(base)),
	))

	// The current token is the one already saved for this image address, or
	// else the one a selected program has for the same provider and host.
	// A token for a different host is never offered.
	source := [2]string{id, base}
	current, verb := draft.keys[source], ui("Kept", "已保留")
	if current == "" {
		if savedOK && saved.Complete() && provider.SameKeySource(saved.Provider, saved.BaseURL, id, base) {
			current = saved.APIKey
		} else {
			verb = ui("Reused", "已沿用")
			for _, app := range targets {
				if current = existingAppKey(e, app, id, base); current != "" {
					break
				}
			}
		}
	}
	savedModel := ""
	if savedOK {
		savedModel = saved.Model
	}
	for {
		key, err := askImageKey(e, in, tokenPrompt(ui("Image Skill", "生图Skill"), id, base, current, ""), current, verb)
		if err != nil {
			return imageModelPick{}, "", err
		}
		draft.keys[source] = key
		current, verb = key, ui("Kept", "已保留")
		query := [2]string{base, key}
		visible, cached := draft.visible[query]
		if !cached {
			visible = loadImageModels(stderr, base, key)
			draft.visible[query] = visible
		}
		pick, err := chooseImageModel(in, stderr, base, visible, savedModel)
		if errors.Is(err, errBack) {
			continue
		}
		return pick, key, err
	}
}

func saveImageAPI(e env, g *skillGuard, id, base, key string, pick imageModelPick, catalogs int, install func() error) (int, error) {
	if err := install(); err != nil {
		return 1, err
	}
	stderr := e.stderr
	path := imageconfig.Path(e.layout.ConfigDir)
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui("deeprouter will write:", "将写入："))
	fmt.Fprintf(stderr, "  %s\n", e.show(path))
	cfg := imageconfig.Config{Provider: id, BaseURL: base, APIKey: key, Model: pick.id}
	if err := g.saveImage(func() error { return imageconfig.Save(path, cfg) }); err != nil {
		fmt.Fprintln(stderr, imageNotSavedLine(g.wrote || g.priorWrite))
		return 1, err
	}
	out := e.stdout
	fmt.Fprintln(out)
	fmt.Fprintln(out, ui("Saved the image Skill address, token, and model:", "已保存生图Skill的地址、令牌和模型："))
	fmt.Fprintf(out, "  %s %s\n", ui("Address", "地址"), base)
	fmt.Fprintf(out, "  %s %s\n", ui("Token  ", "令牌"), textutil.Mask(key))
	fmt.Fprintf(out, "  %s %s %s\n", ui("Model  ", "模型"), pick.id, pick.note())
	fmt.Fprintf(out, "  %s %s\n", ui("File   ", "文件"), e.show(path))
	fmt.Fprintln(out, ui(
		"The file is readable only by you (mode 0600). It is not encrypted.",
		"这个文件只有你能读取（权限 0600），但没有加密。",
	))
	fmt.Fprintln(out, ui(
		"Only the image Skill uses it. Codex, Claude Code, and the other program settings were not changed.",
		"只有生图Skill使用它。Codex、Claude Code 和其他程序的配置没有改动。",
	))
	requestSummary := ui(
		fmt.Sprintf("Only GET /v1/models was sent, to %s, to list models.", imageconfig.Host(base)),
		fmt.Sprintf("只向 %s 发送了 GET /v1/models 来获取模型列表。", imageconfig.Host(base)),
	)
	if catalogs > 1 {
		requestSummary = ui("The image setup sent only GET /v1/models requests for the addresses and tokens entered.", "生图配置仅针对输入的地址和令牌发送了 GET /v1/models 请求。")
	}
	fmt.Fprintln(out, requestSummary+ui(" Nothing was generated or billed. Saving did not check that the address, token, or model works.", "没有生图，不产生费用。保存时没有检查地址、令牌或模型是否可用。"))
	fmt.Fprintln(out, ui(
		"The Skill sends this model unless --model is given.",
		"Skill 默认发送这个模型；命令里写了 --model 时以 --model 为准。",
	))
	fmt.Fprintln(out, ui(
		"When DEEPROUTER_IMAGE_BASE_URL and DEEPROUTER_IMAGE_API_KEY are both set, the Skill uses them instead of this file.",
		"同时设置了 DEEPROUTER_IMAGE_BASE_URL 和 DEEPROUTER_IMAGE_API_KEY 时，Skill 改用这两个环境变量，不读这个文件。",
	))
	return 0, nil
}

// imageModelsTimeout bounds the GET /v1/models request of the model step.
const imageModelsTimeout = 20 * time.Second

// imageCatalog lists what key can see at base with GET {base}/v1/models,
// the free request the program wizard also uses. It never generates or
// probes a model. Tests replace it; they make no network request.
var imageCatalog = func(base, key string) ([]gateway.Model, gateway.Check, error) {
	return fetchModels(base, key, imageModelsTimeout)
}

// imageModelPick is the model the Image Skill entry saves.
type imageModelPick struct {
	id string
	// listed: chosen from this token's filtered GET /v1/models list.
	listed bool
	// typed: entered by hand, whether or not it is listed.
	typed bool
}

func (p imageModelPick) note() string {
	switch {
	case p.typed:
		return ui("(entered by you; not checked)", "（手动输入，未检查）")
	case p.listed:
		return ui("(on this token's model list; not test-generated)", "（在令牌可见列表中，未试生成）")
	}
	return ui("(unverified)", "（未验证）")
}

// loadImageModels fetches the image models this token can see at base. It
// sends GET /v1/models right away, without asking first; the list is
// filtered by gateway.FilterImages, and a model on it is visible to the
// token, which does not mean the account or channel may use it. When the
// request fails or the list has no image model, the menu offers the current
// model and the Skill default as unverified settings. Entering a model ID is
// always offered. Nothing here blocks the save.
func loadImageModels(stderr io.Writer, base, key string) []string {
	host := imageconfig.Host(base)
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui(
		fmt.Sprintf("The model list comes from GET /v1/models on %s, sent with this token to that host only. It is free and generates nothing. Saving does not need the list to load.", host),
		fmt.Sprintf("模型列表来自 %s 的 GET /v1/models，令牌只发给这个地址。这个请求免费，不会生图。获取失败也可以保存。", host),
	))
	fmt.Fprintln(stderr, ui(
		fmt.Sprintf("Fetching models from %s ...", base),
		fmt.Sprintf("正在从 %s 获取模型列表……", base),
	))
	var visible []string
	models, check, err := imageCatalog(base, key)
	if err != nil {
		reason := localHint(check)
		if reason == "" {
			reason = err.Error()
		}
		fmt.Fprintln(stderr, ui("Could not load the model list: ", "无法获取模型列表：")+reason)
	} else {
		for _, id := range gateway.ModelIDs(gateway.FilterImages(models)) {
			if _, err := imageconfig.ParseModel(id); err == nil && id == strings.TrimSpace(id) {
				visible = append(visible, id)
			}
		}
		if len(visible) == 0 {
			fmt.Fprintln(stderr, ui(
				fmt.Sprintf("This token's list (%d models) has no image model this Skill can call with the Images API (POST /v1/images/generations, /v1/images/edits).", len(models)),
				fmt.Sprintf("这个令牌的模型列表（%d 个）里没有本 Skill 能用 Images 接口（POST /v1/images/generations、/v1/images/edits）调用的绘画模型。", len(models)),
			))
		}
	}

	return visible
}

func chooseImageModel(in *bufio.Reader, stderr io.Writer, base string, visible []string, current string) (imageModelPick, error) {
	host := imageconfig.Host(base)
	type entry struct {
		label  string
		id     string
		listed bool
	}
	var entries []entry
	def := 1
	if len(visible) > 0 {
		fmt.Fprintln(stderr, ui(
			fmt.Sprintf("Image models visible to this token on %s. Visible does not mean your account or channel may use a model, and none was test-generated:", host),
			fmt.Sprintf("令牌可见的绘画模型（%s）。可见不代表账号或渠道有权限使用，也没有试生成：", host),
		))
		onList := false
		for i, id := range visible {
			label := id
			if id == current {
				label += ui(" (current)", "（当前）")
				def, onList = i+1, true
			}
			entries = append(entries, entry{label, id, true})
		}
		if current != "" && !onList {
			entries = append(entries, entry{ui(
				fmt.Sprintf("Keep current: %s (not on this list; unverified)", current),
				fmt.Sprintf("保留当前：%s（不在这个列表里，未验证）", current),
			), current, false})
		}
	} else {
		fmt.Fprintln(stderr, ui(
			"There is no token-visible model list. You can still save. These are saved settings only and were not checked against this token:",
			"没有令牌可见的模型列表，仍然可以保存。下面只是保存的设置，没有针对这个令牌检查：",
		))
		if current != "" {
			entries = append(entries, entry{ui(
				fmt.Sprintf("Keep current: %s (unverified)", current),
				fmt.Sprintf("保留当前：%s（未验证）", current),
			), current, false})
		}
		if current != imageconfig.DefaultModel {
			entries = append(entries, entry{ui(
				fmt.Sprintf("%s: the Skill default (unverified)", imageconfig.DefaultModel),
				fmt.Sprintf("%s：Skill 默认模型（未验证）", imageconfig.DefaultModel),
			), imageconfig.DefaultModel, false})
		}
	}
	manual := len(entries) + 1
	defMark := ui(" (default)", "（默认）")
	for {
		for i, en := range entries {
			mark := ""
			if i+1 == def {
				mark = defMark
			}
			fmt.Fprintf(stderr, "  %d) %s%s\n", i+1, en.label, mark)
		}
		fmt.Fprintf(stderr, "  %d) %s\n", manual, ui("Enter a model ID (typed by you; not checked)", "手动输入模型ID（不检查是否可用）"))
		printMenuZero(stderr, false)
		line, err := readLine(in, stderr, fmt.Sprintf("%s [%d]: ", ui("Model", "模型"), def))
		if err != nil {
			return imageModelPick{}, err
		}
		if line == "0" {
			return imageModelPick{}, errBack
		}
		if line == "" {
			line = strconv.Itoa(def)
		}
		n, convErr := strconv.Atoi(line)
		if convErr != nil || n < 1 || n > manual {
			fmt.Fprintln(stderr, ui(fmt.Sprintf("Choose a number from 1 to %d.", manual), fmt.Sprintf("请选择 1 到 %d。", manual)))
			continue
		}
		if n < manual {
			return imageModelPick{id: entries[n-1].id, listed: entries[n-1].listed}, nil
		}
		id, err := promptImageModelID(in, stderr)
		if errors.Is(err, errBack) {
			continue
		}
		if err != nil {
			return imageModelPick{}, err
		}
		return imageModelPick{id: id, typed: true}, nil
	}
}

// promptImageModelID reads a model id until its form is valid. It may be
// any id; it does not have to be on the model list.
func promptImageModelID(in *bufio.Reader, stderr io.Writer) (string, error) {
	for {
		line, err := readBackLine(in, stderr, ui("Model ID: ", "模型ID："))
		if err != nil {
			return "", err
		}
		id, err := imageconfig.ParseModel(line)
		if err == nil && !plausibleModelID(id) {
			err = fmt.Errorf("model id is not valid")
		}
		if err != nil {
			fmt.Fprintln(stderr, localError(err.Error()))
			continue
		}
		return id, nil
	}
}

// promptImageBase asks for a custom image address until it is valid.
// Unlike the program gateway, it must be https and has no Enter default.
func promptImageBase(in *bufio.Reader, stderr io.Writer, current string) (string, error) {
	prompt := ui("Image API address (https://...): ", "生图接口地址（https://...）：")
	if current != "" {
		prompt = ui(fmt.Sprintf("Image API address (Enter keeps %s): ", current), fmt.Sprintf("生图接口地址（回车保留 %s）：", current))
	}
	for {
		line, err := readBackLine(in, stderr, prompt)
		if err != nil {
			return "", err
		}
		if line == "" && current != "" {
			return current, nil
		}
		base, err := imageconfig.ParseBase(line)
		if err != nil {
			fmt.Fprintln(stderr, localError(err.Error()))
			continue
		}
		return base, nil
	}
}

// askImageKey reads the image token the way the program wizard does: hidden
// input, Enter keeps current when there is one, and an empty answer with
// nothing to keep asks again.
func askImageKey(e env, in *bufio.Reader, prompt, current, verb string) (string, error) {
	for {
		backHint(e.stderr)
		line, err := promptSecretLine(e.stdin, e.stderr, in, prompt)
		if err != nil {
			return "", err
		}
		if backCommand(line) {
			return "", errBack
		}
		switch {
		case line == "" && current != "":
			fmt.Fprintln(e.stderr, keyNote(verb, current))
			return current, nil
		case line == "":
			fmt.Fprintln(e.stderr, ui("The token is empty. Enter it again.", "令牌不能为空，请重新输入。"))
			continue
		}
		key, err := textutil.ValidateKey(line)
		if err != nil {
			fmt.Fprintln(e.stderr, keyRetryMessage(err))
			continue
		}
		return key, nil
	}
}

func imageNotSavedLine(installed bool) string {
	if installed {
		return ui(
			"The image address and token were not saved. Earlier saves remain.",
			"生图地址和令牌没有保存。之前的保存仍然有效。",
		)
	}
	return ui("Nothing was written.", "没有写入任何文件。")
}

// imageCancelled ends the Image Skill entry after a prompt returned err.
// The image config file is untouched. End of input exits 2, Ctrl-C at a
// hidden prompt exits 130.
func imageCancelled(stderr io.Writer, err error, installed bool) (int, error) {
	code := 2
	switch {
	case errors.Is(err, errInterrupted):
		code = 130
	case !errors.Is(err, io.EOF) && !errors.Is(err, errSkillDeclined):
		return 1, err
	}
	if installed {
		fmt.Fprintln(stderr, ui("Cancelled.", "已取消。")+" "+imageNotSavedLine(true))
	} else {
		fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
	}
	return code, nil
}
