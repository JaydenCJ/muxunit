package algebra

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/JaydenCJ/muxunit/internal/model"
)

// Substitution is a sed-style regular-expression rewrite applied to case
// names, parsed from "/pattern/replacement/" (any delimiter character).
type Substitution struct {
	re   *regexp.Regexp
	repl string
	src  string
}

// ParseSubstitution parses a sed-style expression. The first character is
// the delimiter, so patterns containing "/" can use e.g. "|old|new|".
// Replacement may reference capture groups as $1, ${name}.
func ParseSubstitution(expr string) (*Substitution, error) {
	if len(expr) < 3 {
		return nil, fmt.Errorf("substitution %q: too short (want /pattern/replacement/)", expr)
	}
	delim := expr[0]
	body := expr[1:]
	if body[len(body)-1] != delim {
		return nil, fmt.Errorf("substitution %q: missing trailing delimiter %q", expr, string(delim))
	}
	body = body[:len(body)-1]
	parts := strings.SplitN(body, string(delim), 2)
	if len(parts) != 2 {
		return nil, fmt.Errorf("substitution %q: want /pattern/replacement/", expr)
	}
	re, err := regexp.Compile(parts[0])
	if err != nil {
		return nil, fmt.Errorf("substitution %q: %w", expr, err)
	}
	return &Substitution{re: re, repl: parts[1], src: expr}, nil
}

// Apply rewrites one name.
func (s *Substitution) Apply(name string) string {
	return s.re.ReplaceAllString(name, s.repl)
}

// String returns the original expression.
func (s *Substitution) String() string { return s.src }

// RewriteOptions is the set of structural transforms `muxunit rewrite`
// applies, in a fixed order: suite renames first, then prefix surgery,
// then case-name substitutions, then time/class scrubbing.
type RewriteOptions struct {
	// RenameSuite maps exact old suite names to new ones.
	RenameSuite map[string]string
	// TrimPrefix removes this prefix from every suite name (a no-op for
	// suites that don't carry it) — the classic shard-label cleanup.
	TrimPrefix string
	// AddPrefix prepends this to every suite name.
	AddPrefix string
	// Subs are applied to every case name, in order.
	Subs []*Substitution
	// SetClass overwrites every case's classname (empty string = leave as-is;
	// use ClearClass to blank them).
	SetClass string
	// ClearClass blanks every classname — useful before diffing reports from
	// runners that disagree on classname conventions.
	ClearClass bool
	// StripTimes zeroes every duration, making retried-shard output
	// byte-reproducible.
	StripTimes bool
}

// Rewrite returns a transformed copy of the report. Suites that end up with
// the same name after renaming remain separate entries; run the result
// through Merge to unify them.
func Rewrite(rep *model.Report, opt RewriteOptions) *model.Report {
	out := &model.Report{}
	for _, s := range rep.Suites {
		ns := s
		if renamed, ok := opt.RenameSuite[ns.Name]; ok {
			ns.Name = renamed
		}
		if opt.TrimPrefix != "" {
			ns.Name = strings.TrimPrefix(ns.Name, opt.TrimPrefix)
		}
		if opt.AddPrefix != "" {
			ns.Name = opt.AddPrefix + ns.Name
		}
		ns.Cases = make([]model.Case, len(s.Cases))
		for i, c := range s.Cases {
			for _, sub := range opt.Subs {
				c.Name = sub.Apply(c.Name)
			}
			if opt.ClearClass {
				c.ClassName = ""
			} else if opt.SetClass != "" {
				c.ClassName = opt.SetClass
			}
			if opt.StripTimes {
				c.Time = 0
			}
			ns.Cases[i] = c
		}
		out.Suites = append(out.Suites, ns)
	}
	return out
}
