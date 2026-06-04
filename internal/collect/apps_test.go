package collect

import (
	"reflect"
	"testing"
)

func TestParseAppList(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want []string
	}{
		{
			name: "trims, drops blanks, sorts",
			in:   "Safari.app\n  Zed.app \n\nArc.app\n",
			want: []string{"Arc.app", "Safari.app", "Zed.app"},
		},
		{
			name: "simple two-line listing",
			in:   "Safari.app\nZed.app",
			want: []string{"Safari.app", "Zed.app"},
		},
		{
			name: "empty -> empty",
			in:   "",
			want: []string{},
		},
		{
			name: "whitespace only -> empty",
			in:   "  \n\t\n  ",
			want: []string{},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseAppList(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseAppList(%q) = %v, want %v", tt.in, got, tt.want)
			}
		})
	}
}

func TestAppsInstalled(t *testing.T) {
	if appsInstalled(true) != "true" {
		t.Errorf("appsInstalled(true) = %q, want true", appsInstalled(true))
	}
	if appsInstalled(false) != "false" {
		t.Errorf("appsInstalled(false) = %q, want false", appsInstalled(false))
	}
}
