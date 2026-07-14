// Package cli wires the muxunit verbs to flags, stdio, and exit codes.
// Run is pure with respect to its writers, so integration tests drive the
// full binary behavior in-process.
package cli

import (
	"fmt"
	"io"

	"github.com/JaydenCJ/muxunit/internal/version"
)

// Exit codes, stable across releases: gates key off 1, wrappers off 2/3.
const (
	ExitOK      = 0 // success, and no gate breached
	ExitGate    = 1 // diff found regressions / summary --check found red tests
	ExitUsage   = 2 // bad flags or arguments
	ExitRuntime = 3 // unreadable input, unparsable report, write failure
)

const usageText = `muxunit — merge, diff, filter, and rewrite JUnit and TAP reports

Usage:
  muxunit merge   [flags] <report>...        combine shards into one report
  muxunit diff    [flags] <old> <new>        compare two reports, gate on regressions
  muxunit filter  [flags] <report>...        keep only matching cases
  muxunit rewrite [flags] <report>...        rename suites/cases, scrub times
  muxunit summary [flags] <report>...        counts per suite, optional gate
  muxunit version                            print the version

Inputs may be JUnit XML or TAP (auto-detected; force with --from).
Use "-" to read a single report from stdin.
Run "muxunit <command> -h" for the command's flags.

Exit codes: 0 ok, 1 gate breached, 2 usage error, 3 runtime error.
`

// Run executes one muxunit invocation and returns its exit code.
func Run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usageText)
		return ExitUsage
	}
	switch args[0] {
	case "merge":
		return cmdMerge(args[1:], stdout, stderr)
	case "diff":
		return cmdDiff(args[1:], stdout, stderr)
	case "filter":
		return cmdFilter(args[1:], stdout, stderr)
	case "rewrite":
		return cmdRewrite(args[1:], stdout, stderr)
	case "summary":
		return cmdSummary(args[1:], stdout, stderr)
	case "version", "--version", "-v":
		fmt.Fprintf(stdout, "muxunit %s\n", version.Version)
		return ExitOK
	case "help", "--help", "-h":
		fmt.Fprint(stdout, usageText)
		return ExitOK
	default:
		fmt.Fprintf(stderr, "muxunit: unknown command %q\n\n", args[0])
		fmt.Fprint(stderr, usageText)
		return ExitUsage
	}
}

// stringList is a repeatable string flag.
type stringList []string

func (l *stringList) String() string { return fmt.Sprint([]string(*l)) }

func (l *stringList) Set(v string) error {
	*l = append(*l, v)
	return nil
}
