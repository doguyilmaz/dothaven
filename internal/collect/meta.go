package collect

import (
	"os"
	"runtime"
	"time"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// MetaCollector records basic machine identity: hostname, OS+arch, and the
// collection date. No subprocesses — everything comes from the runtime.
func MetaCollector(c Ctx) snapshot.Snapshot {
	host, err := os.Hostname()
	if err != nil {
		host = ""
	}
	return snapshot.Snapshot{
		"meta": {
			Pairs: map[string]string{
				"host": host,
				"os":   ParseMetaOS(runtime.GOOS, runtime.GOARCH),
				"date": ParseMetaDate(time.Now()),
			},
		},
	}
}

// ParseMetaOS joins the OS and architecture as "<os> <arch>" (e.g. "darwin arm64").
func ParseMetaOS(goos, goarch string) string {
	return goos + " " + goarch
}

// ParseMetaDate formats a time as YYYY-MM-DD.
func ParseMetaDate(t time.Time) string {
	return t.Format("2006-01-02")
}
