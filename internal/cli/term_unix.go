//go:build !windows

package cli

import (
	"io"
	"os"
	"os/exec"
	"strings"
)

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	// /dev/null is a character device, so ModeCharDevice is not enough.
	// A real terminal answers the termios ioctl.
	return isatty(f.Fd())
}

// disableTerminalEcho turns off echo with stty. ok is false when echo cannot
// be changed. Windows uses the console API instead of stty, because stty is
// not part of the Windows console.
func disableTerminalEcho(f *os.File) (restore func(), ok bool) {
	if _, err := exec.LookPath("stty"); err != nil {
		return func() {}, false
	}
	off := exec.Command("stty", "-echo")
	off.Stdin = f
	if err := off.Run(); err != nil {
		return func() {}, false
	}
	return func() {
		on := exec.Command("stty", "echo")
		on.Stdin = f
		_ = on.Run()
	}, true
}

// readMaskedLine turns off echo, canonical mode, and signal keys, then prints
// one '*' per typed character. Paste shows one star per character. Ctrl-C
// and Ctrl-D arrive as bytes, so the terminal settings saved with stty -g
// are always put back. A pipe returns errMaskedUnavailable.
func readMaskedLine(f *os.File, echo io.Writer) (string, error) {
	if !isTerminal(f) {
		return "", errMaskedUnavailable
	}
	if _, err := exec.LookPath("stty"); err != nil {
		return "", errMaskedUnavailable
	}
	save := exec.Command("stty", "-g")
	save.Stdin = f
	saved := ""
	if out, err := save.Output(); err == nil {
		saved = strings.TrimSpace(string(out))
	}
	raw := exec.Command("stty", "-echo", "-icanon", "-isig", "min", "1", "time", "0")
	raw.Stdin = f
	if err := raw.Run(); err != nil {
		return "", errMaskedUnavailable
	}
	defer func() {
		args := []string{"echo", "icanon", "isig"}
		if saved != "" {
			args = []string{saved}
		}
		on := exec.Command("stty", args...)
		on.Stdin = f
		_ = on.Run()
	}()
	return consumeMasked(f, echo)
}
