package sys

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"sync"
	"time"
)

// tempPrefix starts every private temporary folder dothaven makes. The
// process ID follows it, so a later run can tell a folder whose owner is gone.
const tempPrefix = "dothaven-"

// tempMarker is written into every folder PrivateTempDir makes. The sweep
// removes nothing without it: a folder that merely looks like ours is not.
const tempMarker = ".dothaven-temp"

// tempName is exactly what PrivateTempDir names a folder: a known kind, the
// process ID, and MkdirTemp's random digits.
var tempName = regexp.MustCompile(`^dothaven-(push|github|install|restore)-([0-9]+)-[0-9]+$`)

// sweepAge is how old a dead run's folder must be before it is removed. The
// process check cannot see into another PID namespace (containers sharing
// /tmp), and no run of this tool holds a folder for an hour.
const sweepAge = time.Hour

var (
	tempMu sync.Mutex
	temps  = map[string]bool{}
)

// PrivateTempDir makes an owner-only temporary folder for decrypted or
// in-progress data, and returns it with the function that removes it. Until
// then it is tracked, so RemoveTempDirs can clear it on a forced exit, which
// skips deferred calls. kind names the use (restore, push, install).
func PrivateTempDir(kind string) (string, func(), error) {
	dir, err := os.MkdirTemp("", fmt.Sprintf("%s%s-%d-", tempPrefix, kind, os.Getpid()))
	if err != nil {
		return "", func() {}, err
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, err
	}
	if err := os.WriteFile(filepath.Join(dir, tempMarker), nil, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		return "", func() {}, err
	}
	tempMu.Lock()
	temps[dir] = true
	tempMu.Unlock()
	return dir, func() {
		_ = os.RemoveAll(dir)
		tempMu.Lock()
		delete(temps, dir)
		tempMu.Unlock()
	}, nil
}

// RemoveTempDirs removes every folder PrivateTempDir made that is still there.
// It is for the forced exit on a second Ctrl-C, where no defer runs.
func RemoveTempDirs() {
	tempMu.Lock()
	defer tempMu.Unlock()
	for dir := range temps {
		_ = os.RemoveAll(dir)
		delete(temps, dir)
	}
}

// SweepTempDirs removes temporary folders left by dothaven runs that are no
// longer alive — a crash, a kill -9, a power cut in the middle of a restore —
// so decrypted files do not stay behind. Only this user's folders are
// considered, and only those whose process is gone.
func SweepTempDirs() int {
	base := os.TempDir()
	entries, err := os.ReadDir(base)
	if err != nil {
		return 0
	}
	n := 0
	for _, e := range entries {
		pid, ok := tempOwner(e.Name())
		if !ok || !e.IsDir() || pid == os.Getpid() || processAlive(pid) {
			continue
		}
		p := filepath.Join(base, e.Name())
		if !ownedByMe(p) {
			continue
		}
		marker, err := os.Lstat(filepath.Join(p, tempMarker))
		if err != nil || !marker.Mode().IsRegular() || time.Since(marker.ModTime()) < sweepAge {
			continue
		}
		if os.RemoveAll(p) == nil {
			n++
		}
	}
	return n
}

// tempOwner parses the process ID out of "dothaven-<kind>-<pid>-<random>".
func tempOwner(name string) (int, bool) {
	m := tempName.FindStringSubmatch(name)
	if m == nil {
		return 0, false
	}
	pid, err := strconv.Atoi(m[2])
	return pid, err == nil && pid > 0
}
