// Command provenance is the single distributed binary: CLI, merge gate,
// exporter and local editor server.
package main

import (
	"os"

	"github.com/dynamatt/provenance/internal/cli"
)

func main() {
	os.Exit(cli.Execute(os.Args[1:], os.Stdout, os.Stderr))
}
