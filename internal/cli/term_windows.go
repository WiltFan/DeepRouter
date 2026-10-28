//go:build windows

package cli

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

const (
	enableProcessedInput = 0x0001
	enableLineInput      = 0x0002
	enableEchoInput      = 0x0004
)

var (
	kernel32           = syscall.NewLazyDLL("kernel32.dll")
	procGetConsoleMode = kernel32.NewProc("GetConsoleMode")
	procSetConsoleMode = kernel32.NewProc("SetConsoleMode")
)

func isTerminal(r io.Reader) bool {
	f, ok := r.(*os.File)
	if !ok {
		return false
	}
	return consoleMode(f) != nil
}

func consoleMode(f *os.File) *uint32 {
	var mode uint32
	r, _, _ := procGetConsoleMode.Call(f.Fd(), uintptr(unsafe.Pointer(&mode)))
	if r == 0 {
		return nil
	}
	return &mode
}

// disableTerminalEcho clears ENABLE_ECHO_INPUT on a Windows console.
// A pipe is not a console, so ok is false and the prompt still reads a line.
// This does not open /dev/tty, which Windows does not have.
func disableTerminalEcho(f *os.File) (restore func(), ok bool) {
	mode := consoleMode(f)
	if mode == nil {
		return func() {}, false
	}
	next := *mode &^ enableEchoInput
	r, _, _ := procSetConsoleMode.Call(f.Fd(), uintptr(next))
	if r == 0 {
		return func() {}, false
	}
	original := *mode
	return func() {
		procSetConsoleMode.Call(f.Fd(), uintptr(original))
	}, true
}

// readMaskedLine disables line input, echo, and processed input, then prints
// one '*' per character. Ctrl-C arrives as a byte instead of ending the
// process, so the console mode is always put back. A pipe is not a console,
// so it returns errMaskedUnavailable.
func readMaskedLine(f *os.File, echo io.Writer) (string, error) {
	mode := consoleMode(f)
	if mode == nil {
		return "", errMaskedUnavailable
	}
	next := *mode &^ enableEchoInput &^ enableLineInput &^ enableProcessedInput
	r, _, _ := procSetConsoleMode.Call(f.Fd(), uintptr(next))
	if r == 0 {
		return "", errMaskedUnavailable
	}
	original := *mode
	defer procSetConsoleMode.Call(f.Fd(), uintptr(original))
	return consumeMasked(f, echo)
}
