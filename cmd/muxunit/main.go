// Command muxunit merges, diffs, filters, and rewrites JUnit and TAP
// reports across CI shards. All logic lives in internal packages; this
// file only adapts the process boundary.
package main

import (
	"os"

	"github.com/JaydenCJ/muxunit/internal/cli"
)

func main() {
	os.Exit(cli.Run(os.Args[1:], os.Stdout, os.Stderr))
}
