package collect

import (
	"reflect"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/registry"
)

func TestManagedDotNames(t *testing.T) {
	t.Run("derives top-level ~/.X names from the real registry", func(t *testing.T) {
		s := ManagedDotNames(registry.Entries)
		for _, want := range []string{".zshrc", ".gitconfig", ".config", ".ssh", ".bunfig.toml"} {
			if !s[want] {
				t.Errorf("expected managed set to contain %q", want)
			}
		}
	})

	t.Run("takes the first path segment after ~/", func(t *testing.T) {
		e := registry.Entry{ID: "x", Name: "x", Paths: map[string]string{"darwin": "~/.foo/bar.json"}}
		got := ManagedDotNames([]registry.Entry{e})
		want := map[string]bool{".foo": true}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %v, want %v", got, want)
		}
	})

	t.Run("falls back to linux path when darwin missing", func(t *testing.T) {
		e := registry.Entry{ID: "x", Paths: map[string]string{"linux": "~/.barlinux"}}
		got := ManagedDotNames([]registry.Entry{e})
		if !got[".barlinux"] {
			t.Errorf("expected linux fallback to yield .barlinux, got %v", got)
		}
	})

	t.Run("ignores non-~ paths (windows-only)", func(t *testing.T) {
		e := registry.Entry{ID: "x", Paths: map[string]string{"windows": "%USERPROFILE%/.win"}}
		got := ManagedDotNames([]registry.Entry{e})
		if len(got) != 0 {
			t.Errorf("expected empty set, got %v", got)
		}
	})
}

func TestParseLsA(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{"splits, trims, drops blanks", ".zshrc\n.config\n\n  .ssh \n", []string{".zshrc", ".config", ".ssh"}},
		{"empty", "", nil},
		{"only whitespace", "  \n\t\n", nil},
		{"single entry", ".zshrc", []string{".zshrc"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseLsA(tt.in); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseLsA(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestClassifyDotfiles(t *testing.T) {
	t.Run("splits managed/review, drops noise and non-dot entries", func(t *testing.T) {
		got := ClassifyDotfiles(
			[]string{".zshrc", ".app-store", ".DS_Store", ".aws", "Documents", ".."},
			map[string]bool{".zshrc": true},
			map[string]bool{".DS_Store": true},
		)
		if !reflect.DeepEqual(got.Managed, []string{".zshrc"}) {
			t.Errorf("managed = %v, want [.zshrc]", got.Managed)
		}
		if !reflect.DeepEqual(got.Review, []string{".app-store", ".aws"}) {
			t.Errorf("review = %v, want [.app-store .aws]", got.Review)
		}
	})

	t.Run("empty -> empty buckets", func(t *testing.T) {
		got := ClassifyDotfiles(nil, map[string]bool{}, map[string]bool{})
		if len(got.Managed) != 0 || len(got.Review) != 0 {
			t.Errorf("expected empty buckets, got %+v", got)
		}
	})

	t.Run("sorts entries before bucketing", func(t *testing.T) {
		got := ClassifyDotfiles(
			[]string{".zoo", ".alpha", ".beta"},
			map[string]bool{},
			map[string]bool{},
		)
		if !reflect.DeepEqual(got.Review, []string{".alpha", ".beta", ".zoo"}) {
			t.Errorf("review = %v, want sorted", got.Review)
		}
	})

	t.Run("non-dot and dot/dotdot are dropped", func(t *testing.T) {
		got := ClassifyDotfiles(
			[]string{".", "..", "Documents", "bin", ".real"},
			map[string]bool{},
			map[string]bool{},
		)
		if !reflect.DeepEqual(got.Review, []string{".real"}) {
			t.Errorf("review = %v, want [.real]", got.Review)
		}
	})
}
