package cli

import (
	"bytes"
	"strings"
	"testing"
)

// go test has no terminal, so these exercise the off-a-terminal path — the one
// that used to let migrate apply unattended.
func TestConfirmWriteRefusesWithoutATerminal(t *testing.T) {
	var out bytes.Buffer
	err := confirmWrite(&out, "do the thing?", false)

	var ee ExitError
	if !asExitError(err, &ee) || ee.Code != 1 {
		t.Fatalf("want ExitError{1}, got %#v", err)
	}
	if !strings.Contains(out.String(), "--yes") || !strings.Contains(out.String(), "--dry-run") {
		t.Errorf("refusal should name both ways out, got: %q", out.String())
	}
}

func TestConfirmWriteHonorsExplicitYes(t *testing.T) {
	var out bytes.Buffer
	if err := confirmWrite(&out, "do the thing?", true); err != nil {
		t.Fatalf("--yes should pass straight through, got %v", err)
	}
	if out.Len() != 0 {
		t.Errorf("nothing to say when consent was explicit, got: %q", out.String())
	}
}

func asExitError(err error, target *ExitError) bool {
	ee, ok := err.(ExitError)
	if ok {
		*target = ee
	}
	return ok
}
