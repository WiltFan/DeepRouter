package e2e

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/testgate"
)

func TestOpenCodeAndPiConnectionConfig(t *testing.T) {
	home := t.TempDir()
	bin := filepath.Join(t.TempDir(), "deeprouter")
	if runtime.GOOS == "windows" {
		bin += ".exe"
	}
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = moduleRoot(t)
	if output, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, output)
	}
	srv := testgate.New()
	defer srv.Close()
	openDir := filepath.Join(home, "configured-opencode")
	piDir := filepath.Join(home, "configured-pi")
	openPath := filepath.Join(openDir, "opencode.json")
	modelPath := filepath.Join(piDir, "models.json")
	authPath := filepath.Join(piDir, "auth.json")
	settingsPath := filepath.Join(piDir, "settings.json")
	env := []string{
		"HOME=" + home, "USERPROFILE=" + home, "SYSTEMROOT=" + os.Getenv("SYSTEMROOT"), "LANG=en_US.UTF-8",
		"OPENCODE_CONFIG_DIR=" + openDir, "XDG_CONFIG_HOME=" + filepath.Join(home, "xdg"), "PI_CODING_AGENT_DIR=" + piDir,
	}
	runFor := func(t *testing.T, expect int, args ...string) result {
		t.Helper()
		output := runBin(t, bin, env, args...)
		combined := output.stdout + output.stderr
		for _, key := range []string{testgate.GoodKey, testgate.AzureKey, testgate.BadKey} {
			if strings.Contains(combined, key) {
				t.Fatal("CLI output leaked a test credential")
			}
		}
		if output.code != expect {
			t.Fatalf("exit %d, expected %d\n%s", output.code, expect, combined)
		}
		return output
	}
	run := func(expect int, args ...string) result {
		t.Helper()
		return runFor(t, expect, args...)
	}
	readFor := func(t *testing.T, path string, wants ...string) string {
		t.Helper()
		raw, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(raw), want) {
				t.Fatalf("missing expected field or value in %s", filepath.Base(path))
			}
		}
		return string(raw)
	}
	read := func(path string, wants ...string) string {
		t.Helper()
		return readFor(t, path, wants...)
	}
	setup := []string{"setup", "--apps", "opencode,pi", "--base-url", srv.URL + "/v1/", "--api-key", testgate.GoodKey,
		"--pi-key", testgate.AzureKey, "--opencode-model", "vendor/model", "--pi-model", "vendor/model"}
	output := run(0, setup...)
	for _, want := range []string{"configured-opencode", "configured-pi"} {
		if !strings.Contains(output.stdout, want) || !strings.Contains(output.stderr, want) || strings.Contains(output.stdout+output.stderr, home) {
			t.Fatalf("missing redacted path for %s", want)
		}
	}
	for _, want := range []string{"reopen OpenCode Desktop when it uses a local server", "OpenCode Desktop: desktop presence", "/model", "opencode:", "pi:"} {
		if !strings.Contains(output.stdout, want) {
			t.Fatalf("missing setup hint for %s", want)
		}
	}

	read(openPath, `"package": "@opencode/ai/providers/openai-compatible"`, `"model": "deeprouter/vendor/model"`, srv.URL+`/v1"`, testgate.GoodKey)
	read(modelPath, `"api": "openai-completions"`, `"id":"vendor/model"`, srv.URL+`/v1"`)
	read(authPath, `"type": "api_key"`, testgate.AzureKey)
	read(settingsPath, `"defaultProvider": "deeprouter"`, `"defaultModel": "vendor/model"`)
	for _, path := range []string{openPath, modelPath, authPath, settingsPath} {
		if info, err := os.Stat(path); err != nil || !permOK(info.Mode().Perm(), 0o600) {
			t.Fatalf("config permissions for %s: %v", filepath.Base(path), err)
		}
	}
	if strings.Contains(read(modelPath), testgate.AzureKey) || strings.Contains(read(settingsPath), testgate.AzureKey) {
		t.Fatal("Pi credential leaked outside auth.json")
	}
	for _, path := range []string{filepath.Join(home, ".config"), filepath.Join(home, "xdg"), filepath.Join(home, ".pi"), filepath.Join(home, ".codex")} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("unexpected default directory %s: %v", filepath.Base(path), err)
		}
	}
	if output := run(0, setup...); !strings.Contains(output.stdout, "Already up to date") {
		t.Fatal("repeat was not idempotent")
	}
	run(0, "test")
	settingsBefore := strings.Replace(read(settingsPath), "{", "{\n  \"quietStartup\": true,", 1)
	if err := os.WriteFile(settingsPath, []byte(settingsBefore), 0o600); err != nil {
		t.Fatal(err)
	}
	openBefore := read(openPath)
	modelsBefore := read(modelPath)
	run(0, "switch", "--opencode-model", "vendor/second", "--pi-model", "vendor/second")
	read(openPath, `"model": "deeprouter/vendor/second"`, `"vendor/model":`)
	read(modelPath, `"id":"vendor/second"`, `"id":"vendor/model"`)
	read(settingsPath, `"quietStartup": true`, `"defaultModel": "vendor/second"`)
	run(0, "restore", "--id", "latest")
	if read(openPath) != openBefore || read(modelPath) != modelsBefore || read(settingsPath) != settingsBefore {
		t.Fatal("restore did not restore exact configuration bytes")
	}
	jsonc := filepath.Join(openDir, "opencode.jsonc")
	const commented = "{\n  // keep this comment\n  \"theme\": \"system\",\n}\n"
	if err := os.WriteFile(jsonc, []byte(commented), 0o600); err != nil {
		t.Fatal(err)
	}
	run(0, setup...)
	read(jsonc, "// keep this comment", `"theme": "system",`, `"model": "deeprouter/vendor/model"`)
	if read(openPath) != openBefore {
		t.Fatal("changed lower-priority OpenCode JSON")
	}
	run(0, "restore", "--id", "latest")
	if read(jsonc) != commented {
		t.Fatal("restore changed the original JSONC bytes")
	}
	const malformed = `{"theme": "first", "theme": "second"}`
	if err := os.WriteFile(jsonc, []byte(malformed), 0o600); err != nil {
		t.Fatal(err)
	}
	brokenSetup := append(append([]string(nil), setup...), "--pi-model", "vendor/other")
	if output := run(1, brokenSetup...); !strings.Contains(output.stderr, "duplicate key") || read(jsonc) != malformed || read(openPath) != openBefore || read(modelPath) != modelsBefore {
		t.Fatal("invalid JSONC was not refused before applying changes")
	}

	if err := os.Remove(jsonc); err != nil {
		t.Fatal(err)
	}
	run(0, "profile", "add", "alternate", "--provider", "wolfai", "--base-url", srv.URL, "--api-key", testgate.AzureKey,
		"--opencode-model", "vendor/alternate", "--pi-model", "vendor/alternate", "--opencode-protocol", "openai-responses", "--pi-protocol", "anthropic-messages")
	run(0, "switch", "--profile", "alternate")
	read(openPath, `"model": "wolfai/vendor/alternate"`, `"deeprouter":`, `"wolfai":`, `"package": "@opencode/ai/providers/openai-compatible/responses"`)
	read(modelPath, `"api": "anthropic-messages"`)
	read(authPath, `"deeprouter":`, `"wolfai":`)
	read(settingsPath, `"defaultProvider": "wolfai"`)
	run(0, "switch", "--pi-protocol", "openai-responses")
	read(modelPath, `"api": "openai-responses"`, `"id":"vendor/alternate"`)
	run(0, "setup", "--apps", "opencode,pi", "--provider", "openai", "--base-url", srv.URL+"/v1", "--api-key", testgate.GoodKey,
		"--opencode-model", "vendor/official", "--pi-model", "vendor/official")
	read(openPath, `"model": "openai-api/vendor/official"`, srv.URL+`/v1"`)
	read(settingsPath, `"defaultProvider": "openai-api"`, `"defaultModel": "vendor/official"`)
	run(0, "restore")
	for _, path := range []string{openPath, modelPath, authPath, settingsPath} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("restore left %s: %v", filepath.Base(path), err)
		}
	}
	if read(jsonc) != commented {
		t.Fatal("full restore did not restore the added JSONC fixture")
	}
	if err := os.Remove(jsonc); err != nil {
		t.Fatal(err)
	}

	for _, preset := range []string{"deeprouter", "wolfai", "linkmesh", "custom"} {
		for _, protocol := range []struct {
			api, adapter, openSuffix, piSuffix, route string
		}{
			{"openai-completions", "openai-compatible", "/v1", "/v1", "/v1/chat/completions"},
			{"openai-responses", "openai-compatible/responses", "/v1", "/v1", "/v1/responses"},
			{"anthropic-messages", "anthropic-compatible", "/v1", "", "/v1/messages"},
			{"google-generative-ai", "google", "/v1beta", "/v1beta", "/v1beta/models/gemini-manual:generateContent"},
		} {
			t.Run(preset+"/"+protocol.api, func(t *testing.T) {
				input, model := "vendor/manual", "vendor/manual"
				if protocol.api == "google-generative-ai" {
					input, model = "models/gemini-manual", "gemini-manual"
				}
				args := []string{"setup", "--apps", "opencode,pi", "--provider", preset, "--base-url", srv.URL + "/v1/", "--api-key", testgate.GoodKey,
					"--pi-key", testgate.AzureKey, "--opencode-model", input, "--pi-model", input, "--opencode-protocol", protocol.api, "--pi-protocol", protocol.api}
				runFor(t, 0, args...)
				open := readFor(t, openPath, `"package": "@opencode/ai/providers/`+protocol.adapter+`"`, `"baseURL": "`+srv.URL+protocol.openSuffix+`"`, `"model": "`+preset+`/`+model+`"`, testgate.GoodKey)
				models := readFor(t, modelPath, `"api": "`+protocol.api+`"`, `"baseUrl": "`+srv.URL+protocol.piSuffix+`"`, `"id":"`+model+`"`)
				readFor(t, authPath, testgate.AzureKey)
				readFor(t, settingsPath, `"defaultProvider": "`+preset+`"`, `"defaultModel": "`+model+`"`)
				if strings.Contains(open+models, "/v1/v1") || strings.Contains(models, testgate.AzureKey) {
					t.Fatal("unexpected version path or credential in config")
				}
				if got := runFor(t, 0, "test").stdout; strings.Count(got, "POST "+protocol.route+" ") != 2 {
					t.Fatalf("test did not check both clients using %s", protocol.route)
				}
				if got := runFor(t, 0, args...).stdout; !strings.Contains(got, "Already up to date") {
					t.Fatal("repeat was not idempotent")
				}
			})
		}
	}
	run(2, "setup", "--apps", "opencode,pi", "--provider", "openai", "--base-url", srv.URL, "--api-key", testgate.GoodKey,
		"--opencode-model", "vendor/manual", "--pi-model", "vendor/manual", "--opencode-protocol", "anthropic-messages")
}
