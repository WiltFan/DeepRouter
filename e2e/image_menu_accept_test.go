//go:build !windows

package e2e

import (
	"context"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Fake keys typed into the binary on the pseudoterminal. They must never
// appear in a transcript, a log, or any file except the image config.
const (
	imageE2EKeyA   = "sk-test-e2eImageKeyAAAAfake000000000001"
	imageE2EKeyB   = "sk-test-e2eImageKeyBBBBfake000000000002"
	imageE2ELegacy = "sk-test-e2eLegacyDeepRouterKEY00000003"
)

// imageCfg is ~/.deeprouter/image-skill.json under home.
func imageCfg(home string) string {
	return filepath.Join(home, ".deeprouter", "image-skill.json")
}

type savedImageCfg struct {
	Provider string `json:"provider"`
	BaseURL  string `json:"base_url"`
	APIKey   string `json:"api_key"`
	Model    string `json:"model"`
}

func readImageCfg(path string) (savedImageCfg, bool) {
	var c savedImageCfg
	b, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(b, &c) != nil {
		return c, false
	}
	return c, true
}

// noToolConfig reports which program or profile config files exist under
// home. The image Skill entry must create none of them.
func noToolConfig(home string) string {
	var found []string
	for _, rel := range []string{".deeprouter/config.json", ".codex", ".claude/settings.json", ".grok", ".gemini", ".zshrc", ".bashrc", ".profile"} {
		if exists(filepath.Join(home, rel)) {
			found = append(found, rel)
		}
	}
	return strings.Join(found, ", ")
}

// keyFree reports whether text holds none of the fake keys (or their tails).
func keyFree(text string) bool {
	for _, k := range []string{imageE2EKeyA, imageE2EKeyB, imageE2ELegacy, skillE2EKey, imageE2EListA, imageE2EListB, imageE2EList401} {
		if strings.Contains(text, k) || strings.Contains(text, k[8:]) {
			return false
		}
	}
	return true
}

var (
	// menuStep answers the no-arg program list; 6 is the Image Skill row.
	menuStep      = func(send string) ptyStep { return ptyStep{"Programs [1]: ", send} }
	programsStep  = ptyStep{"Programs [1,2]: ", "\r"}
	addressStep   = func(send string) ptyStep { return ptyStep{"Address [1]: ", send} }
	imageKeyStep  = func(send string) ptyStep { return ptyStep{"for Image Skill", send} }
	customURLStep = func(send string) ptyStep { return ptyStep{"Image API address (https://...): ", send} }
	loadStep      = func(send string) ptyStep { return ptyStep{"Load the image models this token can see? [Y/n] ", send} }
	modelStep     = func(send string) ptyStep { return ptyStep{"Model [", send} }
	// skipModel answers the model step with no request: skip the list, Enter.
	skipModel = []ptyStep{loadStep("n\r"), modelStep("\r")}
)

// steps joins step lists.
func steps(parts ...[]ptyStep) []ptyStep {
	var out []ptyStep
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func (s *skillSuite) mainMenu(t *testing.T) {
	if s.python == "" {
		t.Skip("python3 is required to allocate a terminal")
	}
	noArgs := []string{}
	t.Run("S16a_enter_is_program_setup", func(t *testing.T) {
		sc := s.scenario(t, "S16a", "PTY no-arg: the first screen is the program list; Enter is Codex / ChatGPT setup; Ctrl-D there writes nothing")
		home := t.TempDir()
		before := snapTree(home)
		r := s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("\r"), {"Platform [", "\x04"}})
		sc.check("first screen is the program list", strings.Contains(r.Transcript, "Choose which programs to configure. Press Enter for Codex / ChatGPT.") &&
			strings.Index(r.Transcript, "DeepRouter") < strings.Index(r.Transcript, "Choose which programs"), "")
		sc.check("no extra top menu", !strings.Contains(r.Transcript, "Choose [1]") && !strings.Contains(r.Transcript, "Configure programs (Codex"), "")
		sc.check("program list ends with the optional Image Skill row", strings.Contains(r.Transcript, "5) Gemini CLI") &&
			strings.Contains(r.Transcript, "6) Image Skill ") && strings.Contains(r.Transcript, "optional: install the image Skill"), "")
		sc.check("Enter went to platform setup", strings.Contains(r.Transcript, "Choose a platform"), "")
		sc.check("no image Skill step", !strings.Contains(r.Transcript, "Programs [1,2]") && !strings.Contains(r.Transcript, "Address [1]"), "")
		sc.check("exit 2", r.Exit == 2, fmt.Sprint(r.Exit))
		sc.check("setup cancelled line", strings.Contains(r.Transcript, "Setup cancelled. Nothing was written."), "")
		sc.check("HOME unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
	})
	for _, c := range []struct {
		id    string
		steps []ptyStep
		exit  int
	}{
		{"S16b", []ptyStep{menuStep("6\r"), {"Programs [1,2]: ", "\x04"}}, 2},
		{"S16c", []ptyStep{menuStep("\x04")}, 2},
		{"S16e", []ptyStep{menuStep("\x03")}, 130},
	} {
		t.Run(c.id, func(t *testing.T) {
			sc := s.scenario(t, c.id, fmt.Sprintf("PTY no-arg: %q writes nothing", c.steps[len(c.steps)-1].Send))
			home := t.TempDir()
			before := snapTree(home)
			r := s.runPTYArgs(sc, home, "", nil, noArgs, c.steps)
			sc.check(fmt.Sprintf("exit %d", c.exit), r.Exit == c.exit, fmt.Sprint(r.Exit))
			sc.check("cancelled line", strings.Contains(r.Transcript, "ancelled. Nothing was written."), "")
			sc.check("HOME unchanged", snapDiff(before, snapTree(home)) == "", snapDiff(before, snapTree(home)))
		})
	}
	t.Run("S16d_non_tty", func(t *testing.T) {
		sc := s.scenario(t, "S16d", "non-TTY no-arg: no menu, pipe not read, exit 2")
		home := t.TempDir()
		r := s.noWrites(sc, home, home, nil, "6\n\n1\npiped-answer-not-a-key\n", 2)
		sc.check("run-in-a-terminal hint", strings.Contains(r.Stderr, "Run deeprouter in a terminal.") && strings.Contains(r.Stderr, "setup --api-key-stdin"), r.Stderr)
		sc.check("no menu or program list", !strings.Contains(r.Stderr, "Choose [1]") && !strings.Contains(r.Stderr, "Programs [") && !strings.Contains(r.Stderr, "Image Skill"), r.Stderr)
		sc.check("no image config", !exists(imageCfg(home)), "")
	})
}

func (s *skillSuite) imageConfig(t *testing.T) {
	if s.python == "" {
		t.Skip("python3 is required to allocate a terminal")
	}
	noArgs := []string{}
	presets := []struct{ id, answer, provider, base string }{
		{"S17a1", "\r", "deeprouter", "https://deeprouter.top"},
		{"S17a2", "2\r", "wolfai", "https://wolfai.top"},
		{"S17a3", "3\r", "linkmesh", "https://www.linkmesh.org"},
	}
	for _, p := range presets {
		t.Run(p.id+"_preset_"+p.provider, func(t *testing.T) {
			sc := s.scenario(t, p.id, "PTY Image Skill: install both, preset "+p.provider+", typed key")
			home := t.TempDir()
			r := s.runPTYArgs(sc, home, "", nil, noArgs, steps([]ptyStep{menuStep("6\r"), programsStep,
				addressStep(p.answer), imageKeyStep(imageE2EKeyA + "\r")}, skipModel))
			sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
			sc.check("no scope or Install? question, no program setup", !strings.Contains(r.Transcript, "Scope [1]") && !strings.Contains(r.Transcript, "Install for:") &&
				!strings.Contains(r.Transcript, "Install? [Y/n]") && !strings.Contains(r.Transcript, "Choose a platform"), "")
			sc.check("Claude tree equals embed", treeDiff(skillDir(home, ".claude")) == "", treeDiff(skillDir(home, ".claude")))
			sc.check("Codex tree equals embed", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
			sc.check("picker shows the three gateways then custom", strings.Contains(r.Transcript, "1) DeepRouter  https://deeprouter.top (default)") &&
				strings.Contains(r.Transcript, "2) WolfAI  https://wolfai.top") && strings.Contains(r.Transcript, "3) LinkMesh  https://www.linkmesh.org") &&
				strings.Contains(r.Transcript, "4) Custom base URL"), "")
			// The program picker above has five rows; the address picker has four.
			_, addr, _ := strings.Cut(r.Transcript, "Choose the image API address.")
			sc.check("picker has no official APIs", addr != "" && !strings.Contains(r.Transcript, "api.openai.com") && !strings.Contains(r.Transcript, "Anthropic") &&
				!strings.Contains(addr, "5) "), "")
			cfg, ok := readImageCfg(imageCfg(home))
			sc.check("skipped list: default model saved as unverified", cfg.Model == "gpt-image-2.6-flare" &&
				strings.Contains(r.Transcript, "Skipped the model list.") && strings.Contains(r.Transcript, "Model   gpt-image-2.6-flare (unverified)") &&
				strings.Contains(r.Transcript, "No API call was made"), cfg.Model)
			sc.check("image config has the preset and the typed key", ok && cfg.Provider == p.provider && cfg.BaseURL == p.base && cfg.APIKey == imageE2EKeyA,
				fmt.Sprintf("ok=%v provider=%s base=%s keyMatches=%v", ok, cfg.Provider, cfg.BaseURL, cfg.APIKey == imageE2EKeyA))
			sc.check("file mode 0600", mode(imageCfg(home)) == 0o600, mode(imageCfg(home)).String())
			sc.check("dir mode 0700", mode(filepath.Join(home, ".deeprouter")) == 0o700, mode(filepath.Join(home, ".deeprouter")).String())
			sc.check("no program or profile config written", noToolConfig(home) == "", noToolConfig(home))
			sc.check("transcript has no key", keyFree(r.Transcript), "")
			sc.check("transcript shows the masked key", strings.Contains(r.Transcript, "sk-****0001"), "")
			sc.check("key not in the Skill folders", !filesContain(filepath.Join(home, ".claude"), imageE2EKeyA) && !filesContain(filepath.Join(home, ".agents"), imageE2EKeyA), "")
		})
	}

	t.Run("S17b_custom_then_reconfigure", func(t *testing.T) {
		sc := s.scenario(t, "S17b", "PTY Image Skill: custom URL (bad ones refused), cancel keeps file, Enter keeps key, other host gets no key, reconfigure")
		home := t.TempDir()
		r := s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
			customURLStep("http://img.e2e.example\r"),
			{"must start with https://", ""},
			customURLStep("https://user:pw@img.e2e.example\r"),
			{"username or password", ""},
			customURLStep("https://img.e2e.example/?a=1\r"),
			{"query string or fragment", ""},
			customURLStep("https://img.e2e.example/v1/\r"),
			{"The token is sent only to img.e2e.example.", ""},
			{"Enter the gateway token (https://img.e2e.example) for Image Skill: ", imageE2EKeyA + "\r"}, loadStep("n\r"), modelStep("\r")})
		sc.check("exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		cfg, ok := readImageCfg(imageCfg(home))
		sc.check("custom base normalized, key saved", ok && cfg.Provider == "custom" && cfg.BaseURL == "https://img.e2e.example" && cfg.APIKey == imageE2EKeyA,
			fmt.Sprintf("provider=%s base=%s", cfg.Provider, cfg.BaseURL))
		first, _ := os.ReadFile(imageCfg(home))

		// Cancel at the key prompt: Ctrl-D, then Ctrl-C.
		for _, c := range []struct {
			send string
			exit int
		}{{"\x04", 2}, {"\x03", 130}} {
			r = s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("2\r"), imageKeyStep(c.send)})
			sc.check(fmt.Sprintf("cancel %q exits %d", c.send, c.exit), r.Exit == c.exit, fmt.Sprint(r.Exit))
			now, _ := os.ReadFile(imageCfg(home))
			sc.check(fmt.Sprintf("cancel %q leaves the file byte for byte", c.send), string(now) == string(first), "")
		}

		// A different custom host: Enter has nothing to reuse.
		r = s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
			customURLStep("https://other.e2e.example\r"),
			{"Enter the gateway token (https://other.e2e.example) for Image Skill: ", "\r"},
			{"The token is empty. Enter it again.", "\x04"}})
		sc.check("other host: exit 2", r.Exit == 2, fmt.Sprint(r.Exit))
		sc.check("other host: saved key not offered", !strings.Contains(r.Transcript, "[sk-****0001]") && !strings.Contains(r.Transcript, "Kept ("), "")
		now, _ := os.ReadFile(imageCfg(home))
		sc.check("other host: file unchanged", string(now) == string(first), "")

		// Same host again: Enter keeps the saved key.
		r = s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
			customURLStep("https://img.e2e.example\r"), {"for Image Skill [sk-****0001]: ", "\r"}, loadStep("n\r"),
			{"1) Keep current: gpt-image-2.6-flare (unverified) (default)", ""}, modelStep("\r")})
		sc.check("same host: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		sc.check("same host: Kept line", strings.Contains(r.Transcript, "Kept (sk-****0001"), "")
		sc.check("re-run shows the current address", strings.Contains(r.Transcript, "Current image address: https://img.e2e.example (token sk-****0001)"), "")
		sc.check("re-run does not ask to install again", !strings.Contains(r.Transcript, "Install? [Y/n]"), "")

		// Reconfigure to WolfAI with a new key.
		r = s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("2\r"),
			{"Enter the WolfAI token (create one at wolfai.top) for Image Skill: ", imageE2EKeyB + "\r"}, loadStep("n\r"), modelStep("\r")})
		sc.check("reconfigure: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
		cfg, _ = readImageCfg(imageCfg(home))
		sc.check("reconfigure: WolfAI with the new key", cfg.Provider == "wolfai" && cfg.BaseURL == "https://wolfai.top" && cfg.APIKey == imageE2EKeyB, cfg.Provider)
		sc.check("no program or profile config written", noToolConfig(home) == "", noToolConfig(home))
		for _, run := range sc.Runs {
			if !keyFree(run.Transcript) {
				sc.check("transcripts hold no key", false, run.Label)
			}
		}

		// skill install --force after a local edit does not touch the config.
		mustWrite(t, filepath.Join(skillDir(home, ".agents"), "SKILL.md"), "edited\n")
		before, _ := os.ReadFile(imageCfg(home))
		s.install(sc, home, "--force")
		after, _ := os.ReadFile(imageCfg(home))
		sc.check("skill install --force keeps the image config", string(before) == string(after), "")
	})

	t.Run("S17c_enter_reuses_env_key_for_deeprouter_only", func(t *testing.T) {
		sc := s.scenario(t, "S17c", "PTY Image Skill: DEEPROUTER_API_KEY is the current key for DeepRouter only")
		home := t.TempDir()
		env := map[string]string{"DEEPROUTER_API_KEY": imageE2ELegacy}
		r := s.runPTYArgs(sc, home, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("3\r"),
			{"Enter the LinkMesh token (create one at www.linkmesh.org) for Image Skill: ", "\r"},
			{"The token is empty. Enter it again.", "\x04"}})
		sc.check("LinkMesh: DeepRouter key not offered, exit 2", r.Exit == 2 && !strings.Contains(r.Transcript, "[sk-****0003]"), fmt.Sprint(r.Exit))
		sc.check("LinkMesh: no image config", !exists(imageCfg(home)), "")
		r = s.runPTYArgs(sc, home, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("\r"),
			{"for Image Skill [sk-****0003]: ", "\r"}, loadStep("n\r"), modelStep("\r")})
		sc.check("DeepRouter: Enter reuses DEEPROUTER_API_KEY", r.Exit == 0 && strings.Contains(r.Transcript, "Reused (sk-****0003"), fmt.Sprint(r.Exit))
		cfg, _ := readImageCfg(imageCfg(home))
		sc.check("saved for the image Skill only", cfg.APIKey == imageE2ELegacy && cfg.BaseURL == "https://deeprouter.top" && noToolConfig(home) == "", noToolConfig(home))
		sc.check("transcripts hold no key", keyFree(r.Transcript), "")
	})

	t.Run("S17d_installed_script_reads_saved_pair", func(t *testing.T) {
		sc := s.scenario(t, "S17d", "installed script uses the pair the binary saved; env pair wins; half pair, --host refused; recover reads no config")
		home, work := t.TempDir(), t.TempDir()
		r := s.runPTYArgs(sc, home, "", nil, noArgs, []ptyStep{menuStep("6\r"), {"Programs [1,2]: ", "1\r"}, addressStep("4\r"),
			customURLStep("https://img.e2e.example/api\r"), imageKeyStep(imageE2EKeyA + "\r"),
			loadStep("n\r"), modelStep("2\r"), {"Model ID: ", "grok-3-image\r"}})
		if !sc.check("binary saved the image config", r.Exit == 0 && exists(imageCfg(home)), fmt.Sprint(r.Exit)) {
			return
		}
		script := filepath.Join(skillDir(home, ".agents"), "scripts", "deeprouter_image.py")
		result := filepath.Join(s.logDir, "S17d.driver.json")
		driver := filepath.Join(s.root, "e2e", "testdata", "image_config_driver.py")
		args := []string{"-I", "-B", driver, "--script", script, "--work", work, "--out", result, "--expect-base", "https://img.e2e.example/api", "--expect-model", "grok-3-image"}
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		cmd := exec.CommandContext(ctx, s.python, args...)
		cmd.Dir = work
		cmd.Env = s.env(home, map[string]string{"DEEPROUTER_API_KEY": imageE2ELegacy, "DEEPROUTER_E2E_EXPECT_KEY": imageE2EKeyA})
		out, err := cmd.CombinedOutput()
		sc.Runs = append(sc.Runs, skillRun{Label: "(image config driver)", Argv: append([]string{"python3"}, args...), Cwd: work,
			Env: shownEnv(cmd.Env), Stdout: string(out), Exit: exitCode(err)})
		if !sc.check("driver exited 0", err == nil, fmt.Sprint(err)) {
			return
		}
		raw, err := os.ReadFile(result)
		if err != nil {
			t.Fatal(err)
		}
		var res struct {
			Cases []struct {
				Name   string       `json:"name"`
				Checks []skillCheck `json:"checks"`
			} `json:"cases"`
		}
		if err := json.Unmarshal(raw, &res); err != nil {
			t.Fatal(err)
		}
		names := map[string]bool{}
		for _, c := range res.Cases {
			names[c.Name] = true
			for _, ch := range c.Checks {
				sc.check(c.Name+": "+ch.Name, ch.OK, ch.Detail)
			}
		}
		for _, want := range []string{"file_pair_used", "model_flag_wins", "file_pair_download_has_no_key", "env_pair_overrides_file", "half_env_pair_refused", "host_flag_refused_with_file", "recover_reads_no_config"} {
			sc.check("driver ran "+want, names[want], "")
		}
		sc.check("driver result holds no key", keyFree(string(raw)), "")
		sc.check("installed Skill unchanged (no __pycache__)", treeDiff(skillDir(home, ".agents")) == "", treeDiff(skillDir(home, ".agents")))
	})
}

// Fake keys for S18. The mock answers GET /v1/models differently for each.
const (
	imageE2EListA   = "sk-test-e2eListKeyAAAAfake000000000004"
	imageE2EListB   = "sk-test-e2eListKeyBBBBfake000000000005"
	imageE2EList401 = "sk-test-e2eListKey401fake00000000000006"
)

// modelsMock is a local HTTPS server for GET /v1/models only. Its
// certificate is trusted through SSL_CERT_FILE; nothing else is. It records
// each request as method, path, and which fake key was sent.
type modelsMock struct {
	srv      *httptest.Server
	certFile string
	mu       sync.Mutex
	seen     []string
}

func newModelsMock(t *testing.T) *modelsMock {
	m := &modelsMock{}
	lists := map[string]string{
		imageE2EListA: `{"object":"list","data":[{"id":"gpt-5.4"},{"id":"grok-4"},{"id":"gpt-image-2"},{"id":"sora-2"},{"id":"veo-3"},
			{"id":"gemini-3-pro-image-preview"},{"id":"gpt-image-2.6-flare"},{"id":"grok-3-image"},{"id":"my-image-model"}]}`,
		imageE2EListB: `{"object":"list","data":[{"id":"claude-sonnet-4-6"},{"id":"doubao-seedream-5-0-260128"},{"id":"grok-3-image"},{"id":"grok-imagine-video"}]}`,
	}
	m.srv = httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		who := "other"
		for k := range lists {
			if r.Header.Get("Authorization") == "Bearer "+k {
				who = k[len(k)-4:]
			}
		}
		if r.Header.Get("Authorization") == "Bearer "+imageE2EList401 {
			who = "401"
		}
		m.mu.Lock()
		m.seen = append(m.seen, r.Method+" "+r.URL.Path+" key="+who)
		m.mu.Unlock()
		body, ok := lists[strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")]
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" || !ok {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"error":{"message":"invalid token","type":"new_api_error"}}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	t.Cleanup(m.srv.Close)
	m.certFile = filepath.Join(t.TempDir(), "mock-ca.pem")
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: m.srv.Certificate().Raw})
	if err := os.WriteFile(m.certFile, pemBytes, 0o600); err != nil {
		t.Fatal(err)
	}
	return m
}

func (m *modelsMock) requests() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.seen...)
}

// imageModels drives the model step on the real binary against modelsMock.
func (s *skillSuite) imageModels(t *testing.T) {
	if s.python == "" {
		t.Skip("python3 is required to allocate a terminal")
	}
	mock := newModelsMock(t)
	base := mock.srv.URL // https://127.0.0.1:PORT
	host := strings.TrimPrefix(base, "https://")
	env := map[string]string{"SSL_CERT_FILE": mock.certFile}
	noArgs := []string{}
	keyPrompt := "Enter the gateway token (" + base + ") for Image Skill"
	home := t.TempDir()

	sc := s.scenario(t, "S18", "PTY Image Skill model step: token-visible Images models from a mocked GET /v1/models; failures still save; manual ID")
	// a. Token A sees flare: it is first and the Enter default.
	r := s.runPTYArgs(sc, home, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
		customURLStep(base + "\r"), {keyPrompt, imageE2EListA + "\r"}, loadStep("\r"),
		{"Image models visible to this token on " + host + ".", ""}, modelStep("\r")})
	menu := r.Transcript[strings.Index(r.Transcript, "Image models visible"):]
	sc.check("a: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("a: flare first and default, then gpt-image-2, grok-3-image, then manual entry",
		strings.Contains(menu, "1) gpt-image-2.6-flare (default)") && strings.Contains(menu, "2) gpt-image-2") &&
			strings.Contains(menu, "3) grok-3-image") && strings.Contains(menu, "4) Enter a model ID (typed by you; not checked)"), "")
	sc.check("a: no chat, video, Gemini, or bare 'image' ids", !strings.Contains(menu, "gpt-5.4") && !strings.Contains(menu, "grok-4") &&
		!strings.Contains(menu, "sora") && !strings.Contains(menu, "veo") && !strings.Contains(menu, "gemini") && !strings.Contains(menu, "my-image-model"), "")
	sc.check("a: visibility is not permission", strings.Contains(r.Transcript, "Visible does not mean your account or channel may use a model, and none was test-generated"), "")
	cfg, _ := readImageCfg(imageCfg(home))
	sc.check("a: saved flare", cfg.Model == "gpt-image-2.6-flare" && cfg.APIKey == imageE2EListA && cfg.BaseURL == base, cfg.Model)
	sc.check("a: summary says only GET /v1/models was sent", strings.Contains(r.Transcript, "Only GET /v1/models was sent, to "+host), "")

	// b. Same address, token B: the list is fetched again and has no flare.
	r = s.runPTYArgs(sc, home, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
		customURLStep(base + "\r"), {keyPrompt, imageE2EListB + "\r"}, loadStep("\r"),
		{"Image models visible to this token on " + host + ".", ""}, modelStep("2\r")})
	menu = r.Transcript[strings.Index(r.Transcript, "Image models visible"):]
	sc.check("b: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("b: new list, first is the default, flare not added", strings.Contains(menu, "1) doubao-seedream-5-0-260128 (default)") &&
		strings.Contains(menu, "2) grok-3-image") && !strings.Contains(menu, "gpt-image-2.6-flare (default)") && !strings.Contains(menu, "gpt-image-2\r\n") &&
		!strings.Contains(menu, "video"), "")
	sc.check("b: saved model kept as an unverified option", strings.Contains(menu, "3) Keep current: gpt-image-2.6-flare (not on this list; unverified)"), "")
	cfg, _ = readImageCfg(imageCfg(home))
	sc.check("b: saved grok-3-image with token B", cfg.Model == "grok-3-image" && cfg.APIKey == imageE2EListB, cfg.Model)

	// c. A refused token: the list fails, and a typed id still saves.
	r = s.runPTYArgs(sc, home, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
		customURLStep(base + "\r"), {keyPrompt, imageE2EList401 + "\r"}, loadStep("\r"), {"Could not load the model list", ""},
		modelStep("3\r"), {"Model ID: ", "\r"}, {"model id is empty", ""}, {"Model ID: ", "my-unlisted-model\r"}})
	sc.check("c: exit 0", r.Exit == 0, fmt.Sprint(r.Exit))
	sc.check("c: no fake list; current and default offered unverified, manual last",
		strings.Contains(r.Transcript, "There is no token-visible model list. You can still save.") &&
			strings.Contains(r.Transcript, "1) Keep current: grok-3-image (unverified) (default)") &&
			strings.Contains(r.Transcript, "2) gpt-image-2.6-flare: the Skill default (unverified)") &&
			strings.Contains(r.Transcript, "3) Enter a model ID") && !strings.Contains(r.Transcript, "Image models visible"), "")
	cfg, _ = readImageCfg(imageCfg(home))
	sc.check("c: saved the typed id", cfg.Model == "my-unlisted-model" && cfg.APIKey == imageE2EList401, cfg.Model)
	sc.check("c: typed id not called visible or verified", strings.Contains(r.Transcript, "Model   my-unlisted-model (entered by you; not checked)") &&
		!strings.Contains(r.Transcript, "on this token's model list"), "")

	// d. No connection at all (nothing listens): Enter still saves.
	closed := httptest.NewTLSServer(http.NotFoundHandler())
	deadBase := closed.URL
	closed.Close()
	home2 := t.TempDir()
	r = s.runPTYArgs(sc, home2, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
		customURLStep(deadBase + "\r"), {"Enter the gateway token (" + deadBase + ") for Image Skill", imageE2EListA + "\r"}, loadStep("\r"),
		{"Could not load the model list", ""}, modelStep("\r")})
	cfg, _ = readImageCfg(imageCfg(home2))
	sc.check("d: offline save exits 0 with the default, unverified", r.Exit == 0 && cfg.Model == "gpt-image-2.6-flare" && cfg.BaseURL == deadBase &&
		strings.Contains(r.Transcript, "Model   gpt-image-2.6-flare (unverified)"), fmt.Sprint(r.Exit))
	sc.check("d: mode 0600", mode(imageCfg(home2)) == 0o600, mode(imageCfg(home2)).String())

	// e. Ctrl-D at the model menu writes nothing.
	before, _ := os.ReadFile(imageCfg(home))
	r = s.runPTYArgs(sc, home, "", env, noArgs, []ptyStep{menuStep("6\r"), programsStep, addressStep("4\r"),
		customURLStep(base + "\r"), {keyPrompt, imageE2EListA + "\r"}, loadStep("\r"), modelStep("\x04")})
	after, _ := os.ReadFile(imageCfg(home))
	sc.check("e: cancel at the model menu exits 2, file byte for byte", r.Exit == 2 && string(before) == string(after), fmt.Sprint(r.Exit))

	want := []string{"GET /v1/models key=0004", "GET /v1/models key=0005", "GET /v1/models key=401", "GET /v1/models key=0004"}
	got := mock.requests()
	sc.check("mock saw only GET /v1/models, each with the token typed for that run", strings.Join(got, "|") == strings.Join(want, "|"), strings.Join(got, ", "))
	sc.check("no program or profile config written", noToolConfig(home) == "" && noToolConfig(home2) == "", noToolConfig(home))
	for _, run := range sc.Runs {
		if !keyFree(run.Transcript) {
			sc.check("transcripts hold no key", false, run.Label)
		}
	}
}
