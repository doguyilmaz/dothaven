package release

import "testing"

func TestCompare(t *testing.T) {
	tests := []struct {
		a, b string
		want int
	}{
		{"0.4.0", "0.5.0", -1},
		{"0.5.0", "0.4.0", 1},
		{"0.5.0", "0.5.0", 0},
		// Release tags carry a v, the ldflags-embedded version does not.
		// Comparing them raw is the whole reason this function exists.
		{"v0.5.0", "0.5.0", 0},
		{"0.4.0", "v0.5.0", -1},
		// A string compare gets this backwards, and 0.9 -> 0.10 is exactly
		// where this tool will be when it first matters.
		{"0.10.0", "0.9.0", 1},
		{"1.2", "1.2.0", 0},
		{"1.2.1", "1.2", 1},
		// A pre-release sorts below the release it leads to.
		{"1.2.0-rc.1", "1.2.0", -1},
		{"1.2.0", "1.2.0-rc.1", 1},
		{"1.2.0-rc.1", "1.2.0-rc.2", -1},
		{"1.2.0-rc.2", "1.2.0-rc.2", 0},
		// Garbage must not panic or claim an upgrade.
		{"", "", 0},
		{"", "0.5.0", -1},
		{"not-a-version", "0.5.0", -1},
	}
	for _, tt := range tests {
		if got := Compare(tt.a, tt.b); got != tt.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestComparable(t *testing.T) {
	for _, v := range []string{"0.4.0", "v0.5.0", "1.2", "1.2.0-rc.1", "0"} {
		if !Comparable(v) {
			t.Errorf("Comparable(%q) = false, want true", v)
		}
	}
	// "dev" is what a `go build` binary reports; the rest must not slip past
	// and trigger a check against a version that does not exist.
	for _, v := range []string{"", "dev", "unknown", "not-a-version", "  "} {
		if Comparable(v) {
			t.Errorf("Comparable(%q) = true, want false", v)
		}
	}
}

func TestNewer(t *testing.T) {
	tests := []struct {
		name            string
		current, latest string
		want            bool
	}{
		{"upgrade available", "0.4.0", "0.5.0", true},
		{"tag prefix on latest", "0.4.0", "v0.5.0", true},
		{"already current", "0.5.0", "0.5.0", false},
		{"local build is ahead", "0.6.0", "0.5.0", false},
		// A `go build` binary has no version to compare, and nagging someone
		// who is working on dothaven itself is pure noise.
		{"dev build never nags", "dev", "0.5.0", false},
		{"empty current never nags", "", "0.5.0", false},
		{"empty latest never nags", "0.4.0", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Newer(tt.current, tt.latest); got != tt.want {
				t.Errorf("Newer(%q, %q) = %v, want %v", tt.current, tt.latest, got, tt.want)
			}
		})
	}
}
