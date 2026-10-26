//go:build windows

package cli

import (
	"os"
	"testing"
)

func TestPipeIsNotAWindowsConsole(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = r.Close()
		_ = w.Close()
	})
	if isTerminal(r) {
		t.Fatal("a pipe was treated as a console")
	}
	if _, ok := disableTerminalEcho(r); ok {
		t.Fatal("echo was disabled on a pipe")
	}
}
