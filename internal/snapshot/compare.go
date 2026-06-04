package snapshot

import (
	"fmt"
	"sort"
	"strings"
)

// Status of a section within a diff.
type Status string

const (
	StatusAdded   Status = "added"   // present only in left
	StatusRemoved Status = "removed" // present only in right
	StatusChanged Status = "changed"
	StatusEqual   Status = "equal"
)

type ItemsDiff struct {
	Added   []string
	Removed []string
	Common  []string
}

type PairChange struct{ Left, Right string }

type PairsDiff struct {
	Added   map[string]string
	Removed map[string]string
	Changed map[string]PairChange
	Common  map[string]string
}

type ContentDiff struct {
	Left    *string
	Right   *string
	Changed bool
}

type SectionDiff struct {
	Status  Status
	Items   ItemsDiff
	Pairs   PairsDiff
	Content ContentDiff
}

// SnapshotDiff maps a section id to its diff.
type SnapshotDiff map[string]SectionDiff

// Compare diffs two snapshots. ORIENTATION: the first argument is "left"; what
// exists only in left is "added", only in right is "removed". (Callers pass the
// newer snapshot as left, so "added" reads as "added since the older one".)
func Compare(left, right Snapshot) SnapshotDiff {
	diff := make(SnapshotDiff, len(left)+len(right))
	for name := range left {
		l := left[name]
		if r, ok := right[name]; ok {
			d := sectionDiff(&l, &r)
			d.Status = computeStatus(d)
			diff[name] = d
		} else {
			d := sectionDiff(&l, nil)
			d.Status = StatusAdded
			diff[name] = d
		}
	}
	for name := range right {
		if _, ok := left[name]; !ok {
			r := right[name]
			d := sectionDiff(nil, &r)
			d.Status = StatusRemoved
			diff[name] = d
		}
	}
	return diff
}

func sectionDiff(left, right *Section) SectionDiff {
	return SectionDiff{
		Items:   diffItems(left, right),
		Pairs:   diffPairs(left, right),
		Content: diffContent(left, right),
	}
}

func rawsOf(s *Section) []string {
	if s == nil {
		return nil
	}
	out := make([]string, len(s.Items))
	for i, it := range s.Items {
		out[i] = it.Raw
	}
	return out
}

func toSet(xs []string) map[string]struct{} {
	m := make(map[string]struct{}, len(xs))
	for _, x := range xs {
		m[x] = struct{}{}
	}
	return m
}

// diffItems keys on the item's raw string; order follows left for added/common,
// right for removed (matching the source-of-truth iteration order).
func diffItems(left, right *Section) ItemsDiff {
	lraws, rraws := rawsOf(left), rawsOf(right)
	lset, rset := toSet(lraws), toSet(rraws)
	d := ItemsDiff{Added: []string{}, Removed: []string{}, Common: []string{}}
	for _, r := range lraws {
		if _, ok := rset[r]; ok {
			d.Common = append(d.Common, r)
		} else {
			d.Added = append(d.Added, r)
		}
	}
	for _, r := range rraws {
		if _, ok := lset[r]; !ok {
			d.Removed = append(d.Removed, r)
		}
	}
	return d
}

func pairsOf(s *Section) map[string]string {
	if s == nil || s.Pairs == nil {
		return map[string]string{}
	}
	return s.Pairs
}

func diffPairs(left, right *Section) PairsDiff {
	lp, rp := pairsOf(left), pairsOf(right)
	d := PairsDiff{
		Added:   map[string]string{},
		Removed: map[string]string{},
		Changed: map[string]PairChange{},
		Common:  map[string]string{},
	}
	for k, v := range lp {
		if rv, ok := rp[k]; !ok {
			d.Added[k] = v
		} else if rv != v {
			d.Changed[k] = PairChange{Left: v, Right: rv}
		} else {
			d.Common[k] = v
		}
	}
	for k, v := range rp {
		if _, ok := lp[k]; !ok {
			d.Removed[k] = v
		}
	}
	return d
}

func diffContent(left, right *Section) ContentDiff {
	var l, r *string
	if left != nil {
		l = left.Content
	}
	if right != nil {
		r = right.Content
	}
	return ContentDiff{Left: l, Right: r, Changed: !eqStrPtr(l, r)}
}

func eqStrPtr(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// A both-present section is "changed" iff any item add/remove, any pair
// add/remove/change, or a content change. Items in common alone don't count.
func computeStatus(d SectionDiff) Status {
	if len(d.Items.Added) > 0 || len(d.Items.Removed) > 0 ||
		len(d.Pairs.Added) > 0 || len(d.Pairs.Removed) > 0 || len(d.Pairs.Changed) > 0 ||
		d.Content.Changed {
		return StatusChanged
	}
	return StatusEqual
}

// FormatOptions controls how a diff is rendered.
type FormatOptions struct {
	LeftLabel   string
	RightLabel  string
	Color       bool
	ChangesOnly bool // skip equal sections and the dim "=" common lines
}

// Format renders a diff to text. Sections and pair keys are emitted in
// deterministic alphabetical order. No trailing newline.
func (d SnapshotDiff) Format(o FormatOptions) string {
	left, right := o.LeftLabel, o.RightLabel
	if left == "" {
		left = "left"
	}
	if right == "" {
		right = "right"
	}
	var green, red, yellow, dim, reset string
	if o.Color {
		green, red, yellow, dim, reset = "\x1b[32m", "\x1b[31m", "\x1b[33m", "\x1b[2m", "\x1b[0m"
	}

	var lines []string
	for _, name := range sortedDiffKeys(d) {
		s := d[name]
		if o.ChangesOnly && s.Status == StatusEqual {
			continue
		}
		switch s.Status {
		case StatusAdded:
			lines = append(lines, fmt.Sprintf("%s+ [%s]%s  (only in %s)", green, name, reset, left))
		case StatusRemoved:
			lines = append(lines, fmt.Sprintf("%s- [%s]%s  (only in %s)", red, name, reset, right))
		default:
			lines = append(lines, fmt.Sprintf("[%s]", name))
		}
		for _, it := range s.Items.Added {
			lines = append(lines, fmt.Sprintf("  %s+ %s%s  (only in %s)", green, it, reset, left))
		}
		for _, it := range s.Items.Removed {
			lines = append(lines, fmt.Sprintf("  %s- %s%s  (only in %s)", red, it, reset, right))
		}
		if !o.ChangesOnly {
			for _, it := range s.Items.Common {
				lines = append(lines, fmt.Sprintf("  %s= %s%s", dim, it, reset))
			}
		}
		for _, k := range sortedKeys(s.Pairs.Added) {
			lines = append(lines, fmt.Sprintf("  %s+ %s = %s%s  (only in %s)", green, k, s.Pairs.Added[k], reset, left))
		}
		for _, k := range sortedKeys(s.Pairs.Removed) {
			lines = append(lines, fmt.Sprintf("  %s- %s = %s%s  (only in %s)", red, k, s.Pairs.Removed[k], reset, right))
		}
		for _, k := range sortedChangeKeys(s.Pairs.Changed) {
			c := s.Pairs.Changed[k]
			lines = append(lines, fmt.Sprintf("  %s~ %s = %s → %s%s", yellow, k, c.Left, c.Right, reset))
		}
		if !o.ChangesOnly {
			for _, k := range sortedKeys(s.Pairs.Common) {
				lines = append(lines, fmt.Sprintf("  %s= %s = %s%s", dim, k, s.Pairs.Common[k], reset))
			}
		}
		if s.Content.Changed {
			switch {
			case s.Content.Left != nil && s.Content.Right != nil:
				lines = append(lines, fmt.Sprintf("  %s~ content changed%s", yellow, reset))
			case s.Content.Left != nil:
				lines = append(lines, fmt.Sprintf("  %s+ content%s  (only in %s)", green, reset, left))
			default:
				lines = append(lines, fmt.Sprintf("  %s- content%s  (only in %s)", red, reset, right))
			}
		}
	}
	return strings.Join(lines, "\n")
}

func sortedDiffKeys(d SnapshotDiff) []string {
	ks := make([]string, 0, len(d))
	for k := range d {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedKeys(m map[string]string) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}

func sortedChangeKeys(m map[string]PairChange) []string {
	ks := make([]string, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	return ks
}
