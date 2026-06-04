package collect

import (
	"reflect"
	"testing"
)

func TestParseVersionToken(t *testing.T) {
	tests := []struct {
		name, text, word, want string
	}{
		{"rustc", "rustc 1.96.0 (ac68faa20 2026-05-25)", "rustc", "1.96.0"},
		{"cargo", "cargo 1.96.0 (30a34c682 2026-05-25)", "cargo", "1.96.0"},
		{"missing", "nope", "rustc", ""},
		{"empty", "", "rustc", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseVersionToken(tt.text, tt.word); got != tt.want {
				t.Errorf("ParseVersionToken(%q,%q) = %q, want %q", tt.text, tt.word, got, tt.want)
			}
		})
	}
}

func TestParseGoVersion(t *testing.T) {
	tests := []struct {
		name, text string
		want       *GoInfo
	}{
		{"valid", "go version go1.26.3 darwin/arm64", &GoInfo{Version: "go1.26.3", Platform: "darwin/arm64"}},
		{"empty", "", nil},
		{"garbage", "command not found", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseGoVersion(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseGoVersion(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseRustupToolchains(t *testing.T) {
	tests := []struct {
		name, text string
		want       []RustToolchain
	}{
		{
			"name+flags and plain",
			"stable-aarch64-apple-darwin (active, default)\nnightly-aarch64-apple-darwin",
			[]RustToolchain{
				{Name: "stable-aarch64-apple-darwin", Flags: "active, default"},
				{Name: "nightly-aarch64-apple-darwin", Flags: ""},
			},
		},
		{"no installed toolchains prose", "no installed toolchains", nil},
		{
			"ignores prose status line",
			"stable-aarch64-apple-darwin (default)\nerror: rustup could not choose a version",
			[]RustToolchain{{Name: "stable-aarch64-apple-darwin", Flags: "default"}},
		},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseRustupToolchains(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseRustupToolchains(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseCargoCrates(t *testing.T) {
	tests := []struct {
		name, text string
		want       []Crate
	}{
		{
			"registry crates, ignore binaries",
			"ripgrep v14.1.0:\n    rg\nfd-find v10.2.0:\n    fd",
			[]Crate{{Name: "ripgrep", Version: "14.1.0"}, {Name: "fd-find", Version: "10.2.0"}},
		},
		{
			"git source before colon",
			"cargo-watch v8.5.0 (https://github.com/watchexec/cargo-watch#a1b2c3d):\n    cargo-watch",
			[]Crate{{Name: "cargo-watch", Version: "8.5.0"}},
		},
		{
			"path source before colon",
			"localtool v0.2.0 (/Users/me/proj):\n    localtool",
			[]Crate{{Name: "localtool", Version: "0.2.0"}},
		},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseCargoCrates(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseCargoCrates(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseSwiftVersion(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"apple swift", "swift-driver version: 1.148.6 Apple Swift version 6.3.1 (swiftlang-6.3.1)", "6.3.1"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseSwiftVersion(tt.text); got != tt.want {
				t.Errorf("ParseSwiftVersion(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseXcodeVersion(t *testing.T) {
	tests := []struct {
		name, text string
		want       *XcodeInfo
	}{
		{"version+build", "Xcode 26.4.1\nBuild version 17E202", &XcodeInfo{Version: "26.4.1", Build: "17E202"}},
		{"version only", "Xcode 26.4.1", &XcodeInfo{Version: "26.4.1", Build: ""}},
		{"empty", "", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseXcodeVersion(tt.text); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseXcodeVersion(%q) = %+v, want %+v", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseZigVersion(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"single line", "0.13.0\n", "0.13.0"},
		{"command not found", "command not found: zig", ""},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseZigVersion(tt.text); got != tt.want {
				t.Errorf("ParseZigVersion(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}

func TestParseAdbVersion(t *testing.T) {
	tests := []struct {
		name, text, want string
	}{
		{"platform-tools line", "Android Debug Bridge version 1.0.41\nVersion 36.0.2-14143358\nInstalled as /x", "36.0.2-14143358"},
		{"empty", "", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseAdbVersion(tt.text); got != tt.want {
				t.Errorf("ParseAdbVersion(%q) = %q, want %q", tt.text, got, tt.want)
			}
		})
	}
}
