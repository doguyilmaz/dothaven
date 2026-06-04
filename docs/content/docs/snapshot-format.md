---
title: Snapshot format
weight: 7
---

A snapshot is dothaven's machine inventory: a single JSON file produced by
`dothaven collect`. It is the unit that the snapshot-reading commands consume —
`compare`, `list`, and `doctor` all parse the same format. The schema is small,
deterministic, and designed to read cleanly in a git diff.

## Top-level shape

A snapshot is a JSON object whose keys are **section ids** and whose values are
**sections**:

```go
// Snapshot is a machine inventory keyed by section id (e.g. "runtimes.go").
type Snapshot map[string]Section
```

A section id is a dotted name describing one area of inventory, for example
`runtimes.go`, `packages.brew`, or `dotfiles.zshrc`. The set of ids depends on
what the collectors find on the machine; there is no fixed enumeration in the
format itself.

## The Section model

Each section carries up to three independent fields. All three are optional and
omitted from the JSON when empty.

```go
type Section struct {
	Pairs   map[string]string `json:"pairs,omitempty"`
	Items   []Item            `json:"items,omitempty"`
	Content *string           `json:"content,omitempty"`
}

type Item struct {
	Raw     string   `json:"raw"`
	Columns []string `json:"columns,omitempty"`
}
```

| Field     | JSON key  | Type                | Purpose                                                        |
| --------- | --------- | ------------------- | ------------------------------------------------------------- |
| `Pairs`   | `pairs`   | object (string→str) | Key/value metadata, e.g. `version → "go1.26.3"`.              |
| `Items`   | `items`   | array of `Item`     | Tabular rows, e.g. one installed package per row.            |
| `Content` | `content` | string (nullable)   | A free-form text block, e.g. the body of a shell rc file.    |

An `Item` keeps both the original line (`raw`) and its split form (`columns`).
`raw` is always present; `columns` is omitted when the row was not split.

### Why `content` is a pointer

`Content` is a `*string`, not a plain `string`, so three states stay
distinguishable in JSON:

- **absent** — the field is `nil` and omitted entirely (the section has no
  content block).
- **empty** — the field points at `""` and serializes as `"content": ""`
  (a content block that exists but is empty).
- **non-empty** — the field points at the text.

A plain `string` would collapse the first two cases together. The pointer keeps
"no content" and "empty content" apart.

## Determinism

Snapshots are written so that re-running `collect` on an unchanged machine
produces a byte-identical-shaped file, and so that real changes show up as
minimal, readable git diffs. The serializer enforces three rules:

```go
enc := json.NewEncoder(&buf)
enc.SetEscapeHTML(false)
enc.SetIndent("", "  ")
```

- **Alphabetical keys.** `encoding/json` emits map keys in deterministic
  alphabetical order. Both the top-level section ids and the keys inside `pairs`
  are sorted, so the diff between two runs reflects content changes, not map
  iteration order.
- **Two-space indent.** Pretty-printed with a two-space indent and a trailing
  newline.
- **HTML escaping disabled.** Go's encoder escapes `<`, `>`, and `&` by default
  (a safe choice for HTML embedding, useless here). dothaven turns it off so
  values like URLs (`?a=1&b=2`), version constraints (`node>=18`), and shell
  snippets stay readable instead of being mangled into `<` / `&`.

{{< callout type="info" >}}
Snapshots are plain JSON. You can read, `grep`, `jq`, and version-control them
directly — nothing about the format is dothaven-specific beyond the section
naming convention.
{{< /callout >}}

## A realistic example

A trimmed snapshot with one section of each shape:

```json
{
  "meta": {
    "pairs": {
      "host": "studio.local",
      "os": "darwin"
    }
  },
  "packages.brew": {
    "items": [
      { "raw": "git 2.44.0", "columns": ["git", "2.44.0"] },
      { "raw": "jq 1.7.1", "columns": ["jq", "1.7.1"] }
    ]
  },
  "runtimes.go": {
    "pairs": {
      "version": "go1.26.3"
    }
  },
  "dotfiles.zshrc": {
    "content": "export EDITOR=nvim\nalias ll='ls -la'\n"
  }
}
```

Note the ordering: section ids (`dotfiles.zshrc`, `meta`, `packages.brew`,
`runtimes.go`) and pair keys (`host` before `os`) are alphabetical, regardless
of the order the collectors ran in. Empty fields never appear — `meta` has only
`pairs`, so no `items` or `content` keys are emitted.

## Parsing is fail-loud

`Parse` decodes a snapshot with `json.Unmarshal`. Missing section fields default
to their zero values (`nil` map, slice, or pointer), so a section that only
defines `pairs` round-trips cleanly. But anything structurally wrong is a loud
error rather than a silent coercion:

```go
func Parse(data []byte) (Snapshot, error) {
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("invalid snapshot: %w", err)
	}
	return s, nil
}
```

A non-object root, malformed JSON, or a non-string value inside `pairs` all fail
parsing. Because commands like `compare` and `doctor` read arbitrary files
supplied by the user, failing fast surfaces a bad input immediately instead of
producing a misleading half-parsed diff.

## How comparison works

`compare` diffs two snapshots and prints what changed. The first argument is the
**left** side, the second is the **right** side.

```bash
dothaven compare old.json new.json
```

With no arguments, it picks the two newest `.json` reports in `reports/`:

```bash
dothaven compare
```

### Orientation

The diff is oriented around the **left** snapshot. Callers pass the newer
snapshot as left, so the labels read naturally as change-since-the-older-run:

- **added** — present only in left (only in the newer snapshot).
- **removed** — present only in right (only in the older snapshot).
- **changed** — present in both, but with differing contents.
- **equal** — present in both and identical.

### What counts as a change

For a section present on both sides, the three fields are diffed independently:

- **Items** are matched on their `raw` string. An item is _added_ if its `raw`
  is in left only, _removed_ if in right only, _common_ otherwise.
- **Pairs** are matched on key. A key is _added_ (left only), _removed_ (right
  only), _changed_ (present on both with different values), or _common_.
- **Content** is compared by value, treating `nil` as distinct from `""`.

A both-present section is reported as **changed** if there is any item add or
remove, any pair add, remove, or change, or a content change. Items that merely
exist on both sides do not, on their own, make a section changed — common
content is not noise.

### Output

`compare` renders only the differences (equal sections and the dim `=` common
lines are suppressed), colorizing the output when stdout is a terminal:

```text
[runtimes.go]
  ~ version = go1.26.2 → go1.26.3
+ [packages.cargo]  (only in new.json)
  + ripgrep 14.1.0  (only in new.json)
- [fonts.user]  (only in old.json)
```

Leading `+`, `-`, and `~` mark added, removed, and changed entries; section
headers without a marker are sections that exist on both sides. When nothing
differs, `compare` prints `No differences found.`

## Producing a snapshot

Snapshots come from `collect`, which inventories the live machine and writes a
timestamped file:

```bash
dothaven collect
```

```text
Report saved to: /path/to/reports/studio.local-20260604-101500.json
```

Relevant flags:

| Flag           | Effect                                                                 |
| -------------- | --------------------------------------------------------------------- |
| `-o`, `--output` | Output directory. Default: `./reports` in a git repo, else `~/Downloads`. |
| `--no-redact`  | Keep raw values (skip secret redaction).                              |
| `--slim`       | Truncate long file contents to 10 lines.                             |

To read a single section back out of the most recent report by fuzzy name:

```bash
dothaven list runtimes
```

{{< cards >}}
  {{< card link="../commands" title="Commands" subtitle="Full reference for collect, compare, list, and more" >}}
  {{< card link="../quick-start" title="Quick start" subtitle="Collect and compare your first snapshot" >}}
{{< /cards >}}
