package collect

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseBrewList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"trims, drops blanks, sorts", "git\n  jq \n\nbat\n", []string{"bat", "git", "jq"}},
		{"empty", "", []string{}},
		{"whitespace only", "   \n  ", []string{}},
		{"single", "git", []string{"git"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseBrewList(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseBrewList(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestParseBrewfile(t *testing.T) {
	regression := strings.Join([]string{
		`tap "facebook/fb"`,
		`brew "glib"`,
		`go "github.com/go-delve/delve/cmd/dlv"`,
		`npm "@swmansion/argent"`,
		`cargo "ripgrep"`,
		`whalebrew "whalebrew/whalesay"`,
		`cask "stats"`,
		`vscode "anthropic.claude-code"`,
	}, "\n")

	tests := []struct {
		name string
		in   string
		want string
	}{
		{
			"keeps directives, drops cold-cache progress noise",
			"✔︎ JSON API formula.jws.json\ntap \"facebook/fb\"\nbrew \"glib\"\ncask \"stats\"\nmas \"Xcode\", id: 497799835",
			"tap \"facebook/fb\"\nbrew \"glib\"\ncask \"stats\"\nmas \"Xcode\", id: 497799835",
		},
		{
			"keeps comments, trims surrounding blank lines",
			"\n# Core lib\nbrew \"glib\"\n\n",
			"# Core lib\nbrew \"glib\"",
		},
		{
			"preserves non-brew directives (regression for allowlist data loss)",
			regression,
			regression,
		},
		{"empty", "", ""},
		{
			"drops ==> and Warning:/Error: noise",
			"==> Downloading\nWarning: skip\nError: boom\nbrew \"git\"",
			"brew \"git\"",
		},
		{
			"drops checkmark variants",
			"✓ done\n✗ failed\n⚠ warn\nℹ info\nbrew \"jq\"",
			"brew \"jq\"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseBrewfile(tt.in); got != tt.want {
				t.Errorf("ParseBrewfile(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}
}
