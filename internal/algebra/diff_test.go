// Tests for diff: bucket classification, the regression gate, and
// order-insensitivity between report versions.
package algebra

import (
	"testing"

	"github.com/JaydenCJ/muxunit/internal/model"
)

func TestDiffIdenticalGreenReportsIsEmptyAndOrderInsensitive(t *testing.T) {
	r := rep(suite("s", kase("a", model.Pass), kase("b", model.Skip)))
	d := Diff(r, r)
	if d.Total() != 0 || d.HasRegressions() {
		t.Fatalf("self-diff must be empty and green, got %d changes", d.Total())
	}
	// Suite/case order must never register as a change.
	oldR := rep(suite("b", kase("x", model.Pass)), suite("a", kase("y", model.Pass)))
	newR := rep(suite("a", kase("y", model.Pass)), suite("b", kase("x", model.Pass)))
	if d := Diff(oldR, newR); d.Total() != 0 {
		t.Fatalf("suite order must not create changes: %+v", d)
	}
}

func TestDiffUnchangedRedIsListedAsStillFailing(t *testing.T) {
	// Known-broken tests stay visible in every diff so nobody mistakes
	// "no new failures" for "everything passes" — but they never trip
	// the regression gate.
	r := rep(suite("s", kase("broken", model.Fail)))
	d := Diff(r, r)
	if len(d.StillFailing) != 1 || d.StillFailing[0].ID != "s/broken" {
		t.Fatalf("still-failing wrong: %+v", d.StillFailing)
	}
	if d.HasRegressions() {
		t.Fatal("still-failing must not be a regression")
	}
}

func TestDiffStatusTransitionBuckets(t *testing.T) {
	// Every status transition lands in exactly one bucket, and only new
	// red trips the gate. fail→error stays "still failing": changing
	// failure flavor must not re-trip a gate that already knows the test
	// is broken.
	tests := []struct {
		name           string
		from, to       model.Status
		wantKind       ChangeKind
		wantRegression bool
	}{
		{"pass to fail", model.Pass, model.Fail, NewFailure, true},
		{"skip to error", model.Skip, model.Error, NewFailure, true},
		{"fail to pass", model.Fail, model.Pass, Fixed, false},
		{"fail to error", model.Fail, model.Error, StillFailing, false},
		{"error to skip", model.Error, model.Skip, StatusChanged, false},
		{"pass to skip", model.Pass, model.Skip, StatusChanged, false},
	}
	for _, tc := range tests {
		d := Diff(rep(suite("s", kase("a", tc.from))), rep(suite("s", kase("a", tc.to))))
		if d.Total() != 1 {
			t.Fatalf("%s: want exactly one change, got %+v", tc.name, d)
		}
		all := append(append(append(append(d.NewFailures, d.Fixed...), d.StillFailing...), d.StatusChanged...), d.Added...)
		if all[0].Kind != tc.wantKind || all[0].From != tc.from || all[0].To != tc.to {
			t.Fatalf("%s: got %+v, want kind %s", tc.name, all[0], tc.wantKind)
		}
		if d.HasRegressions() != tc.wantRegression {
			t.Fatalf("%s: regression = %v, want %v", tc.name, d.HasRegressions(), tc.wantRegression)
		}
	}
}

func TestDiffAddedRemovedAndCounts(t *testing.T) {
	oldR := rep(suite("s", kase("gone", model.Pass), kase("stays", model.Pass)))
	newR := rep(suite("s", kase("stays", model.Pass), kase("fresh", model.Pass), kase("red", model.Fail)))
	d := Diff(oldR, newR)
	if len(d.Added) != 2 || d.Added[0].ID != "s/fresh" || d.Added[1].ID != "s/red" {
		t.Fatalf("added wrong: %+v", d.Added)
	}
	if len(d.Removed) != 1 || d.Removed[0].ID != "s/gone" {
		t.Fatalf("removed wrong: %+v", d.Removed)
	}
	if d.OldCounts.Total != 2 || d.NewCounts.Total != 3 || d.NewCounts.Failed != 1 {
		t.Fatalf("counts wrong: old %+v new %+v", d.OldCounts, d.NewCounts)
	}
}

func TestDiffAddedRedTestIsARegression(t *testing.T) {
	// A brand-new test that lands already failing must trip the gate even
	// though there is no old status to compare against — while adding and
	// removing green tests must not.
	green := Diff(rep(suite("s", kase("a", model.Pass))),
		rep(suite("s", kase("b", model.Pass))))
	if green.HasRegressions() {
		t.Fatal("adding/removing green tests is not a regression")
	}
	d := Diff(rep(suite("s", kase("a", model.Pass))),
		rep(suite("s", kase("a", model.Pass), kase("new", model.Fail))))
	if len(d.NewFailures) != 0 || len(d.Added) != 1 {
		t.Fatalf("added-red misbucketed: %+v", d)
	}
	if !d.HasRegressions() {
		t.Fatal("an added failing test is a regression")
	}
}

func TestDiffChangesAreSortedByID(t *testing.T) {
	oldR := rep(suite("s", kase("z", model.Pass), kase("a", model.Pass), kase("m", model.Pass)))
	newR := rep(suite("s", kase("z", model.Fail), kase("a", model.Fail), kase("m", model.Fail)))
	d := Diff(oldR, newR)
	if len(d.NewFailures) != 3 {
		t.Fatalf("want 3 new failures, got %d", len(d.NewFailures))
	}
	if d.NewFailures[0].ID != "s/a" || d.NewFailures[2].ID != "s/z" {
		t.Fatalf("changes not sorted: %+v", d.NewFailures)
	}
}

func TestDiffMessageUsesFirstLineOfRedCase(t *testing.T) {
	// JUnit failures often have no message attribute, only a body; the
	// diff line should still say something useful — but only line one.
	newR := rep(suite("s", model.Case{Name: "a", Status: model.Fail, Detail: "assert failed\nstack line 2"}))
	d := Diff(rep(suite("s", kase("a", model.Pass))), newR)
	if d.NewFailures[0].Message != "assert failed" {
		t.Fatalf("message = %q", d.NewFailures[0].Message)
	}
	// An explicit message wins over the detail body.
	newR2 := rep(suite("s", model.Case{Name: "a", Status: model.Fail, Message: "boom", Detail: "long trace"}))
	d2 := Diff(rep(suite("s", kase("a", model.Pass))), newR2)
	if d2.NewFailures[0].Message != "boom" {
		t.Fatalf("message = %q, want boom", d2.NewFailures[0].Message)
	}
}
