# Collectors

Collectors are functions that gather machine state and return structured data. They're the core data pipeline behind the `collect` command.

## Collector Interface

```typescript
interface CollectorContext {
  redact: boolean;   // true by default
  home: string;      // $HOME — injected for testability
}

type CollectorResult = Record<string, Section>;
type Collector = (ctx: CollectorContext) => Promise<CollectorResult>;
```

Every collector:
- Takes a `CollectorContext` with redaction flag and home directory
- Returns a `CollectorResult` — a map of section IDs to `Section` objects
- Returns `{}` if data is unavailable (no errors for missing tools/files)
- Wraps external commands in try/catch (tools may not be installed)

## Section Structure

Each section in a `.json` report uses `Section` from `src/snapshot/types.ts`:

```typescript
interface Section {
  name: string;
  pairs: Record<string, string>;              // Key-value metadata
  items: { raw: string; columns: string[] }[]; // Tabular data
  content: string | null;                      // Full text content
}
```

On disk, sections are written as a flat map of section ID → section, with empty
fields omitted and pretty-printed (2-space):

```json
{
  "runtimes.go": { "pairs": { "version": "go1.26.3" } },
  "packages.bun.global": { "items": [ { "raw": "eas-cli@16.19.2", "columns": ["eas-cli", "16.19.2"] } ] },
  "shell.zshrc": { "content": "alias ll='ls -la'\n..." }
}
```

The `makeSection()` helper creates sections with sensible defaults:

```typescript
makeSection("meta", { pairs: { host: "MacBook-Pro", os: "Darwin arm64" } })
makeSection("shell.zshrc", { content: "export PATH=..." })
makeSection("apps.brew.formulae", { items: [{ raw: "bat", columns: ["bat"] }] })
```

## Collector Lineup

The `collect` command runs these collectors in parallel via `Promise.allSettled`:

```typescript
const collectors = [
  collectMeta,                           // Machine metadata
  registryCollector(registryEntries),    // All 23 registry entries
  collectSsh,                            // SSH host table
  collectOllama,                         // Ollama model list
  collectApps,                           // macOS apps
  collectHomebrew,                       // Homebrew packages
];
```

If any collector fails (e.g., Homebrew not installed), the others still succeed. Only fulfilled results are merged.

---

## Registry Collector

**Source**: `src/registry/collector.ts`
**Type**: Generated from registry entries

The registry collector processes all 23 `ConfigEntry` objects and handles four kinds:

### File Kind

Reads the file and stores content:

```
~/.zshrc → section "shell.zshrc" with content = file text
```

If `ctx.redact` is true and entry has a custom `redact()` function, it's applied before storing.

### File + Metadata Kind

Checks existence and counts lines without storing full content:

```
~/.p10k.zsh → section "terminal.p10k" with pairs = { exists: "true", lines: "1247" }
```

Used for large files where content isn't useful in a snapshot.

### Directory Kind

Lists directory contents:

```
~/.claude/skills/ → section "ai.claude.skills" with items = [{ raw: "superskill.md" }, ...]
```

Uses `Bun.Glob('*').scan()` — lists top-level entries only, not recursive.

### JSON Extract Kind

Reads JSON and extracts specific fields as key-value pairs:

```
~/.claude/settings.json with fields: ["permissions", "enabledPlugins"]
→ section "ai.claude.settings" with pairs = { readOnly: "true", ... }
```

- Object fields are flattened (nested keys become top-level pairs)
- Scalar fields are stringified
- If `fields: []` (empty array), all top-level keys are extracted

### Registry Sections Produced

| Section ID | Kind | Category |
|------------|------|----------|
| `ai.claude.settings` | json-extract | ai |
| `ai.claude.skills` | dir | ai |
| `ai.claude.md` | file | ai |
| `ai.cursor.mcp` | file | ai |
| `ai.cursor.skills` | dir | ai |
| `ai.gemini.settings` | json-extract | ai |
| `ai.gemini.skills` | dir | ai |
| `ai.gemini.md` | file | ai |
| `ai.windsurf.mcp` | file | ai |
| `ai.windsurf.skills` | dir | ai |
| `shell.zshrc` | file | shell |
| `git.config` | file | git |
| `git.ignore` | file | git |
| `gh.config` | file | git |
| `editor.zed` | file | editor |
| `editor.cursor` | file | editor |
| `editor.nvim` | file | editor |
| `editor.vimrc` | file | editor |
| `terminal.p10k` | file+metadata | terminal |
| `terminal.tmux` | file | terminal |
| `ssh.config` | file | ssh |
| `npm.config` | file | npm |
| `bun.config` | file | bun |

---

## Hand-Written Collectors

### `collectMeta`

**Source**: `src/collectors/meta.ts`
**Section**: `meta`
**Type**: pairs

Captures machine identity:

| Pair | Source |
|------|--------|
| `host` | `os.hostname()` |
| `os` | `uname -s` + `uname -m` (e.g., `Darwin arm64`) |
| `date` | `new Date().toISOString()` date portion (e.g., `2026-04-07`) |

This is always the first section in a `.json` report.

### `collectSsh`

**Source**: `src/collectors/ssh.ts`
**Section**: `ssh.hosts`
**Type**: items (tabular)

Parses `~/.ssh/config` into a structured host table:

| Column | Source | Redacted |
|--------|--------|----------|
| Host | `Host` directive | Never (alias name) |
| HostName | `HostName` directive | `[REDACTED]` when `ctx.redact` |
| IdentityFile | `IdentityFile` directive | `[REDACTED]` when `ctx.redact` |

The parser:
1. Splits config by `Host` blocks
2. Extracts `HostName` and `IdentityFile` from each block
3. Ignores comments and blank lines
4. Returns structured items with columns

::: tip Why hand-written?
The registry handles `ssh.config` as a **file** (for backup — copies raw content). The SSH collector produces a **structured table** (for collect — parsed host entries). Same source file, different output formats.
:::

### `collectOllama`

**Source**: `src/collectors/ollama.ts`
**Section**: `ai.ollama.models`
**Type**: items (tabular)

Runs `ollama list` and parses the output:

| Column | Source |
|--------|--------|
| Name | Model name (e.g., `llama3.2:latest`) |
| Size | Model size (e.g., `2.0 GB`) |
| Modified | Last modified date |

Parsing splits on 2+ whitespace characters to handle Ollama's column-aligned output. Returns `{}` if Ollama isn't installed or the command fails.

### `collectApps`

**Source**: `src/collectors/apps.ts`
**Sections**: `apps.raycast`, `apps.alttab`, `apps.macos`
**Platform**: macOS only (`if (process.platform !== "darwin") return {}`)

**Raycast** (`apps.raycast`):
- Checks `/Applications/Raycast.app/Contents/Info.plist` existence
- Returns `pairs: { installed: "true" | "false" }`

**AltTab** (`apps.alttab`):
- Checks `/Applications/AltTab.app/Contents/Info.plist` existence
- If installed, checks for preferences via `defaults read com.lwouis.alt-tab-macos`
- Returns `pairs: { installed: "true" | "false", preferences: "exists" }`

**macOS Apps** (`apps.macos`):
- Runs `ls /Applications/`
- Returns sorted list of all applications as items

### `collectHomebrew`

**Source**: `src/collectors/homebrew.ts`
**Sections**: `apps.brew.formulae`, `apps.brew.casks`
**Platform**: macOS only (`if (process.platform !== "darwin") return {}`)

**Formulae** (`apps.brew.formulae`):
- Runs `brew list --formula`
- Returns sorted list as items

**Casks** (`apps.brew.casks`):
- Runs `brew list --cask`
- Returns sorted list as items

Both wrap commands in try/catch — if Homebrew isn't installed, returns `{}` silently.

---

## Collector Execution

### Parallel Execution

All collectors run simultaneously via `Promise.allSettled`:

```typescript
const results = await Promise.allSettled(collectors.map((c) => c(ctx)));

const sections: CollectorResult = {};
for (const result of results) {
  if (result.status === "fulfilled") {
    Object.assign(sections, result.value);
  }
  // Rejected results are silently ignored
}
```

This means:
- A failing collector (e.g., `ollama` not installed) doesn't block other collectors
- All available data is gathered regardless of individual failures
- No error messages for expected failures (missing tools)

### Post-Collection Processing

After collectors finish, the collect command applies:

1. **Sensitivity scan** (if `redact` is true):
   - Each section with `content` is scanned
   - Sections with `skip` action are removed
   - Sections with `redact` action have values replaced with `[REDACTED]`

2. **Slim mode** (if `--slim` is true):
   - Content sections are truncated to 10 lines
   - A `... (N more lines)` suffix is appended

3. **Serialization**:
   - `serializeSnapshot()` from `src/snapshot/serialize.ts` converts the section map to JSON text (native `JSON.stringify`, pretty-printed 2-space, empty fields omitted)
