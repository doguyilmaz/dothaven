package registry

import (
	"strings"
	"testing"
)

func TestParseIncludesNormalisesAndRefusesEscapes(t *testing.T) {
	home := "/home/u"
	inc := ParseIncludes(`# comment
~/.config/raycast
/home/u/bin   # trailing comment
$HOME/.config/raycast
!~/.lesshst
../etc/passwd
/etc/hosts
~/../other
~
relative/path
`, home)
	if strings.Join(inc.Paths, ",") != "~/.config/raycast,~/bin" {
		t.Errorf("Paths = %v", inc.Paths)
	}
	if strings.Join(inc.Declined, ",") != "~/.lesshst" {
		t.Errorf("Declined = %v", inc.Declined)
	}
}

func TestFormatIncludesRoundTrips(t *testing.T) {
	in := Includes{Paths: []string{"~/b", "~/a"}, Declined: []string{"~/c"}}
	out := ParseIncludes(FormatIncludes(in), "/h")
	if strings.Join(out.Paths, ",") != "~/a,~/b" || strings.Join(out.Declined, ",") != "~/c" {
		t.Errorf("round trip = %+v", out)
	}
}

func TestIncludeEntries(t *testing.T) {
	es := IncludeEntries([]string{"~/bin", "~/.foorc"}, func(p string) bool { return strings.HasSuffix(p, "/bin") }, "/h")
	if es[0].Kind != Dir || es[0].BackupDest != "extra/bin" || es[0].Category != ExtraCategory {
		t.Errorf("bin entry = %+v", es[0])
	}
	if es[1].Kind != File || es[1].BackupDest != "extra/.foorc" || es[1].Sensitivity != Medium {
		t.Errorf("foorc entry = %+v", es[1])
	}
}
