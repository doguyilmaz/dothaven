package sys

import (
	"context"
	"os"
	"strings"
)

// Fake is an in-memory Env for tests: ReadFile/ListDir/Exists are served from
// maps, and Run looks up a stdout by the space-joined command.
type Fake struct {
	Files   map[string]string   // path → contents
	Dirs    map[string][]string // path → entry names
	Cmds    map[string]string   // "cmd arg…" → stdout
	Vars    map[string]string   // env var → value
	HomeDir string
}

func (f *Fake) Run(_ context.Context, args ...string) (string, error) {
	return f.Cmds[strings.Join(args, " ")], nil
}

func (f *Fake) ReadFile(path string) ([]byte, error) {
	if c, ok := f.Files[path]; ok {
		return []byte(c), nil
	}
	return nil, os.ErrNotExist
}

func (f *Fake) ListDir(path string) ([]string, error) {
	if d, ok := f.Dirs[path]; ok {
		return d, nil
	}
	return nil, os.ErrNotExist
}

func (f *Fake) Exists(path string) bool {
	if _, ok := f.Files[path]; ok {
		return true
	}
	_, ok := f.Dirs[path]
	return ok
}

func (f *Fake) Getenv(key string) string { return f.Vars[key] }
func (f *Fake) Home() string             { return f.HomeDir }
