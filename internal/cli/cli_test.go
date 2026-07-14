// In-process integration tests: every subcommand end to end through
// cli.Run, with real files in temp dirs and assertions on real output and
// exit codes. No network, no external binaries.
package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// run executes one muxunit invocation and returns (exit, stdout, stderr).
func run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	var out, errb bytes.Buffer
	code := Run(args, &out, &errb)
	return code, out.String(), errb.String()
}

// write drops a fixture file into dir and returns its path.
func write(t *testing.T, dir, name, content string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

const shard1XML = `<testsuites><testsuite name="api">
<testcase classname="Auth" name="creates" time="0.5"/>
<testcase classname="Auth" name="rejects" time="0.2"><failure message="want 422, got 500">trace</failure></testcase>
</testsuite></testsuites>`

const shard2XML = `<testsuites><testsuite name="api">
<testcase classname="Auth" name="deletes" time="0.1"/>
</testsuite><testsuite name="web">
<testcase name="renders home" time="0.3"/>
</testsuite></testsuites>`

const retryXML = `<testsuites><testsuite name="api">
<testcase classname="Auth" name="rejects" time="0.2"/>
</testsuite></testsuites>`

const shardTAP = `TAP version 13
1..2
ok 1 - boots
not ok 2 - handles unicode
  ---
  message: bad rune
  ...
`

func TestVersionPrintsManifestVersion(t *testing.T) {
	code, out, _ := run(t, "version")
	if code != ExitOK || out != "muxunit 0.1.0\n" {
		t.Fatalf("code=%d out=%q", code, out)
	}
	code2, out2, _ := run(t, "--version")
	if code2 != ExitOK || out2 != out {
		t.Fatalf("--version differs: %q", out2)
	}
}

func TestExplicitHelpExitsZero(t *testing.T) {
	// Asking for help is a successful run; only *wrong* usage is exit 2.
	code, out, _ := run(t, "--help")
	if code != ExitOK || !strings.Contains(out, "Usage:") {
		t.Fatalf("--help: code=%d out=%q", code, out)
	}
	for _, cmd := range []string{"merge", "diff", "filter", "rewrite", "summary"} {
		code, _, errOut := run(t, cmd, "-h")
		if code != ExitOK {
			t.Fatalf("%s -h: code=%d, want 0", cmd, code)
		}
		if !strings.Contains(errOut, "Usage of muxunit "+cmd) {
			t.Fatalf("%s -h: flag usage missing: %q", cmd, errOut)
		}
	}
}

func TestNoArgsAndUnknownCommandExit2(t *testing.T) {
	code, _, errOut := run(t)
	if code != ExitUsage || !strings.Contains(errOut, "Usage:") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	code, _, errOut = run(t, "explode")
	if code != ExitUsage || !strings.Contains(errOut, "unknown command") {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
}

func TestMergeCombinesJUnitShards(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "shard-1.xml", shard1XML)
	b := write(t, dir, "shard-2.xml", shard2XML)
	code, out, errOut := run(t, "merge", a, b)
	if code != ExitOK {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, `<testsuites tests="4" failures="1"`) {
		t.Fatalf("merged counters wrong:\n%s", out)
	}
	if !strings.Contains(out, `name="web"`) {
		t.Fatalf("second shard's suite missing:\n%s", out)
	}
}

func TestMergeMixesJUnitAndTAPInputs(t *testing.T) {
	// The everyday polyglot pipeline: a Go suite as JUnit XML plus a bats
	// suite as TAP, merged into a single JUnit artifact for the CI UI.
	dir := t.TempDir()
	a := write(t, dir, "go.xml", shard1XML)
	b := write(t, dir, "bats.tap", shardTAP)
	code, out, _ := run(t, "merge", a, b)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, `name="bats"`) {
		t.Fatalf("TAP suite not labelled from its file name:\n%s", out)
	}
	if !strings.Contains(out, `handles unicode`) || !strings.Contains(out, "bad rune") {
		t.Fatalf("TAP failure detail lost in conversion:\n%s", out)
	}
}

func TestMergeDedupePreferPassResolvesRetry(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "run.xml", shard1XML)  // rejects: fail
	b := write(t, dir, "retry.xml", retryXML) // rejects: pass
	code, out, _ := run(t, "merge", "--dedupe", "prefer-pass", a, b)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, `failures="0"`) {
		t.Fatalf("retried flake should be green under prefer-pass:\n%s", out)
	}
	if strings.Count(out, `name="rejects"`) != 1 {
		t.Fatalf("duplicate not collapsed:\n%s", out)
	}
}

func TestMergeOutputFormatsAndFileTarget(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "shard-1.xml", shard1XML)
	code, tapOut, _ := run(t, "merge", "--to", "tap", a)
	if code != ExitOK || !strings.HasPrefix(tapOut, "TAP version 13\n1..2\n") {
		t.Fatalf("tap output wrong (code=%d):\n%s", code, tapOut)
	}
	code, jsonOut, _ := run(t, "merge", "--to", "json", a)
	if code != ExitOK || !strings.Contains(jsonOut, `"schema_version": 1`) {
		t.Fatalf("json output wrong (code=%d):\n%s", code, jsonOut)
	}
	// -o writes the report to a file and keeps stdout silent.
	outPath := filepath.Join(dir, "merged.xml")
	code, stdout, _ := run(t, "merge", "-o", outPath, a)
	if code != ExitOK || stdout != "" {
		t.Fatalf("code=%d stdout=%q", code, stdout)
	}
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `<testsuites`) {
		t.Fatalf("output file wrong:\n%s", data)
	}
}

func TestMergeReadsStdin(t *testing.T) {
	old := os.Stdin
	r, w, _ := os.Pipe()
	os.Stdin = r
	defer func() { os.Stdin = old }()
	go func() {
		w.WriteString(shardTAP)
		w.Close()
	}()
	code, out, errOut := run(t, "merge", "-")
	if code != ExitOK {
		t.Fatalf("code=%d err=%q", code, errOut)
	}
	if !strings.Contains(out, `name="stdin"`) {
		t.Fatalf("stdin TAP suite should be labelled 'stdin':\n%s", out)
	}
}

func TestDiffFindsRegressionAndExits1(t *testing.T) {
	dir := t.TempDir()
	oldP := write(t, dir, "old.xml", retryXML)  // rejects: pass
	newP := write(t, dir, "new.xml", shard1XML) // rejects: fail, creates: added
	code, out, _ := run(t, "diff", oldP, newP)
	if code != ExitGate {
		t.Fatalf("code=%d, want %d", code, ExitGate)
	}
	if !strings.Contains(out, "new failures (1)") || !strings.Contains(out, "api/Auth/rejects") {
		t.Fatalf("diff output wrong:\n%s", out)
	}
	if !strings.Contains(out, "diff: REGRESSIONS") {
		t.Fatalf("verdict missing:\n%s", out)
	}
}

func TestDiffCleanExitsZero(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.xml", shard1XML)
	code, out, _ := run(t, "diff", a, a)
	if code != ExitOK {
		t.Fatalf("code=%d out=%s", code, out)
	}
	if !strings.Contains(out, "still failing (1)") {
		t.Fatalf("known-red test should be listed as still failing:\n%s", out)
	}
}

func TestDiffFailOnModes(t *testing.T) {
	dir := t.TempDir()
	oldP := write(t, dir, "old.xml", shard1XML)
	newP := write(t, dir, "new.xml", shard2XML) // different tests, no new red
	// added/removed only → not a regression → exit 0 by default
	code, _, _ := run(t, "diff", oldP, newP)
	if code != ExitOK {
		t.Fatalf("default fail-on regressions: code=%d", code)
	}
	// --fail-on any-change → exit 1
	code, _, _ = run(t, "diff", "--fail-on", "any-change", oldP, newP)
	if code != ExitGate {
		t.Fatalf("any-change: code=%d", code)
	}
	// --fail-on nothing with a real regression → exit 0
	badNew := write(t, dir, "bad.xml", shard1XML)
	goodOld := write(t, dir, "good.xml", retryXML)
	code, _, _ = run(t, "diff", "--fail-on", "nothing", goodOld, badNew)
	if code != ExitOK {
		t.Fatalf("nothing: code=%d", code)
	}
}

func TestDiffJSONFormat(t *testing.T) {
	dir := t.TempDir()
	oldP := write(t, dir, "old.xml", retryXML)
	newP := write(t, dir, "new.xml", shard1XML)
	code, out, _ := run(t, "diff", "--format", "json", oldP, newP)
	if code != ExitGate {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, `"regressions": true`) || !strings.Contains(out, `"kind": "new-failure"`) {
		t.Fatalf("json diff wrong:\n%s", out)
	}
}

func TestFilterOnlyFailedAcrossFormats(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "go.xml", shard1XML)
	b := write(t, dir, "bats.tap", shardTAP)
	code, out, _ := run(t, "filter", "--only-failed", "--to", "tap", a, b)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.HasPrefix(out, "TAP version 13\n1..2\n") {
		t.Fatalf("want exactly the 2 red cases:\n%s", out)
	}
	if strings.Contains(out, "creates") || strings.Contains(out, "boots") {
		t.Fatalf("passing case leaked through --only-failed:\n%s", out)
	}
}

func TestFilterMatchGlob(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "shard-2.xml", shard2XML)
	code, out, _ := run(t, "filter", "--match", "web/**", a)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "renders home") || strings.Contains(out, "deletes") {
		t.Fatalf("glob filter wrong:\n%s", out)
	}
}

func TestFilterInvertQuarantinesRed(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "go.xml", shard1XML)
	code, out, _ := run(t, "filter", "--only-failed", "--invert", a)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, `failures="0"`) || !strings.Contains(out, "creates") {
		t.Fatalf("invert should keep only green:\n%s", out)
	}
}

func TestRewriteTrimPrefixThenMergeUnifiesSuites(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.xml", `<testsuite name="shard-1/api"><testcase name="t1"/></testsuite>`)
	b := write(t, dir, "b.xml", `<testsuite name="shard-2/api"><testcase name="t2"/></testsuite>`)
	code, out, _ := run(t, "rewrite",
		"--sub", "/^t/case-/",
		"--rename-suite", "shard-1/api=api",
		"--rename-suite", "shard-2/api=api",
		a, b)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if strings.Count(out, `<testsuite name="api"`) != 1 {
		t.Fatalf("renamed suites should unify into one:\n%s", out)
	}
	if !strings.Contains(out, "case-1") || !strings.Contains(out, "case-2") {
		t.Fatalf("case-name substitution missing:\n%s", out)
	}
}

func TestRewriteStripTimesProducesReproducibleOutput(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "a.xml", shard1XML)
	code, out, _ := run(t, "rewrite", "--strip-times", a)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if strings.Contains(out, `time="0.500"`) || !strings.Contains(out, `time="0.000"`) {
		t.Fatalf("times not stripped:\n%s", out)
	}
}

func TestSummaryTextAndCheckGate(t *testing.T) {
	dir := t.TempDir()
	a := write(t, dir, "go.xml", shard1XML)
	b := write(t, dir, "bats.tap", shardTAP)
	code, out, _ := run(t, "summary", a, b)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(out, "api") || !strings.Contains(out, "bats") || !strings.Contains(out, "TOTAL") {
		t.Fatalf("summary table wrong:\n%s", out)
	}
	code, _, _ = run(t, "summary", "--check", a, b)
	if code != ExitGate {
		t.Fatalf("--check with red tests: code=%d, want %d", code, ExitGate)
	}
	green := write(t, dir, "green.xml", retryXML)
	code, _, _ = run(t, "summary", "--check", green)
	if code != ExitOK {
		t.Fatalf("--check with all green: code=%d", code)
	}
	code, out, _ = run(t, "summary", "--format", "json", a)
	if code != ExitOK || !strings.Contains(out, `"kind": "summary"`) || !strings.Contains(out, `"failed": 1`) {
		t.Fatalf("summary json wrong (code=%d):\n%s", code, out)
	}
}

func TestForcedFromFormatOverridesDetection(t *testing.T) {
	// A TAP stream in a .log file: detection would still sniff TAP, but a
	// junit-shaped file with a weird extension needs --from. Prove the
	// flag is honored by forcing the wrong parser and expecting failure.
	dir := t.TempDir()
	p := write(t, dir, "report.log", shardTAP)
	code, _, _ := run(t, "summary", "--from", "junit", p)
	if code != ExitRuntime {
		t.Fatalf("forcing junit on TAP should fail to parse, code=%d", code)
	}
	code, _, _ = run(t, "summary", "--from", "tap", p)
	if code != ExitOK {
		t.Fatalf("forcing tap should work, code=%d", code)
	}
}

func TestErrorExitCodesAreStableAndDiagnosed(t *testing.T) {
	// Wrappers script against these: 2 = the invocation is wrong,
	// 3 = the invocation is fine but an input is not.
	dir := t.TempDir()
	a := write(t, dir, "a.xml", shard1XML)
	junk := write(t, dir, "junk.xml", "this is not xml at all")
	usage := [][]string{
		{"merge", "--dedupe", "newest", a},
		{"merge", "--to", "yaml", a},
		{"diff", "only-one.xml"},
		{"filter", "--only-failed", "--status", "skip", a},
		{"filter", "--match", "", a},
		{"rewrite", "--sub", "not-a-substitution", a},
		{"rewrite", "--rename-suite", "no-equals-sign", a},
		{"summary", "--format", "csv", a},
	}
	for _, args := range usage {
		if code, _, _ := run(t, args...); code != ExitUsage {
			t.Fatalf("%v: code=%d, want %d", args, code, ExitUsage)
		}
	}
	runtime := [][]string{
		{"merge", "/nonexistent/report.xml"},
		{"merge", junk},
		{"summary", junk},
	}
	for _, args := range runtime {
		code, _, errOut := run(t, args...)
		if code != ExitRuntime {
			t.Fatalf("%v: code=%d, want %d", args, code, ExitRuntime)
		}
		if !strings.Contains(errOut, "muxunit:") {
			t.Fatalf("%v: error not prefixed: %q", args, errOut)
		}
	}
	// The runtime error must name the offending file.
	if _, _, errOut := run(t, "merge", junk); !strings.Contains(errOut, "junk.xml") {
		t.Fatalf("error must name the offending file: %q", errOut)
	}
}

func TestConvertSingleFileJUnitToTAPRoundTrip(t *testing.T) {
	// merge with one input doubles as the format converter; a junit→tap→
	// junit trip must preserve statuses and counts.
	dir := t.TempDir()
	a := write(t, dir, "a.xml", shard1XML)
	code, tapOut, _ := run(t, "merge", "--to", "tap", a)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	tapPath := write(t, dir, "converted.tap", tapOut)
	code, xmlOut, _ := run(t, "merge", "--to", "junit", tapPath)
	if code != ExitOK {
		t.Fatalf("code=%d", code)
	}
	if !strings.Contains(xmlOut, `tests="2" failures="1"`) {
		t.Fatalf("counts changed across round trip:\n%s", xmlOut)
	}
}
