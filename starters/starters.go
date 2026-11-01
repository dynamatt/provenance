// Package starters holds the starter projects provenance init writes
// (REQ-0003), one folder each, embedded in the binary so that its hash
// covers the starter it writes. A starter is an ordinary project: the
// files a team then owns, edits or deletes.
package starters

import (
	"embed"
	"fmt"
	"io/fs"
	"regexp"
	"slices"
	"strings"
)

//go:embed all:medical-device
var embedded embed.FS

// Default is the starter init writes without --template.
const Default = "medical-device"

// Names are the built-in starters, sorted.
func Names() []string {
	entries, _ := fs.ReadDir(embedded, ".")
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	slices.Sort(names)
	return names
}

// Values fill a starter's blanks: @CODE@ and @NAME@ in .component and
// README.md, @VERSION@ in the CI workflow.
type Values struct {
	Code, Name string
	// Version is the release the CI workflow runs. A starter written by
	// a binary that isn't a release has no CI workflow: there is no
	// published binary to pin.
	Version string
}

// File is one file of a starter, at its slash-separated path.
type File struct {
	Path string
	Data []byte
}

// workflow is the CI gate's path in every starter.
const workflow = ".github/workflows/validate.yml"

// filled are the files whose blanks Files fills; no other file is
// changed, so a template's own {{ }} and @ are left alone.
var filled = map[string]bool{".component": true, "README.md": true, workflow: true}

// Files are starter name's files, sorted by path, with v's values filled
// in.
func Files(name string, v Values) ([]File, error) {
	if !slices.Contains(Names(), name) {
		return nil, fmt.Errorf("no starter %q (starters: %s)", name, strings.Join(Names(), ", "))
	}
	root, err := fs.Sub(embedded, name)
	if err != nil {
		return nil, err
	}
	r := strings.NewReplacer("@CODE@", v.Code, "@NAME@", v.Name, "@VERSION@", v.Version)
	var out []File
	err = fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || p == workflow && !Release(v.Version) {
			return err
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		if filled[p] {
			data = []byte(r.Replace(string(data)))
		}
		out = append(out, File{Path: p, Data: data})
		return nil
	})
	return out, err
}

// release is a published version: v1.2.3 or v1.2.3-pre, not a build of a
// commit after one (v1.2.3-4-gabcdef0) or of uncommitted changes.
var release = regexp.MustCompile(`^v\d+\.\d+\.\d+(-[0-9A-Za-z.]+)?$`)
var describe = regexp.MustCompile(`-\d+-g[0-9a-f]+(-dirty)?$|-dirty$`)

// Release reports whether version is a published release.
func Release(version string) bool {
	return release.MatchString(version) && !describe.MatchString(version)
}
