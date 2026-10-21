package cli

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/dynamatt/provenance/internal/exitcode"
	"github.com/dynamatt/provenance/internal/repo"
	"github.com/dynamatt/provenance/internal/validate"
)

// runValidate reports every violation in the repository and exits 1 while
// any is an error (DES-0018, DES-0049). Problems with the repository's
// files are violations; only the tool and its invocation exit 2.
func runValidate(c *cobra.Command, _ []string) error {
	name := commandName(c)
	if format, _ := c.Flags().GetString("format"); format != "text" {
		return &exitcode.NotImplementedError{Command: name + " --format " + format}
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

	var buf bytes.Buffer
	if err := report.WriteText(&buf); err != nil {
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
