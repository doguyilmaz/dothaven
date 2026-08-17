// Package release compares dothaven versions and works out how the running
// binary was installed, so `dothaven upgrade` can hand the job to whichever
// package manager owns the file instead of overwriting it behind that manager's
// back. Everything here is a pure function; the network and the filesystem live
// in check.go behind a one-method seam.
package release

import (
	"strconv"
	"strings"
)

// Compare orders two versions: -1 if a sorts before b, 0 if equal, +1 after.
//
// Comparison is numeric per segment, which a string compare is not: 0.10.0 is
// above 0.9.0, and this tool is close enough to that boundary for it to matter.
// A leading v is ignored, because release tags carry one and the version
// embedded at build time does not — comparing those two raw is the mistake this
// exists to prevent.
//
// Unparsable input is treated as zero rather than an error: a version that
// makes no sense must not stop a command from running.
func Compare(a, b string) int {
	aNum, aPre, _ := split(a)
	bNum, bPre, _ := split(b)

	for i := 0; i < len(aNum) || i < len(bNum); i++ {
		if d := sign(at(aNum, i) - at(bNum, i)); d != 0 {
			return d
		}
	}

	// Numerically equal, so the pre-release decides. 1.2.0-rc.1 comes before
	// 1.2.0: a release outranks anything leading up to it. Two pre-releases are
	// ordered as text, which is right for rc.1 vs rc.2 and wrong for rc.9 vs
	// rc.10 — an ordering dothaven's own tags never produce.
	switch {
	case aPre == "" && bPre == "":
		return 0
	case aPre == "":
		return 1
	case bPre == "":
		return -1
	}
	return sign(strings.Compare(aPre, bPre))
}

// Comparable reports whether v is a version this can reason about at all.
// A `go build` binary carries "dev", which is the signal to stay quiet rather
// than check for updates against a version that does not exist.
func Comparable(v string) bool {
	_, _, ok := split(v)
	return ok
}

// Newer reports whether latest is a real upgrade over current.
//
// Both sides must actually parse. That is what keeps a `go build` binary
// (version "dev") quiet: it has nothing to compare, and nagging someone working
// on dothaven itself is pure noise. Checking that the version parses rather
// than matching the literal "dev" means no sentinel string has to be kept in
// step with cmd/dothaven.
func Newer(current, latest string) bool {
	if !Comparable(current) || !Comparable(latest) {
		return false
	}
	return Compare(current, latest) < 0
}

// split separates a version into its numeric segments and its pre-release
// suffix, reporting whether the numeric part parsed cleanly.
func split(v string) (nums []int, pre string, ok bool) {
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		pre, v = v[i+1:], v[:i]
	}
	if v == "" {
		return nil, pre, false
	}
	ok = true
	for _, seg := range strings.Split(v, ".") {
		n, err := strconv.Atoi(seg)
		if err != nil {
			ok = false
		}
		nums = append(nums, n)
	}
	return nums, pre, ok
}

// at reads a segment that may not exist: 1.2 and 1.2.0 are the same version.
func at(s []int, i int) int {
	if i < len(s) {
		return s[i]
	}
	return 0
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	}
	return 0
}
