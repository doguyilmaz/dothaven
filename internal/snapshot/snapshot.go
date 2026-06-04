package snapshot

import (
	"bytes"
	"encoding/json"
	"fmt"
)

// Serialize renders the snapshot as pretty (2-space) JSON with a trailing
// newline. HTML escaping is disabled so values like URLs (?a=1&b=2), version
// constraints (node>=18), and shell snippets stay readable. encoding/json emits
// map keys in deterministic alphabetical order, which keeps git diffs stable.
func (s Snapshot) Serialize() ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return nil, fmt.Errorf("serialize snapshot: %w", err)
	}
	return buf.Bytes(), nil
}

// Parse decodes a JSON snapshot. Missing section fields default to their zero
// values (nil map/slice/pointer). A non-object root, malformed JSON, or a
// non-string pair value is a loud error — doctor/compare read arbitrary files,
// so we fail fast rather than silently coerce.
func Parse(data []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invalid snapshot: %w", err)
	}
	return s, nil
}
