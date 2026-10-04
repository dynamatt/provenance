package assets

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	for dest, want := range map[string]string{
		"../assets/loop.svg":   "assets/loop.svg",
		"loop.PNG":             "DOC/loop.PNG",
		"/assets/a%20b.jpg":    "assets/a b.jpg",
		"./img/x.jpeg#section": "DOC/img/x.jpeg",
	} {
		got, inline, err := Resolve("DOC/DOC-1.md", dest)
		if err != nil || inline || got != want {
			t.Errorf("%s: %q, %v, %v; want %q", dest, got, inline, err, want)
		}
	}
	if _, inline, err := Resolve("DOC/DOC-1.md", "data:image/png;base64,AAAA"); !inline || err != nil {
		t.Errorf("data: URI not kept inline: %v %v", inline, err)
	}
	for dest, want := range map[string]string{
		"https://example.com/x.png": "images must be files in the repository",
		"//cdn.example.com/x.png":   "images must be files in the repository",
		"../../x.png":               "is outside the repository",
		"../assets/notes.pdf":       "not a supported image type",
	} {
		if _, _, err := Resolve("DOC/DOC-1.md", dest); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err %v, want %q", dest, err, want)
		}
	}
}

func TestReadAndURL(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "assets"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "assets", "a b.png"), []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
	if b, err := Read(root, "assets/a b.png"); err != nil || string(b) != "png" {
		t.Errorf("Read: %q %v", b, err)
	}
	if _, err := Read(root, "assets/missing.png"); err == nil || err.Error() != "image assets/missing.png does not exist" {
		t.Errorf("missing: %v", err)
	}
	if got := URL("../", "assets/a b.png"); got != "../assets/a%20b.png" {
		t.Errorf("URL = %s", got)
	}
}
