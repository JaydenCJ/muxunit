// Tests for the canonical report model: status parsing, identity keys,
// counting, and the deterministic sort that makes merges order-independent.
package model

import "testing"

func TestStatusParsingRoundTripAliasesAndErrors(t *testing.T) {
	for _, s := range []Status{Pass, Fail, Error, Skip} {
		got, err := ParseStatus(s.String())
		if err != nil || got != s {
			t.Fatalf("round trip %v: got %v, %v", s, got, err)
		}
	}
	// CI wrappers pass whatever their vocabulary uses; muxunit should not
	// be pedantic about "failed" vs "fail".
	aliases := map[string]Status{
		"passed": Pass, "OK": Pass, "Failed": Fail, "failure": Fail,
		"errored": Error, "skipped": Skip, " skip ": Skip,
	}
	for tok, want := range aliases {
		got, err := ParseStatus(tok)
		if err != nil || got != want {
			t.Fatalf("ParseStatus(%q) = %v, %v; want %v", tok, got, err, want)
		}
	}
	if _, err := ParseStatus("flaky"); err == nil {
		t.Fatal("expected an error for unknown status token")
	}
}

func TestSeverityOrdersSkipPassFailError(t *testing.T) {
	// PreferFail/PreferPass dedupe depends on this exact ordering.
	if !(Skip.Severity() < Pass.Severity() &&
		Pass.Severity() < Fail.Severity() &&
		Fail.Severity() < Error.Severity()) {
		t.Fatal("severity order must be Skip < Pass < Fail < Error")
	}
}

func TestKeyStringCollapsesEmptySegments(t *testing.T) {
	if got := (Key{Suite: "api", Name: "get"}).String(); got != "api/get" {
		t.Fatalf("got %q, want api/get", got)
	}
	if got := (Key{Suite: "api", Class: "Auth", Name: "get"}).String(); got != "api/Auth/get" {
		t.Fatalf("got %q, want api/Auth/get", got)
	}
	if got := (Key{Name: "solo"}).String(); got != "solo" {
		t.Fatalf("got %q, want solo", got)
	}
}

func TestCountsTallyEveryStatusTimeAndTotals(t *testing.T) {
	rep := &Report{Suites: []Suite{
		{
			Name: "s",
			Cases: []Case{
				{Name: "a", Status: Pass, Time: 1.5},
				{Name: "b", Status: Fail, Time: 0.5},
				{Name: "c", Status: Error},
				{Name: "d", Status: Skip},
			},
		},
		{Name: "t", Cases: []Case{{Name: "e", Status: Pass}}},
	}}
	c := rep.Counts()
	if c.Total != 5 || c.Passed != 2 || c.Failed != 1 || c.Errored != 1 || c.Skipped != 1 {
		t.Fatalf("bad counts: %+v", c)
	}
	if c.Time != 2.0 {
		t.Fatalf("time = %v, want 2.0", c.Time)
	}
	if got := rep.TotalCases(); got != 5 {
		t.Fatalf("TotalCases = %d, want 5", got)
	}
}

func TestSortOrdersSuitesAndCasesDeterministically(t *testing.T) {
	rep := &Report{Suites: []Suite{
		{Name: "zeta", Cases: []Case{{ClassName: "B", Name: "x"}, {ClassName: "A", Name: "y"}}},
		{Name: "alpha", Cases: []Case{{Name: "b"}, {Name: "a"}}},
	}}
	rep.Sort()
	if rep.Suites[0].Name != "alpha" || rep.Suites[1].Name != "zeta" {
		t.Fatalf("suites not sorted: %q, %q", rep.Suites[0].Name, rep.Suites[1].Name)
	}
	if rep.Suites[0].Cases[0].Name != "a" {
		t.Fatalf("cases not sorted by name: got %q", rep.Suites[0].Cases[0].Name)
	}
	if rep.Suites[1].Cases[0].ClassName != "A" {
		t.Fatalf("cases not sorted by classname first: got %q", rep.Suites[1].Cases[0].ClassName)
	}
}

func TestSortIsStableForDuplicateKeys(t *testing.T) {
	// KeepAll merges legitimately contain duplicates; their relative input
	// order (first run before retry) must survive sorting.
	rep := &Report{Suites: []Suite{{
		Name: "s",
		Cases: []Case{
			{Name: "dup", Status: Fail, Message: "first"},
			{Name: "dup", Status: Pass, Message: "second"},
		},
	}}}
	rep.Sort()
	if rep.Suites[0].Cases[0].Message != "first" {
		t.Fatal("stable sort must keep the first duplicate first")
	}
}

func TestIndexLastDuplicateWins(t *testing.T) {
	rep := &Report{Suites: []Suite{{
		Name: "s",
		Cases: []Case{
			{Name: "dup", Status: Fail},
			{Name: "dup", Status: Pass},
		},
	}}}
	idx := rep.Index()
	if got := idx[Key{Suite: "s", Name: "dup"}]; got.Status != Pass {
		t.Fatalf("last duplicate should win, got %v", got.Status)
	}
}
