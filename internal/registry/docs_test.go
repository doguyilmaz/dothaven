package registry

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var updateDocs = flag.Bool("update-docs", false, "rewrite the full list in docs/content/docs/registry.md from Entries")

const docsPath = "../../docs/content/docs/registry.md"

// The docs' full list is generated from Entries, so what the site promises is
// what a backup carries. Adding an entry without regenerating fails here:
//
//	go test ./internal/registry -run TestDocsList -update-docs
func TestDocsListEveryEntry(t *testing.T) {
	b, err := os.ReadFile(filepath.FromSlash(docsPath))
	if err != nil {
		t.Skip("docs not present")
	}
	doc := string(b)
	const start, end = "## The full list\n", "## Your own paths\n"
	i, j := strings.Index(doc, start), strings.Index(doc, end)
	if i < 0 || j < i {
		t.Fatalf("%s: cannot find the %q … %q section", docsPath, strings.TrimSpace(start), strings.TrimSpace(end))
	}
	want := start + "\n" + renderFullList(Entries)
	if *updateDocs {
		if err := os.WriteFile(filepath.FromSlash(docsPath), []byte(doc[:i]+want+doc[j:]), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	if got := doc[i:j]; got != want {
		t.Errorf("%s is out of date with the registry; run: go test ./internal/registry -run TestDocsList -update-docs", docsPath)
	}
}

func renderFullList(entries []Entry) string {
	var order []string
	byCat := map[string][]Entry{}
	for _, e := range entries {
		if e.Paths["darwin"] == "" && e.Paths["linux"] == "" {
			continue
		}
		if _, ok := byCat[e.Category]; !ok {
			order = append(order, e.Category)
		}
		byCat[e.Category] = append(byCat[e.Category], e)
	}
	var b strings.Builder
	for _, c := range order {
		fmt.Fprintf(&b, "### %s\n\n| Path | What | Handling |\n| --- | --- | --- |\n", c)
		for _, e := range byCat[c] {
			fmt.Fprintf(&b, "| %s | %s | %s |\n", docPath(e), e.Name, docMark(e))
		}
		b.WriteString("\n")
	}
	return b.String()
}

func docPath(e Entry) string {
	show := func(p string) string {
		if e.Kind == Dir {
			p += "/"
		}
		return "`" + p + "`"
	}
	mac, linux := e.Paths["darwin"], e.Paths["linux"]
	switch {
	case mac == linux:
		return show(mac)
	case linux == "":
		return show(mac) + " (macOS)"
	case mac == "":
		return show(linux) + " (Linux)"
	}
	return show(mac) + " (Linux: " + show(linux) + ")"
}

func docMark(e Entry) string {
	switch {
	case e.LocalOnly:
		return "🔑 never pushed to GitHub"
	case e.Sensitivity == High && e.Redact == nil:
		return "🔑"
	case e.Sensitivity != Low:
		return "🔒"
	}
	return ""
}
