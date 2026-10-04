// Package assets resolves the image files entity Markdown refers to
// (Detailed Design §7). Images are repository files, copied into an export:
// the website must work with no network access (Requirements Spec §7), so
// a URL is not allowed.
package assets

import (
	"bytes"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Resolve maps an image destination written in the Markdown of the entity
// file at entityPath (slash-separated, relative to the repository root) to
// the image's repository path. A relative destination is relative to the
// entity file, as in any Markdown viewer; one starting with / is relative
// to the repository root. inline is true for a data: URI, which needs no
// file and is kept as written. Which formats are supported is up to each
// exporter.
func Resolve(entityPath, dest string) (rel string, inline bool, err error) {
	if strings.HasPrefix(dest, "data:") {
		return "", true, nil
	}
	if u, err := url.Parse(dest); err == nil && (u.Scheme != "" || u.Host != "") {
		return "", false, fmt.Errorf("image %s: images must be files in the repository, not URLs, so the site works offline", dest)
	}
	clean, err := url.PathUnescape(dest)
	if err != nil {
		return "", false, fmt.Errorf("image %s: %w", dest, err)
	}
	if before, _, ok := strings.Cut(clean, "#"); ok {
		clean = before
	}
	if strings.HasPrefix(clean, "/") {
		rel = path.Clean(strings.TrimPrefix(clean, "/"))
	} else {
		rel = path.Clean(path.Join(path.Dir(entityPath), clean))
	}
	if rel == ".." || strings.HasPrefix(rel, "../") {
		return "", false, fmt.Errorf("image %s is outside the repository", dest)
	}
	return rel, false, nil
}

// Read reads a resolved image from the repository at root. SVG is text:
// its line endings are normalized to LF, as entity bodies and templates
// are, so a Windows core.autocrlf checkout exports the same bytes.
// Binary formats are read as they are.
func Read(root, rel string) ([]byte, error) {
	b, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(rel)))
	if errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("image %s does not exist", rel)
	}
	if err == nil && strings.EqualFold(path.Ext(rel), ".svg") {
		b = bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n"))
	}
	return b, err
}

// URL is the relative URL of an image copied to the site at its repository
// path, from a page whose path to the site root is rootRel ("" or "../").
func URL(rootRel, rel string) string {
	parts := strings.Split(rel, "/")
	for i, p := range parts {
		parts[i] = url.PathEscape(p)
	}
	return rootRel + strings.Join(parts, "/")
}
