package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spf13/pflag"

	"github.com/dynamatt/provenance/internal/cli"
)

func TestOnePagePerCommandPlusIndex(t *testing.T) {
	root := cli.NewRootCmd()
	pages := generate(root)
	cmds := commands(root)
	if len(pages) != len(cmds)+1 {
		t.Fatalf("got %d pages, want %d commands + _index.md", len(pages), len(cmds))
	}
	for _, c := range cmds {
		p, ok := pages[fileName(c)+".md"]
		if !ok {
			t.Errorf("no page for %q", name(c))
			continue
		}
		if !strings.Contains(pages["_index.md"], "| "+ref(c)+" | "+cell(c.Short)+" | "+flagNames(c)+" |") {
			t.Errorf("index row for %q missing or without its flags", name(c))
		}
		for status, marker := range map[cli.Status]string{
			cli.NotImplemented: "**Not yet implemented.**",
			cli.InDevelopment:  "**In development.**",
		} {
			marked := strings.Contains(p, marker)
			if marked != (cli.CommandStatus(c) == status) {
				t.Errorf("%s: %s marker = %v, status = %q", name(c), marker, marked, cli.CommandStatus(c))
			}
		}
		c.LocalFlags().VisitAll(func(f *pflag.Flag) {
			if f.Name != "help" && !strings.Contains(p, "`--"+f.Name) {
				t.Errorf("%s page is missing flag --%s", name(c), f.Name)
			}
		})
	}
	if !strings.Contains(pages["_index.md"], "`-v, --version`") {
		t.Error("index is missing the global --version flag")
	}
}

func TestEveryRelrefResolvesToAGeneratedPage(t *testing.T) {
	pages := generate(cli.NewRootCmd())
	re := regexp.MustCompile(`relref "([^"]+)"`)
	for file, content := range pages {
		for _, m := range re.FindAllStringSubmatch(content, -1) {
			if _, ok := pages[m[1]]; !ok {
				t.Errorf("%s links to missing page %s", file, m[1])
			}
		}
	}
}

func TestOutputIsDeterministic(t *testing.T) {
	a, b := t.TempDir(), t.TempDir()
	if err := write(a, generate(cli.NewRootCmd())); err != nil {
		t.Fatal(err)
	}
	if err := write(b, generate(cli.NewRootCmd())); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(filepath.Join(a, "*.md"))
	for _, fa := range files {
		da, _ := os.ReadFile(fa)
		db, err := os.ReadFile(filepath.Join(b, filepath.Base(fa)))
		if err != nil || string(da) != string(db) {
			t.Errorf("%s differs between runs", filepath.Base(fa))
		}
	}
}

func TestWriteRemovesPagesForDeletedCommands(t *testing.T) {
	dir := t.TempDir()
	stale := filepath.Join(dir, "removed-command.md")
	keep := filepath.Join(dir, "notes.txt")
	for _, f := range []string{stale, keep} {
		if err := os.WriteFile(f, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := write(dir, map[string]string{"_index.md": "x"}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(stale); !os.IsNotExist(err) {
		t.Error("stale generated page was not removed")
	}
	if _, err := os.Stat(keep); err != nil {
		t.Error("non-Markdown file was removed")
	}
}

func TestProseKeepsLiteralsVerbatim(t *testing.T) {
	got := prose("--out defaults to ./_site (see .provenance-export).\nSkip schema/ and a*b <x>.")
	want := "`--out` defaults to `./_site` (see `.provenance-export`). Skip `schema/` and a\\*b &lt;x&gt;.\n\n"
	if got != want {
		t.Errorf("prose()\n got %q\nwant %q", got, want)
	}
}
