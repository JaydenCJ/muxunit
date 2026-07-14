// Tests for filter: status selection, glob patterns over test IDs,
// inversion, and empty-suite pruning.
package algebra

import (
	"testing"

	"github.com/JaydenCJ/muxunit/internal/model"
)

func mustGlob(t *testing.T, pattern string) *Glob {
	t.Helper()
	g, err := CompileGlob(pattern)
	if err != nil {
		t.Fatalf("CompileGlob(%q): %v", pattern, err)
	}
	return g
}

func statuses(set ...model.Status) map[model.Status]bool {
	m := map[model.Status]bool{}
	for _, s := range set {
		m[s] = true
	}
	return m
}

func mixed() *model.Report {
	return rep(
		suite("api", kase("ok1", model.Pass), kase("bad1", model.Fail), kase("err1", model.Error)),
		suite("web", kase("ok2", model.Pass), kase("skip1", model.Skip)),
	)
}

func TestFilterByStatusKeepsOnlyRed(t *testing.T) {
	out := Filter(mixed(), FilterOptions{Statuses: statuses(model.Fail, model.Error)})
	if len(out.Suites) != 1 || out.Suites[0].Name != "api" {
		t.Fatalf("suites wrong: %+v", out.Suites)
	}
	if len(out.Suites[0].Cases) != 2 {
		t.Fatalf("cases = %d, want 2 red", len(out.Suites[0].Cases))
	}
}

func TestFilterDropsEmptiedSuites(t *testing.T) {
	// "web" has no failing cases; a filtered report must not carry an
	// empty husk that would render as a 0-test suite downstream.
	out := Filter(mixed(), FilterOptions{Statuses: statuses(model.Fail)})
	if len(out.Suites) != 1 {
		t.Fatalf("emptied suite not dropped: %+v", out.Suites)
	}
}

func TestFilterByGlobOnTestID(t *testing.T) {
	out := Filter(mixed(), FilterOptions{Patterns: []*Glob{mustGlob(t, "api/*")}})
	if len(out.Suites) != 1 || len(out.Suites[0].Cases) != 3 {
		t.Fatalf("glob selection wrong: %+v", out.Suites)
	}
}

func TestFilterGlobsUnionAndStatusIntersects(t *testing.T) {
	// Several --match patterns are a union...
	out := Filter(mixed(), FilterOptions{Patterns: []*Glob{
		mustGlob(t, "api/ok*"), mustGlob(t, "web/skip*"),
	}})
	if out.TotalCases() != 2 {
		t.Fatalf("union should keep 2 cases, got %d", out.TotalCases())
	}
	// ...while --status and --match intersect.
	out = Filter(mixed(), FilterOptions{
		Statuses: statuses(model.Pass),
		Patterns: []*Glob{mustGlob(t, "**")},
	})
	if out.TotalCases() != 2 {
		t.Fatalf("status∧glob should keep the 2 passes, got %d", out.TotalCases())
	}
}

func TestFilterInvertFlipsSelection(t *testing.T) {
	// --invert on --only-failed gives "everything except the red tests" —
	// the quarantine workflow.
	out := Filter(mixed(), FilterOptions{
		Statuses: statuses(model.Fail, model.Error),
		Invert:   true,
	})
	if out.TotalCases() != 3 {
		t.Fatalf("invert should keep the 3 non-red cases, got %d", out.TotalCases())
	}
	for _, s := range out.Suites {
		for _, c := range s.Cases {
			if c.Status == model.Fail || c.Status == model.Error {
				t.Fatalf("red case survived inverted filter: %+v", c)
			}
		}
	}
}

func TestFilterPreservesMetadataAndIdentity(t *testing.T) {
	r := mixed()
	r.Suites[0].Hostname = "ci-7"
	r.Suites[0].Properties = []model.Property{{Name: "shard", Value: "3"}}
	out := Filter(r, FilterOptions{Statuses: statuses(model.Fail)})
	if out.Suites[0].Hostname != "ci-7" || len(out.Suites[0].Properties) != 1 {
		t.Fatalf("metadata lost: %+v", out.Suites[0])
	}
	// And a filter with no options is the identity.
	all := Filter(mixed(), FilterOptions{})
	if all.TotalCases() != mixed().TotalCases() {
		t.Fatal("empty filter must be identity")
	}
}
