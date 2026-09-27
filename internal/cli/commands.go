package cli

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/version"
)

// Command surface from Detailed Design §2. Help text is written for users of
// the tool; design-doc section references stay out of it.

func newServeCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "serve",
		Short: "Start the local browser editor, rule-violation viewer and reports viewer",
		Long: `Start the local web server hosting the browser editor, rule-violation viewer and
reports/coverage viewer.

The server binds to localhost only and has no authentication. It is a single-user
editing surface and must not be exposed beyond the local machine.`,
		Args:        noArgs,
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
}

func newInitCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "init [path]",
		Short: "Scaffold a new project from a starter template",
		Long: `Scaffold a new project in path (default: the current directory): the folder structure
and starter files — schema/, rules/, templates/, entity-type folders, .component and
plugins.lock — from the default or a named starter template.`,
		Args:        maxArgs(1),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.Flags().String("template", "", "starter template `name` (default: the built-in default template)")
	return c
}

func newValidateCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "validate",
		Short: "Run the rule engine and report every violation",
		Long: `Run the rule engine against the current (or specified) graph state and print every
violation, errors and warnings. Used locally, at the merge gate and at release-tag time.

--format selects the report: text (default), json for tooling, or junit/sarif so a git
host can show violations natively. The exit code is the only guaranteed CI contract;
the report formats are a convenience.

Exit codes: 0 no error-severity violations, 1 violations found, 2 tool or usage error.`,
		Args: noArgs,
		PreRunE: func(c *cobra.Command, _ []string) error {
			return oneOf(c, "format", "text", "json", "junit", "sarif")
		},
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.Flags().String("path", "", "repository `path` to validate (default: the repository containing the current directory)")
	c.Flags().String("format", "text", "report `format`: text, json, junit or sarif")
	return c
}

func newComponentCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "component",
		Short: "Add, update or remove component repositories (git submodules)",
		Long: `Manage component repositories composed into this one as git submodules under
SUBCOMPONENTS/. Each operation produces an ordinary, reviewable commit.`,
		RunE: group,
	}
	add := &cobra.Command{
		Use:   "add <url>",
		Short: "Register a component repository as a submodule",
		Long: `Register the component repository at url as a git submodule under SUBCOMPONENTS/,
checking its declared component code against the full transitive set of component
codes already composed into this repository.`,
		Args:        exactArgs("url"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	add.Flags().String("path", "", "submodule `dir` under SUBCOMPONENTS/")
	update := &cobra.Command{
		Use:         "update <path>",
		Short:       "Move a pinned component to a different commit",
		Long:        `Update which commit the component submodule at path is pinned to.`,
		Args:        exactArgs("path"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	remove := &cobra.Command{
		Use:         "remove <path>",
		Short:       "Remove a component submodule",
		Long:        `Remove the component submodule reference at path.`,
		Args:        exactArgs("path"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.AddCommand(add, update, remove)
	return c
}

func newRenameCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "rename <old-id> <new-id>",
		Short: "Rename an entity ID and rewrite every reference to it",
		Long: `Rewrite an entity's ID and every structured reference to it — links, inline
cross-references and query-block filters — across the repository, in one commit.

Unavailable once the ID is frozen by appearing in a signed release tag.`,
		Args:        exactArgs("old-id", "new-id"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
}

func newFmtCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "fmt [path...]",
		Short: "Reformat entity files to canonical serialization",
		Long: `Reformat entity files (default: every entity in the repository) to canonical
serialization: stable frontmatter key order, one field per line, stable list-row and
block layout. Clean git merges depend on every file being in this form.

--check verifies without writing; it runs at the merge gate alongside validate.`,
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.Flags().Bool("check", false, "report files that are not canonical instead of rewriting them")
	return c
}

func newDiffCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "diff <ref-a> <ref-b>",
		Short: "Show a rendered, semantic diff between two git refs",
		Long: `Produce a rendered/semantic diff between two git refs, for the whole repository or
one entity.`,
		Args:        exactArgs("ref-a", "ref-b"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.Flags().String("entity", "", "limit the diff to the entity with this `id`")
	return c
}

func newExportCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "export <format>",
		Short: "Export the design history file (website, pdf, docx)",
		Long: `Run the exporter for format: website, pdf, docx, or a project-added exporter.

--scope takes a path to a standalone query file (a YAML from/where document) or to an
entity file. Pointed at a Document entity, export renders that Document through its own
template rather than the generic per-type rendering of its scoped entities.

--out defaults to ./_site. A non-empty output folder is only cleared if it contains a
.provenance-export marker written by a previous export; otherwise export refuses, so it
can never delete a folder it did not create.

Exit codes: 0 output written, 2 anything else. Export never returns 1 — judging content
is validate's job.`,
		Args:        exactArgs("format"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.Flags().String("scope", "", "`path` to a query file or entity file that limits what is exported")
	c.Flags().String("out", "_site", "output folder `path`")
	return c
}

func newReportCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "report",
		Short: "Generate a coverage or progress report",
		Long: `Generate a coverage/progress report from the current branch HEAD.

--scope takes a path to a standalone query file or an entity file and resolves it to an
entity set; pointed at a Document, the Document's scoped entities become the reporting
population.`,
		Args:        noArgs,
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.Flags().String("metric", "", "report only the metric with this `name`")
	c.Flags().String("scope", "", "`path` to a query file or entity file that limits the population")
	c.Flags().Bool("json", false, "emit machine-readable JSON")
	return c
}

func newReleaseCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "release",
		Short: "Run the release gate and create release tags",
		RunE:  group,
	}
	tag := &cobra.Command{
		Use:   "tag <name>",
		Short: "Run the release-tag gate and create the tag",
		Long: `Run the release-tag gate and, on pass, create the tag name capturing the full
transitive bill of materials.

The gate blocks on any error-severity violation. For a system composed of components, it
also requires every pinned component to point to a tagged release.

Exit codes: 0 tag created, 1 gate blocked, 2 tool or usage error.`,
		Args:        exactArgs("name"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	c.AddCommand(tag)
	return c
}

func newPluginCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "plugin <name> [args...]",
		Short: "Invoke an installed plugin",
		Long: `Invoke an installed rendering, exporter or import plugin. Arguments after name are
passed to the plugin unchanged.`,
		Args: func(c *cobra.Command, args []string) error {
			if len(args) == 0 {
				return exitcode.Usage(fmt.Errorf("%s: missing argument <name>", commandName(c)))
			}
			return nil
		},
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	// Flags after the plugin name belong to the plugin.
	c.Flags().SetInterspersed(false)
	return c
}

func newSignCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "sign <id>",
		Short: "Record an electronic signature against an entity or change request",
		Long: `Record an electronic signature — identity, timestamp, meaning and re-authentication —
against the current state of the entity or change request/ECO identified by id.

Prompts for the configured signing provider (OIDC/SSO device-flow login, or a local
platform certificate signature) and writes the resulting cryptographic proof to the
signature ledger in .signatures/.`,
		Args:        exactArgs("id"),
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	verify := &cobra.Command{
		Use:   "verify (<id> | --all)",
		Short: "Check signature records against the content they sign",
		Long: `Recompute the content fingerprint of the entity or change request/ECO identified by id
and check it against the fingerprint its stored signature records are bound to. Reports a
match, content changed since signing, or a corrupt/invalid record, with signer, timestamp,
meaning and provider.

--all sweeps every record in the ledger, optionally narrowed by --scope (a path to a query
file or entity file). Runs entirely offline: no git history, identity provider or
certificate authority needs to be reachable.

Exit codes: 0 all signatures match, 1 mismatch or invalid record, 2 tool or usage error.`,
		Args: func(c *cobra.Command, args []string) error {
			all, _ := c.Flags().GetBool("all")
			if all {
				if c.Flags().Changed("commit") {
					return exitcode.Usage(fmt.Errorf("%s: --commit cannot be used with --all", commandName(c)))
				}
				return noArgs(c, args)
			}
			if c.Flags().Changed("scope") {
				return exitcode.Usage(fmt.Errorf("%s: --scope requires --all", commandName(c)))
			}
			return exactArgs("id")(c, args)
		},
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	verify.Flags().String("commit", "", "verify against the content at this commit `hash` (default: the working tree)")
	verify.Flags().Bool("all", false, "verify every record in the signature ledger")
	verify.Flags().String("scope", "", "with --all, `path` to a query file or entity file that limits the sweep")
	verify.Flags().Bool("json", false, "emit machine-readable JSON")
	c.AddCommand(verify)
	return c
}

func newVerifyCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "verify",
		Short: "Verify binary artifacts and repository content hashes",
		RunE:  group,
	}
	artifact := &cobra.Command{
		Use:   "artifact",
		Short: "Check a binary against the published release manifest",
		Long: `Hash a binary's raw bytes — this binary if --path is not given, or any file, such as an
independently rebuilt candidate — and check it against the published release manifest.
The file being checked is only read, never executed.

Exit codes: 0 hash matches, 1 mismatch, 2 tool or usage error.`,
		Args:        noArgs,
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	artifact.Flags().String("path", "", "`file` to check instead of the running binary")
	content := &cobra.Command{
		Use:   "content",
		Short: "Compute the whole-repository content hash",
		Long: `Compute the whole-repository design history file content hash at HEAD or at a given
commit. The value is suffixed -dirty if the working tree differs from HEAD.

With --expected, compare against a known value and exit 1 on mismatch instead of only
printing it.

Exit codes: 0 printed or matched, 1 mismatch against --expected, 2 tool or usage error.`,
		Args:        noArgs,
		Annotations: notImplementedYet,
		RunE:        notImplemented,
	}
	content.Flags().String("commit", "", "compute the hash at this commit `hash` (default: HEAD)")
	content.Flags().String("expected", "", "expected content `hash`; exit 1 if it differs")
	c.AddCommand(artifact, content)
	return c
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the tool version, source commit and Go toolchain",
		Long: `Print the tool version, source commit and Go toolchain. Include this in bug reports.
Also available as --version.`,
		Args: noArgs,
		RunE: func(c *cobra.Command, _ []string) error {
			_, err := fmt.Fprint(c.OutOrStdout(), version.String())
			return err
		},
	}
}
