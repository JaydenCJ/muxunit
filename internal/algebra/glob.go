package algebra

import (
	"fmt"
	"regexp"
	"strings"
)

// Glob matches test IDs ("suite/class/name") with a familiar dialect:
// `*` matches within a segment, `**` crosses segment boundaries, `?` matches
// one character. Matching is anchored at both ends.
type Glob struct {
	src string
	re  *regexp.Regexp
}

// CompileGlob translates the pattern into an anchored regular expression.
func CompileGlob(pattern string) (*Glob, error) {
	if pattern == "" {
		return nil, fmt.Errorf("empty glob pattern")
	}
	var b strings.Builder
	b.WriteString("^")
	i := 0
	for i < len(pattern) {
		switch c := pattern[i]; c {
		case '*':
			if i+1 < len(pattern) && pattern[i+1] == '*' {
				b.WriteString(".*")
				i += 2
			} else {
				b.WriteString("[^/]*")
				i++
			}
		case '?':
			b.WriteString("[^/]")
			i++
		default:
			b.WriteString(regexp.QuoteMeta(string(c)))
			i++
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		return nil, fmt.Errorf("glob %q: %w", pattern, err)
	}
	return &Glob{src: pattern, re: re}, nil
}

// Match reports whether the test ID matches the pattern.
func (g *Glob) Match(id string) bool { return g.re.MatchString(id) }

// String returns the original pattern text.
func (g *Glob) String() string { return g.src }
