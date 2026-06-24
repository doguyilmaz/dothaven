package collect

import (
	"reflect"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

func TestNestedVersions(t *testing.T) {
	env := &sys.Fake{Dirs: map[string][]string{
		"/h/.sdkman/candidates":        {"java", "kotlin", ".meta"},
		"/h/.sdkman/candidates/java":   {"17.0.1-tem", "21.0.2-tem", "current"},
		"/h/.sdkman/candidates/kotlin": {"1.9.0"},
	}}
	got := nestedVersions(env, "/h/.sdkman/candidates")
	want := []ToolVersion{{"java", "17.0.1-tem"}, {"java", "21.0.2-tem"}, {"kotlin", "1.9.0"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("nestedVersions = %v, want %v", got, want)
	}
	if nestedVersions(env, "/h/.nope") != nil {
		t.Error("missing base should yield nil")
	}
}

func TestFlatVersions(t *testing.T) {
	env := &sys.Fake{Dirs: map[string][]string{
		"/h/.fvm/versions": {"stable", "3.19.0", "current", ".cache"},
	}}
	if got := flatVersions(env, "/h/.fvm/versions"); !reflect.DeepEqual(got, []string{"3.19.0", "stable"}) {
		t.Errorf("flatVersions = %v, want [3.19.0 stable]", got)
	}
	if flatVersions(env, "/h/.nope") != nil {
		t.Error("missing dir should yield nil")
	}
}

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
