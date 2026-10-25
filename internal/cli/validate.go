package cli

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/tmpl"
	"github.com/dynamatt/provenance/internal/validate"
	"github.com/dynamatt/provenance/internal/version"
)

// runValidate reports every violation in the repository and exits 1 while
// any is an error (DES-0018, DES-0049). Problems with the repository's
// files are violations; only the tool and its invocation exit 2.
func runValidate(c *cobra.Command, _ []string) error {
	name := commandName(c)
	outs, err := outputs(c)
	if err != nil {
		return err
	}
	start, _ := c.Flags().GetString("path")
	if start == "" {
		cwd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		start = cwd
	}
	r, err := repo.Open(start)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	report, err := validate.Run(r)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}

	bufs, err := renderOutputs(report, outs)
	if err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	for i, o := range outs {
		if o.file == "" {
			_, err = c.OutOrStdout().Write(bufs[i])
		} else {
			err = writeReport(o.file, bufs[i])
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}
	if report.Errors() > 0 {
		return exitcode.Failed(fmt.Errorf("%s: %s", name, report.Summary()))
	}
	return nil
}

// output is one --output: a format or report template, and the file it
// is written to, "" for standard output.
type output struct{ name, file string }

// outputs parses the --output flags: name or name=file, at most one of
// them to standard output and no file twice. With none, the text report
// goes to standard output.
func outputs(c *cobra.Command) ([]output, error) {
	flags, _ := c.Flags().GetStringArray("output")
	if len(flags) == 0 {
		return []output{{name: "text"}}, nil
	}
	var outs []output
	stdout := ""
	files := map[string]string{}
	for _, f := range flags {
		name, file, hasFile := strings.Cut(f, "=")
		switch {
		case name == "":
			return nil, usagef(c, "--output %q: missing report name", f)
		case hasFile && file == "":
			return nil, usagef(c, "--output %q: missing file after =", f)
		case !hasFile && stdout != "":
			return nil, usagef(c, "--output %s and --output %s both write to standard output; give one a file with %s=<file>", stdout, name, name)
		case !hasFile:
			stdout = name
		default:
			if prev, ok := files[filepath.Clean(file)]; ok {
				return nil, usagef(c, "--output %s and --output %s both write %s", prev, name, file)
			}
			files[filepath.Clean(file)] = name
		}
		outs = append(outs, output{name, file})
	}
	return outs, nil
}

func usagef(c *cobra.Command, format string, args ...any) error {
	return exitcode.Usage(fmt.Errorf("%s: "+format, append([]any{commandName(c)}, args...)...))
}

// renderOutputs renders every output into memory, so nothing is written
// unless all of them render. A name that is neither a format nor a report
// template is a usage error, whatever the others do; a template that
// fails is the content/template violation the report already holds, so it
// fails the run.
func renderOutputs(report *validate.Report, outs []output) ([][]byte, error) {
	var st validate.Stamps
	needStamps := false
	for _, o := range outs {
		err := report.CheckOutput(o.name)
		var unknown *validate.UnknownReportError
		if errors.As(err, &unknown) {
			return nil, exitcode.Usage(err)
		} else if err != nil {
			return nil, err
		}
		needStamps = needStamps || !validate.IsFormat(o.name)
	}
	if needStamps {
		var err error
		if st, err = stamps(report.Root()); err != nil {
			return nil, err
		}
	}
	bufs := make([][]byte, len(outs))
	for i, o := range outs {
		var b bytes.Buffer
		err := report.WriteOutput(&b, o.name, version.Version, st)
		var te *tmpl.Error
		if errors.As(err, &te) {
			return nil, exitcode.Failed(fmt.Errorf("report %s: %w (%s)", o.name, err, report.Summary()))
		} else if err != nil {
			return nil, err
		}
		bufs[i] = b.Bytes()
	}
	return bufs, nil
}

// writeReport writes a report to path, creating its folders. Unlike an
// export it never empties a folder, so a report can be added to an exported
// site (DES-0049).
func writeReport(path string, data []byte) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	return os.WriteFile(path, data, 0o644)
}

// stamps are a report's git stamps: HEAD, and the content hash with
// whether the working tree differs from it. Outside a git repository there
// is nothing to stamp.
func stamps(root string) (validate.Stamps, error) {
	h, err := history.Open(root)
	if errors.Is(err, history.ErrNotRepository) {
		return validate.Stamps{}, nil
	}
	if err != nil {
		return validate.Stamps{}, err
	}
	defer h.Close()
	st := validate.Stamps{GitSHA: h.HEAD()}
	var dirty *history.Dirty
	if st.ContentHash, dirty, err = h.WorkingContentHash(); err != nil {
		return validate.Stamps{}, err
	}
	st.Dirty = dirty.Any()
	return st, nil
}
