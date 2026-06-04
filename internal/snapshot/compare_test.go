package snapshot

import (
	"reflect"
	"testing"
)

func sec(opts func(*Section)) Section {
	s := Section{}
	if opts != nil {
		opts(&s)
	}
	return s
}
func item(raw string, cols ...string) Item {
	if len(cols) == 0 {
		cols = []string{raw}
	}
	return Item{Raw: raw, Columns: cols}
}

func TestCompareOrientation(t *testing.T) {
	d := Compare(
		Snapshot{"onlyLeft": {}},
		Snapshot{"onlyRight": {}},
	)
	if d["onlyLeft"].Status != StatusAdded {
		t.Errorf("left-only should be added, got %s", d["onlyLeft"].Status)
	}
	if d["onlyRight"].Status != StatusRemoved {
		t.Errorf("right-only should be removed, got %s", d["onlyRight"].Status)
	}
}

func TestCompareItems(t *testing.T) {
	left := Snapshot{"s": {Items: []Item{item("keep"), item("ladd")}}}
	right := Snapshot{"s": {Items: []Item{item("keep"), item("radd")}}}
	d := Compare(left, right)["s"]
	if d.Status != StatusChanged {
		t.Errorf("want changed, got %s", d.Status)
	}
	if !reflect.DeepEqual(d.Items.Added, []string{"ladd"}) {
		t.Errorf("added: %v", d.Items.Added)
	}
	if !reflect.DeepEqual(d.Items.Removed, []string{"radd"}) {
		t.Errorf("removed: %v", d.Items.Removed)
	}
	if !reflect.DeepEqual(d.Items.Common, []string{"keep"}) {
		t.Errorf("common: %v", d.Items.Common)
	}
}

func TestComparePairs(t *testing.T) {
	left := Snapshot{"s": {Pairs: map[string]string{"same": "1", "chg": "L", "onlyL": "l"}}}
	right := Snapshot{"s": {Pairs: map[string]string{"same": "1", "chg": "R", "onlyR": "r"}}}
	d := Compare(left, right)["s"]
	if !reflect.DeepEqual(d.Pairs.Added, map[string]string{"onlyL": "l"}) {
		t.Errorf("added: %v", d.Pairs.Added)
	}
	if !reflect.DeepEqual(d.Pairs.Removed, map[string]string{"onlyR": "r"}) {
		t.Errorf("removed: %v", d.Pairs.Removed)
	}
	if d.Pairs.Changed["chg"] != (PairChange{Left: "L", Right: "R"}) {
		t.Errorf("changed: %v", d.Pairs.Changed)
	}
	if !reflect.DeepEqual(d.Pairs.Common, map[string]string{"same": "1"}) {
		t.Errorf("common: %v", d.Pairs.Common)
	}
}

func TestCompareCommonOnlyIsEqual(t *testing.T) {
	c := "c"
	s := Snapshot{"s": {Items: []Item{item("a")}, Pairs: map[string]string{"k": "v"}, Content: &c}}
	clone := Snapshot{"s": {Items: []Item{item("a")}, Pairs: map[string]string{"k": "v"}, Content: &c}}
	if got := Compare(s, clone)["s"].Status; got != StatusEqual {
		t.Errorf("identical sections should be equal, got %s", got)
	}
}

func TestCompareContent(t *testing.T) {
	x := "x"
	d := Compare(Snapshot{"s": {Content: &x}}, Snapshot{"s": {}})["s"]
	if !d.Content.Changed || d.Content.Left == nil || d.Content.Right != nil {
		t.Errorf("content diff wrong: %+v", d.Content)
	}
	if d.Status != StatusChanged {
		t.Errorf("want changed, got %s", d.Status)
	}
}

func TestFormatChangesOnly(t *testing.T) {
	// identical → empty output (so callers can print "No differences")
	s := Snapshot{"x": {Items: []Item{item("i")}, Pairs: map[string]string{"k": "v"}}}
	clone := Snapshot{"x": {Items: []Item{item("i")}, Pairs: map[string]string{"k": "v"}}}
	if out := Compare(s, clone).Format(FormatOptions{ChangesOnly: true}); out != "" {
		t.Errorf("identical snapshots should render empty, got %q", out)
	}
}

func TestFormatChangesOnlyKeepsChanges(t *testing.T) {
	left := Snapshot{
		"keep": {Items: []Item{item("a")}},
		"chg":  {Items: []Item{item("x"), item("only")}},
	}
	right := Snapshot{
		"keep": {Items: []Item{item("a")}},
		"chg":  {Items: []Item{item("x")}},
	}
	out := Compare(left, right).Format(FormatOptions{ChangesOnly: true})
	want := "[chg]\n  + only  (only in left)" // 'keep' (equal) gone, common 'x' gone
	if out != want {
		t.Errorf("changesOnly output:\n got %q\nwant %q", out, want)
	}
}

func TestFormatColorAndLabels(t *testing.T) {
	out := Compare(Snapshot{"x": {Items: []Item{item("i")}}}, Snapshot{}).
		Format(FormatOptions{LeftLabel: "new", RightLabel: "old", Color: false})
	want := "+ [x]  (only in new)\n  + i  (only in new)"
	if out != want {
		t.Errorf("\n got %q\nwant %q", out, want)
	}

	colored := Compare(Snapshot{"x": {Items: []Item{item("i")}}}, Snapshot{"x": {}}).
		Format(FormatOptions{Color: true})
	if !contains(colored, "\x1b[32m+ i\x1b[0m") {
		t.Errorf("expected green add, got %q", colored)
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	}())
}
