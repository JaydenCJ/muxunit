// Tests for the test-ID glob dialect: segment-local *, cross-segment **,
// single-char ?, anchoring, and metacharacter safety.
package algebra

import "testing"

func TestGlobStarStaysWithinSegment(t *testing.T) {
	g := mustGlob(t, "api/*")
	if !g.Match("api/get") {
		t.Fatal("api/* should match api/get")
	}
	if g.Match("api/Auth/get") {
		t.Fatal("api/* must not cross the class segment")
	}
}

func TestGlobDoubleStarCrossesSegments(t *testing.T) {
	g := mustGlob(t, "api/**")
	for _, id := range []string{"api/get", "api/Auth/get", "api/a/b/c"} {
		if !g.Match(id) {
			t.Fatalf("api/** should match %q", id)
		}
	}
	if g.Match("web/get") {
		t.Fatal("api/** must not match another suite")
	}
}

func TestGlobQuestionMarkMatchesOneChar(t *testing.T) {
	g := mustGlob(t, "s/test?")
	if !g.Match("s/test1") || g.Match("s/test12") || g.Match("s/test") {
		t.Fatal("? must match exactly one character")
	}
	if g.Match("s/test/") {
		t.Fatal("? must not match the segment separator")
	}
}

func TestGlobIsAnchored(t *testing.T) {
	g := mustGlob(t, "get")
	if g.Match("api/get") || g.Match("getter") {
		t.Fatal("patterns are anchored at both ends")
	}
	if !g.Match("get") {
		t.Fatal("anchored pattern should match the exact ID")
	}
	if _, err := CompileGlob(""); err == nil {
		t.Fatal("empty pattern should be an error")
	}
}

func TestGlobEscapesRegexpMetacharacters(t *testing.T) {
	// Test names routinely contain dots, parens, brackets — they must be
	// literal, not regexp syntax.
	g := mustGlob(t, "s/TestFoo(sub.case)[0]")
	if !g.Match("s/TestFoo(sub.case)[0]") {
		t.Fatal("metacharacters must match literally")
	}
	if g.Match("s/TestFooXsubYcaseZ[0]") {
		t.Fatal("dot must not act as a regexp wildcard")
	}
}
