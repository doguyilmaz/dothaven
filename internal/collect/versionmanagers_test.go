package collect

import (
	"reflect"
	"testing"
)

func TestParseAsdfList(t *testing.T) {
	in := "nodejs\n  18.20.0\n *20.11.0\npython\n  3.12.1\n"
	got := ParseAsdfList(in)
	want := []ToolVersion{
		{"nodejs", "18.20.0"}, {"nodejs", "20.11.0"}, {"python", "3.12.1"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseAsdfList = %v, want %v", got, want)
	}
	if len(ParseAsdfList("")) != 0 {
		t.Error("empty input should yield no versions")
	}
}

func TestParseVersionLines(t *testing.T) {
	in := "* 3.11.0 (set by /x/.python-version)\n3.12.1\nsystem\n  3.10.4  \n"
	got := ParseVersionLines(in)
	want := []string{"3.11.0", "3.12.1", "3.10.4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParseVersionLines = %v, want %v", got, want)
	}
}

func TestParsePipxList(t *testing.T) {
	in := "black 24.10.0\nruff 0.6.0\npoetry\n"
	got := ParsePipxList(in)
	want := []PkgItem{{"black", "24.10.0"}, {"poetry", ""}, {"ruff", "0.6.0"}} // sorted by name
	if !reflect.DeepEqual(got, want) {
		t.Errorf("ParsePipxList = %v, want %v", got, want)
	}
}
