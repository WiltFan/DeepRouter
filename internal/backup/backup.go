// Package backup stores exact copies of config files before they are changed.
package backup

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/zfg5118/deeprouter/internal/atomicfile"
)

// Store keeps an original snapshot (the bytes before this tool first touched
// a path) and a history of later pre-write copies.
type Store struct {
	Dir string
}

type manifest struct {
	Original map[string]snap `json:"original"`
	History  []entry         `json:"history"`
}

type snap struct {
	Path    string `json:"path"`
	Existed bool   `json:"existed"`
	Mode    uint32 `json:"mode,omitempty"`
	File    string `json:"file,omitempty"`
	// CreatedDirs are parent directories that did not exist when this
	// missing file was first seen, deepest first. Restore removes them
	// only when they are empty.
	CreatedDirs []string `json:"created_dirs,omitempty"`
}

type entry struct {
	ID    string `json:"id"`
	At    string `json:"at"`
	Files []snap `json:"files"`
}

// EntryInfo is a backup a person can restore.
type EntryInfo struct {
	ID    string
	At    string
	Paths []string
}

// EnsureOriginal records path the first time it is seen.
// Later calls do not overwrite that snapshot.
func (s Store) EnsureOriginal(path string) error {
	m, err := s.load()
	if err != nil {
		return err
	}
	if m.Original == nil {
		m.Original = map[string]snap{}
	}
	if _, ok := m.Original[path]; ok {
		return nil
	}
	snap, err := capture(s.Dir, "original", path)
	if err != nil {
		return err
	}
	m.Original[path] = snap
	return s.save(m)
}

// SaveHistory stores the current bytes of each existing path under a new id.
// Missing paths are skipped. The returned id is empty when nothing was saved.
func (s Store) SaveHistory(paths []string) (string, error) {
	var files []snap
	id := time.Now().UTC().Format("20060102T150405.000000000Z")
	for _, path := range paths {
		info, err := os.Lstat(path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		if !info.Mode().IsRegular() {
			return "", fmt.Errorf("%s is not a regular file", path)
		}
		snap, err := capture(s.Dir, id, path)
		if err != nil {
			return "", err
		}
		files = append(files, snap)
	}
	if len(files) == 0 {
		return "", nil
	}
	m, err := s.load()
	if err != nil {
		return "", err
	}
	m.History = append(m.History, entry{
		ID:    id,
		At:    time.Now().UTC().Format(time.RFC3339),
		Files: files,
	})
	if err := s.save(m); err != nil {
		return "", err
	}
	return id, nil
}

// List returns the original snapshot first, then history from oldest to newest.
func (s Store) List() ([]EntryInfo, error) {
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	var out []EntryInfo
	if len(m.Original) > 0 {
		paths := make([]string, 0, len(m.Original))
		for path := range m.Original {
			paths = append(paths, path)
		}
		sort.Strings(paths)
		out = append(out, EntryInfo{ID: "original", Paths: paths})
	}
	for _, e := range m.History {
		info := EntryInfo{ID: e.ID, At: e.At}
		for _, f := range e.Files {
			info.Paths = append(info.Paths, f.Path)
		}
		out = append(out, info)
	}
	return out, nil
}

// Restore writes a snapshot back. id "original" (or empty) restores the
// first-touch copies. id "latest" restores the newest history entry, or the
// original snapshot when history is empty.
func (s Store) Restore(id string) ([]string, error) {
	m, err := s.load()
	if err != nil {
		return nil, err
	}
	if id == "" || id == "original" {
		return restoreSnaps(m.Original)
	}
	if id == "latest" {
		if len(m.History) == 0 {
			return restoreSnaps(m.Original)
		}
		return restoreList(m.History[len(m.History)-1].Files)
	}
	for _, e := range m.History {
		if e.ID == id {
			return restoreList(e.Files)
		}
	}
	return nil, fmt.Errorf("no backup %q", id)
}

func restoreSnaps(snaps map[string]snap) ([]string, error) {
	if len(snaps) == 0 {
		return nil, fmt.Errorf("no original backup yet")
	}
	paths := make([]string, 0, len(snaps))
	for path := range snaps {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	var restored []string
	for _, path := range paths {
		if err := writeSnap(snaps[path]); err != nil {
			return restored, err
		}
		restored = append(restored, path)
	}
	return restored, nil
}

func restoreList(files []snap) ([]string, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("backup is empty")
	}
	var restored []string
	for _, f := range files {
		if err := writeSnap(f); err != nil {
			return restored, err
		}
		restored = append(restored, f.Path)
	}
	return restored, nil
}

func writeSnap(s snap) error {
	if !s.Existed {
		err := os.Remove(s.Path)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		return removeEmptyDirs(s.CreatedDirs)
	}
	data, err := os.ReadFile(s.File)
	if err != nil {
		return fmt.Errorf("read backup for %s: %w", s.Path, err)
	}
	mode := os.FileMode(s.Mode)
	if mode == 0 {
		mode = 0o600
	}
	return atomicfile.Write(s.Path, data, mode, false)
}

func capture(dir, bucket, path string) (snap, error) {
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return snap{Path: path, Existed: false, CreatedDirs: missingParents(path)}, nil
	}
	if err != nil {
		return snap{}, err
	}
	if info.IsDir() {
		return snap{}, fmt.Errorf("%s is a directory", path)
	}
	if !info.Mode().IsRegular() {
		return snap{}, fmt.Errorf("%s is not a regular file", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return snap{}, err
	}
	sum := sha256.Sum256([]byte(path))
	name := hex.EncodeToString(sum[:8]) + ".bin"
	dest := filepath.Join(dir, "files", bucket, name)
	if err := atomicfile.Write(dest, data, 0o600, false); err != nil {
		return snap{}, err
	}
	return snap{
		Path:    path,
		Existed: true,
		Mode:    uint32(info.Mode().Perm()),
		File:    dest,
	}, nil
}

// missingParents lists directories above path that do not exist yet,
// starting at the parent of path. An existing ancestor, and the
// filesystem root, are not included.
func missingParents(path string) []string {
	var dirs []string
	dir := filepath.Dir(filepath.Clean(path))
	for {
		if dir == "" || dir == "." || dir == string(filepath.Separator) {
			break
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		if _, err := os.Stat(dir); err == nil || !errors.Is(err, os.ErrNotExist) {
			break
		}
		dirs = append(dirs, dir)
		dir = parent
	}
	return dirs
}

// removeEmptyDirs deletes dirs, deepest first, when each one is empty.
// A missing directory or one that still has entries is left alone.
func removeEmptyDirs(dirs []string) error {
	for _, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if len(entries) > 0 {
			continue
		}
		if err := os.Remove(dir); err != nil && !errors.Is(err, os.ErrNotExist) {
			continue
		}
	}
	return nil
}

func (s Store) load() (manifest, error) {
	var m manifest
	m.Original = map[string]snap{}
	raw, err := os.ReadFile(s.manifestPath())
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return m, err
	}
	if len(bytesTrim(raw)) == 0 {
		return m, nil
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return m, fmt.Errorf("backup manifest is not valid JSON: %w", err)
	}
	if m.Original == nil {
		m.Original = map[string]snap{}
	}
	return m, nil
}

func (s Store) save(m manifest) error {
	if m.Original == nil {
		m.Original = map[string]snap{}
	}
	raw, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	return atomicfile.Write(s.manifestPath(), raw, 0o600, false)
}

func (s Store) manifestPath() string {
	return filepath.Join(s.Dir, "manifest.json")
}

func bytesTrim(b []byte) []byte {
	i := 0
	j := len(b)
	for i < j && (b[i] == ' ' || b[i] == '\n' || b[i] == '\t' || b[i] == '\r') {
		i++
	}
	for j > i && (b[j-1] == ' ' || b[j-1] == '\n' || b[j-1] == '\t' || b[j-1] == '\r') {
		j--
	}
	return b[i:j]
}
