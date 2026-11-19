package cli

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/zfg5118/deeprouter/internal/apps"
)

func TestProgramMenuLayout(t *testing.T) {
	previous := uiLang
	t.Cleanup(func() { uiLang = previous })
	for _, locale := range []struct{ lang, image, exit, back string }{
		{"en", "Image Skill", "Exit", "Back"},
		{"zh", "生图Skill", "退出", "返回"},
	} {
		t.Run(locale.lang, func(t *testing.T) {
			uiLang = locale.lang
			programs := programMenuRows(true)
			targets := skillTargetRows()
			if programs[4].Title != "OpenCode CLI / Desktop" || programs[7].Title != locale.image || targets[0].Title != "Codex" {
				t.Fatal("expected OpenCode, Image Skill, and Codex rows")
			}
			var lastSkillMenu string
			for _, width := range []int{80, 100, 120, 40} {
				t.Run(fmt.Sprint(width), func(t *testing.T) {
					var programOut, skillOut bytes.Buffer
					printProgramMenu(&programOut, programs, true, width)
					printProgramMenu(&skillOut, targets, false, width)
					programColumn, skillColumn := 27, 19
					if width == 40 {
						programColumn, skillColumn = 3, 3
					} else {
						path := programs[4].Note[strings.LastIndexAny(programs[4].Note, " ：")+1:]
						if !strings.Contains(programOut.String(), path+"\n") {
							t.Fatalf("OpenCode path split at width %d", width)
						}
					}
					assertProgramMenuLayout(t, programOut.String(), programs, width, programColumn, locale.exit)
					assertProgramMenuLayout(t, skillOut.String(), targets, width, skillColumn, locale.back)
					if width == 80 && !strings.HasPrefix(strings.Split(skillOut.String(), "\n")[1], strings.Repeat(" ", skillColumn)) {
						t.Fatal("expected a Codex note continuation at 80 columns")
					}
					if skillOut.String() == lastSkillMenu {
						t.Fatal("Skill notes did not reflow when width changed")
					}
					lastSkillMenu = skillOut.String()
				})
			}
		})
	}
}

func assertProgramMenuLayout(t *testing.T, output string, rows []apps.Info, width, noteColumn int, zeroLabel string) {
	t.Helper()
	lines := strings.Split(strings.TrimSuffix(output, "\n"), "\n")
	for _, line := range lines {
		if displayWidth(line) > width {
			t.Errorf("line exceeds %d columns: %q", width, line)
		}
	}
	for index, row := range rows {
		prefix := fmt.Sprintf("%d) %s", index+1, row.Title)
		if len(lines) == 0 || !strings.HasPrefix(lines[0], prefix) {
			t.Fatalf("row %d does not start with %q: %q", index+1, prefix, lines)
		}
		var notes []string
		if noteColumn > len("1) ") {
			padding := lines[0][len(prefix):]
			note := strings.TrimLeft(padding, " ")
			if note == "" || displayWidth(lines[0][:len(lines[0])-len(note)]) != noteColumn {
				t.Errorf("note must start at column %d: %q", noteColumn, lines[0])
			}
			notes = append(notes, note)
		}
		lines = lines[1:]
		for len(lines) > 0 && strings.HasPrefix(lines[0], " ") {
			note := strings.TrimLeft(lines[0], " ")
			if note == "" || displayWidth(lines[0][:len(lines[0])-len(note)]) != noteColumn {
				t.Errorf("continuation must start at column %d: %q", noteColumn, lines[0])
			}
			notes = append(notes, note)
			lines = lines[1:]
		}
		if strings.Join(strings.Fields(strings.Join(notes, " ")), "") != strings.Join(strings.Fields(row.Note), "") {
			t.Errorf("note text changed: got %q, want %q", notes, row.Note)
		}
	}
	if !slices.Equal(lines, []string{"0) " + zeroLabel}) {
		t.Errorf("expected aligned zero row, got %q", lines)
	}
}

func TestProgramMenusDefaultWidthAndNavigation(t *testing.T) {
	previous := uiLang
	t.Cleanup(func() { uiLang = previous })
	uiLang = "en"
	var out, expected bytes.Buffer
	if width := menuWidth(&out); width != 80 {
		t.Fatalf("non-terminal width = %d, want 80", width)
	}
	programs, image, err := promptPrograms(bufio.NewReader(strings.NewReader("\n")), &out, false, true)
	if err != nil || image || !slices.Equal(programs, []apps.ID{apps.Codex}) {
		t.Fatalf("unexpected program defaults: %v, %v, %v", programs, image, err)
	}
	printProgramMenu(&expected, programMenuRows(true), true, 80)
	if !strings.HasSuffix(out.String(), expected.String()+"Programs [1]: ") {
		t.Fatal("program prompt did not use the default-width layout")
	}
	out.Reset()
	expected.Reset()
	targets, err := promptSkillTargets(bufio.NewReader(strings.NewReader("\n")), &out, nil, false)
	if err != nil || !slices.Equal(targets, []apps.ID{apps.Codex, apps.ClaudeCode}) {
		t.Fatalf("unexpected Skill target defaults: %v, %v", targets, err)
	}
	printProgramMenu(&expected, skillTargetRows(), false, 80)
	if !strings.HasSuffix(out.String(), expected.String()+"Programs [1,2]: ") {
		t.Fatal("Skill prompt did not use the default-width layout")
	}
	if _, _, err := promptPrograms(bufio.NewReader(strings.NewReader("0\n")), &out, false, true); !errors.Is(err, errMenuExit) {
		t.Fatalf("program zero returned %v, want exit", err)
	}
	if _, err := promptSkillTargets(bufio.NewReader(strings.NewReader("0\n")), &out, nil, false); !errors.Is(err, errBack) {
		t.Fatalf("Skill target zero returned %v, want back", err)
	}
}
