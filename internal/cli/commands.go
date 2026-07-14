package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/JaydenCJ/muxunit/internal/algebra"
	"github.com/JaydenCJ/muxunit/internal/model"
	"github.com/JaydenCJ/muxunit/internal/render"
)

// newFlagSet builds a FlagSet that reports usage errors on stderr and never
// calls os.Exit, so Run stays testable.
func newFlagSet(name string, stderr io.Writer) *flag.FlagSet {
	fs := flag.NewFlagSet("muxunit "+name, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return fs
}

// parseFlags parses args and maps the outcome to an exit code. An explicit
// -h/--help is a successful run (exit 0), not a usage error.
func parseFlags(fs *flag.FlagSet, args []string) (code int, done bool) {
	switch err := fs.Parse(args); {
	case err == nil:
		return ExitOK, false
	case errors.Is(err, flag.ErrHelp):
		return ExitOK, true
	default:
		return ExitUsage, true
	}
}

// usageErr prints the message and returns the usage exit code.
func usageErr(stderr io.Writer, format string, args ...any) int {
	fmt.Fprintf(stderr, "muxunit: "+format+"\n", args...)
	return ExitUsage
}

// runtimeErr prints the error and returns the runtime exit code.
func runtimeErr(stderr io.Writer, err error) int {
	fmt.Fprintf(stderr, "muxunit: %v\n", err)
	return ExitRuntime
}

// ioFlags is the flag trio shared by every report-producing command.
type ioFlags struct {
	from string
	to   string
	out  string
}

func (f *ioFlags) register(fs *flag.FlagSet) {
	fs.StringVar(&f.from, "from", "auto", "input format: auto, junit, tap")
	fs.StringVar(&f.to, "to", "junit", "output format: junit, tap, json")
	fs.StringVar(&f.out, "o", "", "write output to this file instead of stdout")
}

func (f *ioFlags) formats() (Format, Format, error) {
	from, err := parseFormat(f.from, false)
	if err != nil {
		return FormatAuto, FormatAuto, fmt.Errorf("--from: %w", err)
	}
	to, err := parseFormat(f.to, true)
	if err != nil {
		return FormatAuto, FormatAuto, fmt.Errorf("--to: %w", err)
	}
	if to == FormatAuto {
		to = FormatJUnit
	}
	return from, to, nil
}

func cmdMerge(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("merge", stderr)
	var iof ioFlags
	iof.register(fs)
	dedupe := fs.String("dedupe", "all", "duplicate policy: all, first, last, prefer-pass, prefer-fail")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, "merge: need at least one report file")
	}
	from, to, err := iof.formats()
	if err != nil {
		return usageErr(stderr, "merge: %v", err)
	}
	policy, err := algebra.ParseDedupePolicy(*dedupe)
	if err != nil {
		return usageErr(stderr, "merge: --dedupe: %v", err)
	}
	reps, err := readReports(fs.Args(), from, os.Stdin)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	merged := algebra.Merge(reps, policy)
	if err := writeReport(merged, to, iof.out, stdout); err != nil {
		return runtimeErr(stderr, err)
	}
	return ExitOK
}

func cmdDiff(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("diff", stderr)
	from := fs.String("from", "auto", "input format: auto, junit, tap")
	format := fs.String("format", "text", "output format: text, json")
	failOn := fs.String("fail-on", "regressions", "exit 1 on: regressions, any-change, nothing")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() != 2 {
		return usageErr(stderr, "diff: need exactly two reports: <old> <new>")
	}
	inFmt, err := parseFormat(*from, false)
	if err != nil {
		return usageErr(stderr, "diff: --from: %v", err)
	}
	switch *failOn {
	case "regressions", "any-change", "nothing":
	default:
		return usageErr(stderr, "diff: --fail-on: unknown mode %q", *failOn)
	}
	reps, err := readReports(fs.Args(), inFmt, os.Stdin)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	d := algebra.Diff(reps[0], reps[1])
	switch *format {
	case "text":
		err = render.WriteDiffText(stdout, d)
	case "json":
		err = render.WriteDiffJSON(stdout, d)
	default:
		return usageErr(stderr, "diff: --format: unknown format %q", *format)
	}
	if err != nil {
		return runtimeErr(stderr, err)
	}
	breached := (*failOn == "regressions" && d.HasRegressions()) ||
		(*failOn == "any-change" && d.Total() > 0)
	if breached {
		return ExitGate
	}
	return ExitOK
}

func cmdFilter(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("filter", stderr)
	var iof ioFlags
	iof.register(fs)
	var matches stringList
	fs.Var(&matches, "match", "keep cases whose suite/class/name ID matches this glob (repeatable)")
	status := fs.String("status", "", "keep only these statuses, comma-separated: pass,fail,error,skip")
	onlyFailed := fs.Bool("only-failed", false, "shorthand for --status fail,error")
	invert := fs.Bool("invert", false, "keep exactly the cases the selection would drop")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, "filter: need at least one report file")
	}
	from, to, err := iof.formats()
	if err != nil {
		return usageErr(stderr, "filter: %v", err)
	}
	opt := algebra.FilterOptions{Invert: *invert}
	if *onlyFailed {
		if *status != "" {
			return usageErr(stderr, "filter: --only-failed and --status are mutually exclusive")
		}
		*status = "fail,error"
	}
	if *status != "" {
		opt.Statuses = map[model.Status]bool{}
		for _, tok := range strings.Split(*status, ",") {
			st, err := model.ParseStatus(tok)
			if err != nil {
				return usageErr(stderr, "filter: --status: %v", err)
			}
			opt.Statuses[st] = true
		}
	}
	for _, pat := range matches {
		g, err := algebra.CompileGlob(pat)
		if err != nil {
			return usageErr(stderr, "filter: --match: %v", err)
		}
		opt.Patterns = append(opt.Patterns, g)
	}
	reps, err := readReports(fs.Args(), from, os.Stdin)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	filtered := algebra.Filter(algebra.Merge(reps, algebra.KeepAll), opt)
	if err := writeReport(filtered, to, iof.out, stdout); err != nil {
		return runtimeErr(stderr, err)
	}
	return ExitOK
}

func cmdRewrite(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("rewrite", stderr)
	var iof ioFlags
	iof.register(fs)
	var renames, subs stringList
	fs.Var(&renames, "rename-suite", "rename a suite: old=new (repeatable)")
	fs.Var(&subs, "sub", "sed-style case-name rewrite /pattern/replacement/ (repeatable)")
	trimPrefix := fs.String("trim-prefix", "", "remove this prefix from every suite name")
	addPrefix := fs.String("add-prefix", "", "prepend this to every suite name")
	setClass := fs.String("set-class", "", "overwrite every case's classname")
	clearClass := fs.Bool("clear-class", false, "blank every case's classname")
	stripTimes := fs.Bool("strip-times", false, "zero all durations for reproducible output")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, "rewrite: need at least one report file")
	}
	from, to, err := iof.formats()
	if err != nil {
		return usageErr(stderr, "rewrite: %v", err)
	}
	opt := algebra.RewriteOptions{
		TrimPrefix: *trimPrefix,
		AddPrefix:  *addPrefix,
		SetClass:   *setClass,
		ClearClass: *clearClass,
		StripTimes: *stripTimes,
	}
	if len(renames) > 0 {
		opt.RenameSuite = map[string]string{}
		for _, r := range renames {
			old, updated, ok := strings.Cut(r, "=")
			if !ok || old == "" {
				return usageErr(stderr, "rewrite: --rename-suite: want old=new, got %q", r)
			}
			opt.RenameSuite[old] = updated
		}
	}
	for _, expr := range subs {
		sub, err := algebra.ParseSubstitution(expr)
		if err != nil {
			return usageErr(stderr, "rewrite: --sub: %v", err)
		}
		opt.Subs = append(opt.Subs, sub)
	}
	reps, err := readReports(fs.Args(), from, os.Stdin)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	rewritten := algebra.Rewrite(algebra.Merge(reps, algebra.KeepAll), opt)
	// Renames can make suite names collide; merge again to unify them.
	rewritten = algebra.Merge([]*model.Report{rewritten}, algebra.KeepAll)
	if err := writeReport(rewritten, to, iof.out, stdout); err != nil {
		return runtimeErr(stderr, err)
	}
	return ExitOK
}

func cmdSummary(args []string, stdout, stderr io.Writer) int {
	fs := newFlagSet("summary", stderr)
	from := fs.String("from", "auto", "input format: auto, junit, tap")
	format := fs.String("format", "text", "output format: text, json")
	check := fs.Bool("check", false, "exit 1 when any test failed or errored")
	if code, done := parseFlags(fs, args); done {
		return code
	}
	if fs.NArg() == 0 {
		return usageErr(stderr, "summary: need at least one report file")
	}
	inFmt, err := parseFormat(*from, false)
	if err != nil {
		return usageErr(stderr, "summary: --from: %v", err)
	}
	reps, err := readReports(fs.Args(), inFmt, os.Stdin)
	if err != nil {
		return runtimeErr(stderr, err)
	}
	rep := algebra.Merge(reps, algebra.KeepAll)
	switch *format {
	case "text":
		err = render.WriteSummaryText(stdout, rep)
	case "json":
		err = render.WriteSummaryJSON(stdout, rep)
	default:
		return usageErr(stderr, "summary: --format: unknown format %q", *format)
	}
	if err != nil {
		return runtimeErr(stderr, err)
	}
	counts := rep.Counts()
	if *check && counts.Failed+counts.Errored > 0 {
		return ExitGate
	}
	return ExitOK
}
