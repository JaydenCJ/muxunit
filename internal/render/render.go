// Package render turns model reports and diff results into human output
// (aligned text) and machine output (stable, versioned JSON).
package render

import (
	"encoding/json"
	"fmt"
	"io"

	"github.com/JaydenCJ/muxunit/internal/algebra"
	"github.com/JaydenCJ/muxunit/internal/model"
)

// SchemaVersion identifies the JSON envelope layout; bump on breaking change.
const SchemaVersion = 1

// jsonEnvelope wraps every JSON payload muxunit emits.
type jsonEnvelope struct {
	Tool          string `json:"tool"`
	SchemaVersion int    `json:"schema_version"`
	Kind          string `json:"kind"`
	Payload       any    `json:"payload"`
}

// writeJSON emits one indented envelope followed by a newline.
func writeJSON(w io.Writer, kind string, payload any) error {
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	return enc.Encode(jsonEnvelope{
		Tool:          "muxunit",
		SchemaVersion: SchemaVersion,
		Kind:          kind,
		Payload:       payload,
	})
}

// jsonCase mirrors model.Case with the status spelled out.
type jsonCase struct {
	model.Case
	Status string `json:"status"`
}

type jsonSuite struct {
	Name       string           `json:"name"`
	Timestamp  string           `json:"timestamp,omitempty"`
	Hostname   string           `json:"hostname,omitempty"`
	Properties []model.Property `json:"properties,omitempty"`
	Counts     model.Counts     `json:"counts"`
	Cases      []jsonCase       `json:"cases"`
}

type jsonReport struct {
	Counts model.Counts `json:"counts"`
	Suites []jsonSuite  `json:"suites"`
}

// WriteReportJSON emits the whole report as JSON — the `--to json` sink.
func WriteReportJSON(w io.Writer, rep *model.Report) error {
	out := jsonReport{Counts: rep.Counts()}
	for _, s := range rep.Suites {
		js := jsonSuite{
			Name:       s.Name,
			Timestamp:  s.Timestamp,
			Hostname:   s.Hostname,
			Properties: s.Properties,
			Counts:     model.CountsOf(s),
			Cases:      make([]jsonCase, 0, len(s.Cases)),
		}
		for _, c := range s.Cases {
			js.Cases = append(js.Cases, jsonCase{Case: c, Status: c.Status.String()})
		}
		out.Suites = append(out.Suites, js)
	}
	return writeJSON(w, "report", out)
}

// WriteSummaryText prints the per-suite table and totals line that
// `muxunit summary` shows by default.
func WriteSummaryText(w io.Writer, rep *model.Report) error {
	nameWidth := len("TOTAL")
	for _, s := range rep.Suites {
		if n := len(displayName(s.Name)); n > nameWidth {
			nameWidth = n
		}
	}
	fmt.Fprintf(w, "%-*s  %6s  %6s  %6s  %6s  %6s  %9s\n",
		nameWidth, "suite", "total", "pass", "fail", "error", "skip", "time")
	for _, s := range rep.Suites {
		c := model.CountsOf(s)
		fmt.Fprintf(w, "%-*s  %6d  %6d  %6d  %6d  %6d  %8.3fs\n",
			nameWidth, displayName(s.Name), c.Total, c.Passed, c.Failed, c.Errored, c.Skipped, c.Time)
	}
	t := rep.Counts()
	fmt.Fprintf(w, "%-*s  %6d  %6d  %6d  %6d  %6d  %8.3fs\n",
		nameWidth, "TOTAL", t.Total, t.Passed, t.Failed, t.Errored, t.Skipped, t.Time)
	return nil
}

// pluralTests picks the right noun for a test count, so a one-test report
// reads "1 test", never "1 tests".
func pluralTests(n int) string {
	if n == 1 {
		return "test"
	}
	return "tests"
}

func displayName(name string) string {
	if name == "" {
		return "(unnamed)"
	}
	return name
}

// WriteSummaryJSON emits counts only — cheap to parse in a pipeline gate.
func WriteSummaryJSON(w io.Writer, rep *model.Report) error {
	type suiteCounts struct {
		Name   string       `json:"name"`
		Counts model.Counts `json:"counts"`
	}
	payload := struct {
		Counts model.Counts  `json:"counts"`
		Suites []suiteCounts `json:"suites"`
	}{Counts: rep.Counts()}
	for _, s := range rep.Suites {
		payload.Suites = append(payload.Suites, suiteCounts{Name: displayName(s.Name), Counts: model.CountsOf(s)})
	}
	return writeJSON(w, "summary", payload)
}

// WriteDiffText prints the human diff: one section per non-empty bucket,
// then a one-line verdict.
func WriteDiffText(w io.Writer, d algebra.DiffResult) error {
	section := func(title string, changes []algebra.Change, arrow bool) {
		if len(changes) == 0 {
			return
		}
		fmt.Fprintf(w, "%s (%d)\n", title, len(changes))
		for _, c := range changes {
			switch {
			case arrow:
				fmt.Fprintf(w, "  %s  %s -> %s", c.ID, c.From, c.To)
			default:
				fmt.Fprintf(w, "  %s  %s", c.ID, c.To)
			}
			if c.Message != "" {
				fmt.Fprintf(w, "  (%s)", c.Message)
			}
			fmt.Fprintln(w)
		}
	}
	section("new failures", d.NewFailures, true)
	section("fixed", d.Fixed, true)
	section("still failing", d.StillFailing, true)
	section("status changed", d.StatusChanged, true)
	section("added", d.Added, false)
	section("removed", d.Removed, false)

	if d.Total() == 0 {
		fmt.Fprintln(w, "no changes")
	}
	fmt.Fprintf(w, "old: %d %s (%d red)  new: %d %s (%d red)\n",
		d.OldCounts.Total, pluralTests(d.OldCounts.Total), d.OldCounts.Failed+d.OldCounts.Errored,
		d.NewCounts.Total, pluralTests(d.NewCounts.Total), d.NewCounts.Failed+d.NewCounts.Errored)
	if d.HasRegressions() {
		fmt.Fprintln(w, "diff: REGRESSIONS")
	} else {
		fmt.Fprintln(w, "diff: ok")
	}
	return nil
}

// WriteDiffJSON emits the diff buckets plus a regression verdict.
func WriteDiffJSON(w io.Writer, d algebra.DiffResult) error {
	type change struct {
		ID      string `json:"id"`
		Kind    string `json:"kind"`
		From    string `json:"from"`
		To      string `json:"to"`
		Message string `json:"message,omitempty"`
	}
	conv := func(cs []algebra.Change) []change {
		out := make([]change, 0, len(cs))
		for _, c := range cs {
			out = append(out, change{
				ID: c.ID, Kind: string(c.Kind),
				From: c.From.String(), To: c.To.String(), Message: c.Message,
			})
		}
		return out
	}
	payload := struct {
		NewFailures   []change     `json:"new_failures"`
		Fixed         []change     `json:"fixed"`
		StillFailing  []change     `json:"still_failing"`
		StatusChanged []change     `json:"status_changed"`
		Added         []change     `json:"added"`
		Removed       []change     `json:"removed"`
		OldCounts     model.Counts `json:"old_counts"`
		NewCounts     model.Counts `json:"new_counts"`
		Regressions   bool         `json:"regressions"`
	}{
		NewFailures:   conv(d.NewFailures),
		Fixed:         conv(d.Fixed),
		StillFailing:  conv(d.StillFailing),
		StatusChanged: conv(d.StatusChanged),
		Added:         conv(d.Added),
		Removed:       conv(d.Removed),
		OldCounts:     d.OldCounts,
		NewCounts:     d.NewCounts,
		Regressions:   d.HasRegressions(),
	}
	return writeJSON(w, "diff", payload)
}
