package collect

import (
	"reflect"
	"testing"
)

func TestParseExtensions(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "trims, drops blanks, sorts",
			in:   "anthropic.claude-code\n\n  aaron-bond.better-comments \n",
			want: []string{"aaron-bond.better-comments", "anthropic.claude-code"},
		},
		{
			name: "empty -> empty",
			in:   "",
			want: []string{},
		},
		{
			name: "whitespace only -> empty",
			in:   "   \n\t\n  ",
			want: []string{},
		},
		{
			name: "single extension",
			in:   "some.ext",
			want: []string{"some.ext"},
		},
		{
			name: "multiple sorted",
			in:   "anthropic.claude-code\nadpyke.codesnap",
			want: []string{"adpyke.codesnap", "anthropic.claude-code"},
		},
		{
			name: "trailing newline only",
			in:   "anthropic.claude-code\n",
			want: []string{"anthropic.claude-code"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseExtensions(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseExtensions(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}
