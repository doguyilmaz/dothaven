package chezmoi

import (
	"strings"
	"testing"
	"text/template"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

func TestTemplatize(t *testing.T) {
	cases := []struct {
		name, content, home, want string
		changed                   bool
	}{
		{"path prefix", "export PATH=/Users/dev/bin:$PATH", "/Users/dev", "export PATH={{ .chezmoi.homeDir }}/bin:$PATH", true},
		{"multiple", "a=/home/u/x\nb=/home/u/y", "/home/u", "a={{ .chezmoi.homeDir }}/x\nb={{ .chezmoi.homeDir }}/y", true},
		{"sibling not corrupted", "p=/Users/devops/bin", "/Users/dev", "p=/Users/devops/bin", false},
		{"no home present", "theme = dark", "/Users/dev", "theme = dark", false},
		{"empty home guarded", "/anything/here", "", "/anything/here", false},
		{"root home guarded", "/etc/hosts", "/", "/etc/hosts", false},
		{"literal delimiters escaped", "keys = {{key='a'}}\ndir=/home/u/x", "/home/u",
			`keys = {{ "{{" }}key='a'{{ "}}" }}` + "\ndir={{ .chezmoi.homeDir }}/x", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, changed := Templatize(c.content, c.home)
			if got != c.want || changed != c.changed {
				t.Errorf("Templatize(%q,%q) = (%q,%v), want (%q,%v)", c.content, c.home, got, changed, c.want, c.changed)
			}
		})
	}
}

func TestShouldTemplate(t *testing.T) {
	yes := registry.Entry{Category: "shell", Kind: registry.File}
	if !ShouldTemplate(yes) {
		t.Error("a shell File entry should be templatable")
	}
	noDir := registry.Entry{Category: "shell", Kind: registry.Dir}
	if ShouldTemplate(noDir) {
		t.Error("a Dir entry should not be templated")
	}
	noCat := registry.Entry{Category: "secrets", Kind: registry.File}
	if ShouldTemplate(noCat) {
		t.Error("a non-config category should not be templated")
	}
}

// The escaped output must parse and render back to the original, with only the
// home directory swapped — which is exactly what chezmoi apply will do.
func TestTemplatizeRendersBack(t *testing.T) {
	in := "local k = {{ mods='CMD' }}\nprintf '{{.Name}}'\npath=/home/u/bin\n"
	out, _ := Templatize(in, "/home/u")
	tpl, err := template.New("x").Parse(out)
	if err != nil {
		t.Fatalf("escaped template does not parse: %v\n%s", err, out)
	}
	var b strings.Builder
	data := map[string]any{"chezmoi": map[string]any{"homeDir": "/Users/new"}}
	if err := tpl.Execute(&b, data); err != nil {
		t.Fatal(err)
	}
	want := strings.ReplaceAll(in, "/home/u/", "/Users/new/")
	if b.String() != want {
		t.Errorf("rendered:\n%q\nwant:\n%q", b.String(), want)
	}
}
