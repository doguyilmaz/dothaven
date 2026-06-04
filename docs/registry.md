# Config Registry

The config registry is the **single source of truth** for what configs exist, where they live on each OS, and how they should be collected, backed up, and restored. It replaced 11 hardcoded collector files and a 70-line backup sources file with a single declarative array.

## Why a Registry?

Before the registry, adding a new tool to the CLI required editing three separate places:

1. **Collector file** (e.g., `src/collectors/shell.ts`) — to read the file
2. **Backup sources** (`src/backup/sources.ts`) — to copy the file
3. **Restore map** (implicitly via backup sources) — to know where to put it back

Now, you add **one entry** to `src/registry/entries.ts` and collection, backup, and restore all pick it up automatically.

## ConfigEntry Type

```typescript
type Platform = "darwin" | "linux" | "win32";

type EntryKind =
  | { type: "file" }                           // Read full file content
  | { type: "file"; metadata: true }           // Only check existence + line count
  | { type: "dir" }                            // List directory contents
  | { type: "json-extract"; fields: string[] }; // Extract specific JSON fields as pairs

interface ConfigEntry {
  id: string;                                 // Section name in JSON snapshot
  name: string;                               // Human-readable label
  paths: Partial<Record<Platform, string>>;   // Per-OS source paths
  category: string;                           // Powers --only/--skip filtering
  kind: EntryKind;                            // How to process this entry
  backupDest: string;                         // Relative path in backup directory
  sensitivity: "low" | "medium" | "high";     // Sensitivity classification
  redact?: (content: string) => string;       // Optional custom redaction function
}
```

### Entry Kinds Explained

#### `{ type: "file" }` — Standard file

Reads the full file content. Used for most configs (`.zshrc`, `.gitconfig`, MCP configs, etc.).

- **Collect**: reads file → creates section with `content` field
- **Backup**: reads file → scans → redacts → writes copy

#### `{ type: "file", metadata: true }` — Metadata-only file

Checks if file exists and counts lines. Does **not** read full content into the report.

- **Collect**: creates section with `pairs: { exists: "true", lines: "N" }`
- **Backup**: reads and copies the full file (metadata is a collect-side optimization)

Currently used for `.p10k.zsh` — the file is large and its content isn't useful in a JSON snapshot, but its existence and size matter.

#### `{ type: "dir" }` — Directory listing

Scans directory contents and lists file names.

- **Collect**: creates section with `items` (file names)
- **Backup**: copies all files recursively

Used for skills directories (`~/.claude/skills/`, `~/.cursor/skills/`, etc.).

#### `{ type: "json-extract", fields: string[] }` — JSON field extraction

Reads a JSON file and extracts specific fields as key-value pairs.

- If `fields` is non-empty: extracts only those fields
- If `fields` is empty (`[]`): extracts **all** top-level fields
- Object values are flattened: `{ permissions: { readOnly: true } }` → `pairs: { readOnly: "true" }`
- Scalar values are stringified: `{ version: 2 }` → `pairs: { version: "2" }`

Used for Claude settings (`fields: ["permissions", "enabledPlugins"]`) and Gemini settings (`fields: []` — extract all).

## Complete Entry List

### AI — Claude

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `ai.claude.settings` | json-extract (`permissions`, `enabledPlugins`) | `~/.claude/settings.json` | `ai/claude/settings.json` |
| `ai.claude.skills` | dir | `~/.claude/skills` | `ai/claude/skills` |
| `ai.claude.md` | file | `~/.claude/CLAUDE.md` | `ai/claude/CLAUDE.md` |

### AI — Cursor

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `ai.cursor.mcp` | file | `~/.cursor/mcp.json` | `ai/cursor/mcp.json` |
| `ai.cursor.skills` | dir | `~/.cursor/skills` | `ai/cursor/skills` |

### AI — Gemini

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `ai.gemini.settings` | json-extract (all fields) | `~/.gemini/settings.json` | `ai/gemini/settings.json` |
| `ai.gemini.skills` | dir | `~/.gemini/skills` | `ai/gemini/skills` |
| `ai.gemini.md` | file | `~/.gemini/GEMINI.md` | `ai/gemini/GEMINI.md` |

### AI — Windsurf

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `ai.windsurf.mcp` | file | `~/.codeium/windsurf/mcp_config.json` | `ai/windsurf/mcp_config.json` |
| `ai.windsurf.skills` | dir | `~/.codeium/windsurf/skills` | `ai/windsurf/skills` |

### Shell

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `shell.zshrc` | file | `~/.zshrc` | `shell/.zshrc` |

::: info Windows exclusion
Shell configs have no `win32` path — they're automatically skipped on Windows.
:::

### Git

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `git.config` | file | `~/.gitconfig` | `git/.gitconfig` |
| `git.ignore` | file | `~/.gitignore_global` | `git/.gitignore_global` |
| `gh.config` | file | `~/.config/gh/config.yml` | `git/gh/config.yml` |

### Editors

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `editor.zed` | file | `~/.config/zed/settings.json` | `editor/zed/settings.json` |
| `editor.cursor` | file | `~/Library/Application Support/Cursor/User/settings.json` | `editor/cursor/settings.json` |
| `editor.nvim` | file | `~/.config/nvim/init.lua` | `editor/nvim/init.lua` |
| `editor.vimrc` | file | `~/.vimrc` | `editor/.vimrc` |

### Terminal

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `terminal.p10k` | file (metadata) | `~/.p10k.zsh` | `terminal/.p10k.zsh` |
| `terminal.tmux` | file | `~/.tmux.conf` | `terminal/.tmux.conf` |

### SSH

| ID | Kind | Path (macOS) | Backup Dest | Sensitivity | Custom Redact |
|----|------|-------------|-------------|-------------|---------------|
| `ssh.config` | file | `~/.ssh/config` | `ssh/config` | medium | `redactSshConfig()` |

### npm

| ID | Kind | Path (macOS) | Backup Dest | Sensitivity | Custom Redact |
|----|------|-------------|-------------|-------------|---------------|
| `npm.config` | file | `~/.npmrc` | `npm/.npmrc` | high | `redactNpmTokens()` |

### Bun

| ID | Kind | Path (macOS) | Backup Dest |
|----|------|-------------|-------------|
| `bun.config` | file | `~/.bunfig.toml` | `bun/.bunfig.toml` |

## Path Resolution

Paths in registry entries use template strings:

| Template | Expanded To | Platform |
|----------|-------------|----------|
| `~` | `$HOME` | All |
| `%APPDATA%` | `process.env.APPDATA` | Windows |
| `%USERPROFILE%` | `process.env.USERPROFILE` (fallback: `$HOME`) | Windows |

The `resolvePath(entry, home)` function handles expansion:

```typescript
function resolvePath(entry: ConfigEntry, home: string): string | null {
  const platform = process.platform as Platform;
  const template = entry.paths[platform];
  if (!template) return null;  // No path for this OS → skip
  return template
    .replace("~", home)
    .replace("%APPDATA%", Bun.env.APPDATA ?? "")
    .replace("%USERPROFILE%", Bun.env.USERPROFILE ?? home);
}
```

If an entry has no path for the current platform, it returns `null` and the entry is skipped everywhere — collection, backup, and restore.

## How the Registry Generates Collectors

`registryCollector(entries)` returns a single `Collector` function that processes all entries:

```typescript
function registryCollector(entries: ConfigEntry[]): Collector {
  return async (ctx) => {
    const result: CollectorResult = {};
    for (const entry of entries) {
      // Resolve path for current platform
      // Switch on entry.kind.type: file, dir, json-extract
      // Apply redaction if ctx.redact and entry.redact exists
      // Wrap in try/catch — missing files silently skipped
    }
    return result;
  };
}
```

This single function replaced 11 separate collector files.

## How the Registry Generates Backup Sources

`registryBackupSources(entries)` groups entries by category and creates `BackupSource[]`:

```typescript
function registryBackupSources(entries: ConfigEntry[]): BackupSource[] {
  // Filter entries for current platform
  // Group by category
  // For each category: create BackupSource with entries(home) function
  // File entries include custom redact function if defined
  // Dir entries are typed as BackupDir
}
```

The resulting `BackupSource[]` is used directly by the backup command and the restore plan builder.

## Non-Registry Collectors

Some data sources are too complex for the declarative registry and remain as hand-written collectors:

| Collector | Why Not Registry |
|-----------|-----------------|
| `collectMeta` | Dynamic data (hostname, OS, date) — not a file |
| `collectSsh` | Parses SSH config into structured host table with columns — needs custom parsing |
| `collectOllama` | Runs `ollama list` command — not a file |
| `collectApps` | Checks app existence, reads macOS defaults — complex multi-source |
| `collectHomebrew` | Runs `brew list` commands — not a file |

These collectors run alongside the registry collector in `Promise.allSettled`.

## Adding a New Config

To add support for a new tool:

1. Add an entry to `src/registry/entries.ts`:

```typescript
{
  id: "editor.helix",
  name: "Helix Config",
  paths: { darwin: "~/.config/helix/config.toml", linux: "~/.config/helix/config.toml" },
  category: "editor",
  kind: { type: "file" },
  backupDest: "editor/helix/config.toml",
  sensitivity: "low",
},
```

2. That's it. The new entry will automatically:
   - Be collected by `registryCollector` → appears as `editor.helix` section in JSON snapshots
   - Be backed up by `registryBackupSources` → copies to `editor/helix/config.toml` in backup dir
   - Be restorable → `dothaven restore` maps it back to `~/.config/helix/config.toml`
   - Respect `--only editor` and `--skip editor` filtering
   - Be scanned for sensitivity

If the new config needs custom redaction, add a `redact` function:

```typescript
{
  // ...
  redact: (content) => content.replace(/token\s*=\s*\S+/g, "token = [REDACTED]"),
}
```
