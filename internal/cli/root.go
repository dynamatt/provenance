// Package cli defines the provenance command tree. Every command in Detailed
// Design §2 is registered here with its real arguments, flags and help text;
// commands without an implementation return exitcode.NotImplementedError.
package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/version"
)

// Execute runs the CLI with args (excluding the binary name) and returns the
// process exit code.
func Execute(args []string, stdout, stderr io.Writer) int {
	root := NewRootCmd()
	root.SetArgs(args)
	root.SetOut(stdout)
	root.SetErr(stderr)

	cmd, err := root.ExecuteC()
	if err != nil {
		fmt.Fprintln(stderr, err)
		if exitcode.IsUsage(err) {
			fmt.Fprintf(stderr, "Run '%s --help' for usage.\n", cmd.CommandPath())
		}
	}
	return exitcode.Of(err)
}

// NewRootCmd builds the full command tree. It is exported so tools such as the
// documentation generator can walk the same definitions the binary uses.
func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "provenance",
		Short: "Git-native design control: validate, export and sign a design history file",
		Long: `Provenance manages a medical device design history file stored as plain text in a git
repository: typed entities (requirements, design, risk, verification), the links between
them, the rules they must satisfy, and the documents rendered from them.

The same binary runs locally and as the CI merge gate. Exit codes are the CI contract:
  0  success
  1  expected failure reported (violations found, hash or signature mismatch, gate blocked)
  2  tool or usage error (bad arguments, missing file, unreadable repository)`,
		Version:       version.Version,
		SilenceErrors: true,
		SilenceUsage:  true,
		// Keep the command list to the product surface; shell completion can be
		// added later as a deliberate decision.
		CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
	}
	// version.String contains no template actions, so it is used verbatim.
	root.SetVersionTemplate(version.String())
	root.Flags().BoolP("version", "v", false, "print the tool version, source commit and Go toolchain")
	root.SetFlagErrorFunc(func(c *cobra.Command, err error) error {
		return exitcode.Usage(fmt.Errorf("%s: %w", commandName(c), err))
	})

	root.AddCommand(
		newServeCmd(),
		newInitCmd(),
		newValidateCmd(),
		newComponentCmd(),
		newRenameCmd(),
		newFmtCmd(),
		newDiffCmd(),
		newExportCmd(),
		newReportCmd(),
		newReleaseCmd(),
		newPluginCmd(),
		newSignCmd(),
		newVerifyCmd(),
		newVersionCmd(),
	)
	return root
}

// commandName is the command path without the binary name, e.g. "sign verify".
func commandName(c *cobra.Command) string {
	path := c.CommandPath()
	if i := strings.IndexByte(path, ' '); i >= 0 {
		return path[i+1:]
	}
	return path
}

// statusAnnotation marks commands whose implementation has not landed yet.
// The documentation generator reads it, so the CLI reference flags exactly the
// commands that still return "not implemented". Remove the annotation together
// with the notImplemented RunE when a command is built.
const statusAnnotation = "provenance/status"

var notImplementedYet = map[string]string{statusAnnotation: "not-implemented"}

// Implemented reports whether c has a real implementation in this build.
func Implemented(c *cobra.Command) bool {
	return c.Annotations[statusAnnotation] != "not-implemented"
}

// notImplemented is the RunE for commands that exist in the CLI surface but
// have no implementation in this build.
func notImplemented(c *cobra.Command, _ []string) error {
	return &exitcode.NotImplementedError{Command: commandName(c)}
}

// group is the RunE for commands that only hold subcommands: bare invocation
// shows help, anything else is an unknown subcommand.
func group(c *cobra.Command, args []string) error {
	if len(args) == 0 {
		return c.Help()
	}
	return exitcode.Usage(fmt.Errorf("%s: unknown subcommand %q", commandName(c), args[0]))
}

// exactArgs requires exactly the named positional arguments and reports the
// first missing one by name.
func exactArgs(names ...string) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if len(args) < len(names) {
			return exitcode.Usage(fmt.Errorf("%s: missing argument <%s>", commandName(c), names[len(args)]))
		}
		if len(args) > len(names) {
			return exitcode.Usage(fmt.Errorf("%s: unexpected argument %q", commandName(c), args[len(names)]))
		}
		return nil
	}
}

// maxArgs allows up to n optional positional arguments.
func maxArgs(n int) cobra.PositionalArgs {
	return func(c *cobra.Command, args []string) error {
		if len(args) > n {
			return exitcode.Usage(fmt.Errorf("%s: unexpected argument %q", commandName(c), args[n]))
		}
		return nil
	}
}

// oneOf validates that a string flag holds one of the allowed values.
func oneOf(c *cobra.Command, flag string, allowed ...string) error {
	v, err := c.Flags().GetString(flag)
	if err != nil {
		return err
	}
	for _, a := range allowed {
		if v == a {
			return nil
		}
	}
	return exitcode.Usage(fmt.Errorf("%s: invalid --%s %q (want one of: %s)",
		commandName(c), flag, v, strings.Join(allowed, ", ")))
}

func noArgs(c *cobra.Command, args []string) error { return maxArgs(0)(c, args) }
