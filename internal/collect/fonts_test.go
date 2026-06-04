package collect

import (
	"reflect"
	"testing"
)

func TestFilterFonts(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		want []string
	}{
		{
			name: "keeps font files (case-insensitive), drops others, sorts",
			in:   []string{"b.ttf", "a.OTF", "readme.txt", ".DS_Store", "c.woff2", "d.ttc"},
			want: []string{"a.OTF", "b.ttf", "c.woff2", "d.ttc"},
		},
		{
			name: "empty -> empty",
			in:   []string{},
			want: []string{},
		},
		{
			name: "all non-fonts -> empty",
			in:   []string{"notes.txt", "junk", ".DS_Store"},
			want: []string{},
		},
		{
			name: "all supported extensions",
			in:   []string{"a.ttf", "b.ttc", "c.otf", "d.otc", "e.dfont", "f.woff", "g.woff2", "h.pfb"},
			want: []string{"a.ttf", "b.ttc", "c.otf", "d.otc", "e.dfont", "f.woff", "g.woff2", "h.pfb"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := FilterFonts(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("FilterFonts(%v) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
