package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"os/signal"
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

// cmdMainMenu is deeprouter with no arguments in a terminal. Enter takes the
// program setup wizard, as before the menu existed. The image Skill entry is
// optional and never runs as part of program setup.
func cmdMainMenu(stdout, stderr io.Writer, stdin io.Reader) (int, error) {
	in := bufio.NewReader(stdin)
	// Ctrl-C at the menu cancels like Ctrl-C at the wizard's first question.
	// Each entry installs its own handler, so this one stops first.
	sig := make(chan os.Signal, 1)
	quit := make(chan struct{})
	signal.Notify(sig, os.Interrupt)
	go func() {
		select {
		case <-sig:
			fmt.Fprintln(stderr)
			fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
			exitProcess(130)
		case <-quit:
		}
	}()
	stopSignals := func() {
		signal.Stop(sig)
		close(quit)
	}
	line, err := chooseMainMenu(in, stderr)
	stopSignals()
	if err != nil {
		if errors.Is(err, io.EOF) {
			fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
			return 2, nil
		}
		return 1, err
	}
	switch line {
	case "1":
		fmt.Fprintln(stderr)
		return toolWizard(stdout, stderr, stdin, in)
	case "2":
		fmt.Fprintln(stderr)
		return imageSkillMenu(stdout, stderr, stdin, in)
	}
	fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
	return 2, nil
}

// chooseMainMenu prints the menu and returns "1", "2", or "3".
func chooseMainMenu(in *bufio.Reader, stderr io.Writer) (string, error) {
	fmt.Fprintln(stderr, "DeepRouter")
	fmt.Fprintf(stderr, "  1) %s\n", ui("Configure programs (Codex, Claude Code, ...) (default)", "配置程序（Codex、Claude Code 等）（默认）"))
	fmt.Fprintf(stderr, "  2) %s\n", ui("Image Skill: install, then set its own address and token", "生图Skill：安装，并设置它单独使用的地址和令牌"))
	fmt.Fprintf(stderr, "  3) %s\n", ui("Quit", "退出"))
	for {
		line, err := readLine(in, stderr, ui("Choose [1]: ", "选择 [1]："))
		if err != nil {
			return "", err
		}
		switch strings.ToLower(line) {
		case "", "1":
			return "1", nil
		case "2":
			return "2", nil
		case "3", "q":
			return "3", nil
		}
		fmt.Fprintln(stderr, ui("Choose 1, 2, or 3.", "请选择 1、2 或 3。"))
	}
}

// imageSkillMenu is the main menu's Image Skill entry: the deeprouter skill
// install steps, then the image API address and token. Running it again
// updates the Skill if needed and replaces the address and token.
func imageSkillMenu(stdout, stderr io.Writer, stdin io.Reader, in *bufio.Reader) (int, error) {
	e, err := loadEnv(stderr, stdin, stdout)
	if err != nil {
		return 1, err
	}
	loc, err := skillLocations()
	if err != nil {
		return 1, err
	}
	g := guardSkill(stderr)
	defer g.stop()
	fmt.Fprintln(stderr, ui("Image Skill", "生图Skill"))
	fmt.Fprintln(stderr, ui(
		"Installs or updates the bundled image Skill, then saves the image API address and token it uses. Program setup is not changed.",
		"安装或更新内置的生图Skill，然后保存它使用的生图接口地址和令牌。不会改动程序配置。",
	))
	plans, code, err := skillInstallSteps(in, stdout, stderr, loc, g)
	if code != 0 || err != nil {
		return code, err
	}
	installed := false
	for _, p := range plans {
		installed = installed || p.State != imageskill.Same
	}
	return configureImageAPI(e, in, g, skillPlanApps(plans), installed)
}

func skillPlanApps(plans []imageskill.Plan) []apps.ID {
	var ids []apps.ID
	for _, id := range imageskill.Targets() {
		for _, p := range plans {
			if p.App == id {
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
// the program wizard. The only request it can make is GET /v1/models to the
// chosen address, when the user agrees, to list the models the token can
// see. Saving never depends on that request.
func configureImageAPI(e env, in *bufio.Reader, g *skillGuard, targets []apps.ID, installed bool) (int, error) {
	stderr := e.stderr
	zh := promptLocaleZH()
	path := imageconfig.Path(e.layout.ConfigDir)
	saved, savedOK, err := imageconfig.Load(path)
	if err != nil {
		fmt.Fprintln(stderr, imageNotSavedLine(installed))
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
	p, custom, err := choosePreset(in, stderr, zh, provider.Hosted(), 1, intro, ui("Address", "地址"))
	if err != nil {
		return imageCancelled(stderr, err, installed)
	}
	id, base := p.ID, ""
	if custom {
		id = provider.CustomID
		if base, err = promptImageBase(in, stderr); err != nil {
			return imageCancelled(stderr, err, installed)
		}
	} else if base, err = imageconfig.ParseBase(p.BaseURL); err != nil {
		return 1, err
	}
	fmt.Fprintln(stderr, ui(
		fmt.Sprintf("Image API address %s. The token is sent only to %s.", base, imageconfig.Host(base)),
		fmt.Sprintf("生图接口地址 %s。令牌只会发送到 %s。", base, imageconfig.Host(base)),
	))

	// The current token is the one already saved for this image address, or
	// else the one a selected program has for the same provider and host.
	// A token for a different host is never offered.
	current, verb := "", ui("Kept", "已保留")
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
	key, err := askImageKey(e, in, tokenPrompt(ui("Image Skill", "生图Skill"), id, base, current, ""), current, verb)
	if err != nil {
		return imageCancelled(stderr, err, installed)
	}
	savedModel := ""
	if savedOK {
		savedModel = saved.Model
	}
	pick, err := chooseImageModel(in, stderr, base, key, savedModel)
	if err != nil {
		return imageCancelled(stderr, err, installed)
	}

	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui("deeprouter will write:", "将写入："))
	fmt.Fprintf(stderr, "  %s\n", e.show(path))
	cfg := imageconfig.Config{Provider: id, BaseURL: base, APIKey: key, Model: pick.id}
	if err := g.hold(func() error { return imageconfig.Save(path, cfg) }); err != nil {
		fmt.Fprintln(stderr, imageNotSavedLine(installed))
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
	if pick.fetched {
		fmt.Fprintln(out, ui(
			fmt.Sprintf("Only GET /v1/models was sent, to %s, to list models. Nothing was generated or billed. Saving did not check that the address, token, or model works.", imageconfig.Host(base)),
			fmt.Sprintf("只向 %s 发送了 GET /v1/models 来获取模型列表，没有生图，不产生费用。保存时没有检查地址、令牌或模型是否可用。", imageconfig.Host(base)),
		))
	} else {
		fmt.Fprintln(out, ui(
			"No API call was made and nothing was billed. The address, token, and model were not checked.",
			"没有调用任何接口，不产生费用，也没有检查地址、令牌或模型。",
		))
	}
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
	// fetched: GET /v1/models was sent, whatever the result.
	fetched bool
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

// chooseImageModel offers the image models this token can see at base,
// after asking whether to load the list. The list is GET /v1/models
// filtered by gateway.FilterImages; a model on it is visible to the token,
// which does not mean the account or channel may use it. When the list is
// skipped, fails, or has no image model, the menu offers the current model
// and the Skill default as unverified settings. Entering a model ID is
// always offered. Nothing here blocks the save.
func chooseImageModel(in *bufio.Reader, stderr io.Writer, base, key, current string) (imageModelPick, error) {
	host := imageconfig.Host(base)
	fmt.Fprintln(stderr)
	fmt.Fprintln(stderr, ui(
		fmt.Sprintf("The model list comes from GET /v1/models on %s, sent with this token to that host only. It is free and generates nothing. Saving does not need it.", host),
		fmt.Sprintf("模型列表来自 %s 的 GET /v1/models，令牌只发给这个地址。这个请求免费，不会生图。不获取也可以保存。", host),
	))
	load, err := askYes(in, stderr, ui("Load the image models this token can see? [Y/n] ", "获取令牌可见的绘画模型？[Y/n] "), true)
	if err != nil {
		return imageModelPick{}, err
	}
	var visible []string
	pick := imageModelPick{}
	if !load {
		fmt.Fprintln(stderr, ui("Skipped the model list.", "已跳过模型列表。"))
	} else {
		pick.fetched = true
		fmt.Fprintln(stderr, ui(
			fmt.Sprintf("Fetching models from %s ...", base),
			fmt.Sprintf("正在从 %s 获取模型列表……", base),
		))
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
	}

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
		line, err := readLine(in, stderr, fmt.Sprintf("%s [%d]: ", ui("Model", "模型"), def))
		if err != nil {
			return imageModelPick{}, err
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
			pick.id, pick.listed = entries[n-1].id, entries[n-1].listed
			return pick, nil
		}
		id, err := promptImageModelID(in, stderr)
		if err != nil {
			return imageModelPick{}, err
		}
		pick.id, pick.typed = id, true
		return pick, nil
	}
}

// promptImageModelID reads a model id until its form is valid. It may be
// any id; it does not have to be on the model list.
func promptImageModelID(in *bufio.Reader, stderr io.Writer) (string, error) {
	for {
		line, err := readLine(in, stderr, ui("Model ID: ", "模型ID："))
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
func promptImageBase(in *bufio.Reader, stderr io.Writer) (string, error) {
	for {
		line, err := readLine(in, stderr, ui(
			"Image API address (https://...): ",
			"生图接口地址（https://...）：",
		))
		if err != nil {
			return "", err
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
		line, err := promptSecretLine(e.stdin, e.stderr, in, prompt)
		if err != nil {
			return "", err
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
			"The image address and token were not saved. The Skill installed above stays.",
			"生图地址和令牌没有保存。上面安装的 Skill 保持不变。",
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
	case !errors.Is(err, io.EOF):
		return 1, err
	}
	if installed {
		fmt.Fprintln(stderr, ui("Cancelled.", "已取消。")+" "+imageNotSavedLine(true))
	} else {
		fmt.Fprintln(stderr, ui("Cancelled. Nothing was written.", "已取消，没有写入任何文件。"))
	}
	return code, nil
}
