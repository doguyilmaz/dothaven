// Package macprefs reads macOS preference domains and decides which keys are
// a setting somebody chose and which are just state an app happened to write.
//
// Everything here is a pure function over bytes; running `defaults` is the
// caller's job.
package macprefs

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Kind is what a preference value holds.
type Kind int

const (
	// Composite is the zero value: anything that is not a single scalar —
	// dictionaries, arrays, data blobs, dates. `defaults write` cannot replay
	// these from a flat value, and they are overwhelmingly app state.
	Composite Kind = iota
	String
	Int
	Float
	Bool
)

// Value is one preference, rendered for `defaults write`.
type Value struct {
	Kind Kind
	// S is the literal to pass to `defaults write`: the string itself, the
	// digits, or "true"/"false". Empty for Composite.
	S string
}

// Parse decodes the XML plist `defaults export` produces into its top-level
// keys. Nested keys are deliberately not flattened: `defaults write` addresses
// top-level keys only.
func Parse(b []byte) (map[string]Value, error) {
	dec := xml.NewDecoder(bytes.NewReader(b))
	out := map[string]Value{}

	depth := 0         // how many <dict>/<array> levels deep we are
	var pending string // the key whose value comes next
	haveKey := false
	sawRoot := false

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("malformed plist: %w", err)
		}

		switch t := tok.(type) {
		case xml.StartElement:
			name := t.Name.Local

			// Anything below the outer dict is somebody else's structure. Skip
			// the whole subtree so a nested <key> cannot be mistaken for a
			// preference — writing one back would invent a top-level key that
			// never existed.
			if depth > 1 || (depth == 1 && !haveKey && name != "key") {
				if err := dec.Skip(); err != nil {
					return nil, fmt.Errorf("malformed plist: %w", err)
				}
				continue
			}

			switch name {
			case "plist":
				sawRoot = true
			case "dict", "array":
				depth++
				if depth > 1 && haveKey {
					out[pending] = Value{Kind: Composite}
					haveKey = false
					dec.Skip()
					depth--
				}
			case "key":
				var s string
				if err := dec.DecodeElement(&s, &t); err != nil {
					return nil, fmt.Errorf("malformed plist: %w", err)
				}
				pending, haveKey = s, true
			default:
				if !haveKey {
					if err := dec.Skip(); err != nil {
						return nil, fmt.Errorf("malformed plist: %w", err)
					}
					continue
				}
				v, err := scalar(dec, &t)
				if err != nil {
					return nil, err
				}
				out[pending] = v
				haveKey = false
			}

		case xml.EndElement:
			if t.Name.Local == "dict" || t.Name.Local == "array" {
				depth--
			}
		}
	}
	if !sawRoot && len(out) == 0 {
		return nil, errors.New("not a plist")
	}
	return out, nil
}

// scalar decodes one value element, collapsing everything that is not a single
// value to Composite.
func scalar(dec *xml.Decoder, start *xml.StartElement) (Value, error) {
	switch start.Name.Local {
	case "true", "false":
		// Self-closing, so there is no character data to read.
		if err := dec.Skip(); err != nil {
			return Value{}, err
		}
		return Value{Bool, start.Name.Local}, nil
	case "string", "integer", "real":
		var s string
		if err := dec.DecodeElement(&s, start); err != nil {
			return Value{}, fmt.Errorf("malformed plist: %w", err)
		}
		s = strings.TrimSpace(s)
		switch start.Name.Local {
		case "string":
			return Value{String, s}, nil
		case "integer":
			return Value{Int, s}, nil
		default:
			return Value{Float, s}, nil
		}
	default:
		// data, date, and anything Apple adds later.
		if err := dec.Skip(); err != nil {
			return Value{}, err
		}
		return Value{Kind: Composite}, nil
	}
}
