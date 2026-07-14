package algebra

import (
	"sort"

	"github.com/JaydenCJ/muxunit/internal/model"
)

// ChangeKind classifies one entry in a report diff.
type ChangeKind string

const (
	// NewFailure was passing (or skipped) before and is now red.
	NewFailure ChangeKind = "new-failure"
	// Fixed was red before and now passes.
	Fixed ChangeKind = "fixed"
	// StillFailing was red before and is still red (possibly fail↔error).
	StillFailing ChangeKind = "still-failing"
	// StatusChanged covers the remaining transitions (pass↔skip).
	StatusChanged ChangeKind = "status-changed"
	// Added exists only in the new report.
	Added ChangeKind = "added"
	// Removed exists only in the old report.
	Removed ChangeKind = "removed"
)

// Change is one test whose presence or outcome differs between two reports.
type Change struct {
	Key  model.Key    `json:"-"`
	ID   string       `json:"id"`
	Kind ChangeKind   `json:"kind"`
	From model.Status `json:"-"`
	To   model.Status `json:"-"`
	// Message carries the new failure message for red entries.
	Message string `json:"message,omitempty"`
}

// DiffResult groups every change between an old and a new report.
type DiffResult struct {
	NewFailures   []Change `json:"new_failures"`
	Fixed         []Change `json:"fixed"`
	StillFailing  []Change `json:"still_failing"`
	StatusChanged []Change `json:"status_changed"`
	Added         []Change `json:"added"`
	Removed       []Change `json:"removed"`
	OldCounts     model.Counts
	NewCounts     model.Counts
}

// Total returns the number of recorded changes across all buckets.
func (d DiffResult) Total() int {
	return len(d.NewFailures) + len(d.Fixed) + len(d.StillFailing) +
		len(d.StatusChanged) + len(d.Added) + len(d.Removed)
}

// HasRegressions reports whether the diff contains anything a gate should
// fail on: a new failure, or an added test that is already red.
func (d DiffResult) HasRegressions() bool {
	if len(d.NewFailures) > 0 {
		return true
	}
	for _, c := range d.Added {
		if c.To == model.Fail || c.To == model.Error {
			return true
		}
	}
	return false
}

func red(s model.Status) bool { return s == model.Fail || s == model.Error }

// Diff compares two reports case-by-case using the (suite, class, name) key.
// It is order-insensitive: shard arrival order never produces a change.
func Diff(oldRep, newRep *model.Report) DiffResult {
	oldIdx := oldRep.Index()
	newIdx := newRep.Index()

	keys := make([]model.Key, 0, len(oldIdx)+len(newIdx))
	for k := range oldIdx {
		keys = append(keys, k)
	}
	for k := range newIdx {
		if _, ok := oldIdx[k]; !ok {
			keys = append(keys, k)
		}
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].String() < keys[j].String() })

	var d DiffResult
	d.OldCounts = oldRep.Counts()
	d.NewCounts = newRep.Counts()

	for _, k := range keys {
		oc, inOld := oldIdx[k]
		nc, inNew := newIdx[k]
		switch {
		case inOld && !inNew:
			d.Removed = append(d.Removed, Change{
				Key: k, ID: k.String(), Kind: Removed, From: oc.Status, To: oc.Status,
			})
		case !inOld && inNew:
			d.Added = append(d.Added, Change{
				Key: k, ID: k.String(), Kind: Added, From: nc.Status, To: nc.Status,
				Message: redMessage(nc),
			})
		default:
			ch := Change{Key: k, ID: k.String(), From: oc.Status, To: nc.Status}
			switch {
			case red(nc.Status) && !red(oc.Status):
				ch.Kind = NewFailure
				ch.Message = redMessage(nc)
				d.NewFailures = append(d.NewFailures, ch)
			case red(oc.Status) && !red(nc.Status) && nc.Status == model.Pass:
				ch.Kind = Fixed
				d.Fixed = append(d.Fixed, ch)
			case red(oc.Status) && red(nc.Status):
				ch.Kind = StillFailing
				ch.Message = redMessage(nc)
				d.StillFailing = append(d.StillFailing, ch)
			case oc.Status != nc.Status:
				ch.Kind = StatusChanged
				d.StatusChanged = append(d.StatusChanged, ch)
			}
		}
	}
	return d
}

// redMessage extracts a one-line reason from a red case, if any.
func redMessage(c model.Case) string {
	if !red(c.Status) {
		return ""
	}
	if c.Message != "" {
		return firstLine(c.Message)
	}
	return firstLine(c.Detail)
}

func firstLine(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			return s[:i]
		}
	}
	return s
}
