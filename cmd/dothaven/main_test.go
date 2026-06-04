package main

import (
	"testing"

	"github.com/rogpeppe/go-internal/testscript"
)

// TestMain lets the test binary re-exec itself as the `dothaven` command, so
// testscript .txtar files can drive the real CLI end-to-end.
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"dothaven": main,
	})
}

// TestScripts runs every .txtar in testdata/script against the real binary in a
// hermetic temp dir with HOME isolated. Scripts cover the FS/render commands
// (scan, security, compare, list, help/version) — no external tools are invoked.
func TestScripts(t *testing.T) {
	testscript.Run(t, testscript.Params{
		Dir: "testdata/script",
		Setup: func(e *testscript.Env) error {
			e.Setenv("HOME", e.WorkDir)
			return nil
		},
	})
}
