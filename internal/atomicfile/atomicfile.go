// Package atomicfile writes files by rename so a crash cannot leave a partial config.
package atomicfile

import (
	"os"
	"path/filepath"
	"runtime"
)

// Write replaces path with data, using mode for a newly created file.
// An existing file keeps its mode when keepMode is true.
func Write(path string, data []byte, mode os.FileMode, keepMode bool) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	if keepMode {
		if info, err := os.Stat(path); err == nil && info.Mode().IsRegular() {
			mode = info.Mode().Perm()
		}
	}
	tmp := path + ".deeprouter.tmp"
	f, err := os.OpenFile(tmp, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, werr := f.Write(data)
	cerr := f.Close()
	if werr != nil {
		_ = os.Remove(tmp)
		return werr
	}
	if cerr != nil {
		_ = os.Remove(tmp)
		return cerr
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		if runtime.GOOS == "windows" {
			_ = os.Remove(path)
			if err2 := os.Rename(tmp, path); err2 == nil {
				return nil
			}
		}
		_ = os.Remove(tmp)
		return err
	}
	return nil
}
