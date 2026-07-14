// Tests for the JUnit XML reader/writer: real-world dialect quirks on the
// way in, byte-deterministic structure on the way out.
package junit

import (
	"bytes"
	"strings"
	"testing"

	"github.com/JaydenCJ/muxunit/internal/model"
)

func parse(t *testing.T, xml string) *model.Report {
	t.Helper()
	rep, err := Parse(strings.NewReader(xml))
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return rep
}

const sampleSuites = `<?xml version="1.0" encoding="UTF-8"?>
<testsuites>
  <testsuite name="api" tests="3" failures="1" time="2.5" timestamp="2026-07-01T10:00:00" hostname="ci-1">
    <properties>
      <property name="shard" value="1"/>
    </properties>
    <testcase classname="Auth" name="creates a user" time="1.25"/>
    <testcase classname="Auth" name="rejects bad email" time="0.75">
      <failure message="expected 422, got 500">stack trace here</failure>
    </testcase>
    <testcase classname="Auth" name="soft deletes" time="0.5">
      <skipped message="flaky on ci"/>
    </testcase>
  </testsuite>
</testsuites>
`

func TestParseTestsuitesRoot(t *testing.T) {
	rep := parse(t, sampleSuites)
	if len(rep.Suites) != 1 {
		t.Fatalf("suites = %d, want 1", len(rep.Suites))
	}
	s := rep.Suites[0]
	if s.Name != "api" || s.Hostname != "ci-1" || s.Timestamp != "2026-07-01T10:00:00" {
		t.Fatalf("suite metadata lost: %+v", s)
	}
	if len(s.Properties) != 1 || s.Properties[0].Name != "shard" || s.Properties[0].Value != "1" {
		t.Fatalf("properties lost: %+v", s.Properties)
	}
	if len(s.Cases) != 3 {
		t.Fatalf("cases = %d, want 3", len(s.Cases))
	}
}

func TestParseMapsChildElementsToStatuses(t *testing.T) {
	rep := parse(t, sampleSuites)
	cs := rep.Suites[0].Cases
	if cs[0].Status != model.Pass {
		t.Fatalf("case 0 = %v, want pass", cs[0].Status)
	}
	if cs[1].Status != model.Fail || cs[1].Message != "expected 422, got 500" || cs[1].Detail != "stack trace here" {
		t.Fatalf("failure not captured: %+v", cs[1])
	}
	if cs[2].Status != model.Skip || cs[2].Message != "flaky on ci" {
		t.Fatalf("skip not captured: %+v", cs[2])
	}
	rep2 := parse(t, `<testsuite name="s"><testcase name="x"><system-out>hello</system-out><system-err>oops</system-err></testcase></testsuite>`)
	c := rep2.Suites[0].Cases[0]
	if c.SystemOut != "hello" || c.SystemErr != "oops" {
		t.Fatalf("system streams lost: %+v", c)
	}
}

func TestParseBareTestsuiteRoot(t *testing.T) {
	// pytest and phpunit emit a bare <testsuite> root with no wrapper.
	rep := parse(t, `<testsuite name="pytest"><testcase classname="test_mod" name="test_ok" time="0.1"/></testsuite>`)
	if len(rep.Suites) != 1 || rep.Suites[0].Name != "pytest" {
		t.Fatalf("bare root not accepted: %+v", rep.Suites)
	}
}

func TestParseErrorElementAndSeverityPrecedence(t *testing.T) {
	rep := parse(t, `<testsuite name="s"><testcase name="boom"><error message="OOM" type="java.lang.OutOfMemoryError">heap dump</error></testcase></testsuite>`)
	c := rep.Suites[0].Cases[0]
	if c.Status != model.Error || c.Message != "OOM" || c.Detail != "heap dump" {
		t.Fatalf("error element mishandled: %+v", c)
	}
	// Some producers emit multiple children; the most severe one must win.
	rep = parse(t, `<testsuite name="s"><testcase name="x"><skipped/><failure message="f"/><error message="e"/></testcase></testsuite>`)
	if got := rep.Suites[0].Cases[0].Status; got != model.Error {
		t.Fatalf("status = %v, want error", got)
	}
}

func TestParseNestedSuitesAreFlattenedWithJoinedNames(t *testing.T) {
	rep := parse(t, `<testsuites><testsuite name="gradle"><testsuite name="unit"><testcase name="a"/></testsuite><testsuite name="integ"><testcase name="b"/></testsuite></testsuite></testsuites>`)
	if len(rep.Suites) != 2 {
		t.Fatalf("suites = %d, want 2 (container without cases is dropped)", len(rep.Suites))
	}
	if rep.Suites[0].Name != "gradle/unit" || rep.Suites[1].Name != "gradle/integ" {
		t.Fatalf("nested names wrong: %q, %q", rep.Suites[0].Name, rep.Suites[1].Name)
	}
}

func TestParseToleratesMissingAndLocaleTimes(t *testing.T) {
	rep := parse(t, `<testsuite name="s"><testcase name="a"/><testcase name="b" time="1,234.5"/><testcase name="c" time="bogus"/></testsuite>`)
	cs := rep.Suites[0].Cases
	if cs[0].Time != 0 {
		t.Fatalf("missing time should be 0, got %v", cs[0].Time)
	}
	if cs[1].Time != 1234.5 {
		t.Fatalf("grouped time should parse to 1234.5, got %v", cs[1].Time)
	}
	if cs[2].Time != 0 {
		t.Fatalf("unparsable time should be 0, got %v", cs[2].Time)
	}
}

func TestParseRejectsNonXML(t *testing.T) {
	if _, err := Parse(strings.NewReader("ok 1 - this is TAP, not XML")); err == nil {
		t.Fatal("expected a parse error for non-XML input")
	}
}

func TestWriteRecomputesCounters(t *testing.T) {
	rep := parse(t, sampleSuites)
	var buf bytes.Buffer
	if err := Write(&buf, rep); err != nil {
		t.Fatalf("Write: %v", err)
	}
	out := buf.String()
	if !strings.HasPrefix(out, `<?xml version="1.0" encoding="UTF-8"?>`) {
		t.Fatalf("missing XML declaration:\n%s", out)
	}
	if !strings.Contains(out, `<testsuites tests="3" failures="1" errors="0" skipped="1"`) {
		t.Fatalf("root counters wrong:\n%s", out)
	}
	if !strings.Contains(out, `<testsuite name="api" tests="3" failures="1" errors="0" skipped="1"`) {
		t.Fatalf("suite counters wrong:\n%s", out)
	}
}

func TestWriteParseRoundTripPreservesEverything(t *testing.T) {
	orig := parse(t, sampleSuites)
	var buf bytes.Buffer
	if err := Write(&buf, orig); err != nil {
		t.Fatalf("Write: %v", err)
	}
	again, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("re-Parse: %v", err)
	}
	if len(again.Suites) != 1 || len(again.Suites[0].Cases) != 3 {
		t.Fatalf("shape changed: %+v", again)
	}
	for i, c := range again.Suites[0].Cases {
		o := orig.Suites[0].Cases[i]
		if c.Name != o.Name || c.ClassName != o.ClassName || c.Status != o.Status ||
			c.Time != o.Time || c.Message != o.Message || c.Detail != o.Detail {
			t.Fatalf("case %d changed:\nwas  %+v\nnow  %+v", i, o, c)
		}
	}
	if again.Suites[0].Properties[0] != orig.Suites[0].Properties[0] {
		t.Fatal("properties changed in round trip")
	}
	var second bytes.Buffer
	if err := Write(&second, orig); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(buf.Bytes(), second.Bytes()) {
		t.Fatal("two writes of the same report differ byte-wise")
	}
}

func TestWriteEscapesXMLSpecials(t *testing.T) {
	rep := &model.Report{Suites: []model.Suite{{
		Name: "s<uite>",
		Cases: []model.Case{{
			Name: `x "quoted" & <tagged>`, Status: model.Fail,
			Message: `a < b & c`, Detail: "line1\nline2",
		}},
	}}}
	var buf bytes.Buffer
	if err := Write(&buf, rep); err != nil {
		t.Fatal(err)
	}
	again, err := Parse(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatalf("re-Parse escaped output: %v", err)
	}
	c := again.Suites[0].Cases[0]
	if c.Name != `x "quoted" & <tagged>` || c.Message != `a < b & c` || c.Detail != "line1\nline2" {
		t.Fatalf("escaping lost data: %+v", c)
	}
}
