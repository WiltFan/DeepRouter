package imageskill

import (
	"fmt"
	"os"
	"path/filepath"
)

// Hooks the tests replace to make a step fail.
var (
	rename    = os.Rename
	writeFile = os.WriteFile
)

// Txn is one finished install that can still be undone. Commit drops the
// copy of the folder it replaced and the old bundled folder. Rollback puts
// the previous state back.
type Txn struct {
	plan    Plan
	created []string // parent folders this install made, outermost first
	backup  string   // previous Skill folder, moved aside; empty if none
	legacy  string   // old bundled deeprouter-image-api folder, moved aside; empty if none
	done    bool
}

// Install writes the embedded Skill to p.Dir. A Missing folder is created,
// and a Different one is replaced as a whole, so files the user added there
// do not survive. Same writes nothing.
//
// An old deeprouter-image-api folder that is LegacyBundled is moved aside
// once the new folder is in place and deleted by Commit. A LegacyKept one
// is never touched, and a Skipped plan is an error.
//
// The files are written to a temporary folder next to p.Dir and renamed into
// place, so the Skill folder is never half written. On an error everything
// this call made is removed and a replaced folder is put back.
func Install(p Plan) (*Txn, error) {
	if p.Skipped() {
		return nil, fmt.Errorf("%s is not the bundled Skill and is kept; not installing %s beside it", p.LegacyDir, p.Dir)
	}
	t := &Txn{plan: p}
	if !p.Writes() {
		t.done = true
		return t, nil
	}
	if p.State != Same {
		if err := t.write(); err != nil {
			return nil, err
		}
	}
	if p.Legacy == LegacyBundled {
		if err := t.moveLegacy(); err != nil {
			if rerr := t.Rollback(); rerr != nil {
				return nil, fmt.Errorf("%v; %v", err, rerr)
			}
			return nil, err
		}
	}
	return t, nil
}

// write puts the embedded Skill at plan.Dir, replacing a Different folder.
func (t *Txn) write() error {
	p := t.plan
	var err error
	if t.created, err = mkdirs(p.Base, p.Root); err != nil {
		t.removeCreated()
		return err
	}
	stage, err := os.MkdirTemp(p.Root, "."+Name+".tmp-")
	if err != nil {
		t.removeCreated()
		return err
	}
	if err := writeTree(stage); err != nil {
		_ = os.RemoveAll(stage)
		t.removeCreated()
		return err
	}
	if p.State == Different {
		// A free name next to the folder: make it, then drop it so the old
		// folder can be renamed to it.
		backup, err := os.MkdirTemp(p.Root, "."+Name+".old-")
		if err == nil {
			err = os.Remove(backup)
		}
		if err == nil {
			err = rename(p.Dir, backup)
		}
		if err != nil {
			_ = os.RemoveAll(stage)
			return fmt.Errorf("move the old Skill folder aside: %w", err)
		}
		t.backup = backup
	} else if _, err := os.Lstat(p.Dir); err == nil {
		// Something appeared there after Check.
		_ = os.RemoveAll(stage)
		return fmt.Errorf("%s appeared while installing; nothing was replaced", p.Dir)
	}
	if err := rename(stage, p.Dir); err != nil {
		_ = os.RemoveAll(stage)
		if t.backup != "" {
			if rerr := rename(t.backup, p.Dir); rerr != nil {
				return fmt.Errorf("install %s: %v; the previous folder is at %s", p.Dir, err, t.backup)
			}
		}
		t.removeCreated()
		return err
	}
	return nil
}

// moveLegacy moves the old bundled folder aside, after checking again that
// it is still exactly a bundled Skill.
func (t *Txn) moveLegacy() error {
	p := t.plan
	if l, err := checkLegacy(p.LegacyDir); err != nil {
		return err
	} else if l != LegacyBundled {
		return fmt.Errorf("%s changed while installing; it was kept", p.LegacyDir)
	}
	aside, err := os.MkdirTemp(p.Root, "."+Name+".legacy-")
	if err == nil {
		err = os.Remove(aside)
	}
	if err == nil {
		err = rename(p.LegacyDir, aside)
	}
	if err != nil {
		return fmt.Errorf("move the old %s folder aside: %w", LegacyName, err)
	}
	t.legacy = aside
	return nil
}

// Commit keeps the install and removes the replaced folder and the old
// bundled folder.
func (t *Txn) Commit() {
	if t == nil || t.done {
		return
	}
	t.done = true
	if t.backup != "" {
		_ = os.RemoveAll(t.backup)
	}
	if t.legacy != "" {
		_ = os.RemoveAll(t.legacy)
	}
}

// Rollback undoes the install: the old bundled folder goes back, the new
// folder is removed, and the replaced one, if any, goes back. Folders this
// install created are removed when empty.
func (t *Txn) Rollback() error {
	if t == nil || t.done {
		return nil
	}
	t.done = true
	if t.legacy != "" {
		if err := rename(t.legacy, t.plan.LegacyDir); err != nil {
			return fmt.Errorf("put back %s: %v; the old folder is at %s", t.plan.LegacyDir, err, t.legacy)
		}
	}
	if t.plan.State == Same {
		return nil
	}
	if err := os.RemoveAll(t.plan.Dir); err != nil {
		return err
	}
	if t.backup != "" {
		if err := rename(t.backup, t.plan.Dir); err != nil {
			return fmt.Errorf("put back %s: %v; the previous folder is at %s", t.plan.Dir, err, t.backup)
		}
	}
	t.removeCreated()
	return nil
}

// removeCreated deletes the folders this install made, innermost first,
// and only while they are empty.
func (t *Txn) removeCreated() {
	for i := len(t.created) - 1; i >= 0; i-- {
		if os.Remove(t.created[i]) != nil {
			return
		}
	}
	t.created = nil
}

// mkdirs creates root and any missing folders between base and root with
// mode 0700, and returns the ones it made. base itself must exist.
func mkdirs(base, root string) ([]string, error) {
	if info, err := os.Stat(base); err != nil {
		return nil, err
	} else if !info.IsDir() {
		return nil, fmt.Errorf("%s is not a directory", base)
	}
	var missing []string
	for dir := root; dir != base; dir = filepath.Dir(dir) {
		if _, err := os.Lstat(dir); err == nil {
			break
		} else if !os.IsNotExist(err) {
			return nil, err
		}
		missing = append([]string{dir}, missing...)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	var created []string
	for _, dir := range missing {
		if err := os.Mkdir(dir, 0o700); err != nil {
			return created, err
		}
		created = append(created, dir)
	}
	return created, nil
}

func writeTree(dir string) error {
	files := Files()
	for _, rel := range FileList() {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			return err
		}
		if err := writeFile(path, files[rel], 0o644); err != nil {
			return err
		}
	}
	return nil
}
