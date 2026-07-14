// Package model defines the canonical, format-neutral test-report model that
// every parser produces and every writer and algebra operation consumes.
// JUnit XML and TAP are both lossy projections of this model; keeping the
// algebra here means merge/diff/filter/rewrite never care about syntax.
package model

import (
	"fmt"
	"sort"
	"strings"
)

// Status is the outcome of a single test case.
type Status int

const (
	// Pass is a successful test.
	Pass Status = iota
	// Fail is an assertion failure (JUnit <failure>, TAP "not ok").
	Fail
	// Error is an infrastructure/runtime error (JUnit <error>, TAP bail-out
	// or a test point the plan promised but the stream never delivered).
	Error
	// Skip is a test that did not run (JUnit <skipped>, TAP SKIP/TODO).
	Skip
)

// String returns the lowercase canonical name used in CLI flags and JSON.
func (s Status) String() string {
	switch s {
	case Pass:
		return "pass"
	case Fail:
		return "fail"
	case Error:
		return "error"
	case Skip:
		return "skip"
	}
	return fmt.Sprintf("status(%d)", int(s))
}

// ParseStatus converts a CLI/JSON token back into a Status. It accepts the
// canonical names plus common aliases so `--status failed,errors` just works.
func ParseStatus(tok string) (Status, error) {
	switch strings.ToLower(strings.TrimSpace(tok)) {
	case "pass", "passed", "ok":
		return Pass, nil
	case "fail", "failed", "failure":
		return Fail, nil
	case "error", "errored", "errors":
		return Error, nil
	case "skip", "skipped":
		return Skip, nil
	}
	return Pass, fmt.Errorf("unknown status %q (want pass, fail, error, or skip)", tok)
}

// Severity orders statuses from best to worst; merge dedupe policies use it
// to decide which duplicate of a retried test to keep.
// Skip(0) < Pass(1) < Fail(2) < Error(3).
func (s Status) Severity() int {
	switch s {
	case Skip:
		return 0
	case Pass:
		return 1
	case Fail:
		return 2
	default:
		return 3
	}
}

// Property is a named suite-level property; order is preserved from the input.
type Property struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// Case is one test case, format-neutral.
type Case struct {
	Name      string  `json:"name"`
	ClassName string  `json:"classname,omitempty"`
	Status    Status  `json:"-"`
	Time      float64 `json:"time"`
	// Message is the short failure/skip reason (JUnit message attribute,
	// TAP directive reason).
	Message string `json:"message,omitempty"`
	// Detail is the long body: JUnit failure text or a TAP YAML block.
	Detail    string `json:"detail,omitempty"`
	SystemOut string `json:"system_out,omitempty"`
	SystemErr string `json:"system_err,omitempty"`
}

// Suite is a named group of cases with optional metadata.
type Suite struct {
	Name       string     `json:"name"`
	Timestamp  string     `json:"timestamp,omitempty"`
	Hostname   string     `json:"hostname,omitempty"`
	Properties []Property `json:"properties,omitempty"`
	Cases      []Case     `json:"cases"`
}

// Report is an ordered list of suites — the unit every muxunit verb operates on.
type Report struct {
	Suites []Suite `json:"suites"`
}

// Key identifies a case across shards and report versions: same suite, same
// class, same name means "the same test", regardless of which file it came from.
type Key struct {
	Suite string
	Class string
	Name  string
}

// String renders the key the way the CLI prints and matches test IDs:
// suite/class/name with empty segments collapsed.
func (k Key) String() string {
	parts := make([]string, 0, 3)
	for _, p := range []string{k.Suite, k.Class, k.Name} {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return strings.Join(parts, "/")
}

// KeyOf builds the identity key for a case within a named suite.
func KeyOf(suiteName string, c Case) Key {
	return Key{Suite: suiteName, Class: c.ClassName, Name: c.Name}
}

// Counts aggregates outcome totals for a report or suite.
type Counts struct {
	Total   int     `json:"total"`
	Passed  int     `json:"passed"`
	Failed  int     `json:"failed"`
	Errored int     `json:"errored"`
	Skipped int     `json:"skipped"`
	Time    float64 `json:"time"`
}

// Add folds one case into the tally.
func (t *Counts) Add(c Case) {
	t.Total++
	t.Time += c.Time
	switch c.Status {
	case Pass:
		t.Passed++
	case Fail:
		t.Failed++
	case Error:
		t.Errored++
	case Skip:
		t.Skipped++
	}
}

// CountsOf tallies every case in the suite.
func CountsOf(s Suite) Counts {
	var t Counts
	for _, c := range s.Cases {
		t.Add(c)
	}
	return t
}

// Counts tallies every case in the report.
func (r *Report) Counts() Counts {
	var t Counts
	for _, s := range r.Suites {
		for _, c := range s.Cases {
			t.Add(c)
		}
	}
	return t
}

// TotalCases returns the number of cases across all suites.
func (r *Report) TotalCases() int {
	n := 0
	for _, s := range r.Suites {
		n += len(s.Cases)
	}
	return n
}

// Sort orders suites by name and cases by (class, name) so that identical
// logical reports serialize byte-identically regardless of shard arrival order.
// Sorting is stable, so duplicate keys keep their relative input order.
func (r *Report) Sort() {
	sort.SliceStable(r.Suites, func(i, j int) bool {
		return r.Suites[i].Name < r.Suites[j].Name
	})
	for i := range r.Suites {
		cs := r.Suites[i].Cases
		sort.SliceStable(cs, func(a, b int) bool {
			if cs[a].ClassName != cs[b].ClassName {
				return cs[a].ClassName < cs[b].ClassName
			}
			return cs[a].Name < cs[b].Name
		})
	}
}

// Index maps every case key to its case. Later duplicates win, which matches
// "the last observation is current" and is what diff wants after a merge.
func (r *Report) Index() map[Key]Case {
	idx := make(map[Key]Case, r.TotalCases())
	for _, s := range r.Suites {
		for _, c := range s.Cases {
			idx[KeyOf(s.Name, c)] = c
		}
	}
	return idx
}
