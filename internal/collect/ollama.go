package collect

import (
	"regexp"
	"strings"

	"github.com/doguyilmaz/dothaven/internal/snapshot"
)

// OllamaModel is one row of `ollama list` (the ID column is dropped).
type OllamaModel struct {
	Name     string
	Size     string
	Modified string
}

// ollamaColSep splits a table row on runs of 2+ spaces.
var ollamaColSep = regexp.MustCompile(`\s{2,}`)

// ParseOllamaList parses `ollama list` table output (header row + columns
// NAME  ID  SIZE  MODIFIED). The ID column is dropped; rows without a name are
// skipped.
func ParseOllamaList(text string) []OllamaModel {
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) <= 1 {
		return nil
	}
	var out []OllamaModel
	for _, line := range lines[1:] {
		var parts []string
		for _, p := range ollamaColSep.Split(line, -1) {
			if p = strings.TrimSpace(p); p != "" {
				parts = append(parts, p)
			}
		}
		m := OllamaModel{}
		if len(parts) > 0 {
			m.Name = parts[0]
		}
		if len(parts) > 2 {
			m.Size = parts[2]
		}
		if len(parts) > 3 {
			m.Modified = parts[3]
		}
		if m.Name != "" {
			out = append(out, m)
		}
	}
	return out
}

// OllamaCollector runs `ollama list` and reports installed models.
func OllamaCollector(c Ctx) snapshot.Snapshot {
	out, _ := c.Env.Run(c.Context, "ollama", "list")
	models := ParseOllamaList(out)
	if len(models) == 0 {
		return snapshot.Snapshot{}
	}

	items := make([]snapshot.Item, 0, len(models))
	for _, m := range models {
		var cols []string
		for _, v := range []string{m.Name, m.Size, m.Modified} {
			if v != "" {
				cols = append(cols, v)
			}
		}
		items = append(items, snapshot.Item{Raw: strings.Join(cols, " | "), Columns: cols})
	}

	return snapshot.Snapshot{
		"ai.ollama.models": snapshot.Section{Items: items},
	}
}
