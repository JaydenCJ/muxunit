// Tests for the TAP reader/writer: directives, YAML diagnostics, truncated
// streams, bail-outs, and the single-suite round trip.
package tap

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JaydenCJ/muxunit/internal/model"
)

func parse(t *testing.T, stream string) *model.Report {
	t.Helper()
	rep, err := Parse(strings.NewReader(stream), "suite")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return rep
}

func cases(t *testing.T, stream string) []model.Case {
	t.Helper()
	return parse(t, stream).Suites[0].Cases
}

func TestParseBasicOkAndNotOk(t *testing.T) {
	rep := parse(t, "TAP version 13\n1..2\nok 1 - adds numbers\nnot ok 2 - divides by zero\n")
	if rep.Suites[0].Name != "suite" {
		t.Fatalf("suite name = %q, want the caller's label", rep.Suites[0].Name)
	}
	cs := rep.Suites[0].Cases
	if len(cs) != 2 {
		t.Fatalf("cases = %d, want 2", len(cs))
	}
	if cs[0].Status != model.Pass || cs[0].Name != "adds numbers" {
		t.Fatalf("case 0 wrong: %+v", cs[0])
	}
	if cs[1].Status != model.Fail || cs[1].Name != "divides by zero" {
		t.Fatalf("case 1 wrong: %+v", cs[1])
	}
}

func TestParseUnnumberedAndUndescribedPoints(t *testing.T) {
	// prove and bats emit "ok" lines without numbers; names must still be
	// stable so merge/diff keys work.
	cs := cases(t, "1..2\nok\nnot ok\n")
	if cs[0].Name != "test 1" || cs[1].Name != "test 2" {
		t.Fatalf("synthetic names wrong: %q, %q", cs[0].Name, cs[1].Name)
	}
}

func TestParseDirectives(t *testing.T) {
	// SKIP and TODO change the meaning of ok/not ok; both are
	// case-insensitive, and a "not ok # TODO" is an *expected* failure —
	// it must never turn a merged report red.
	tests := []struct {
		line       string
		wantStatus model.Status
		wantName   string
		wantMsg    string // substring of Message
	}{
		{"ok 1 - needs docker # SKIP no docker on ci", model.Skip, "needs docker", "no docker on ci"},
		{"not ok 1 - future feature # TODO not built yet", model.Skip, "future feature", "not built yet"},
		{"ok 1 - surprise # TODO expected to fail", model.Pass, "surprise", ""},
		{"ok 1 - a # skip lowercase", model.Skip, "a", "lowercase"},
		{"not ok 1 - b # Todo mixed", model.Skip, "b", "mixed"},
	}
	for _, tc := range tests {
		cs := cases(t, "1..1\n"+tc.line+"\n")
		if cs[0].Status != tc.wantStatus {
			t.Fatalf("%q: status = %v, want %v", tc.line, cs[0].Status, tc.wantStatus)
		}
		if cs[0].Name != tc.wantName {
			t.Fatalf("%q: directive leaked into name: %q", tc.line, cs[0].Name)
		}
		if tc.wantMsg != "" && !strings.Contains(cs[0].Message, tc.wantMsg) {
			t.Fatalf("%q: reason lost: %q", tc.line, cs[0].Message)
		}
	}
}

func TestParseYAMLBlocks(t *testing.T) {
	// A terminated block attaches verbatim to the preceding point...
	cs := cases(t, "TAP version 13\n1..1\nnot ok 1 - broken\n  ---\n  message: boom\n  got: 500\n  ...\n")
	if !strings.Contains(cs[0].Detail, "message: boom") || !strings.Contains(cs[0].Detail, "got: 500") {
		t.Fatalf("yaml block lost: %q", cs[0].Detail)
	}
	// The conventional "message:" key is promoted to the case message so
	// converted JUnit failures and diff lines carry a one-line reason.
	if cs[0].Message != "boom" {
		t.Fatalf("yaml message not promoted: %q", cs[0].Message)
	}
	// ...and an unterminated block ends implicitly at the next top-level line.
	cs = cases(t, "1..2\nnot ok 1 - a\n  ---\n  message: boom\nok 2 - b\n")
	if len(cs) != 2 || !strings.Contains(cs[0].Detail, "message: boom") {
		t.Fatalf("unterminated yaml mishandled: %+v", cs)
	}
	if cs[1].Status != model.Pass {
		t.Fatalf("point after unterminated yaml mishandled: %+v", cs[1])
	}
}

func TestParseIgnoresCommentsVersionsAndSubtestNoise(t *testing.T) {
	// node-tap prints indented subtest streams; only the parent summary
	// line is a top-level verdict.
	stream := "TAP version 14\n# a comment\n1..1\n    ok 1 - inner\n    1..1\nok 1 - outer\n# trailing comment\n"
	cs := cases(t, stream)
	if len(cs) != 1 || cs[0].Name != "outer" {
		t.Fatalf("noise not ignored: %+v", cs)
	}
}

func TestParsePlanEdgeCases(t *testing.T) {
	// The runner promised 4 tests and crashed after 2. A merge tool that
	// silently reports 2/2 green is lying; the gap must be visible.
	cs := cases(t, "1..4\nok 1 - a\nok 2 - b\n")
	if len(cs) != 4 {
		t.Fatalf("cases = %d, want 4 (2 real + 2 synthetic)", len(cs))
	}
	for _, c := range cs[2:] {
		if c.Status != model.Error || !strings.Contains(c.Message, "never ran") {
			t.Fatalf("missing point should be a labelled error, got %+v", c)
		}
	}
	// "1..0 # Skipped" is an intentionally empty stream, not an error.
	rep := parse(t, "1..0 # Skipped: no tests on this platform\n")
	if len(rep.Suites[0].Cases) != 0 {
		t.Fatalf("skip-all should produce no cases: %+v", rep.Suites[0].Cases)
	}
	props := rep.Suites[0].Properties
	if len(props) != 1 || props[0].Name != "tap.skip_all" {
		t.Fatalf("skip-all reason not recorded: %+v", props)
	}
}

func TestParseBailOutBecomesError(t *testing.T) {
	cs := cases(t, "1..3\nok 1 - a\nBail out! database is gone\nok 2 - ignored after bail\n")
	if len(cs) != 2 {
		t.Fatalf("cases = %d, want 2 (points after bail are unreliable)", len(cs))
	}
	last := cs[len(cs)-1]
	if last.Status != model.Error || !strings.Contains(last.Message, "database is gone") {
		t.Fatalf("bail out not surfaced: %+v", last)
	}
}

func TestParseRejectsGarbage(t *testing.T) {
	if _, err := Parse(strings.NewReader("<xml>this is junit</xml>"), "s"); err == nil {
		t.Fatal("expected an error for non-TAP input")
	}
	if _, err := Parse(strings.NewReader(""), "s"); err == nil {
		t.Fatal("expected an error for an empty stream (no plan, no points)")
	}
}

func TestWriteRoundTripSingleSuite(t *testing.T) {
	orig := parse(t, "1..3\nok 1 - a\nnot ok 2 - b\nok 3 - c # SKIP later\n")
	var buf bytes.Buffer
	if err := Write(&buf, orig); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if !strings.HasPrefix(buf.String(), "TAP version 13\n1..3\n") {
		t.Fatalf("missing version/plan header:\n%s", buf.String())
	}
	again, err := Parse(bytes.NewReader(buf.Bytes()), "suite")
	if err != nil {
		t.Fatalf("re-Parse: %v\n%s", err, buf.String())
	}
	oc, ac := orig.Suites[0].Cases, again.Suites[0].Cases
	if len(ac) != len(oc) {
		t.Fatalf("case count changed: %d -> %d", len(oc), len(ac))
	}
	for i := range oc {
		if ac[i].Name != oc[i].Name || ac[i].Status != oc[i].Status {
			t.Fatalf("case %d changed: %+v -> %+v", i, oc[i], ac[i])
		}
	}
}

func TestWriteFailureEmitsYAMLDiagnostics(t *testing.T) {
	rep := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{{
		Name: "b", Status: model.Fail, Message: "expected 1, got 2", Detail: "assert.go:10\nassert.go:22",
	}}}}}
	var buf bytes.Buffer
	if err := Write(&buf, rep); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"not ok 1 - b", "  ---", "message:", "severity: fail", "assert.go:10", "  ..."} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestWriteMultiSuitePrefixesAndNumbersSequentially(t *testing.T) {
	// TAP is flat: with more than one suite, descriptions carry the suite
	// name and point numbers keep counting across suites.
	rep := &model.Report{Suites: []model.Suite{
		{Name: "api", Cases: []model.Case{{Name: "a", Status: model.Pass}, {Name: "b", Status: model.Pass}}},
		{Name: "web", Cases: []model.Case{{Name: "c", Status: model.Pass}}},
	}}
	var buf bytes.Buffer
	if err := Write(&buf, rep); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "1..3") || !strings.Contains(out, "ok 1 - api > a") || !strings.Contains(out, "ok 3 - web > c") {
		t.Fatalf("multi-suite output wrong:\n%s", out)
	}
}

func TestHashEscapingRoundTrips(t *testing.T) {
	// "#" starts a directive, so literal hashes are escaped on write and
	// unescaped on read — both directions must agree.
	cs := cases(t, "1..1\nok 1 - issue \\#42 regression\n")
	if cs[0].Name != "issue #42 regression" {
		t.Fatalf("escaped hash mangled on parse: %q", cs[0].Name)
	}
	rep := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{{Name: "issue #42", Status: model.Pass}}}}}
	var buf bytes.Buffer
	if err := Write(&buf, rep); err != nil {
		t.Fatal(err)
	}
	again, err := Parse(bytes.NewReader(buf.Bytes()), "s")
	if err != nil {
		t.Fatal(err)
	}
	if again.Suites[0].Cases[0].Name != "issue #42" {
		t.Fatalf("hash not preserved through round trip: %q", again.Suites[0].Cases[0].Name)
	}
}
