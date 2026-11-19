package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/zfg5118/deeprouter/internal/apps"
)

const defaultMenuWidth = 80

func menuWidth(out io.Writer) int {
	if file, ok := out.(*os.File); ok {
		if width := terminalColumns(file); width > 0 {
			return width
		}
	}
	return defaultMenuWidth
}

func printProgramMenu(out io.Writer, rows []apps.Info, exits bool, width int) {
	const titleColumn = len("1) ")
	const minNoteWidth = 24
	width = max(width, titleColumn+2)
	noteColumn := titleColumn
	for _, row := range rows {
		noteColumn = max(noteColumn, titleColumn+displayWidth(row.Title)+2)
	}
	stackNotes := width-noteColumn < minNoteWidth
	if stackNotes {
		noteColumn = titleColumn
	}
	titleIndent := strings.Repeat(" ", titleColumn)
	noteIndent := strings.Repeat(" ", noteColumn)
	for index, row := range rows {
		titles := wrapMenuText(row.Title, width-titleColumn)
		fmt.Fprintf(out, "%d) %s", index+1, strings.Join(titles, "\n"+titleIndent))
		notes := wrapMenuText(row.Note, width-noteColumn)
		if len(notes) > 0 {
			if stackNotes {
				fmt.Fprint(out, "\n", noteIndent)
			} else {
				fmt.Fprint(out, strings.Repeat(" ", noteColumn-titleColumn-displayWidth(row.Title)))
			}
			fmt.Fprint(out, strings.Join(notes, "\n"+noteIndent))
		}
		fmt.Fprintln(out)
	}
	for index, line := range wrapMenuText(menuZeroLabel(exits), width-titleColumn) {
		prefix := titleIndent
		if index == 0 {
			prefix = "0) "
		}
		fmt.Fprintln(out, prefix+line)
	}
}

func wrapMenuText(text string, width int) []string {
	width = max(width, 2)
	var lines []string
	for text = strings.TrimSpace(text); text != ""; {
		columns, boundary, pathBoundary, end := 0, 0, 0, len(text)
		var previous rune
		for offset, char := range text {
			wide := wideRune(char)
			cjkBoundary := (wideRune(previous) || wide) &&
				!unicode.Is(unicode.Ps, previous) && !(wide && unicode.IsPunct(char))
			if unicode.IsSpace(char) || unicode.IsSpace(previous) || cjkBoundary {
				boundary = offset
			}
			if previous == '/' || previous == '\\' {
				pathBoundary = offset
			}
			columns++
			if wide {
				columns++
			}
			if columns > width {
				end = offset
				if boundary > 0 {
					end = boundary
				} else if pathBoundary > 0 {
					end = pathBoundary
				}
				break
			}
			previous = char
		}
		lines = append(lines, strings.TrimRightFunc(text[:end], unicode.IsSpace))
		text = strings.TrimLeftFunc(text[end:], unicode.IsSpace)
	}
	return lines
}
