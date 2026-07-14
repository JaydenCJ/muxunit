// Tests for merge: suite unification, dedupe policies, property merging,
// and order-independence — the properties that make sharded CI output safe
// to combine.
package algebra

import (
	"testing"

	"github.com/JaydenCJ/muxunit/internal/model"
)

func suite(name string, cases ...model.Case) model.Suite {
	return model.Suite{Name: name, Cases: cases}
}

func rep(suites ...model.Suite) *model.Report {
	return &model.Report{Suites: suites}
}

func kase(name string, st model.Status) model.Case {
	return model.Case{Name: name, Status: st}
}

func TestMergeUnifiesSameNamedSuitesOnly(t *testing.T) {
	m := Merge([]*model.Report{
		rep(suite("api", kase("a", model.Pass))),
		rep(suite("api", kase("b", model.Fail))),
		rep(suite("web", kase("a", model.Pass))),
	}, KeepAll)
	if len(m.Suites) != 2 {
		t.Fatalf("suites = %d, want 2 — same case name in a different suite is a different test", len(m.Suites))
	}
	if m.Suites[0].Name != "api" || len(m.Suites[0].Cases) != 2 {
		t.Fatalf("api suite not unified: %+v", m.Suites[0])
	}
}

func TestMergeKeepAllRetainsDuplicates(t *testing.T) {
	m := Merge([]*model.Report{
		rep(suite("s", kase("dup", model.Fail))),
		rep(suite("s", kase("dup", model.Pass))),
	}, KeepAll)
	if len(m.Suites[0].Cases) != 2 {
		t.Fatalf("keep-all must retain both runs, got %d", len(m.Suites[0].Cases))
	}
}

func TestMergeDedupePolicies(t *testing.T) {
	// The retry workflow: the same test key appears in several shards with
	// different outcomes; each policy picks a different survivor.
	shards := func(statuses ...model.Status) []*model.Report {
		out := make([]*model.Report, len(statuses))
		for i, st := range statuses {
			out[i] = rep(suite("s", model.Case{Name: "dup", Status: st, Message: string(rune('1' + i))}))
		}
		return out
	}
	tests := []struct {
		name       string
		policy     DedupePolicy
		statuses   []model.Status
		wantStatus model.Status
		wantMsg    string
	}{
		{"first keeps run 1", KeepFirst, []model.Status{model.Fail, model.Pass}, model.Fail, "1"},
		{"last keeps run 2", KeepLast, []model.Status{model.Fail, model.Pass}, model.Pass, "2"},
		{"prefer-pass turns a retried flake green", PreferPass, []model.Status{model.Fail, model.Pass}, model.Pass, "2"},
		{"prefer-fail keeps any red run", PreferFail, []model.Status{model.Pass, model.Error, model.Pass}, model.Error, "2"},
		// Severity ranks Skip below Pass: prefer-pass keeps the least
		// severe outcome (skip), prefer-fail ranks pass above skip.
		{"prefer-pass ranks skip below pass", PreferPass, []model.Status{model.Pass, model.Skip}, model.Skip, "2"},
		{"prefer-fail ranks pass above skip", PreferFail, []model.Status{model.Skip, model.Pass}, model.Pass, "2"},
	}
	for _, tc := range tests {
		m := Merge(shards(tc.statuses...), tc.policy)
		if len(m.Suites[0].Cases) != 1 {
			t.Fatalf("%s: duplicate not collapsed: %+v", tc.name, m.Suites[0].Cases)
		}
		c := m.Suites[0].Cases[0]
		if c.Status != tc.wantStatus || c.Message != tc.wantMsg {
			t.Fatalf("%s: kept %v/%q, want %v/%q", tc.name, c.Status, c.Message, tc.wantStatus, tc.wantMsg)
		}
	}
}

func TestMergeOutputIndependentOfShardOrder(t *testing.T) {
	a := rep(suite("zeta", kase("z", model.Pass)), suite("api", kase("b", model.Fail)))
	b := rep(suite("api", kase("a", model.Pass)))
	ab := Merge([]*model.Report{a, b}, KeepAll)
	ba := Merge([]*model.Report{b, a}, KeepAll)
	if len(ab.Suites) != len(ba.Suites) {
		t.Fatal("suite counts differ by input order")
	}
	for i := range ab.Suites {
		if ab.Suites[i].Name != ba.Suites[i].Name {
			t.Fatalf("suite order differs: %q vs %q", ab.Suites[i].Name, ba.Suites[i].Name)
		}
		if len(ab.Suites[i].Cases) != len(ba.Suites[i].Cases) {
			t.Fatal("case counts differ by input order")
		}
		for j := range ab.Suites[i].Cases {
			if ab.Suites[i].Cases[j].Name != ba.Suites[i].Cases[j].Name {
				t.Fatal("case order differs by input order")
			}
		}
	}
	// Merge with a single input is the canonical normalize/convert path
	// used by rewrite; it must sort without losing anything.
	one := Merge([]*model.Report{rep(suite("s", kase("b", model.Fail), kase("a", model.Pass)))}, KeepAll)
	if len(one.Suites[0].Cases) != 2 || one.Suites[0].Cases[0].Name != "a" {
		t.Fatalf("normalize failed: %+v", one.Suites[0].Cases)
	}
}

func TestMergePropertiesFirstWins(t *testing.T) {
	s1 := suite("s", kase("a", model.Pass))
	s1.Properties = []model.Property{{Name: "shard", Value: "1"}, {Name: "os", Value: "linux"}}
	s2 := suite("s", kase("b", model.Pass))
	s2.Properties = []model.Property{{Name: "shard", Value: "2"}, {Name: "arch", Value: "arm64"}}
	m := Merge([]*model.Report{rep(s1), rep(s2)}, KeepAll)
	props := m.Suites[0].Properties
	if len(props) != 3 {
		t.Fatalf("props = %+v, want 3 entries", props)
	}
	if props[0].Name != "shard" || props[0].Value != "1" {
		t.Fatalf("first shard value must win: %+v", props[0])
	}
}

func TestParseDedupePolicyTokens(t *testing.T) {
	good := map[string]DedupePolicy{
		"all": KeepAll, "first": KeepFirst, "last": KeepLast,
		"prefer-pass": PreferPass, "PREFER-FAIL": PreferFail,
	}
	for tok, want := range good {
		got, err := ParseDedupePolicy(tok)
		if err != nil || got != want {
			t.Fatalf("ParseDedupePolicy(%q) = %v, %v", tok, got, err)
		}
	}
	if _, err := ParseDedupePolicy("newest"); err == nil {
		t.Fatal("expected error for unknown policy")
	}
}
