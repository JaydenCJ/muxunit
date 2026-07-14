// Package algebra implements the report operations muxunit exposes as verbs:
// merge, diff, filter, and rewrite. Everything here is pure — reports in,
// reports (or diff results) out — so every rule is unit-testable without
// touching XML, TAP, or the filesystem.
package algebra

import (
	"fmt"
	"strings"

	"github.com/JaydenCJ/muxunit/internal/model"
)

// DedupePolicy decides what happens when the same test key appears more than
// once across (or within) merged reports — the everyday reality of retried
// CI shards.
type DedupePolicy int

const (
	// KeepAll keeps every occurrence, duplicates included.
	KeepAll DedupePolicy = iota
	// KeepFirst keeps the first occurrence in input order.
	KeepFirst
	// KeepLast keeps the last occurrence in input order (retries win).
	KeepLast
	// PreferPass keeps the best outcome — a retried flake that eventually
	// passed counts as a pass. Ties resolve to the last occurrence.
	PreferPass
	// PreferFail keeps the worst outcome — any red run keeps the test red.
	// Ties resolve to the last occurrence.
	PreferFail
)

// ParseDedupePolicy maps the CLI token to a policy.
func ParseDedupePolicy(tok string) (DedupePolicy, error) {
	switch strings.ToLower(strings.TrimSpace(tok)) {
	case "all", "keep-all":
		return KeepAll, nil
	case "first":
		return KeepFirst, nil
	case "last":
		return KeepLast, nil
	case "prefer-pass":
		return PreferPass, nil
	case "prefer-fail":
		return PreferFail, nil
	}
	return KeepAll, fmt.Errorf("unknown dedupe policy %q (want all, first, last, prefer-pass, or prefer-fail)", tok)
}

// Merge combines any number of reports into one. Suites with the same name
// are unified; duplicate case keys are resolved by the policy. Suite
// properties keep the first occurrence of each property name. The result is
// sorted, so merge output is independent of shard arrival order.
func Merge(reports []*model.Report, policy DedupePolicy) *model.Report {
	type slot struct {
		c     model.Case
		count int
	}
	suiteIdx := map[string]int{}
	out := &model.Report{}
	caseIdx := map[model.Key]*slot{}

	for _, rep := range reports {
		for _, s := range rep.Suites {
			si, ok := suiteIdx[s.Name]
			if !ok {
				si = len(out.Suites)
				suiteIdx[s.Name] = si
				out.Suites = append(out.Suites, model.Suite{
					Name:      s.Name,
					Timestamp: s.Timestamp,
					Hostname:  s.Hostname,
				})
			}
			mergeProperties(&out.Suites[si], s.Properties)
			for _, c := range s.Cases {
				key := model.KeyOf(s.Name, c)
				existing, dup := caseIdx[key]
				if !dup || policy == KeepAll {
					out.Suites[si].Cases = append(out.Suites[si].Cases, c)
					if !dup {
						caseIdx[key] = &slot{c: c, count: 1}
					} else {
						existing.count++
					}
					continue
				}
				existing.count++
				switch policy {
				case KeepFirst:
					// nothing to do
				case KeepLast:
					existing.c = c
					replaceCase(&out.Suites[si], key, c)
				case PreferPass:
					if c.Status.Severity() <= existing.c.Status.Severity() {
						existing.c = c
						replaceCase(&out.Suites[si], key, c)
					}
				case PreferFail:
					if c.Status.Severity() >= existing.c.Status.Severity() {
						existing.c = c
						replaceCase(&out.Suites[si], key, c)
					}
				}
			}
		}
	}
	out.Sort()
	return out
}

// replaceCase swaps the stored case for key inside suite s.
func replaceCase(s *model.Suite, key model.Key, c model.Case) {
	for i := range s.Cases {
		if model.KeyOf(s.Name, s.Cases[i]) == key {
			s.Cases[i] = c
			return
		}
	}
}

// mergeProperties appends properties whose names are not present yet,
// preserving first-wins semantics across shards.
func mergeProperties(dst *model.Suite, props []model.Property) {
	seen := map[string]bool{}
	for _, p := range dst.Properties {
		seen[p.Name] = true
	}
	for _, p := range props {
		if !seen[p.Name] {
			dst.Properties = append(dst.Properties, p)
			seen[p.Name] = true
		}
	}
}
