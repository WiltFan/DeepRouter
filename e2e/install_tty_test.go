//go:build !windows

package e2e

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestInstallSkipsSetupWithoutTTY(t *testing.T) {
	root := moduleRoot(t)
	home := t.TempDir()
	cmd := exec.Command("sh", "install.sh", "--from-source", "--no-path")
	cmd.Dir = root
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Env = installEnv(home, false)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("install: %v\n%s", err, out)
	}
	text := string(out)
	bin := filepath.Join(home, ".local", "bin", "deeprouter")
	if !strings.Contains(text, "Next, run:") || !strings.Contains(text, bin) {
		t.Fatalf("hint:\n%s", text)
	}
	if strings.Contains(text, "Choose which programs") || strings.Contains(text, "Platform [") || strings.Contains(text, "Starting ") {
		t.Fatalf("setup was launched:\n%s", text)
	}
}

func TestInstallLaunchesSetupWhenTTY(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required to allocate a terminal")
	}
	root := moduleRoot(t)
	home := t.TempDir()
	prefix := filepath.Join(home, "bin")
	out := runInstallOnTTY(t, root, home, prefix, false)
	bin := filepath.Join(prefix, "deeprouter")
	if !strings.Contains(out, "Starting "+bin) || !strings.Contains(out, "Choose which programs") {
		t.Fatalf("wizard did not start:\n%s", out)
	}
	if strings.Contains(out, "Next, run:") {
		t.Fatalf("hint replaced the launch:\n%s", out)
	}
	if _, err := os.Stat(bin); err != nil {
		t.Fatal(err)
	}
}

func TestInstallOptOutSkipsSetupOnTTY(t *testing.T) {
	if _, err := exec.LookPath("python3"); err != nil {
		t.Skip("python3 is required to allocate a terminal")
	}
	root := moduleRoot(t)
	home := t.TempDir()
	prefix := filepath.Join(home, "bin")
	out := runInstallOnTTY(t, root, home, prefix, true)
	bin := filepath.Join(prefix, "deeprouter")
	if !strings.Contains(out, "Next, run:") || !strings.Contains(out, bin) {
		t.Fatalf("hint:\n%s", out)
	}
	if strings.Contains(out, "Choose which programs") || strings.Contains(out, "Platform [") || strings.Contains(out, "Starting ") {
		t.Fatalf("opt-out still launched setup:\n%s", out)
	}
}

func installEnv(home string, optOut bool) []string {
	env := make([]string, 0, len(os.Environ())+4)
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "HOME=") || strings.HasPrefix(kv, "SHELL=") || strings.HasPrefix(kv, "DEEPROUTER_NO_SETUP=") || strings.HasPrefix(kv, "DEEPROUTER_SETUP_VERSION=") {
			continue
		}
		env = append(env, kv)
	}
	env = append(env, "HOME="+home, "SHELL=/bin/zsh")
	if optOut {
		env = append(env, "DEEPROUTER_NO_SETUP=1")
	}
	return env
}

func runInstallOnTTY(t *testing.T, root, home, prefix string, optOut bool) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	mode := "launch"
	if optOut {
		mode = "skip"
	}
	cmd := exec.CommandContext(ctx, "python3", "-c", installPTYScript, prefix, root, home, mode)
	cmd.Env = installEnv(home, optOut)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("pty install: %v\n%s", err, out)
	}
	return string(out)
}

const installPTYScript = `
import os, select, signal, sys, time
prefix, root, home, mode = sys.argv[1:5]
pid, fd = os.forkpty()
if pid == 0:
    os.chdir(root)
    os.environ["HOME"] = home
    os.environ["SHELL"] = "/bin/zsh"
    os.environ.pop("DEEPROUTER_SETUP_VERSION", None)
    if mode == "skip":
        os.environ["DEEPROUTER_NO_SETUP"] = "1"
    else:
        os.environ.pop("DEEPROUTER_NO_SETUP", None)
    os.execvp("sh", ["sh", "install.sh", "--prefix", prefix, "--from-source", "--no-path"])
buf = b""
deadline = time.time() + 150
launched = False

def reap(child):
    end = time.time() + 5
    while time.time() < end:
        done, _ = os.waitpid(child, os.WNOHANG)
        if done == child:
            return
        time.sleep(0.1)
    try:
        os.kill(child, signal.SIGKILL)
    except OSError:
        pass
    try:
        os.waitpid(child, 0)
    except ChildProcessError:
        pass

while time.time() < deadline:
    ready, _, _ = select.select([fd], [], [], 0.5)
    if ready:
        try:
            chunk = os.read(fd, 4096)
        except OSError:
            break
        if not chunk:
            break
        buf += chunk
        if mode == "launch" and (b"Platform [" in buf or b"Choose which programs" in buf or b"Programs [1]:" in buf):
            launched = True
            try:
                os.kill(pid, signal.SIGTERM)
            except OSError:
                pass
            break
    done, _ = os.waitpid(pid, os.WNOHANG)
    if done == pid:
        break
else:
    try:
        os.kill(pid, signal.SIGKILL)
    except OSError:
        pass
reap(pid)
sys.stdout.buffer.write(buf)
if mode == "launch" and not launched:
    sys.exit(1)
`
