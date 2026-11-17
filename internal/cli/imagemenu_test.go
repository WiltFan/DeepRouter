package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/imageconfig"
	"github.com/zfg5118/deeprouter/internal/store"
	"github.com/zfg5118/deeprouter/internal/testgate"
	"github.com/zfg5118/deeprouter/internal/textutil"
)

// Fake tokens only. None of them is ever sent anywhere: saving the image
// config makes no request.
const (
	imgKeyA = "sk-test-imageAAAAfake000000000000000001"
	imgKeyB = "sk-test-imageBBBBfake000000000000000002"
	toolKey = "sk-test-toolKEYfake00000000000000000003"
)

// skipModel answers the model step without a request: skip the list, then
// Enter for the default.
const skipModel = "n\n\n"

// No cli test reaches the network from the image model step. Tests that
// need a model list set imageCatalog with withCatalog.
func init() {
	imageCatalog = func(base, key string) ([]gateway.Model, gateway.Check, error) {
		return nil, gateway.Check{Hint: "no network in tests"}, errors.New("no network in tests")
	}
}

// runMenu runs deeprouter with no arguments on a fake terminal.
func runMenu(t *testing.T, home, answers string) (string, string, int) {
	t.Helper()
	isolateHome(t, home)
	t.Setenv("DEEPROUTER_API_KEY", "")
	withTerminal(t)
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
	var stdout, stderr bytes.Buffer
	code := mainIO(nil, "0.1.0", &stdout, &stderr, bytes.NewBufferString(answers))
	return stdout.String(), stderr.String(), code
}

// imageAnswers picks Image Skill, both programs, user scope, and (when the
// Skill is not installed yet) confirms the install, then the rest.
func imageAnswers(install bool, rest string) string {
	s := "2\n\n\n"
	if install {
		s += "\n"
	}
	return s + rest
}

func imageCfgPath(home string) string {
	return imageconfig.Path(filepath.Join(home, ".deeprouter"))
}

func loadImageCfg(t *testing.T, home string) imageconfig.Config {
	t.Helper()
	cfg, ok, err := imageconfig.Load(imageCfgPath(home))
	if err != nil || !ok {
		t.Fatalf("image config: ok=%v err=%v", ok, err)
	}
	return cfg
}

// toolFiles is every file under home except the image config and the Skill
// folders, so a test can show that saving the image config changed nothing
// else.
func toolFiles(t *testing.T, home string) map[string]string {
	t.Helper()
	out := map[string]string{}
	_ = filepath.WalkDir(home, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		rel, _ := filepath.Rel(home, p)
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == ".claude/skills" || rel == ".agents/skills" {
				return filepath.SkipDir
			}
			return nil
		}
		if rel == ".deeprouter/"+imageconfig.FileName {
			return nil
		}
		b, _ := os.ReadFile(p)
		out[rel] = string(b)
		return nil
	})
	return out
}

func assertSameFiles(t *testing.T, before, after map[string]string) {
	t.Helper()
	for k, v := range before {
		if after[k] != v {
			t.Errorf("%s changed", k)
		}
	}
	for k := range after {
		if _, ok := before[k]; !ok {
			t.Errorf("%s was created", k)
		}
	}
}

func assertNoSecret(t *testing.T, text string, keys ...string) {
	t.Helper()
	for _, k := range keys {
		if strings.Contains(text, k) || strings.Contains(text, k[8:]) {
			t.Fatalf("output shows a token:\n%s", text)
		}
	}
}

func saveToolProfile(t *testing.T, home string, prof store.Profile) {
	t.Helper()
	st := store.State{Version: 1, Active: prof.Name, Profiles: []store.Profile{prof}}
	if err := store.Save(store.Path(filepath.Join(home, ".deeprouter")), st); err != nil {
		t.Fatal(err)
	}
}

func TestMainMenuEnterRunsProgramWizard(t *testing.T) {
	home := t.TempDir()
	srv := testgate.New()
	defer srv.Close()
	stdout, stderr, code := runMenu(t, home, "\n"+wizardCustom("", srv.URL+"\n"+testgate.GoodKey+"\n\n\nn\n"))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{"1) Configure programs (Codex, Claude Code, ...) (default)", "2) Image Skill", "Choose [1]: ", "Programs [1]:"} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("missing %q\n%s", want, stderr)
		}
	}
	if !strings.Contains(mustRead(t, filepath.Join(home, ".codex", "config.toml")), testgate.GoodKey) {
		t.Fatal("wizard did not write Codex")
	}
	if _, err := os.Lstat(imageCfgPath(home)); !os.IsNotExist(err) {
		t.Fatalf("program setup wrote the image config: %v", err)
	}
	if _, err := os.Lstat(claudeSkillDir(home)); !os.IsNotExist(err) {
		t.Fatalf("program setup installed the Skill: %v", err)
	}
	if strings.Contains(stderr, "Image API address") || strings.Contains(stderr, "Install for:") {
		t.Fatalf("program setup went through the image Skill:\n%s", stderr)
	}
}

func TestMainMenuQuitEOFAndBadChoice(t *testing.T) {
	for name, answers := range map[string]string{"quit": "3\n", "q": "q\n", "eof": "", "bad then quit": "9\nx\n3\n"} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			stdout, stderr, code := runMenu(t, home, answers)
			if code != 2 || !strings.Contains(stderr, "Cancelled. Nothing was written.") {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			if name == "bad then quit" && strings.Count(stderr, "Choose 1, 2, or 3.") != 2 {
				t.Fatalf("bad choice not re-asked:\n%s", stderr)
			}
			if entries, _ := os.ReadDir(home); len(entries) != 0 {
				t.Fatalf("wrote %v", entries)
			}
		})
	}
}

func TestMainMenuChinese(t *testing.T) {
	home := t.TempDir()
	t.Setenv("LANG", "zh_CN.UTF-8")
	t.Setenv("LC_ALL", "zh_CN.UTF-8")
	_, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n"+skipModel))
	if code != 0 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	for _, want := range []string{"1) 配置程序（Codex、Claude Code 等）（默认）", "2) 生图Skill", "选择 [1]：",
		"选择生图接口地址", "1) DeepRouter  https://deeprouter.top（默认）", "4) 自定义网关地址", "地址 [1]: ",
		"请输入 DeepRouter 的令牌（在 deeprouter.top 创建），用于 生图Skill："} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("missing %q\n%s", want, stderr)
		}
	}
}

func TestNoArgsNonTTYSkipsMenu(t *testing.T) {
	out, errOut, code := run(t, t.TempDir(), bytes.NewReader([]byte("2\n\n\n\n1\n"+imgKeyA+"\n")))
	if code != 2 || !strings.Contains(errOut, "Run deeprouter in a terminal.") || strings.Contains(errOut, "Choose [1]") {
		t.Fatalf("code %d\n%s\n%s", code, out, errOut)
	}
}

func TestImageSkillPresets(t *testing.T) {
	presets := []struct{ answer, id, base string }{
		{"\n", "deeprouter", "https://deeprouter.top"},
		{"1\n", "deeprouter", "https://deeprouter.top"},
		{"2\n", "wolfai", "https://wolfai.top"},
		{"3\n", "linkmesh", "https://www.linkmesh.org"},
	}
	for _, p := range presets {
		t.Run(p.id+strings.TrimSpace(p.answer), func(t *testing.T) {
			home := t.TempDir()
			stdout, stderr, code := runMenu(t, home, imageAnswers(true, p.answer+imgKeyA+"\n"+skipModel))
			if code != 0 {
				t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
			}
			assertSkillTree(t, claudeSkillDir(home))
			assertSkillTree(t, codexSkillDir(home))
			cfg := loadImageCfg(t, home)
			if cfg.Provider != p.id || cfg.BaseURL != p.base || cfg.APIKey != imgKeyA {
				t.Fatalf("config %+v", cfg)
			}
			for _, want := range []string{
				"1) DeepRouter  https://deeprouter.top (default)",
				"2) WolfAI  https://wolfai.top",
				"3) LinkMesh  https://www.linkmesh.org",
				"4) Custom base URL",
				"Address [1]: ",
			} {
				if !strings.Contains(stderr, want) {
					t.Fatalf("missing %q\n%s", want, stderr)
				}
			}
			for _, official := range []string{"OpenAI", "api.openai.com", "Anthropic", "Gemini", "xAI", "5)"} {
				if strings.Contains(stderr[strings.Index(stderr, "Choose the image API address"):], official) {
					t.Fatalf("image picker lists %q\n%s", official, stderr)
				}
			}
			if !strings.Contains(stderr, "The token is sent only to "+imageconfig.Host(p.base)) {
				t.Fatalf("destination host not shown\n%s", stderr)
			}
			if !strings.Contains(stdout, textutil.Mask(imgKeyA)) || !strings.Contains(stdout, "not encrypted") || !strings.Contains(stdout, "No API call") {
				t.Fatalf("stdout:\n%s", stdout)
			}
			assertNoSecret(t, stdout+stderr, imgKeyA)
			if runtime.GOOS != "windows" {
				assertPerm(t, imageCfgPath(home), 0o600)
				assertPerm(t, filepath.Join(home, ".deeprouter"), 0o700)
			}
			for _, tool := range []string{".deeprouter/config.json", ".codex", ".claude/settings.json", ".zshrc"} {
				if _, err := os.Lstat(filepath.Join(home, tool)); !os.IsNotExist(err) {
					t.Fatalf("image Skill touched %s: %v", tool, err)
				}
			}
		})
	}
}

func TestImageSkillCustomAddress(t *testing.T) {
	home := t.TempDir()
	bad := []string{
		"",
		"http://img.example.com",
		"img.example.com",
		"https://user:pw@img.example.com",
		"https://img.example.com/?a=1",
		"https://img.example.com/#frag",
		"https://",
		"https://bad host",
	}
	answers := "4\n" + strings.Join(bad, "\n") + "\nhttps://Img.Example.com/v1/\n" + imgKeyA + "\n" + skipModel
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, answers))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	for _, want := range []string{
		"image API address is empty",
		"image API address must start with https://",
		"must not include a username or password",
		"must not include a query string or fragment",
		"image API address is missing a host",
		"image API address is not a valid URL",
	} {
		if !strings.Contains(stderr, want) {
			t.Fatalf("missing %q\n%s", want, stderr)
		}
	}
	if n := strings.Count(stderr, "Image API address (https://...): "); n != len(bad)+1 {
		t.Fatalf("asked %d times\n%s", n, stderr)
	}
	prompt := "Enter the gateway token (https://img.example.com) for Image Skill: "
	i := strings.Index(stderr, "The token is sent only to img.example.com.")
	j := strings.Index(stderr, prompt)
	if i < 0 || j < i {
		t.Fatalf("destination not shown before the token prompt\n%s", stderr)
	}
	cfg := loadImageCfg(t, home)
	if cfg.Provider != "custom" || cfg.BaseURL != "https://img.example.com" || cfg.APIKey != imgKeyA {
		t.Fatalf("config %+v", cfg)
	}
	assertNoSecret(t, stdout+stderr, imgKeyA)
}

func TestImageSkillEnterReusesCurrentToolKey(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)
	saveToolProfile(t, home, store.Profile{Name: "default", Label: "DeepRouter", BaseURL: "https://deeprouter.top", Provider: "deeprouter", APIKey: toolKey})
	before := toolFiles(t, home)
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n\n"+skipModel))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if !strings.Contains(stderr, "for Image Skill ["+textutil.Mask(toolKey)+"]: ") || !strings.Contains(stderr, "Reused (") {
		t.Fatalf("Enter did not offer the current token\n%s", stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.APIKey != toolKey || cfg.BaseURL != "https://deeprouter.top" {
		t.Fatalf("config %+v", cfg)
	}
	assertSameFiles(t, before, toolFiles(t, home))
	assertNoSecret(t, stdout+stderr, toolKey)
}

func TestImageSkillDeepRouterEnvKeyIsCurrentOnlyForDeepRouter(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)
	// runMenu clears DEEPROUTER_API_KEY; set it after.
	stdout, stderr, code := runMenuWithEnv(t, home, map[string]string{"DEEPROUTER_API_KEY": toolKey}, imageAnswers(true, "2\n\n"))
	if code != 2 || !strings.Contains(stderr, "The token is empty. Enter it again.") || strings.Contains(stderr, textutil.Mask(toolKey)) {
		t.Fatalf("WolfAI reused the DeepRouter env token: code %d\n%s\n%s", code, stdout, stderr)
	}
	if _, err := os.Lstat(imageCfgPath(home)); !os.IsNotExist(err) {
		t.Fatalf("wrote the image config: %v", err)
	}
	stdout, stderr, code = runMenuWithEnv(t, home, map[string]string{"DEEPROUTER_API_KEY": toolKey}, imageAnswers(false, "1\n\n"+skipModel))
	if code != 0 || !strings.Contains(stderr, "["+textutil.Mask(toolKey)+"]") {
		t.Fatalf("DeepRouter did not offer DEEPROUTER_API_KEY: code %d\n%s\n%s", code, stdout, stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.APIKey != toolKey {
		t.Fatalf("config %+v", cfg)
	}
}

func runMenuWithEnv(t *testing.T, home string, env map[string]string, answers string) (string, string, int) {
	t.Helper()
	isolateHome(t, home)
	withTerminal(t)
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
	for k, v := range env {
		t.Setenv(k, v)
	}
	var stdout, stderr bytes.Buffer
	code := mainIO(nil, "0.1.0", &stdout, &stderr, bytes.NewBufferString(answers))
	return stdout.String(), stderr.String(), code
}

func TestImageSkillEnterWithoutCurrentKeyAsksAgain(t *testing.T) {
	home := t.TempDir()
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n\n\n"))
	if code != 2 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Count(stderr, "The token is empty. Enter it again.") != 2 {
		t.Fatalf("empty token not re-asked\n%s", stderr)
	}
	if strings.Contains(stdout, "Saved the image Skill") {
		t.Fatalf("faked success\n%s", stdout)
	}
	if !strings.Contains(stderr, "Cancelled. The image address and token were not saved.") {
		t.Fatalf("cancel line\n%s", stderr)
	}
	if _, err := os.Lstat(imageCfgPath(home)); !os.IsNotExist(err) {
		t.Fatalf("wrote the image config: %v", err)
	}
}

func TestImageSkillNewKeyGoesOnlyToImageConfig(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)
	saveToolProfile(t, home, store.Profile{Name: "default", Label: "DeepRouter", BaseURL: "https://deeprouter.top", Provider: "deeprouter", APIKey: toolKey})
	mustWriteFile(t, filepath.Join(home, ".codex", "config.toml"), "model = \"x\"\n")
	mustWriteFile(t, filepath.Join(home, ".claude", "settings.json"), "{}\n")
	before := toolFiles(t, home)
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyB+"\n"+skipModel))
	if code != 0 {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.APIKey != imgKeyB {
		t.Fatalf("config %+v", cfg)
	}
	assertSameFiles(t, before, toolFiles(t, home))
	st, _ := store.Load(store.Path(filepath.Join(home, ".deeprouter")))
	if p, _ := st.Find("default"); p.APIKey != toolKey {
		t.Fatalf("tool profile key changed")
	}
	assertNoSecret(t, stdout+stderr, imgKeyB, toolKey)
}

func TestImageSkillKeyForAnotherHostIsNotReused(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)
	// The tool profile and the saved image config are both for host A.
	saveToolProfile(t, home, store.Profile{Name: "default", Label: "Custom", BaseURL: "https://a.example.com", Provider: "custom", APIKey: toolKey})
	if _, _, code := runMenu(t, home, imageAnswers(true, "4\nhttps://a.example.com\n"+imgKeyA+"\n"+skipModel)); code != 0 {
		t.Fatal(code)
	}
	saved, _ := os.ReadFile(imageCfgPath(home))

	// Host B: Enter has nothing to reuse.
	stdout, stderr, code := runMenu(t, home, imageAnswers(false, "4\nhttps://b.example.com\n\n"))
	if code != 2 || !strings.Contains(stderr, "The token is empty. Enter it again.") {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, textutil.Mask(imgKeyA)+"]") || strings.Contains(stderr, textutil.Mask(toolKey)) {
		t.Fatalf("offered a token for another host\n%s", stderr)
	}
	if !strings.Contains(stderr, "Enter the gateway token (https://b.example.com) for Image Skill: ") {
		t.Fatalf("host B not shown\n%s", stderr)
	}
	if now, _ := os.ReadFile(imageCfgPath(home)); string(now) != string(saved) {
		t.Fatal("cancel changed the image config")
	}

	// A preset is not host A either.
	_, stderr, code = runMenu(t, home, imageAnswers(false, "1\n\n"))
	if code != 2 || !strings.Contains(stderr, "The token is empty. Enter it again.") {
		t.Fatalf("preset reused a custom-host token: %d\n%s", code, stderr)
	}

	// Host A again: Enter keeps the image token, not the tool token.
	stdout, stderr, code = runMenu(t, home, imageAnswers(false, "4\nhttps://a.example.com/v1\n\n"+skipModel))
	if code != 0 || !strings.Contains(stderr, "Kept (") || !strings.Contains(stderr, "["+textutil.Mask(imgKeyA)+"]") {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.APIKey != imgKeyA {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageSkillCustomHostUsesToolKeyForSameHostOnly(t *testing.T) {
	home := t.TempDir()
	isolateHome(t, home)
	saveToolProfile(t, home, store.Profile{Name: "default", Label: "Custom", BaseURL: "https://a.example.com/v1", Provider: "custom", APIKey: toolKey})
	_, stderr, code := runMenu(t, home, imageAnswers(true, "4\nhttps://a.example.com\n\n"+skipModel))
	if code != 0 || !strings.Contains(stderr, "Reused (") {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.APIKey != toolKey || cfg.BaseURL != "https://a.example.com" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageSkillReconfigureAndCancel(t *testing.T) {
	home := t.TempDir()
	if _, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n"+skipModel)); code != 0 {
		t.Fatalf("first run %d\n%s", code, stderr)
	}
	first, _ := os.ReadFile(imageCfgPath(home))

	// Cancel (EOF) at the token prompt, at the address, and at the custom URL.
	for name, rest := range map[string]string{"token": "2\n", "address": "", "custom url": "4\n"} {
		stdout, stderr, code := runMenu(t, home, imageAnswers(false, rest))
		if code != 2 {
			t.Fatalf("%s: code %d\n%s\n%s", name, code, stdout, stderr)
		}
		if !strings.Contains(stderr, "Cancelled. Nothing was written.") {
			t.Fatalf("%s: cancel line\n%s", name, stderr)
		}
		if now, _ := os.ReadFile(imageCfgPath(home)); string(now) != string(first) {
			t.Fatalf("%s: cancel changed the file", name)
		}
	}

	// Reconfigure: the Skill is already installed, so there is no install
	// question, and the current address is shown.
	stdout, stderr, code := runMenu(t, home, imageAnswers(false, "2\n"+imgKeyB+"\n"+skipModel))
	if code != 0 {
		t.Fatalf("reconfigure %d\n%s\n%s", code, stdout, stderr)
	}
	if strings.Contains(stderr, "Install? [Y/n]") || !strings.Contains(stdout, "already installed, unchanged") {
		t.Fatalf("reinstall asked\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stderr, "Current image address: https://deeprouter.top (token "+textutil.Mask(imgKeyA)+")") {
		t.Fatalf("current address not shown\n%s", stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.Provider != "wolfai" || cfg.BaseURL != "https://wolfai.top" || cfg.APIKey != imgKeyB {
		t.Fatalf("config %+v", cfg)
	}
	assertNoSecret(t, stdout+stderr, imgKeyA, imgKeyB)
}

func TestImageSkillCancelAtInstallWritesNothing(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runMenu(t, home, "2\n\n\nn\n")
	if code != 2 || !strings.Contains(stderr, "Cancelled. Nothing was written.") || strings.Contains(stderr, "Address [1]") {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if entries, _ := os.ReadDir(home); len(entries) != 0 {
		t.Fatalf("wrote %v", entries)
	}
}

func TestImageSkillCancelAfterInstallSaysTheSkillStays(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"))
	if code != 2 || !strings.Contains(stderr, "The image address and token were not saved. The Skill installed above stays.") {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	assertSkillTree(t, codexSkillDir(home))
	if _, err := os.Lstat(imageCfgPath(home)); !os.IsNotExist(err) {
		t.Fatalf("wrote the image config: %v", err)
	}
}

func TestImageSkillRefusesSymlinkedConfig(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	home := t.TempDir()
	outside := filepath.Join(t.TempDir(), "elsewhere.json")
	mustWriteFile(t, outside, "{}\n")
	if err := os.MkdirAll(filepath.Join(home, ".deeprouter"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, imageCfgPath(home)); err != nil {
		t.Fatal(err)
	}
	stdout, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n"+skipModel))
	if code != 1 || !strings.Contains(stderr, "symbolic link") {
		t.Fatalf("code %d\n%s\n%s", code, stdout, stderr)
	}
	if b, _ := os.ReadFile(outside); string(b) != "{}\n" {
		t.Fatal("wrote through the link")
	}
	assertNoSecret(t, stdout+stderr, imgKeyA)
}

func TestSkillInstallKeepsImageConfig(t *testing.T) {
	home := t.TempDir()
	if _, stderr, code := runMenu(t, home, imageAnswers(true, "1\n"+imgKeyA+"\n"+skipModel)); code != 0 {
		t.Fatalf("%d\n%s", code, stderr)
	}
	saved, _ := os.ReadFile(imageCfgPath(home))
	mustWriteFile(t, filepath.Join(codexSkillDir(home), "SKILL.md"), "edited\n")
	if _, errOut, code := runSkill(t, home, nil, "skill", "install", "--force"); code != 0 {
		t.Fatalf("%d %s", code, errOut)
	}
	if now, _ := os.ReadFile(imageCfgPath(home)); string(now) != string(saved) {
		t.Fatal("skill install changed the image config")
	}
	for _, dir := range []string{codexSkillDir(home), claudeSkillDir(home)} {
		_ = filepath.WalkDir(dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				if b, _ := os.ReadFile(p); bytes.Contains(b, []byte(imgKeyA)) {
					t.Errorf("token in the Skill folder: %s", p)
				}
			}
			return nil
		})
	}
	var raw map[string]any
	if err := json.Unmarshal(saved, &raw); err != nil || raw["api_key"] != imgKeyA {
		t.Fatalf("saved file %v", err)
	}
}

func mustWriteFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}
