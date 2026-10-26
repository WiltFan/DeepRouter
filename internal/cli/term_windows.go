//go:build windows

package cli

import (
	"io"
	"os"
	"syscall"
	"unsafe"
)

const enableEchoInput = 0x0004

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
