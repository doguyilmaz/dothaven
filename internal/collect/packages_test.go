package collect

import (
	"reflect"
	"testing"
)

func TestParseUvTool(t *testing.T) {
	in := "ruff v0.4.0\n- ruff\nblack v24.10.0\n- black\n- blackd\n"
	got := ParseUvTool(in)
	want := []PkgItem{{"black", "24.10.0"}, {"ruff", "0.4.0"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseUvTool = %v, want %v", got, want)
	}
}

func TestParseComposerGlobal(t *testing.T) {
	in := `{"installed":[{"name":"laravel/installer","version":"v5.0.1"},{"name":"friendsofphp/php-cs-fixer","version":"3.0.0"}]}`
	got := ParseComposerGlobal(in)
	want := []PkgItem{{"friendsofphp/php-cs-fixer", "3.0.0"}, {"laravel/installer", "5.0.1"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseComposerGlobal = %v, want %v", got, want)
	}
	if len(ParseComposerGlobal("not json")) != 0 {
		t.Error("invalid JSON should yield no packages")
	}
}

func TestParsePubGlobal(t *testing.T) {
	in := "melos 6.0.0\nmason_cli 0.1.0 from path /x\n"
	got := ParsePubGlobal(in)
	want := []PkgItem{{"mason_cli", "0.1.0"}, {"melos", "6.0.0"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParsePubGlobal = %v, want %v", got, want)
	}
}

func TestParseDotnetTool(t *testing.T) {
	in := "Package Id      Version      Commands\n-------------------------------------\ndotnetsay       2.1.4        dotnetsay\ncsharprepl      0.6.0        csharprepl\n"
	got := ParseDotnetTool(in)
	want := []PkgItem{{"csharprepl", "0.6.0"}, {"dotnetsay", "2.1.4"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseDotnetTool = %v, want %v", got, want)
	}
}

func TestParseNpmGlobal(t *testing.T) {
	real := `{"name":"lib","dependencies":{"@swmansion/argent":{"version":"0.9.0","overridden":false},"corepack":{"version":"0.35.0","overridden":false},"npm":{"version":"11.13.0","overridden":false}}}`

	tests := []struct {
		name string
		in   string
		want []PkgItem
	}{
		{
			name: "extracts name + version, sorted",
			in:   real,
			want: []PkgItem{
				{Name: "@swmansion/argent", Version: "0.9.0"},
				{Name: "corepack", Version: "0.35.0"},
				{Name: "npm", Version: "11.13.0"},
			},
		},
		{
			name: "missing version field → empty version string",
			in:   `{"dependencies":{"foo":{}}}`,
			want: []PkgItem{{Name: "foo", Version: ""}},
		},
		{name: "absent dependencies → empty", in: `{"name":"lib"}`, want: []PkgItem{}},
		{name: "null dependencies → empty", in: `{"dependencies":null}`, want: []PkgItem{}},
		{name: "invalid json → empty", in: "not json", want: []PkgItem{}},
		{name: "empty → empty", in: "", want: []PkgItem{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseNpmGlobal(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseBunGlobal(t *testing.T) {
	real := "/Users/doguyilmaz/.bun/install/global node_modules (3520)\n" +
		"├── @anthropic-ai/claude-code@2.1.20\n" +
		"├── @google/gemini-cli@0.45.0\n" +
		"├── eas-cli@16.19.2\n" +
		"└── yo@5.1.0"

	tests := []struct {
		name string
		in   string
		want []PkgItem
	}{
		{
			name: "parses tree, skips header, scoped names + last row, sorted",
			in:   real,
			want: []PkgItem{
				{Name: "@anthropic-ai/claude-code", Version: "2.1.20"},
				{Name: "@google/gemini-cli", Version: "0.45.0"},
				{Name: "eas-cli", Version: "16.19.2"},
				{Name: "yo", Version: "5.1.0"},
			},
		},
		{
			name: "row without a version → empty version string",
			in:   "header\n└── lonelypkg",
			want: []PkgItem{{Name: "lonelypkg", Version: ""}},
		},
		{
			name: "ignores blank lines and non-tree lines",
			in:   "header\n\n├── a@1.0.0\nrandom noise\n└── b@2.0.0\n",
			want: []PkgItem{{Name: "a", Version: "1.0.0"}, {Name: "b", Version: "2.0.0"}},
		},
		{name: "empty → empty", in: "", want: []PkgItem{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseBunGlobal(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParsePnpmGlobal(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []PkgItem
	}{
		{
			name: "array form",
			in:   `[{"path":"/x","dependencies":{"tldr":{"version":"3.3.0","from":"tldr"}}}]`,
			want: []PkgItem{{Name: "tldr", Version: "3.3.0"}},
		},
		{
			name: "object form",
			in:   `{"dependencies":{"tldr":{"version":"3.3.0"}}}`,
			want: []PkgItem{{Name: "tldr", Version: "3.3.0"}},
		},
		{name: "invalid → empty", in: "x", want: []PkgItem{}},
		{name: "empty → empty", in: "", want: []PkgItem{}},
		{name: "empty array → empty", in: "[]", want: []PkgItem{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParsePnpmGlobal(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestParseFnmList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []NodeVersion
	}{
		{
			name: "parses versions and default flag",
			in:   "* v20.20.2\n* v24.16.0 default\n* system",
			want: []NodeVersion{
				{Version: "v20.20.2", IsDefault: false},
				{Version: "v24.16.0", IsDefault: true},
				{Version: "system", IsDefault: false},
			},
		},
		{
			name: "tolerates rows without '*' and extra whitespace",
			in:   "  v18.0.0  \n  v20.0.0 default ",
			want: []NodeVersion{
				{Version: "v18.0.0", IsDefault: false},
				{Version: "v20.0.0", IsDefault: true},
			},
		},
		{
			name: "isolates version with comma-separated aliases",
			in:   "* v24.16.0 default, lts-latest",
			want: []NodeVersion{{Version: "v24.16.0", IsDefault: true}},
		},
		{
			name: "non-default alias",
			in:   "* v22.0.0 lts-jod",
			want: []NodeVersion{{Version: "v22.0.0", IsDefault: false}},
		},
		{name: "empty → empty", in: "", want: []NodeVersion{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseFnmList(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestSplitSpec(t *testing.T) {
	tests := []struct {
		in   string
		want PkgItem
	}{
		{"eas-cli@16.19.2", PkgItem{Name: "eas-cli", Version: "16.19.2"}},
		{"@scope/pkg@1.2.3", PkgItem{Name: "@scope/pkg", Version: "1.2.3"}},
		{"@scope/pkg", PkgItem{Name: "@scope/pkg", Version: ""}},
		{"lonelypkg", PkgItem{Name: "lonelypkg", Version: ""}},
	}
	for _, tt := range tests {
		if got := splitSpec(tt.in); got != tt.want {
			t.Errorf("splitSpec(%q) = %+v, want %+v", tt.in, got, tt.want)
		}
	}
}
