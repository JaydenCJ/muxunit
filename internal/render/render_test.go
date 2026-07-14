// Tests for rendering: the JSON envelope contract and the text layouts a
// human reads in CI logs.
package render

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"github.com/JaydenCJ/muxunit/internal/algebra"
	"github.com/JaydenCJ/muxunit/internal/model"
)

func sample() *model.Report {
	return &model.Report{Suites: []model.Suite{{
		Name: "api",
		Cases: []model.Case{
			{Name: "a", Status: model.Pass, Time: 0.5},
			{Name: "b", Status: model.Fail, Time: 0.25, Message: "boom"},
			{Name: "c", Status: model.Skip},
		},
	}}}
}

// decode unmarshals the envelope and returns the payload as a generic map.
func decode(t *testing.T, data []byte) map[string]any {
	t.Helper()
	var env struct {
		Tool          string         `json:"tool"`
		SchemaVersion int            `json:"schema_version"`
		Kind          string         `json:"kind"`
		Payload       map[string]any `json:"payload"`
	}
	if err := json.Unmarshal(data, &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, data)
	}
	if env.Tool != "muxunit" || env.SchemaVersion != SchemaVersion {
		t.Fatalf("bad envelope: %+v", env)
	}
	return env.Payload
}

func TestReportJSONEnvelopeAndStatuses(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteReportJSON(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	payload := decode(t, buf.Bytes())
	suites := payload["suites"].([]any)
	if len(suites) != 1 {
		t.Fatalf("suites = %d", len(suites))
	}
	cases := suites[0].(map[string]any)["cases"].([]any)
	statuses := []string{}
	for _, c := range cases {
		statuses = append(statuses, c.(map[string]any)["status"].(string))
	}
	if strings.Join(statuses, ",") != "pass,fail,skip" {
		t.Fatalf("statuses = %v", statuses)
	}
}

func TestSummaryTextHasHeaderRowsAndTotal(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSummaryText(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"suite", "api", "TOTAL"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 3 {
		t.Fatalf("want header + 1 suite + total = 3 lines, got %d:\n%s", len(lines), out)
	}
	// Anonymous suites still get a readable row.
	unnamed := &model.Report{Suites: []model.Suite{{Cases: []model.Case{{Name: "a", Status: model.Pass}}}}}
	buf.Reset()
	if err := WriteSummaryText(&buf, unnamed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "(unnamed)") {
		t.Fatalf("unnamed suite should render a placeholder:\n%s", buf.String())
	}
}

func TestSummaryJSONCounts(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteSummaryJSON(&buf, sample()); err != nil {
		t.Fatal(err)
	}
	payload := decode(t, buf.Bytes())
	counts := payload["counts"].(map[string]any)
	if counts["total"].(float64) != 3 || counts["failed"].(float64) != 1 {
		t.Fatalf("counts wrong: %v", counts)
	}
}

func TestDiffTextShowsBucketsAndVerdict(t *testing.T) {
	oldR := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{
		{Name: "a", Status: model.Pass}, {Name: "gone", Status: model.Pass},
	}}}}
	newR := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{
		{Name: "a", Status: model.Fail, Message: "boom"},
	}}}}
	var buf bytes.Buffer
	if err := WriteDiffText(&buf, algebra.Diff(oldR, newR)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	for _, want := range []string{"new failures (1)", "s/a  pass -> fail  (boom)", "removed (1)", "diff: REGRESSIONS"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in:\n%s", want, out)
		}
	}
}

func TestDiffTextFooterPluralizesTestCounts(t *testing.T) {
	// "1 tests" in a CI log reads as sloppiness; the footer must agree in
	// number on both sides of the comparison.
	oldR := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{
		{Name: "a", Status: model.Pass},
	}}}}
	newR := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{
		{Name: "a", Status: model.Pass}, {Name: "b", Status: model.Fail},
	}}}}
	var buf bytes.Buffer
	if err := WriteDiffText(&buf, algebra.Diff(oldR, newR)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "old: 1 test (0 red)  new: 2 tests (1 red)") {
		t.Fatalf("footer not pluralized correctly:\n%s", buf.String())
	}
}

func TestDiffTextCleanRunSaysNoChanges(t *testing.T) {
	r := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{{Name: "a", Status: model.Pass}}}}}
	var buf bytes.Buffer
	if err := WriteDiffText(&buf, algebra.Diff(r, r)); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "no changes") || !strings.Contains(out, "diff: ok") {
		t.Fatalf("clean diff output wrong:\n%s", out)
	}
}

func TestDiffJSONRegressionsFlag(t *testing.T) {
	oldR := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{{Name: "a", Status: model.Pass}}}}}
	newR := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{{Name: "a", Status: model.Error}}}}}
	var buf bytes.Buffer
	if err := WriteDiffJSON(&buf, algebra.Diff(oldR, newR)); err != nil {
		t.Fatal(err)
	}
	payload := decode(t, buf.Bytes())
	if payload["regressions"].(bool) != true {
		t.Fatalf("regressions flag missing: %v", payload)
	}
	nf := payload["new_failures"].([]any)
	entry := nf[0].(map[string]any)
	if entry["from"] != "pass" || entry["to"] != "error" || entry["kind"] != "new-failure" {
		t.Fatalf("change entry wrong: %v", entry)
	}
}

func TestDiffJSONEmptyBucketsAreArraysNotNull(t *testing.T) {
	// jq pipelines do `.payload.new_failures | length`; null would break them.
	r := &model.Report{Suites: []model.Suite{{Name: "s", Cases: []model.Case{{Name: "a", Status: model.Pass}}}}}
	var buf bytes.Buffer
	if err := WriteDiffJSON(&buf, algebra.Diff(r, r)); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), `"new_failures": null`) {
		t.Fatalf("empty buckets must serialize as [], not null:\n%s", buf.String())
	}
}
