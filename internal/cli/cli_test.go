package cli

import (
	"bytes"
	"errors"
	"reflect"
	"runtime"
	"sort"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
)

func run(args ...string) (code int, stdout, stderr string) {
	var out, errOut bytes.Buffer
	code = Execute(args, &out, &errOut)
	return code, out.String(), errOut.String()
}

// The command surface of Detailed Design §2, plus version.
var wantCommands = []string{
	"component",
	"component add",
	"component remove",
	"component update",
	"diff",
	"export",
	"fmt",
	"init",
	"plugin",
	"release",
	"release tag",
	"rename",
	"report",
	"serve",
	"sign",
	"sign verify",
	"validate",
	"verify",
	"verify artifact",
	"verify content",
	"version",
}

func TestEveryCommandExists(t *testing.T) {
	var got []string
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Name() == "help" {
				continue
			}
			got = append(got, commandName(sub))
			walk(sub)
		}
	}
	walk(NewRootCmd())
	sort.Strings(got)

	if strings.Join(got, "\n") != strings.Join(wantCommands, "\n") {
		t.Errorf("command tree mismatch\n got: %q\nwant: %q", got, wantCommands)
	}
}

func TestRootHelpListsEveryTopLevelCommand(t *testing.T) {
	code, out, _ := run("--help")
	if code != 0 {
		t.Fatalf("--help exit code = %d, want 0", code)
	}
	for _, c := range wantCommands {
		if strings.Contains(c, " ") {
			continue
		}
		if !strings.Contains(out, "\n  "+c+" ") {
			t.Errorf("--help does not list %q", c)
		}
	}
}

func TestExportHelpShowsArgumentsAndFlags(t *testing.T) {
	code, out, _ := run("export", "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0", code)
	}
	for _, want := range []string{"export <format>", "--scope", "--out", `(default "_site")`} {
		if !strings.Contains(out, want) {
			t.Errorf("export --help missing %q", want)
		}
	}
}

// Each stub is invoked with every flag it declares, so these cases also prove
// that the flags parse.
func TestStubsExitTwoWithNotImplemented(t *testing.T) {
	cases := []struct {
		name string
		args []string
	}{
		{"serve", []string{"serve"}},
		{"init", []string{"init", "--template", "medical", "newproj"}},
		{"validate", []string{"validate", "--path", ".", "--format", "sarif"}},
		{"component add", []string{"component", "add", "https://example.com/x.git", "--path", "SUBCOMPONENTS/x"}},
		{"component update", []string{"component", "update", "SUBCOMPONENTS/x"}},
		{"component remove", []string{"component", "remove", "SUBCOMPONENTS/x"}},
		{"rename", []string{"rename", "REQ-0001", "REQ-0100"}},
		{"fmt", []string{"fmt", "--check", "REQ/REQ-0001.md", "REQ/REQ-0002.md"}},
		{"diff", []string{"diff", "main", "HEAD", "--entity", "REQ-0001"}},
		{"report", []string{"report", "--metric", "coverage", "--scope", "DOC/DOC-0001.md", "--json"}},
		{"release tag", []string{"release", "tag", "v1.0.0"}},
		{"plugin", []string{"plugin", "importer", "--anything", "goes"}},
		{"sign", []string{"sign", "REQ-0001"}},
		{"sign verify", []string{"sign", "verify", "REQ-0001", "--commit", "abc123", "--json"}},
		{"sign verify", []string{"sign", "verify", "--all", "--scope", "DOC/DOC-0001.md", "--json"}},
		{"verify artifact", []string{"verify", "artifact", "--path", "bin/provenance"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, out, errOut := run(tc.args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if want := tc.name + ": not implemented yet\n"; errOut != want {
				t.Errorf("stderr = %q, want %q", errOut, want)
			}
			if out != "" {
				t.Errorf("stdout = %q, want empty", out)
			}
		})
	}
}

func TestUsageErrorsExitTwo(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"export"}, "export: missing argument <format>"},
		{[]string{"export", "website", "extra"}, `export: unexpected argument "extra"`},
		{[]string{"export", "website", "--bogus"}, "export: unknown flag: --bogus"},
		{[]string{"rename", "REQ-0001"}, "rename: missing argument <new-id>"},
		{[]string{"diff", "main"}, "diff: missing argument <ref-b>"},
		{[]string{"component", "add"}, "component add: missing argument <url>"},
		{[]string{"release", "tag"}, "release tag: missing argument <name>"},
		{[]string{"plugin"}, "plugin: missing argument <name>"},
		{[]string{"sign"}, "sign: missing argument <id>"},
		{[]string{"sign", "verify"}, "sign verify: missing argument <id>"},
		{[]string{"sign", "verify", "--all", "REQ-0001"}, `sign verify: unexpected argument "REQ-0001"`},
		{[]string{"sign", "verify", "--all", "--commit", "abc"}, "sign verify: --commit cannot be used with --all"},
		{[]string{"sign", "verify", "REQ-0001", "--scope", "x"}, "sign verify: --scope requires --all"},
		{[]string{"validate", "--format", "xml"}, `validate: invalid --format "xml"`},
		{[]string{"verify", "nope"}, `verify: unknown subcommand "nope"`},
		{[]string{"version", "extra"}, `version: unexpected argument "extra"`},
		{[]string{"nope"}, `unknown command "nope"`},
		{[]string{"--bogus"}, "unknown flag: --bogus"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			code, _, errOut := run(tc.args...)
			if code != 2 {
				t.Errorf("exit code = %d, want 2", code)
			}
			if !strings.Contains(errOut, tc.want) {
				t.Errorf("stderr = %q, want it to contain %q", errOut, tc.want)
			}
			if strings.Contains(errOut, "not implemented") {
				t.Errorf("usage error reached the stub: %q", errOut)
			}
		})
	}
}

func TestGroupWithoutSubcommandShowsHelp(t *testing.T) {
	for _, g := range []string{"component", "release", "verify"} {
		code, out, _ := run(g)
		if code != 0 {
			t.Errorf("%s: exit code = %d, want 0", g, code)
		}
		if !strings.Contains(out, "Available Commands:") {
			t.Errorf("%s: help not shown, got %q", g, out)
		}
	}
}

func TestVersion(t *testing.T) {
	code, out, errOut := run("version")
	if code != 0 || errOut != "" {
		t.Fatalf("version: exit %d, stderr %q", code, errOut)
	}
	for _, want := range []string{"provenance ", "commit: ", "go: " + runtime.Version()} {
		if !strings.Contains(out, want) {
			t.Errorf("version output %q missing %q", out, want)
		}
	}
	code, flagOut, _ := run("--version")
	if code != 0 {
		t.Errorf("--version exit code = %d, want 0", code)
	}
	if flagOut != out {
		t.Errorf("--version output %q differs from version output %q", flagOut, out)
	}
}

// The documentation generator trusts the not-implemented annotation, so it must
// mark exactly the commands whose RunE is the stub.
func TestNotImplementedAnnotationMatchesStubs(t *testing.T) {
	var walk func(*cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Name() == "help" {
				continue
			}
			isStub := sub.RunE != nil &&
				reflect.ValueOf(sub.RunE).Pointer() == reflect.ValueOf(notImplemented).Pointer()
			if isStub != (CommandStatus(sub) == NotImplemented) {
				t.Errorf("%s: stub = %v but status = %q", commandName(sub), isStub, CommandStatus(sub))
			}
			walk(sub)
		}
	}
	walk(NewRootCmd())
}

func TestStubReturnsNotImplemented(t *testing.T) {
	var ni *exitcode.NotImplementedError
	err := notImplemented(NewRootCmd(), nil)
	if !errors.As(err, &ni) {
		t.Errorf("notImplemented returned %v", err)
	}
}
