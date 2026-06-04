// Package snapshot defines the machine-inventory model and its JSON
// serialization, comparison, and diff rendering.
package snapshot

// Snapshot is a machine inventory keyed by section id (e.g. "runtimes.go").
type Snapshot map[string]Section

// Section holds one area of inventory. Empty fields are omitted on serialize.
//
//   - Pairs:   key/value metadata (e.g. version → "go1.26.3")
//   - Items:   tabular rows (e.g. installed packages)
//   - Content: a free-form text block (e.g. a shell rc file)
//
// Content is a pointer so the three states stay distinct: absent (nil → omitted),
// empty (&"" → emitted as ""), and non-empty.
type Section struct {
	Pairs   map[string]string `json:"pairs,omitempty"`
	Items   []Item            `json:"items,omitempty"`
	Content *string           `json:"content,omitempty"`
}

// Item is one tabular row: the original line plus its split columns.
type Item struct {
	Raw     string   `json:"raw"`
	Columns []string `json:"columns,omitempty"`
}
