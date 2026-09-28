package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

// runLogger keeps a log of what a run did and how long each part took, so a
// stall on someone's machine can be traced afterwards: which step, which
// external command, which file. It records names and timings only, never file
// contents, command output or anything typed.
type runLogger struct {
	mu    sync.Mutex
	f     *os.File
	path  string
	start time.Time

	cur      string    // the file being read
	curSince time.Time // since when
	warned   time.Duration
	stopDog  chan struct{}
}

var runlog = &runLogger{}

// keepLogs is how many run logs stay on disk.
const keepLogs = 10

// slowRead is when a file that has not finished reading gets a log line.
const slowRead = 5 * time.Second

func (l *runLogger) open(env *sys.OS, command string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		return
	}
	dir := filepath.Join(env.CacheDir(), "logs")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return
	}
	l.start = time.Now()
	name := l.start.Format("2006-01-02T15-04-05") + "-" + strings.ReplaceAll(strings.TrimPrefix(command, "dothaven "), " ", "-") + ".log"
	f, err := os.OpenFile(filepath.Join(dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return
	}
	l.f, l.path = f, f.Name()
	fmt.Fprintf(f, "%s %s\n", l.start.Format(time.RFC3339), command)
	pruneLogs(dir)
	l.stopDog = make(chan struct{})
	go l.watch()
}

func pruneLogs(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	var logs []string
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".log") {
			logs = append(logs, e.Name())
		}
	}
	sort.Strings(logs) // named by time
	for len(logs) > keepLogs {
		_ = os.Remove(filepath.Join(dir, logs[0]))
		logs = logs[1:]
	}
}

func (l *runLogger) line(format string, args ...any) {
	if l.f == nil {
		return
	}
	fmt.Fprintf(l.f, "%8.1fs  %s\n", time.Since(l.start).Seconds(), fmt.Sprintf(format, args...))
}

// stepf records a step of the run.
func (l *runLogger) stepf(format string, args ...any) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.line(format, args...)
}

// reading records where a backup's walk is. A new entry gets a line; a file
// only gets one if it is still being read after slowRead (see watch).
func (l *runLogger) reading(dest string, file bool) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !file {
		l.line("reading %s", dest)
	}
	l.cur, l.curSince, l.warned = dest, time.Now(), 0
}

// command records an external command, its time and how it ended.
func (l *runLogger) command(args []string, took time.Duration, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	end := "ok"
	if err != nil {
		end = err.Error()
	}
	l.line("ran %s (%s, %s)", strings.Join(args, " "), took.Round(time.Millisecond), end)
}

// watch notes a file that is taking long to read: on a Mac that is usually
// macOS holding the read for a privacy prompt, or a cloud file downloading.
func (l *runLogger) watch() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for {
		select {
		case <-l.stopDog:
			return
		case <-t.C:
			l.mu.Lock()
			if l.cur != "" {
				if d := time.Since(l.curSince); d >= slowRead && d >= 2*l.warned {
					l.line("still reading %s after %s", l.cur, d.Round(time.Second))
					l.warned = d
				}
			}
			l.mu.Unlock()
		}
	}
}

// done clears the file being read, when a walk ends.
func (l *runLogger) done() {
	l.mu.Lock()
	l.cur = ""
	l.mu.Unlock()
}

func (l *runLogger) close(err error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	close(l.stopDog)
	var ee ExitError
	switch {
	case err == nil:
		l.line("finished")
	case errors.As(err, &ee):
		l.line("ended with exit code %d", ee.Code)
	default:
		l.line("ended: %v", err)
	}
	_ = l.f.Close()
	l.f = nil
}

// Path is the log of this run, or "" when there is none.
func (l *runLogger) Path() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.path
}
