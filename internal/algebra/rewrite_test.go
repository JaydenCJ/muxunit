// Tests for rewrite: suite renaming, prefix surgery, sed-style case-name
// substitutions, classname scrubbing, and time stripping.
package algebra

import (
	"testing"

	"github.com/JaydenCJ/muxunit/internal/model"
)

func TestRewriteRenameSuiteExactMatch(t *testing.T) {
	out := Rewrite(rep(suite("old-name", kase("a", model.Pass)), suite("other", kase("b", model.Pass))),
		RewriteOptions{RenameSuite: map[string]string{"old-name": "new-name"}})
	if out.Suites[0].Name != "new-name" {
		t.Fatalf("rename missed: %q", out.Suites[0].Name)
	}
	if out.Suites[1].Name != "other" {
		t.Fatalf("rename must be exact-match only: %q", out.Suites[1].Name)
	}
}

func TestRewritePrefixSurgeryOnSuiteNames(t *testing.T) {
	// The canonical use case: shards named "shard-1-api", "shard-2-api"
	// collapse to "api" and can then be merged.
	out := Rewrite(rep(suite("shard-1-api", kase("a", model.Pass))),
		RewriteOptions{TrimPrefix: "shard-1-"})
	if out.Suites[0].Name != "api" {
		t.Fatalf("trim failed: %q", out.Suites[0].Name)
	}
	// No-op for suites that don't carry the prefix.
	out = Rewrite(rep(suite("api", kase("a", model.Pass))),
		RewriteOptions{TrimPrefix: "shard-1-"})
	if out.Suites[0].Name != "api" {
		t.Fatalf("trim must be a no-op without the prefix: %q", out.Suites[0].Name)
	}
	// And --add-prefix namespaces suites for cross-repo aggregation.
	out = Rewrite(rep(suite("api", kase("a", model.Pass))),
		RewriteOptions{AddPrefix: "backend/"})
	if out.Suites[0].Name != "backend/api" {
		t.Fatalf("prefix failed: %q", out.Suites[0].Name)
	}
}

func TestRewriteSubsRewriteCaseNamesInOrder(t *testing.T) {
	sub, err := ParseSubstitution(`/^test_/it /`)
	if err != nil {
		t.Fatal(err)
	}
	out := Rewrite(rep(suite("s", kase("test_login", model.Pass))),
		RewriteOptions{Subs: []*Substitution{sub}})
	if out.Suites[0].Cases[0].Name != "it login" {
		t.Fatalf("sub failed: %q", out.Suites[0].Cases[0].Name)
	}
	// Substitutions chain in flag order: a->b then b->c yields c.
	s1, _ := ParseSubstitution("/a/b/")
	s2, _ := ParseSubstitution("/b/c/")
	out = Rewrite(rep(suite("s", kase("a", model.Pass))),
		RewriteOptions{Subs: []*Substitution{s1, s2}})
	if got := out.Suites[0].Cases[0].Name; got != "c" {
		t.Fatalf("order matters: a->b->c, got %q", got)
	}
}

func TestRewriteSubCaptureGroupsAndAlternateDelimiters(t *testing.T) {
	sub, err := ParseSubstitution(`/^(\w+)\[(\d+)\]$/$1 case $2/`)
	if err != nil {
		t.Fatal(err)
	}
	out := Rewrite(rep(suite("s", kase("param[3]", model.Pass))),
		RewriteOptions{Subs: []*Substitution{sub}})
	if got := out.Suites[0].Cases[0].Name; got != "param case 3" {
		t.Fatalf("capture groups broken: %q", got)
	}
	// Names with slashes need a different delimiter, exactly like sed.
	sub, err = ParseSubstitution(`|api/v1|api/v2|`)
	if err != nil {
		t.Fatal(err)
	}
	out = Rewrite(rep(suite("s", kase("api/v1/users", model.Pass))),
		RewriteOptions{Subs: []*Substitution{sub}})
	if got := out.Suites[0].Cases[0].Name; got != "api/v2/users" {
		t.Fatalf("alternate delimiter broken: %q", got)
	}
}

func TestParseSubstitutionRejectsMalformedExpressions(t *testing.T) {
	for _, expr := range []string{"", "/", "/a/", "/a/b", "/[invalid/x/"} {
		if _, err := ParseSubstitution(expr); err == nil {
			t.Fatalf("expected error for %q", expr)
		}
	}
}

func TestRewriteScrubsClassnamesAndTimes(t *testing.T) {
	in := rep(suite("s", model.Case{Name: "a", ClassName: "com.example.T", Time: 1.25, Status: model.Pass}))
	out := Rewrite(in, RewriteOptions{ClearClass: true, StripTimes: true})
	c := out.Suites[0].Cases[0]
	if c.ClassName != "" || c.Time != 0 {
		t.Fatalf("scrub failed: %+v", c)
	}
	out = Rewrite(in, RewriteOptions{SetClass: "canonical"})
	if out.Suites[0].Cases[0].ClassName != "canonical" {
		t.Fatalf("classname not set: %q", out.Suites[0].Cases[0].ClassName)
	}
}

func TestRewriteDoesNotMutateInput(t *testing.T) {
	in := rep(suite("s", model.Case{Name: "a", ClassName: "K", Time: 1, Status: model.Pass}))
	Rewrite(in, RewriteOptions{AddPrefix: "x/", ClearClass: true, StripTimes: true})
	c := in.Suites[0].Cases[0]
	if in.Suites[0].Name != "s" || c.ClassName != "K" || c.Time != 1 {
		t.Fatalf("input mutated: %+v", in.Suites[0])
	}
}
