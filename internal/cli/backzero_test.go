package cli

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/gateway"
	"github.com/zfg5118/deeprouter/internal/imageconfig"
	"github.com/zfg5118/deeprouter/internal/provider"
)

const fakeBackKey = "sk-fake-back-zero-0123456789"

var backLangs = []struct{ name, lang, hint, entered string }{
	{"en", "en_US.UTF-8", "Type 0 to return.", "Entered"},
	{"zh", "zh_CN.UTF-8", "输入 0 可回到上一步。", "已输入"},
}

// backLines is the official command first, then the hidden aliases.
var backLines = []string{"0", "back", "BACK", "返回"}

func setBackLang(t *testing.T, lang string) {
	t.Helper()
	t.Setenv("LC_ALL", "")
	t.Setenv("DEEPROUTER_LANG", "")
	t.Setenv("LANG", lang)
	uiLang = ""
	t.Cleanup(func() { uiLang = "" })
}

func TestBackHintSaysZero(t *testing.T) {
	for _, l := range backLangs {
		t.Run(l.name, func(t *testing.T) {
			setBackLang(t, l.lang)
			var out bytes.Buffer
			backHint(&out)
			got := strings.TrimSpace(out.String())
			if got != l.hint {
				t.Fatalf("hint %q, want %q", got, l.hint)
			}
			for _, alias := range []string{"back", "返回"} {
				if strings.Contains(strings.ToLower(got), alias) {
					t.Fatalf("hint mentions the alias %q: %q", alias, got)
				}
			}
		})
	}
}

func TestBackCommandForms(t *testing.T) {
	for _, line := range []string{"0", " 0 ", "back", "Back", "BACK", "返回"} {
		if !backCommand(line) {
			t.Errorf("%q should be a back command", line)
		}
	}
	for _, line := range []string{"", "00", "1", "0 1", "backup", "返回上一步", "gpt-5"} {
		if backCommand(line) {
			t.Errorf("%q should not be a back command", line)
		}
	}
}

// maskedTerminal makes stdin a terminal whose masked read returns line.
func maskedTerminal(t *testing.T, line string) *os.File {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close(); _ = w.Close() })
	prevTerm, prevRead := stdinIsTerminal, readMasked
	stdinIsTerminal = func(io.Reader) bool { return true }
	readMasked = func(f *os.File, echo io.Writer) (string, error) {
		fmtStars(echo, line)
		return line, nil
	}
	t.Cleanup(func() { stdinIsTerminal, readMasked = prevTerm, prevRead })
	return r
}

func fmtStars(echo io.Writer, line string) {
	_, _ = io.WriteString(echo, strings.Repeat("*", len(line))+"\n")
}

func TestReadSecretLineBackSkipsEntered(t *testing.T) {
	for _, l := range backLangs {
		for _, line := range backLines {
			t.Run(l.name+"/"+line, func(t *testing.T) {
				setBackLang(t, l.lang)
				// Masked terminal path.
				r := maskedTerminal(t, line)
				var out bytes.Buffer
				got, err := readSecretLine(r, &out, bufio.NewReader(r), "token: ", keyNote, true)
				if err != nil || got != line {
					t.Fatalf("got %q, %v", got, err)
				}
				if strings.Contains(out.String(), l.entered) {
					t.Fatalf("false confirmation:\n%s", out.String())
				}
				// Line-reader path on a terminal that cannot mask.
				out.Reset()
				in := bufio.NewReader(strings.NewReader(line + "\n"))
				got, err = readSecretLine(strings.NewReader(""), &out, in, "token: ", keyNote, true)
				if err != nil || got != line {
					t.Fatalf("got %q, %v", got, err)
				}
				if strings.Contains(out.String(), l.entered) {
					t.Fatalf("false confirmation:\n%s", out.String())
				}
			})
		}
	}
}

func TestReadSecretLineRealTokenStillConfirmed(t *testing.T) {
	for _, l := range backLangs {
		t.Run(l.name, func(t *testing.T) {
			setBackLang(t, l.lang)
			r := maskedTerminal(t, fakeBackKey)
			var out bytes.Buffer
			if _, err := readSecretLine(r, &out, bufio.NewReader(r), "token: ", keyNote, true); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), l.entered) {
				t.Fatalf("no confirmation:\n%s", out.String())
			}
			if strings.Contains(out.String(), fakeBackKey) {
				t.Fatalf("token echoed:\n%s", out.String())
			}
			// Prompts that do not offer back keep the old behaviour for 0.
			r = maskedTerminal(t, "0")
			out.Reset()
			if _, err := promptSecretLine(r, &out, bufio.NewReader(r), "token: "); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), l.entered) {
				t.Fatalf("promptSecretLine dropped the confirmation:\n%s", out.String())
			}
		})
	}
}

func TestAskKeyBack(t *testing.T) {
	for _, l := range backLangs {
		for _, line := range backLines {
			for _, kind := range []string{"gpt", "chat"} {
				t.Run(l.name+"/"+line+"/"+kind, func(t *testing.T) {
					setBackLang(t, l.lang)
					r := maskedTerminal(t, line)
					var out bytes.Buffer
					s := &session{
						e:        env{stdin: r, stderr: &out},
						in:       bufio.NewReader(r),
						provider: provider.CustomID,
						baseURL:  "https://gw.example.com",
					}
					_, _, err := s.askKey(apps.Info{Title: "Codex / ChatGPT", ModelKind: kind}, "", "")
					if !errors.Is(err, errBack) {
						t.Fatalf("err %v\n%s", err, out.String())
					}
					text := out.String()
					if strings.Contains(text, l.entered) {
						t.Fatalf("false confirmation:\n%s", text)
					}
					if !strings.Contains(text, l.hint) {
						t.Fatalf("hint missing:\n%s", text)
					}
				})
			}
		}
	}
}

func TestAskImageKeyBack(t *testing.T) {
	for _, l := range backLangs {
		for _, line := range backLines {
			t.Run(l.name+"/"+line, func(t *testing.T) {
				setBackLang(t, l.lang)
				r := maskedTerminal(t, line)
				var out bytes.Buffer
				e := env{stdin: r, stderr: &out}
				_, err := askImageKey(e, bufio.NewReader(r), "token: ", "", "Kept")
				if !errors.Is(err, errBack) {
					t.Fatalf("err %v\n%s", err, out.String())
				}
				if strings.Contains(out.String(), l.entered) || !strings.Contains(out.String(), l.hint) {
					t.Fatalf("output:\n%s", out.String())
				}
			})
		}
	}
}

// Every text prompt that prints the hint returns errBack for each back line.
func TestTextPromptsReturnOnBack(t *testing.T) {
	steps := map[string]func(in *bufio.Reader, out io.Writer) error{
		"gateway": func(in *bufio.Reader, out io.Writer) error {
			_, err := promptGateway(in, out, true, "")
			return err
		},
		"client model": func(in *bufio.Reader, out io.Writer) error {
			s := &session{
				e:        env{stderr: out},
				in:       in,
				provider: provider.CustomID,
				baseURL:  "https://gw.example.com",
				catalogs: map[string][]gateway.Model{"fake": nil},
			}
			_, err := s.askClientModel(apps.Info{Title: "Claude Code"}, "fake", "")
			return err
		},
		"model picker": func(in *bufio.Reader, out io.Writer) error {
			_, _, err := pickModel(in, out, "Codex", "gpt", nil, promptLocaleZH(), "https://gw.example.com", "fake", "", familyProbe{}, "")
			return err
		},
		"image address": func(in *bufio.Reader, out io.Writer) error {
			_, err := promptImageBase(in, out, "")
			return err
		},
		"image model id": func(in *bufio.Reader, out io.Writer) error {
			_, err := promptImageModelID(in, out, imageconfig.ProtocolImages, nil)
			return err
		},
		"skill replace confirm": func(in *bufio.Reader, out io.Writer) error {
			_, err := askYesWithBack(in, out, "Replace? [y/N] ", false, true)
			return err
		},
	}
	for _, l := range backLangs {
		for name, step := range steps {
			for _, line := range backLines {
				t.Run(l.name+"/"+name+"/"+line, func(t *testing.T) {
					setBackLang(t, l.lang)
					var out bytes.Buffer
					err := step(bufio.NewReader(strings.NewReader(line+"\n")), &out)
					if !errors.Is(err, errBack) {
						t.Fatalf("err %v\n%s", err, out.String())
					}
					if got := strings.Count(out.String(), l.hint); got != 1 {
						t.Fatalf("hint printed %d times:\n%s", got, out.String())
					}
				})
			}
		}
	}
}

// A plain yes/no question has no back, so 0 is just an invalid answer.
func TestAskYesWithoutBackIgnoresZero(t *testing.T) {
	setBackLang(t, "en_US.UTF-8")
	var out bytes.Buffer
	ok, err := askYesWithBack(bufio.NewReader(strings.NewReader("0\ny\n")), &out, "Go? [y/N] ", false, false)
	if err != nil || !ok || strings.Contains(out.String(), "Type 0 to return.") {
		t.Fatalf("ok=%v err=%v\n%s", ok, err, out.String())
	}
}

// The protocol menu already lists 0) Back, so it must not repeat the hint.
func TestMenuWithZeroRowHasNoHint(t *testing.T) {
	for _, l := range backLangs {
		t.Run(l.name, func(t *testing.T) {
			setBackLang(t, l.lang)
			var out bytes.Buffer
			_, err := chooseImageProtocol(bufio.NewReader(strings.NewReader("0\n")), &out, "", "")
			if !errors.Is(err, errBack) {
				t.Fatalf("err %v", err)
			}
			if strings.Contains(out.String(), l.hint) || !strings.Contains(out.String(), "0) ") {
				t.Fatalf("output:\n%s", out.String())
			}
		})
	}
}
