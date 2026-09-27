package repo

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestOpenFromNestedDirectory(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, ComponentFile), "# comment\ncode: NEURO\nname: NeuroPulse\n")
	nested := filepath.Join(root, "REQ", "deeper")
	if err := os.MkdirAll(nested, 0o755); err != nil {
		t.Fatal(err)
	}

	r, err := Open(nested)
	if err != nil {
		t.Fatal(err)
	}
	want, _ := filepath.EvalSymlinks(root)
	got, _ := filepath.EvalSymlinks(r.Root)
	if got != want {
		t.Errorf("Root = %q, want %q", r.Root, root)
	}
	if r.Component != (Component{Code: "NEURO", Name: "NeuroPulse"}) {
		t.Errorf("Component = %+v", r.Component)
	}
}

func TestNotFound(t *testing.T) {
	_, err := Find(t.TempDir())
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestDirectoryNamedComponentIsIgnored(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, ComponentFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Find(dir); !errors.Is(err, ErrNotFound) {
		t.Fatalf("err = %v, want ErrNotFound", err)
	}
}

func TestInvalidComponent(t *testing.T) {
	cases := map[string]string{
		"code: NEURO\n":      ".component: missing name",
		"name: NeuroPulse\n": ".component: missing code",
		"code: [unclosed\n":  ".component: yaml:",
		"- just\n- a list\n": ".component: yaml:",
	}
	for content, want := range cases {
		root := t.TempDir()
		writeFile(t, filepath.Join(root, ComponentFile), content)
		_, err := Open(root)
		if err == nil || !strings.HasPrefix(err.Error(), want) {
			t.Errorf("content %q: err = %v, want prefix %q", content, err, want)
		}
	}
}
