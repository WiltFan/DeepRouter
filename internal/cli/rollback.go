package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strings"
	"sync"

	"github.com/zfg5118/deeprouter/internal/apply"
	"github.com/zfg5118/deeprouter/internal/atomicfile"
	"github.com/zfg5118/deeprouter/internal/claudedesktop"
	"github.com/zfg5118/deeprouter/internal/store"
	"github.com/zfg5118/deeprouter/internal/userenv"
)

// writeSnapshot is every path one setup write can change, captured right
// before that write. A rollback puts those bytes back, removes the files and
// directories the write created, and returns the Windows user environment to
// its earlier values. Files written by earlier runs are left alone.
type writeSnapshot struct {
	files   []fileState
	dirs    []string
	backups string
	kept    map[string]bool
	env     userenv.Store
	vars    []varState
}

type fileState struct {
	path    string
	existed bool
	skip    bool
	link    string
	data    []byte
	mode    os.FileMode
}

type varState struct {
	key   string
	value string
	set   bool
}

// writePaths lists the files a write of prof and req can create or change,
// in the order the confirmation prompt shows them.
func (e env) writePaths(prof store.Profile, req apply.Request) []string {
	var paths []string
	add := func(path string) {
		if path != "" {
			paths = append(paths, path)
		}
	}
	if req.TouchClaude {
		add(e.layout.ClaudePath)
	}
	if req.TouchCodex {
		add(e.layout.CodexPath)
	}
	if req.TouchGrok {
		add(e.layout.GrokPath)
	}
	if req.TouchGemini {
		add(e.layout.GeminiPath)
	}
	desktop := req.TouchDesktop
	if !req.AppsExplicit {
		desktop = req.TouchClaude
	}
	if desktop && e.layout.DesktopLibrary != "" {
		files, err := claudedesktop.Plan(e.layout.DesktopLibrary, claudedesktop.Update{
			BaseURL:    prof.BaseURL,
			APIKey:     firstNonEmpty(prof.DesktopKey, prof.APIKey),
			Model:      prof.DesktopModel,
			SetModel:   req.SetDesktopModel,
			ClearModel: req.ClearDesktopModel,
		})
		if err == nil {
			add(files.MetaPath)
			add(files.ProfilePath)
		}
	}
	if req.TouchShell || req.TouchGemini {
		if e.userEnv {
			add(userenv.RecordPath(e.layout.ConfigDir))
		} else {
			add(e.layout.RCFile)
		}
	}
	add(e.stateP)
	return paths
}

func (e env) snapshotWrite(prof store.Profile, req apply.Request) writeSnapshot {
	snap := writeSnapshot{backups: e.backups().Dir}
	paths := e.writePaths(prof, req)
	paths = append(paths, filepath.Join(snap.backups, "manifest.json"))
	seenFile := map[string]bool{}
	seenDir := map[string]bool{}
	for _, path := range paths {
		if seenFile[path] {
			continue
		}
		seenFile[path] = true
		snap.files = append(snap.files, captureFile(path))
		snap.dirs = appendMissingDirs(snap.dirs, path, seenDir)
	}
	snap.kept = listTree(snap.backups)
	if e.userEnv && (req.TouchShell || req.TouchGemini) {
		if st := userenv.Active(); st != nil {
			snap.env = st
			for _, key := range userenv.ManagedKeys {
				value, ok, err := st.Get(key)
				if err != nil {
					continue
				}
				snap.vars = append(snap.vars, varState{key: key, value: value, set: ok})
			}
		}
	}
	return snap
}

func captureFile(path string) fileState {
	st := fileState{path: path}
	info, err := os.Lstat(path)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			st.skip = true
		}
		return st
	}
	st.existed = true
	st.mode = info.Mode().Perm()
	if info.Mode()&os.ModeSymlink != 0 {
		link, err := os.Readlink(path)
		if err != nil {
			st.skip = true
		}
		st.link = link
		return st
	}
	data, err := os.ReadFile(path)
	if err != nil {
		st.skip = true
		return st
	}
	st.data = data
	return st
}

func (f fileState) restore() error {
	if f.skip {
		return nil
	}
	if !f.existed {
		if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	if f.link != "" {
		if cur, err := os.Readlink(f.path); err == nil && cur == f.link {
			return nil
		}
		if err := os.Remove(f.path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return os.Symlink(f.link, f.path)
	}
	if cur, err := os.ReadFile(f.path); err == nil && bytes.Equal(cur, f.data) {
		if info, err := os.Lstat(f.path); err == nil && info.Mode().Perm() != f.mode {
			return os.Chmod(f.path, f.mode)
		}
		return nil
	}
	return atomicfile.Write(f.path, f.data, f.mode, false)
}

// appendMissingDirs adds the parents of path that do not exist yet.
func appendMissingDirs(dirs []string, path string, seen map[string]bool) []string {
	dir := filepath.Dir(path)
	for {
		if _, err := os.Lstat(dir); err == nil {
			return dirs
		}
		if !seen[dir] {
			seen[dir] = true
			dirs = append(dirs, dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return dirs
		}
		dir = parent
	}
}

func listTree(root string) map[string]bool {
	out := map[string]bool{}
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err == nil {
			out[path] = true
		}
		return nil
	})
	return out
}

// removeDeepestFirst removes paths, children before parents. A directory
// that still holds something is left in place.
func removeDeepestFirst(paths []string) {
	sorted := append([]string(nil), paths...)
	sort.Slice(sorted, func(i, j int) bool { return len(sorted[i]) > len(sorted[j]) })
	for _, path := range sorted {
		_ = os.Remove(path)
	}
}

// rollback undoes one write. Every step runs even when an earlier one fails.
func (e env) rollback(snap writeSnapshot) error {
	var errs []string
	for _, f := range snap.files {
		if err := f.restore(); err != nil {
			errs = append(errs, err.Error())
		}
	}
	if snap.backups != "" {
		var added []string
		_ = filepath.WalkDir(snap.backups, func(path string, d fs.DirEntry, err error) error {
			if err == nil && !snap.kept[path] {
				added = append(added, path)
			}
			return nil
		})
		removeDeepestFirst(added)
	}
	removeDeepestFirst(snap.dirs)
	if snap.env != nil {
		changed := false
		for _, v := range snap.vars {
			cur, ok, err := snap.env.Get(v.key)
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			switch {
			case v.set && (!ok || cur != v.value):
				err = snap.env.Set(v.key, v.value)
				changed = true
			case !v.set && ok:
				err = snap.env.Delete(v.key)
				changed = true
			}
			if err != nil {
				errs = append(errs, err.Error())
			}
		}
		if n, ok := snap.env.(interface{ Notify() }); ok && changed {
			n.Notify()
		}
	}
	if len(errs) > 0 {
		return errors.New(strings.Join(errs, "; "))
	}
	return nil
}

func (e env) rollbackSetup(snap writeSnapshot) {
	if err := e.rollback(snap); err != nil {
		fmt.Fprintln(e.stderr, ui("Rollback was incomplete: "+err.Error(), "撤销没有全部完成："+err.Error()))
	}
}

// exitProcess is replaced in tests.
var exitProcess = os.Exit

// interruptGuard handles Ctrl-C during the wizard. Before the write nothing
// has changed, so it only says that. From the write until the connectivity
// test passes or the user picks a recovery option, it rolls the write back
// first. Masked token input reads Ctrl-C as a byte instead.
type interruptGuard struct {
	mu      sync.Mutex
	e       env
	snap    *writeSnapshot
	settled bool
	// kept is a settled write that stays, as opposed to one rolled back.
	kept bool
	sig  chan os.Signal
	quit chan struct{}
}

func (e env) guardInterrupts() *interruptGuard {
	g := &interruptGuard{e: e, sig: make(chan os.Signal, 1), quit: make(chan struct{})}
	signal.Notify(g.sig, os.Interrupt)
	go func() {
		select {
		case <-g.sig:
			exitProcess(g.interrupted())
		case <-g.quit:
		}
	}()
	return g
}

func (g *interruptGuard) stop() {
	signal.Stop(g.sig)
	close(g.quit)
}

// interrupted reports the interrupt and returns the exit code. It holds the
// lock for good, so a write in progress finishes before the rollback and no
// later write starts.
func (g *interruptGuard) interrupted() int {
	g.mu.Lock()
	fmt.Fprintln(g.e.stderr)
	switch {
	case g.snap == nil:
		fmt.Fprintln(g.e.stderr, cancelledLine())
	case !g.settled:
		g.e.rollbackSetup(*g.snap)
		fmt.Fprintln(g.e.stderr, ui("Interrupted. Rolled back the files just written.", "已中断，已撤销刚才的写入。"))
	case g.kept:
		fmt.Fprintln(g.e.stderr, ui("Interrupted. The files above stay as written.", "已中断。上面写入的文件保持不变。"))
	default:
		fmt.Fprintln(g.e.stderr, ui("Interrupted.", "已中断。"))
	}
	return 130
}

// write runs save while an interrupt waits, and arms the rollback first.
func (g *interruptGuard) write(snap writeSnapshot, save func() error) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.snap = &snap
	return save()
}

// settle keeps the write: a later interrupt no longer rolls it back.
func (g *interruptGuard) settle() {
	g.mu.Lock()
	g.settled = true
	g.kept = g.snap != nil
	g.mu.Unlock()
}

// rollback undoes the write once, so an interrupt during it does not run
// a second rollback.
func (g *interruptGuard) rollback() {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.snap != nil && !g.settled {
		g.e.rollbackSetup(*g.snap)
	}
	g.settled = true
}

func cancelledLine() string {
	return ui("Setup cancelled. Nothing was written.", "已取消配置，没有写入任何文件。")
}
