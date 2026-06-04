# Snapshot Format (JSON)

Snapshots are plain JSON files written with a `.json` extension. They're human-readable, git-diffable, and parsed with the language's native `JSON.parse` — the CLI has **zero runtime dependencies**. All read/write/diff logic lives in-tree under `src/snapshot`. This page covers the on-disk shape and how the CLI works with it.

## Format Overview

A snapshot is a **flat map of section id → section**. Each section may carry three kinds of data:

1. **Pairs** — key-value metadata (`pairs`)
2. **Items** — tabular data, lists with columns (`items`)
3. **Content** — free-form text blocks (`content`)

Empty fields are omitted on write, so a section only contains the keys it actually uses. Files are pretty-printed with 2-space indentation.

Section ids use dot notation for hierarchy: `runtimes.go`, `packages.bun.global`, `shell.zshrc`.

## On-Disk Shape

```json
{
  "runtimes.go": { "pairs": { "version": "go1.26.3" } },
  "packages.bun.global": {
    "items": [{ "raw": "eas-cli@16.19.2", "columns": ["eas-cli", "16.19.2"] }]
  },
  "shell.zshrc": { "content": "alias ll='ls -la'\n..." }
}
```

- `pairs` — an object of `string → string`.
- `items` — an array of `{ raw, columns: string[] }`. `raw` is the original line; `columns` is its split form for tabular data.
- `content` — a free-form text block. Preserves whitespace and newlines.

Absent fields are omitted entirely. A pairs-only section has no `items` or `content` key, and so on.

## Example Snapshot

```json
{
  "meta": {
    "pairs": { "host": "MacBook-Pro", "os": "Darwin arm64", "date": "2026-04-07" }
  },
  "ai.claude.settings": {
    "pairs": { "enabledPlugins": "computer-use", "permissions.allow": "Edit,Write" }
  },
  "ai.claude.skills": {
    "items": [
      { "raw": "superskill.md", "columns": ["superskill.md"] },
      { "raw": "web-dev.md", "columns": ["web-dev.md"] }
    ]
  },
  "ai.ollama.models": {
    "items": [
      { "raw": "llama3.2:latest | 2.0 GB | 2 days ago", "columns": ["llama3.2:latest", "2.0 GB", "2 days ago"] },
      { "raw": "codellama:7b | 3.8 GB | 5 days ago", "columns": ["codellama:7b", "3.8 GB", "5 days ago"] }
    ]
  },
  "shell.zshrc": {
    "content": "export PATH=\"$HOME/bin:$PATH\"\nsource \"$HOME/.oh-my-zsh/oh-my-zsh.sh\""
  },
  "terminal.p10k": {
    "pairs": { "exists": "true", "lines": "1247" }
  },
  "apps.brew.formulae": {
    "items": [
      { "raw": "bat", "columns": ["bat"] },
      { "raw": "eza", "columns": ["eza"] },
      { "raw": "fd", "columns": ["fd"] },
      { "raw": "fzf", "columns": ["fzf"] },
      { "raw": "git", "columns": ["git"] },
      { "raw": "ripgrep", "columns": ["ripgrep"] }
    ]
  }
}
```

## Types

In-memory types live in `src/snapshot/types.ts`:

```typescript
interface Section {
  name: string;                                 // Section identifier
  pairs: Record<string, string>;                // Key-value pairs
  items: { raw: string; columns: string[] }[];  // List/tabular items
  content: string | null;                       // Free-form text content
}

type Snapshot = Record<string, Section>;        // section id → section
type CollectorResult = Record<string, Section>;
```

`Snapshot` and `CollectorResult` are both a flat `Record` keyed by section id. In memory every `Section` carries all four fields; on disk the empty ones are omitted.

## Reading and Writing

Serialization lives in `src/snapshot/serialize.ts`:

### `serializeSnapshot(snapshot: Snapshot): string`

Converts a snapshot to its on-disk JSON text. Drops empty `pairs`/`items`/`content`, then pretty-prints with `JSON.stringify` (2-space indent):

```typescript
import { serializeSnapshot } from "./snapshot/serialize";

const text = serializeSnapshot(snapshot);
// → pretty-printed JSON, empty fields omitted
```

### `parseSnapshot(text: string): Snapshot`

Parses snapshot JSON text back into in-memory `Section` objects, rehydrating omitted fields to their empty defaults (`pairs: {}`, `items: []`, `content: null`):

```typescript
import { parseSnapshot } from "./snapshot/serialize";

const snapshot = parseSnapshot(text);
// snapshot["meta"].pairs.host → "MacBook-Pro"
// snapshot["shell.zshrc"].content → "export PATH=..."
// snapshot["apps.brew.formulae"].items → [{ raw: "bat", columns: ["bat"] }, ...]
```

Both functions use native `JSON.stringify` / `JSON.parse` under the hood — no external parser.

## Comparing and Diffing

Diffing lives in `src/snapshot/compare.ts`:

### `compareSnapshots(left: Snapshot, right: Snapshot)`

Computes a structured diff between two snapshots — which sections were added, removed, or modified, and the per-field changes within each.

```typescript
import { compareSnapshots } from "./snapshot/compare";

const diff = compareSnapshots(left, right);
```

### `formatDiff(diff, options): string`

Renders a diff for human reading:

```typescript
import { formatDiff } from "./snapshot/compare";

const output = formatDiff(diff, {
  leftLabel: "home-mac",
  rightLabel: "work-mac",
  color: true,
});
```

Diff colours:

- **green `+`** — added
- **red `-`** — removed
- **yellow `~`** — modified
- **dim `=`** — unchanged

## CLI Usage

| Command | Behavior |
|---------|----------|
| `dotfiles collect` | Writes a `.json` snapshot to `reports/`. |
| `dotfiles compare` | Diffs two `.json` files (globs `*.json` in `reports/`). |
| `dotfiles doctor <snapshot.json>` | Reads a `.json` snapshot and reports on it. |
| `dotfiles list` | Prints a section from the newest `.json`. |

## Section Types in Practice

| Section Pattern | Data Type | Example |
|----------------|-----------|---------|
| `meta` | pairs | `host`, `os`, `date` |
| `ai.claude.settings` | pairs | JSON field extractions |
| `ai.claude.skills` | items | Directory listing |
| `ai.ollama.models` | items (tabular) | Name, size, modified columns |
| `ssh.hosts` | items (tabular) | Host, hostname, identity columns |
| `shell.zshrc` | content | Full file text |
| `git.config` | content | Full file text |
| `terminal.p10k` | pairs | `exists`, `lines` (metadata-only) |
| `apps.raycast` | pairs | `installed` status |
| `apps.brew.formulae` | items | Package list |

## Slim Mode

With `dotfiles collect --slim`, content sections are truncated to 10 lines:

```json
{
  "shell.zshrc": {
    "content": "export PATH=\"$HOME/bin:$PATH\"\nsource \"$HOME/.oh-my-zsh/oh-my-zsh.sh\"\n... (47 more lines)"
  }
}
```

This reduces file size by ~65% — useful for feeding to AI tools where full config content isn't needed but structure and key settings are.

## Naming Convention

Report files follow the pattern:

```
<hostname>-YYYYMMDDHHMMSS.json
```

Examples:

- `MacBook-Pro-20260407143022.json`
- `work-linux-20260401080000.json`

The timestamp prevents overwrites and enables chronological sorting.
