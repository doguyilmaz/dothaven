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

func TestGitReferences(t *testing.T) {
	cfg := `[user]
	name = Me
[core]
	hooksPath = ~/.git-hooks
	excludesFile = ~/.gitignore_global
	editor = vim
[commit]
	template = $HOME/.gitmessage
[include]
	path = .gitconfig.local
[includeIf "gitdir:~/work/"]
	path = "~/work/.gitconfig-work"
[init]
	templateDir = /usr/share/git-core/templates
[alias]
	path = not-a-path
`
	got := GitReferences(cfg, "/h", "/h")
	want := "~/.git-hooks,~/.gitignore_global,~/.gitmessage,~/.gitconfig.local,~/work/.gitconfig-work"
	if strings.Join(got, ",") != want {
		t.Errorf("GitReferences = %v\nwant %s", got, want)
	}
}
