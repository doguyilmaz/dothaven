package collect

import (
	"reflect"
	"testing"
)

func TestParseOllamaList(t *testing.T) {
	const real = "NAME              ID            SIZE      MODIFIED\n" +
		"llama3.2:latest   a80c4f17acd5  2.0 GB    2 weeks ago\n" +
		"qwen2.5:7b        845dbda0ea48  4.7 GB    3 months ago"

	tests := []struct {
		name string
		in   string
		want []OllamaModel
	}{
		{
			name: "parses rows, skips header, drops ID, keeps spaced values",
			in:   real,
			want: []OllamaModel{
				{Name: "llama3.2:latest", Size: "2.0 GB", Modified: "2 weeks ago"},
				{Name: "qwen2.5:7b", Size: "4.7 GB", Modified: "3 months ago"},
			},
		},
		{
			name: "header only",
			in:   "NAME  ID  SIZE  MODIFIED",
			want: nil,
		},
		{
			name: "empty",
			in:   "",
			want: nil,
		},
		{
			name: "whitespace only",
			in:   "   \n  ",
			want: nil,
		},
		{
			name: "row with leading/trailing whitespace and blank line",
			in:   "NAME  ID  SIZE  MODIFIED\n  mistral:7b  abc123  4.1 GB  5 days ago  \n\n",
			want: []OllamaModel{
				{Name: "mistral:7b", Size: "4.1 GB", Modified: "5 days ago"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseOllamaList(tt.in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseOllamaList() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
