package algebra

import "github.com/JaydenCJ/muxunit/internal/model"

// FilterOptions selects cases by status and/or by glob patterns over the
// test ID ("suite/class/name", empty segments collapsed).
type FilterOptions struct {
	// Statuses keeps only cases with one of these outcomes. Nil means all.
	Statuses map[model.Status]bool
	// Patterns keeps cases whose ID matches ANY pattern. Nil means all.
	Patterns []*Glob
	// Invert flips the whole selection: keep exactly what would be dropped.
	Invert bool
}

// Filter returns a new report containing only the selected cases. Suites
// left with zero cases are dropped entirely; suite metadata is preserved
// for suites that survive.
func Filter(rep *model.Report, opt FilterOptions) *model.Report {
	out := &model.Report{}
	for _, s := range rep.Suites {
		kept := model.Suite{
			Name:       s.Name,
			Timestamp:  s.Timestamp,
			Hostname:   s.Hostname,
			Properties: s.Properties,
		}
		for _, c := range s.Cases {
			if opt.selects(s.Name, c) != opt.Invert {
				kept.Cases = append(kept.Cases, c)
			}
		}
		if len(kept.Cases) > 0 {
			out.Suites = append(out.Suites, kept)
		}
	}
	return out
}

// selects applies the positive selection (before inversion).
func (opt FilterOptions) selects(suiteName string, c model.Case) bool {
	if opt.Statuses != nil && !opt.Statuses[c.Status] {
		return false
	}
	if len(opt.Patterns) > 0 {
		id := model.KeyOf(suiteName, c).String()
		for _, g := range opt.Patterns {
			if g.Match(id) {
				return true
			}
		}
		return false
	}
	return true
}
