package cli

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/doguyilmaz/dothaven/internal/sys"
)

// An empty home is a fresh machine: every list the page loops over must be
// [] rather than null, or the page fails to draw at all.
func TestDashSummaryEmptyHomeHasNoNullLists(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", "")
	b, err := json.Marshal(dashSummary(context.Background(), sys.Real(), "test"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), "null") {
		t.Fatalf("summary has a null list: %s", b)
	}
}
