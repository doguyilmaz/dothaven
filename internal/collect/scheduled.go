package collect

import (
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// ScheduledCollector records the jobs this machine runs on a schedule.
//
// Both are classic migration losses: they are invisible day to day, nothing
// reminds you they exist, and a new Mac is simply quieter until you notice
// months later that a sync or a cleanup stopped happening. Neither lives in a
// dotfile, so nothing else here would have caught them.
//
// The plists in ~/Library/LaunchAgents come through the registry as files; this
// records crontab, which has no file to point at — it lives in a spool
// directory only the crontab command may read.
func ScheduledCollector(c Ctx) snapshot.Snapshot {
	out, err := c.Env.Run(c.Context, "crontab", "-l")
	if err != nil {
		return snapshot.Snapshot{} // no crontab is the normal case, not an error
	}
	jobs := ParseCrontab(out)
	if len(jobs) == 0 {
		return snapshot.Snapshot{}
	}
	items := make([]snapshot.Item, 0, len(jobs))
	for _, j := range jobs {
		items = append(items, snapshot.Item{Raw: j})
	}
	return snapshot.Snapshot{"schedule.crontab": snapshot.Section{Items: items}}
}

// ParseCrontab keeps the entries and drops comments and blank lines. Variable
// assignments (PATH=, MAILTO=) are kept: a job that runs without them behaves
// differently, which is exactly the kind of difference that is hard to find
// later.
func ParseCrontab(text string) []string {
	var jobs []string
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		jobs = append(jobs, line)
	}
	return jobs
}
