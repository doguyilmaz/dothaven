---
title: Snapshot format
weight: 15
---

A snapshot is dothaven's machine inventory: a single JSON file. `dothaven collect`
writes one to `~/.local/share/dothaven/snapshots/<host>-<UTC timestamp>.json`, and
every backup carries one as `inventory/snapshot.json` (the installed-software part,
without the config file contents). `compare`, `list`, `missing` and `reinstall` all
read the same format. The schema is small, deterministic, and designed to read
cleanly in a git diff.

## Top-level shape

A snapshot is a JSON object whose keys are **section ids** and whose values are
**sections**:

```go
// Snapshot is a machine inventory keyed by section id (e.g. "runtimes.go").
type Snapshot map[string]Section
```

A section id is a dotted name describing one area of inventory, for example
`runtimes.go`, `apps.brew.formulae`, or `shell.zshrc`. Registry entries use their
registry ID; collectors use the ids listed on the [Collectors](../collectors) page.
The set of ids depends on what is found on the machine; there is no fixed
enumeration in the format itself.

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

- **absent**: the field is `nil` and omitted entirely (the section has no
  content block).
- **empty**: the field points at `""` and serializes as `"content": ""`
  (a content block that exists but is empty).
- **non-empty**: the field points at the text.

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
  snippets stay readable instead of turning into `<` / `&`.

{{< callout type="info" >}}
Snapshots are plain JSON. You can read, `grep`, `jq`, and version-control them
directly. Nothing about the format is specific to dothaven beyond the section
naming convention.
{{< /callout >}}

## A realistic example

A trimmed snapshot with one section of each shape:

```json
{
  "meta": {
    "pairs": {
      "date": "2026-09-27",
      "host": "studio",
      "os": "darwin arm64"
    }
  },
  "packages.npm.global": {
    "items": [
      { "raw": "typescript@5.6.2", "columns": ["typescript", "5.6.2"] },
      { "raw": "@biomejs/biome@1.9.4", "columns": ["@biomejs/biome", "1.9.4"] }
    ]
  },
  "runtimes.go": {
    "pairs": {
      "platform": "darwin/arm64",
      "version": "go1.26.3"
    }
  },
  "shell.zshrc": {
    "content": "export EDITOR=nvim\nalias ll='ls -la'"
  }
}
```

Note the ordering: section ids (`meta`, `packages.npm.global`, `runtimes.go`,
`shell.zshrc`) and pair keys (`date`, `host`, `os`) are alphabetical, regardless
of the order the collectors ran in. Empty fields never appear: `meta` has only
`pairs`, so no `items` or `content` keys are emitted.

## Parsing fails on bad input

`Parse` decodes a snapshot with `json.Unmarshal`. Missing section fields default
to their zero values (`nil` map, slice, or pointer), so a section that only
defines `pairs` round-trips cleanly. But anything structurally wrong is an
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
parsing. Because commands like `compare` and `missing` read arbitrary files
supplied by the user, failing fast surfaces a bad input immediately instead of
producing a misleading half-parsed diff.

## How comparison works

`compare` diffs two snapshots and prints what changed. The first argument is the
**left** side, the second is the **right** side.

```bash
dothaven compare old.json new.json
```

With no arguments, it compares your two newest snapshots, with the **older one
first** (left):

```bash
dothaven compare
```

### Orientation

The diff is oriented around the **left** (first) snapshot:

- **added** (`+`): present only in the left snapshot.
- **removed** (`-`): present only in the right snapshot.
- **changed** (`~`): present in both, but with differing contents.
- **equal**: present in both and identical.

Every added or removed line also names the snapshot it is only in, so the output
reads correctly whichever order you pass the files in.

### What counts as a change

For a section present on both sides, the three fields are diffed independently:

- **Items** are matched on their `raw` string. An item is _added_ if its `raw`
  is in left only, _removed_ if in right only, _common_ otherwise.
- **Pairs** are matched on key. A key is _added_ (left only), _removed_ (right
  only), _changed_ (present on both with different values), or _common_.
- **Content** is compared by value, treating `nil` as distinct from `""`.

A both-present section is reported as **changed** if there is any item add or
remove, any pair add, remove, or change, or a content change. Items that exist
on both sides do not, on their own, make a section changed.

### Output

`compare` renders only the differences (equal sections and the dim `=` common
lines are suppressed), colorizing the output when stdout is a terminal:

```text
- [fonts.user]  (only in new)
  - JetBrainsMono-Regular.ttf  (only in new)
[runtimes.go]
  ~ version = go1.26.2 → go1.26.3
+ [runtimes.rust.crates]  (only in old)
  + ripgrep@14.1.0  (only in old)
```

The labels are the file names without `.json`.

Sections are listed alphabetically. Leading `+`, `-`, and `~` mark added,
removed, and changed entries; section headers without a marker are sections that
exist on both sides. When nothing
differs, `compare` prints `No differences found.`

## Producing a snapshot

Snapshots come from `collect`, which inventories the live machine and writes a
timestamped, owner-only file:

```bash
dothaven collect
```

```text
Snapshot saved to: /Users/you/.local/share/dothaven/snapshots/studio-20260927233425.json
  24 sections. Browse with `dothaven list <section>`, e.g. `dothaven list brew`.
```

Relevant flags:

| Flag           | Effect                                                                 |
| -------------- | --------------------------------------------------------------------- |
| `-o`, `--output` | Output directory (default: `~/.local/share/dothaven/snapshots`). |
| `--no-redact`  | Keep raw values (skip secret redaction).                              |
| `--slim`       | Truncate long file contents to 10 lines.                             |

To read sections back out of the newest snapshot by fuzzy name, or out of a
backup's inventory:

```bash
dothaven list runtimes
dothaven list brew ~/Downloads/backup-studio-20260927232938.tar.gz.age
```

Snapshots from older versions of dothaven, which wrote them next to other files in
the data folder or into a `reports/` folder inside git repositories, are still found
by `list`, `compare` and `missing`.

{{< cards >}}
  {{< card link="../commands" title="Commands" subtitle="Full reference for collect, compare, list, and more" >}}
  {{< card link="../collectors" title="Collectors" subtitle="What each section comes from" >}}
{{< /cards >}}
