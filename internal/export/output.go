package export

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// MarkerFile is written into every output folder. Export only clears a
// non-empty folder that contains it, so it can never delete a folder it did
// not create (Detailed Design §2).
const MarkerFile = ".provenance-export"

const markerContent = "This folder was written by `provenance export`.\n" +
	"Its entire contents are deleted and rewritten on every export.\n"

// NotOursError reports an output folder that export refuses to clear.
type NotOursError struct{ Dir string }

func (e *NotOursError) Error() string {
	return fmt.Sprintf("output folder %s is not empty and has no %s marker, so it was not written by export; "+
		"refusing to overwrite it (choose another --out or empty it yourself)", e.Dir, MarkerFile)
}

// WriteOutput replaces the content of dir with files. dir is created if
// missing; if it exists and is non-empty it must contain MarkerFile. display
// is how dir is named in errors (the path as the user gave it, so output
// never contains absolute paths).
func WriteOutput(dir, display string, files Files) error {
	if err := prepare(dir, display); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, MarkerFile), []byte(markerContent), 0o644); err != nil {
		return err
	}
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		if err := checkRelative(name); err != nil {
			return err
		}
		p := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(p, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}

// prepare leaves dir existing and empty, or refuses.
func prepare(dir, display string) error {
	info, err := os.Stat(dir)
	if errors.Is(err, os.ErrNotExist) {
		return os.MkdirAll(dir, 0o755)
	}
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("output path %s exists and is not a folder", display)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	if len(entries) == 0 {
		return nil
	}
	if _, err := os.Stat(filepath.Join(dir, MarkerFile)); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &NotOursError{Dir: display}
		}
		return err
	}
	for _, e := range entries {
		if err := os.RemoveAll(filepath.Join(dir, e.Name())); err != nil {
			return err
		}
	}
	return nil
}

func checkRelative(name string) error {
	clean := path.Clean(name)
	if name == "" || clean != name || path.IsAbs(name) || clean == ".." || strings.HasPrefix(clean, "../") || name == MarkerFile {
		return fmt.Errorf("exporter produced an invalid output path %q", name)
	}
	return nil
}
