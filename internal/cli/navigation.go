package cli

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/zfg5118/deeprouter/internal/apps"
	"github.com/zfg5118/deeprouter/internal/provider"
)

var errBack = errors.New("return to parent menu")
var errMenuExit = errors.New("exit menu")

func printMenuZero(out io.Writer, exits bool) {
	fmt.Fprintln(out, "  0) "+menuZeroLabel(exits))
}

func menuZeroLabel(exits bool) string {
	if exits {
		return ui("Exit", "退出")
	}
	return ui("Back", "返回")
}

func indexedPrompt(prompt string, rows []apps.Info, current []apps.ID) string {
	var indexes []string
	for _, id := range current {
		for index, row := range rows {
			if row.ID == id {
				indexes = append(indexes, strconv.Itoa(index+1))
				break
			}
		}
	}
	if start := strings.LastIndex(prompt, "["); start >= 0 && len(indexes) > 0 {
		end := strings.Index(prompt[start:], "]") + start
		return prompt[:start+1] + strings.Join(indexes, ",") + prompt[end:]
	}
	return prompt
}

func presetDefault(list []provider.Preset, fallback int, current string) int {
	if current == provider.CustomID {
		return len(list) + 1
	}
	for index, preset := range list {
		if preset.ID == current {
			return index + 1
		}
	}
	return fallback
}

func menuExit(out io.Writer, wrote bool) {
	fmt.Fprintln(out, ui("Exiting.", "退出。"))
	if !wrote {
		fmt.Fprintln(out, ui("Nothing was written.", "没有写入任何文件。"))
	}
}

func mixedMenuZero(line string, out io.Writer) bool {
	for _, field := range strings.FieldsFunc(line, func(r rune) bool { return strings.ContainsRune(", \t、，\u3000", r) }) {
		if field == "0" {
			fmt.Fprintln(out, ui("Choose 0 alone. Mixed selections are not accepted.", "请单独输入 0，不能与其他选项混选。"))
			return true
		}
	}
	return false
}

// backCommand reports whether a text, token, or yes/no line asks to go back.
// 0 is the documented command. back and 返回 are hidden aliases that stay for
// scripts and habits; no prompt mentions them.
func backCommand(line string) bool {
	line = strings.TrimSpace(line)
	return line == "0" || strings.EqualFold(line, "back") || line == "返回"
}

func backHint(out io.Writer) {
	fmt.Fprintln(out, ui("Type 0 to return.", "输入 0 可回到上一步。"))
}

// readBackLine prints the back hint and reads one line. Use readBackLineQuiet
// where the prompt already lists a 0) menu row.
func readBackLine(in *bufio.Reader, out io.Writer, prompt string) (string, error) {
	backHint(out)
	return readBackLineQuiet(in, out, prompt)
}

func readBackLineQuiet(in *bufio.Reader, out io.Writer, prompt string) (string, error) {
	line, err := readLine(in, out, prompt)
	if err == nil && backCommand(line) {
		return "", errBack
	}
	return line, err
}
