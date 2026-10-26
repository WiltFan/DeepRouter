package cli

import (
	"errors"
	"fmt"
	"io"
	"unicode/utf8"
)

// errMaskedUnavailable means the terminal cannot echo one star per character.
var errMaskedUnavailable = errors.New("masked input is unavailable")

// readMasked reads one secret line from a terminal, echoing '*' per character.
// Tests replace it. The Unix and Windows files implement readMaskedLine.
var readMasked = readMaskedLine

// consumeMasked reads one line from r. Backspace erases the last character
// and its star. Enter ends the line.
func consumeMasked(r io.Reader, echo io.Writer) (string, error) {
	var buf []byte
	one := make([]byte, 1)
	for {
		_, err := io.ReadFull(r, one)
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
				fmt.Fprintln(echo)
				return string(buf), nil
			}
			return "", err
		}
		switch one[0] {
		case '\n', '\r':
			fmt.Fprintln(echo)
			return string(buf), nil
		case 0x7f, 0x08:
			if len(buf) == 0 {
				continue
			}
			_, size := utf8.DecodeLastRune(buf)
			if size > 0 && size <= len(buf) {
				buf = buf[:len(buf)-size]
			}
			fmt.Fprint(echo, "\b \b")
		case 0x03:
			fmt.Fprintln(echo)
			return "", fmt.Errorf("interrupted")
		default:
			if one[0] < 32 {
				continue
			}
			buf = append(buf, one[0])
			fmt.Fprint(echo, "*")
		}
	}
}
