package collect

import (
	"reflect"
	"testing"
)

func TestParseNameLines(t *testing.T) {
	in := "  ripgrep\nfd\n\n# comment\nbat  \n"
	got := parseNameLines(in)
	if want := []string{"bat", "fd", "ripgrep"}; !reflect.DeepEqual(got, want) {
		t.Errorf("parseNameLines = %v, want %v", got, want)
	}
}

func TestParseSnapList(t *testing.T) {
	in := "Name     Version  Rev  Tracking  Publisher  Notes\ncode     1.90     160  stable    vscode    classic\nspotify  1.2      70   stable    spotify   -\n"
	got := ParseSnapList(in)
	if want := []string{"code", "spotify"}; !reflect.DeepEqual(got, want) {
		t.Errorf("ParseSnapList = %v, want %v", got, want)
	}
}
