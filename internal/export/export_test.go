package export

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type stubExporter struct{}

func (stubExporter) Export(*Input) (Files, error) { return nil, nil }

func TestRegistryLookup(t *testing.T) {
	r := NewRegistry()
	r.Register("website", stubExporter{})
	r.Register("alpha", stubExporter{})
	r.Defer("pdf")

	if _, err := r.Lookup("website"); err != nil {
		t.Errorf("website: %v", err)
	}
	if _, err := r.Lookup("pdf"); err == nil || err.Error() != "pdf is not available in this release" {
		t.Errorf("pdf: err = %v", err)
	}
	_, err := r.Lookup("nope")
	var unknown *UnknownFormatError
	if !errors.As(err, &unknown) || err.Error() != `unknown format "nope" (available: alpha, website)` {
		t.Errorf("nope: err = %v", err)
	}
}

func readTree(t *testing.T, dir string) map[string]string {
	t.Helper()
	got := map[string]string{}
	err := filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(dir, p)
		b, err := os.ReadFile(p)
		got[filepath.ToSlash(rel)] = string(b)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}

var files = Files{"index.html": []byte("<p>hi</p>"), "entities/REQ-0001.html": []byte("req")}

func TestWriteOutputCreatesFolder(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "a", "_site")
	if err := WriteOutput(dir, "_site", files); err != nil {
		t.Fatal(err)
	}
	got := readTree(t, dir)
	if len(got) != 3 || got["index.html"] != "<p>hi</p>" || got["entities/REQ-0001.html"] != "req" || got[MarkerFile] == "" {
		t.Errorf("tree = %v", got)
	}
}

func TestWriteOutputUsesEmptyFolder(t *testing.T) {
	dir := t.TempDir()
	if err := WriteOutput(dir, "out", files); err != nil {
		t.Fatal(err)
	}
}

func TestWriteOutputReplacesPreviousExport(t *testing.T) {
	dir := t.TempDir()
	if err := WriteOutput(dir, "out", Files{"stale.html": []byte("old"), "old/deep.html": []byte("old")}); err != nil {
		t.Fatal(err)
	}
	if err := WriteOutput(dir, "out", files); err != nil {
		t.Fatal(err)
	}
	got := readTree(t, dir)
	if _, ok := got["stale.html"]; ok || len(got) != 3 {
		t.Errorf("previous export not cleared: %v", got)
	}
}

func TestWriteOutputRefusesForeignFolder(t *testing.T) {
	dir := t.TempDir()
	mine := filepath.Join(dir, "precious.txt")
	if err := os.WriteFile(mine, []byte("keep me"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := WriteOutput(dir, "notmine", files)
	var notOurs *NotOursError
	if !errors.As(err, &notOurs) || !strings.Contains(err.Error(), "notmine") {
		t.Fatalf("err = %v, want NotOursError naming notmine", err)
	}
	if b, _ := os.ReadFile(mine); string(b) != "keep me" {
		t.Error("user file was modified")
	}
	if len(readTree(t, dir)) != 1 {
		t.Error("files were written into a refused folder")
	}
}

func TestWriteOutputRefusesFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "site")
	if err := os.WriteFile(p, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := WriteOutput(p, "site", files); err == nil || !strings.Contains(err.Error(), "not a folder") {
		t.Errorf("err = %v", err)
	}
}

func TestWriteOutputRejectsEscapingPaths(t *testing.T) {
	for _, bad := range []string{"../x.html", "/abs.html", "a/../../x", "", MarkerFile, "a//b"} {
		if err := WriteOutput(t.TempDir(), "out", Files{bad: nil}); err == nil {
			t.Errorf("path %q accepted", bad)
		}
	}
}
