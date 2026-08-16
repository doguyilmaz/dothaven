package collect

import "testing"

func TestParseCrontabKeepsJobsAndSettings(t *testing.T) {
	got := ParseCrontab(`# daily backup
PATH=/usr/local/bin:/usr/bin

0 3 * * * /usr/local/bin/backup.sh

# disabled for now
#0 4 * * * /old.sh
*/5 * * * * ping -c1 example.com
`)
	want := []string{
		"PATH=/usr/local/bin:/usr/bin",
		"0 3 * * * /usr/local/bin/backup.sh",
		"*/5 * * * * ping -c1 example.com",
	}
	if len(got) != len(want) {
		t.Fatalf("got %d entries, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entry %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// A PATH= line is not decoration: a job that runs without it can behave
// differently on the new machine, and that is a hard difference to trace.
func TestParseCrontabKeepsVariableAssignments(t *testing.T) {
	got := ParseCrontab("MAILTO=me@example.com\n0 * * * * true\n")
	if len(got) != 2 || got[0] != "MAILTO=me@example.com" {
		t.Errorf("variable assignments must survive, got %q", got)
	}
}

func TestParseCrontabEmpty(t *testing.T) {
	if got := ParseCrontab("# only comments\n\n"); len(got) != 0 {
		t.Errorf("want nothing, got %q", got)
	}
}
