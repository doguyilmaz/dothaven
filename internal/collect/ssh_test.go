package collect

import (
	"reflect"
	"testing"
)

func TestParseSSHConfig(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    []SSHHost
	}{
		{
			name: "two hosts",
			content: "Host github.com\n" +
				"  HostName github.com\n" +
				"  IdentityFile ~/.ssh/id_ed25519\n" +
				"\n" +
				"Host work-server\n" +
				"  HostName 10.0.0.50\n" +
				"  IdentityFile ~/.ssh/work_key\n",
			want: []SSHHost{
				{Host: "github.com", HostName: "github.com", IdentityFile: "~/.ssh/id_ed25519"},
				{Host: "work-server", HostName: "10.0.0.50", IdentityFile: "~/.ssh/work_key"},
			},
		},
		{
			name:    "empty input",
			content: "",
			want:    nil,
		},
		{
			name:    "only comments and blanks",
			content: "# global config\n\n   \n# another comment\n",
			want:    nil,
		},
		{
			name:    "host without hostname or identity",
			content: "Host bare\n",
			want: []SSHHost{
				{Host: "bare", HostName: "", IdentityFile: ""},
			},
		},
		{
			name: "case insensitive keywords",
			content: "host Example\n" +
				"  hostname example.com\n" +
				"  identityfile ~/.ssh/ex\n",
			want: []SSHHost{
				{Host: "Example", HostName: "example.com", IdentityFile: "~/.ssh/ex"},
			},
		},
		{
			name: "options before any host are ignored",
			content: "HostName orphan.example.com\n" +
				"IdentityFile ~/.ssh/orphan\n" +
				"Host real\n" +
				"  HostName real.example.com\n",
			want: []SSHHost{
				{Host: "real", HostName: "real.example.com", IdentityFile: ""},
			},
		},
		{
			name: "comment lines and indentation are tolerated",
			content: "# header\n" +
				"Host h1\n" +
				"    # inline comment\n" +
				"    HostName h1.local\n" +
				"    IdentityFile ~/.ssh/h1\n",
			want: []SSHHost{
				{Host: "h1", HostName: "h1.local", IdentityFile: "~/.ssh/h1"},
			},
		},
		{
			name: "trailing whitespace trimmed on values",
			content: "Host h\n" +
				"  HostName   spaced.host   \n" +
				"  IdentityFile   ~/.ssh/k  \n",
			want: []SSHHost{
				{Host: "h", HostName: "spaced.host", IdentityFile: "~/.ssh/k"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseSSHConfig(tt.content)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSSHConfig() = %#v, want %#v", got, tt.want)
			}
		})
	}
}
