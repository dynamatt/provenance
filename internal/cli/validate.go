package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/history"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/validate"
	"github.com/dynamatt/provenance/internal/version"
)

// runValidate reports every violation in the repository and exits 1 while
// any is an error (DES-0018, DES-0049). Problems with the repository's
// files are violations; only the tool and its invocation exit 2.
func runValidate(c *cobra.Command, _ []string) error {
	name := commandName(c)
	format, _ := c.Flags().GetString("format")
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

	var buf bytes.Buffer
	if tmpl, _ := c.Flags().GetString("report"); tmpl != "" {
		if err := writeTemplate(&buf, report, tmpl); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	} else if err := report.Write(&buf, format, version.Version); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if out, _ := c.Flags().GetString("out"); out != "" {
		if err := writeReport(out, buf.Bytes()); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	} else if _, err := c.OutOrStdout().Write(buf.Bytes()); err != nil {
		return fmt.Errorf("%s: %w", name, err)
	}
	if report.Errors() > 0 {
		return exitcode.Failed(fmt.Errorf("%s: %s", name, report.Summary()))
	}
	return nil
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

// writeTemplate renders the report template name with the git stamps. A
// name with no template is a usage error; a template that fails is the
// content/template violation the report already holds, so it fails the
// run, writing nothing.
func writeTemplate(w io.Writer, report *validate.Report, name string) error {
	st, err := stamps(report.Root())
	if err != nil {
		return err
	}
	err = report.WriteTemplate(w, name, st)
	var unknown *validate.UnknownReportError
	var te *validate.TemplateError
	switch {
	case errors.As(err, &unknown):
		return exitcode.Usage(err)
	case errors.As(err, &te):
		return exitcode.Failed(fmt.Errorf("report %s: %w (%s)", name, err, report.Summary()))
	}
	return err
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
