package cli

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/provider"
	"github.com/zfg5118/deeprouter/internal/testgate"
)

func TestOpeningMenuZero(t *testing.T) {
	for _, test := range []struct {
		name, input string
		args        []string
		mixed       bool
	}{
		{"main", "0\n", nil, false},
		{"main mixed", "0,1\n1,0\n8、0\n0，1\n0\n", nil, true},
		{"skill", "0\n", []string{"skill"}, false},
		{"skill targets mixed", "1\n0,1\n1，0\n0\n0\n", []string{"skill"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			home := t.TempDir()
			withTerminal(t)
			_, stderr, code := runSkill(t, home, bytes.NewReader([]byte(test.input)), test.args...)
			if code != 0 || !strings.Contains(stderr, "0) Exit") || !strings.Contains(stderr, "Nothing was written.") ||
				strings.Contains(stderr, "Mixed selections are not accepted.") != test.mixed {
				t.Fatalf("code %d\n%s", code, stderr)
			}
			assertEmptyHome(t, home)
		})
	}
}

func TestMenuZeroReturnsWithSelection(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runMenu(t, home, "5,6\n0\n\n0\n0\n")
	if code != 0 || strings.Count(stderr, "Programs [5,6]: ") != 2 || strings.Count(stderr, "0) Back") != 2 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	assertEmptyHome(t, home)
	withTerminal(t)
	_, stderr, code = runSkill(t, home, bytes.NewReader([]byte("\n0\n\n\n")), "skill")
	if code != 0 || strings.Count(stderr, "Choose [1]: ") != 2 || strings.Count(stderr, "Programs [1,2]: ") != 2 {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	assertSkillTree(t, codexSkillDir(home))
	assertSkillTree(t, claudeSkillDir(home))
}

func TestImageBackRetainsAnswersAndCatalog(t *testing.T) {
	home := t.TempDir()
	calls := withCatalog(t, listOf(t, `{"data":[{"id":"gpt-image-2"}]}`))
	// 0 backs out of the token, address, protocol, and model-id steps. back is
	// the hidden alias and is used once at the address step.
	answers := strings.Join([]string{"8", "6", "4", "https://img.example.com", "img-fake-key",
		"", "0", "0", "0", "back", "0", "", "", "", "", "", "2", "0", "2", "gpt-image-fake", ""}, "\n")
	_, stderr, code := runMenu(t, home, answers)
	if code != 0 || len(*calls) != 1 || (*calls)[0] != (catalogCall{"https://img.example.com", "img-fake-key"}) || !strings.Contains(stderr, "Programs [6]: ") {
		t.Fatalf("code %d calls %v\n%s", code, *calls, stderr)
	}
	if cfg := loadImageCfg(t, home); cfg.Model != "gpt-image-fake" || cfg.APIKey != "img-fake-key" {
		t.Fatalf("config %+v", cfg)
	}
}

func TestImageBackToOpeningMenuWritesNothing(t *testing.T) {
	home := t.TempDir()
	_, stderr, code := runMenu(t, home, "8\n\n0\n0\n0\n")
	if code != 0 || !strings.Contains(stderr, "Programs [8]: ") || !strings.Contains(stderr, "Nothing was written.") {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	assertEmptyHome(t, home)
}

func TestMenuExitAfterProgramSave(t *testing.T) {
	home := t.TempDir()
	server := testgate.New()
	defer server.Close()
	answers := "1,8\n" + platformChoice("1", provider.CustomID) + "\n" + server.URL + "\n" + testgate.GoodKey + "\n\n0\n0\n0\n"
	_, stderr, code := runMenuCLIs(t, home, withCodexCLI, answers)
	if code != 0 || !strings.Contains(stderr, "Exiting.") || strings.Contains(stderr, "Nothing was written.") {
		t.Fatalf("code %d\n%s", code, stderr)
	}
	if len(skillTreeList(t, home)) == 0 {
		t.Fatal("program save was not exercised")
	}
}

func TestNavigationDoesNotConsumeReadErrors(t *testing.T) {
	for _, want := range []error{io.EOF, errInterrupted, errors.New("read failed")} {
		_, err := readBackLine(bufio.NewReader(menuErrorReader{want}), io.Discard, "Model ID: ")
		if err != want {
			t.Errorf("got %v, want %v", err, want)
		}
	}
}

type menuErrorReader struct{ err error }

func (r menuErrorReader) Read([]byte) (int, error) { return 0, r.err }
