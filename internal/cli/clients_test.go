package cli

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/testgate"
)

func TestWizardOpenCodeAndPi(t *testing.T) {
	home := t.TempDir()
	withTerminal(t)
	for _, name := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "XDG_CONFIG_HOME", "PI_CODING_AGENT_DIR"} {
		t.Setenv(name, "")
	}
	srv := testgate.New()
	defer srv.Close()
	answers := wizardCustom("5,6", srv.URL+"/v1\n3\n"+testgate.GoodKey+"\nvendor/model\n4\n\n1\n")
	out, errOut, code := runWizard(t, home, "", answers)
	if code != 0 || !strings.Contains(out, "Configured OpenCode, Pi Coding CLI.") {
		t.Fatalf("wizard failed (%d)", code)
	}
	combined := out + errOut
	for _, forbidden := range []string{testgate.GoodKey, "sk-****alue", "Install missing CLIs with npm?"} {
		if strings.Contains(combined, forbidden) {
			t.Fatal("wizard exposed credential material or prompted to install a client")
		}
	}
	openCode := mustRead(t, filepath.Join(home, ".config", "opencode", "opencode.json"))
	models := mustRead(t, filepath.Join(home, ".pi", "agent", "models.json"))
	if !strings.Contains(openCode, `"model": "custom/vendor/model"`) || !strings.Contains(openCode, `"package": "@opencode/ai/providers/anthropic-compatible"`) ||
		!strings.Contains(models, `"id":"gemini-2.5-pro"`) || !strings.Contains(models, `"api": "google-generative-ai"`) {
		t.Fatal("wizard saved the wrong model ID")
	}
	openPicker, piPicker, found := strings.Cut(errOut, "Pi Coding CLI protocol")
	if !found || !strings.Contains(openPicker, "1) claude-fable-5") || strings.Contains(openPicker, "gemini-2.5-pro") ||
		!strings.Contains(piPicker, "1) gemini-2.5-pro") || strings.Contains(piPicker, "claude-fable-5") || strings.Contains(errOut, "gpt-5.4") {
		t.Fatal("wizard showed unrelated catalog suggestions")
	}
}

func TestWizardClientManualModelWithoutCatalog(t *testing.T) {
	withTerminal(t)
	home := t.TempDir()
	for _, name := range []string{"OPENCODE_CONFIG", "OPENCODE_CONFIG_DIR", "XDG_CONFIG_HOME", "PI_CODING_AGENT_DIR"} {
		t.Setenv(name, "")
	}
	gate := testgate.Handler()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/v1/models":
			http.NotFound(w, r)
		case "/v1beta/models/gemini-unlisted:generateContent", "/v1/responses":
			gate.ServeHTTP(w, r)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	answers := wizardCustom("5,6", srv.URL+"\n4\n"+testgate.GoodKey+"\nmodels/gemini-unlisted\n2\n\nvendor/unlisted\n")
	_, errOut, code := runWizard(t, home, "", answers)
	if code != 0 || strings.Contains(errOut, testgate.GoodKey) {
		t.Fatalf("manual wizard setup failed (%d)", code)
	}
	openCode := mustRead(t, filepath.Join(home, ".config", "opencode", "opencode.json"))
	models := mustRead(t, filepath.Join(home, ".pi", "agent", "models.json"))
	if !strings.Contains(openCode, `"model": "custom/gemini-unlisted"`) || !strings.Contains(models, `"id":"vendor/unlisted"`) {
		t.Fatal("wizard failed to save manual model IDs")
	}
}
