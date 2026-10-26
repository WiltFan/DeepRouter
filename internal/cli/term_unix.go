//go:build !windows

package cli

import (
	"io"
	"os"
	"os/exec"
)

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
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

// readMaskedLine turns off echo and canonical mode, then prints one '*' per
// typed character. Paste shows one star per character. A pipe returns
// errMaskedUnavailable.
func readMaskedLine(f *os.File, echo io.Writer) (string, error) {
	if !isTerminal(f) {
		return "", errMaskedUnavailable
	}
	if _, err := exec.LookPath("stty"); err != nil {
		return "", errMaskedUnavailable
	}
	raw := exec.Command("stty", "-echo", "-icanon", "min", "1", "time", "0")
	raw.Stdin = f
	if err := raw.Run(); err != nil {
		return "", errMaskedUnavailable
	}
	defer func() {
		on := exec.Command("stty", "echo", "icanon")
		on.Stdin = f
		_ = on.Run()
	}()
	return consumeMasked(f, echo)
}
