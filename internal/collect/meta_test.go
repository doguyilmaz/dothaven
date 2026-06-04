package collect

import (
	"regexp"
	"testing"
	"time"
)

func TestParseMetaOS(t *testing.T) {
	tests := []struct {
		name   string
		goos   string
		goarch string
		want   string
	}{
		{"darwin arm64", "darwin", "arm64", "darwin arm64"},
		{"linux amd64", "linux", "amd64", "linux amd64"},
		{"empty", "", "", " "},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseMetaOS(tt.goos, tt.goarch); got != tt.want {
				t.Errorf("ParseMetaOS(%q,%q) = %q, want %q", tt.goos, tt.goarch, got, tt.want)
			}
		})
	}
}

func TestParseMetaDate(t *testing.T) {
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{"fixed date", time.Date(2026, 6, 3, 14, 30, 0, 0, time.UTC), "2026-06-03"},
		{"zero-padded month/day", time.Date(2024, 1, 5, 0, 0, 0, 0, time.UTC), "2024-01-05"},
		{"end of year", time.Date(2023, 12, 31, 23, 59, 59, 0, time.UTC), "2023-12-31"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseMetaDate(tt.in); got != tt.want {
				t.Errorf("ParseMetaDate(%v) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}

// matches the TS test assertion: date pairs must match /^\d{4}-\d{2}-\d{2}$/
func TestParseMetaDateFormatRegexp(t *testing.T) {
	re := regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)
	got := ParseMetaDate(time.Now())
	if !re.MatchString(got) {
		t.Errorf("ParseMetaDate(now) = %q, want match %s", got, re.String())
	}
}
