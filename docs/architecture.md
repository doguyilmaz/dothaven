# Architecture

## Design Principles

- **Registry-driven**: a single `ConfigEntry[]` array is the source of truth for all config paths, collection, backup, and restore
- **Safe by default**: sensitivity scanning runs on every collect and backup unless explicitly disabled
- **Graceful degradation**: missing tools, files, or directories are silently skipped — never hard-fail on optional data
- **Parallel where possible**: independent collectors run via `Promise.allSettled`
- **Platform-aware**: per-OS paths in registry, platform guards on OS-specific collectors

## Project Structure

```
dotfiles/
├── bin/
│   └── dotfiles.ts                  # Entry point — Bun runtime check, imports src/cli.ts
│
├── src/
│   ├── cli.ts                       # Command router — switch on argv[2], delegates to commands/
│   │
│   ├── commands/                    # One file per CLI command
│   │   ├── collect.ts               # .json snapshot generation (parallel collectors → scan → serialize)
│   │   ├── backup.ts                # Structured file backup (registry sources → scan → copy)
│   │   ├── scan.ts                  # Standalone sensitivity scanner (file or directory)
│   │   ├── restore.ts               # Restore from backup (plan → pick → execute)
│   │   ├── diff.ts                  # Backup vs live comparison (color-coded, TTY-aware)
│   │   ├── status.ts                # Quick backup summary (age, counts, modified list)
│   │   ├── compare.ts               # Diff two .json files (via src/snapshot compareSnapshots)
│   │   └── list.ts                  # Fuzzy section query from latest report
│   │
│   ├── registry/                    # Config source definitions (single source of truth)
│   │   ├── types.ts                 # ConfigEntry, Platform, EntryKind type definitions
│   │   ├── entries.ts               # 23 config entries — all file-based configs
│   │   ├── resolve.ts               # resolvePath() and getEntriesForPlatform()
│   │   ├── collector.ts             # registryCollector() — generates Collector from entries
│   │   ├── backup.ts                # registryBackupSources() — generates BackupSource[] from entries
│   │   └── index.ts                 # Public re-exports
│   │
│   ├── collectors/                  # Data collection functions
│   │   ├── types.ts                 # CollectorContext, CollectorResult, Collector, makeSection()
│   │   ├── meta.ts                  # Machine metadata (hostname, OS, date)
│   │   ├── ssh.ts                   # SSH config parser → structured host table
│   │   ├── ollama.ts                # Ollama model list (parses `ollama list` output)
│   │   ├── apps.ts                  # macOS apps, Raycast, AltTab (darwin-only)
│   │   └── homebrew.ts              # Homebrew formulae + casks (darwin-only)
│   │
│   ├── snapshot/                    # Snapshot model + JSON serialization + diffing (zero deps)
│   │   ├── types.ts                 # Section, Snapshot, CollectorResult type definitions
│   │   ├── serialize.ts             # serializeSnapshot() / parseSnapshot() (native JSON)
│   │   └── compare.ts               # compareSnapshots() + formatDiff() (in-tree diff)
│   │
│   ├── scan/                        # Sensitivity detection engine
│   │   ├── types.ts                 # ScanPattern, ScanResult, ScanFinding, Severity, ScanSummary
│   │   ├── patterns.ts              # 27+ detection patterns (cached, username-aware)
│   │   ├── scanner.ts               # scanContent(), scanFile(), summarize()
│   │   ├── redactor.ts              # applyRedactions() — replaces matched values with [REDACTED]
│   │   ├── report.ts                # formatReport() — human-readable sensitivity summary
│   │   └── index.ts                 # Public re-exports
│   │
│   ├── backup/                      # Backup file management
│   │   ├── types.ts                 # BackupEntry (file|dir), BackupSource, BackupFile, BackupDir
│   │   └── sources.ts               # Thin wrapper: registryBackupSources(registryEntries)
│   │
│   ├── restore/                     # Restore engine
│   │   ├── types.ts                 # RestoreEntry, RestorePlan, FileStatus, ConflictAction
│   │   ├── plan.ts                  # buildRestoreMap(), buildRestorePlan()
│   │   ├── execute.ts               # executeRestore(), createSnapshot(), printPlan()
│   │   ├── prompt.ts                # Interactive conflict prompt (o/s/d/a/l)
│   │   └── index.ts                 # Public re-exports
│   │
│   └── utils/                       # Shared utilities
│       ├── constants.ts             # REDACTION_MARKER = "[REDACTED]"
│       ├── home.ts                  # getHome() — validates $HOME, exits on missing
│       ├── redact.ts                # redactSshConfig(), redactNpmTokens()
│       ├── resolve-output.ts        # resolveOutputDir() — -o / git repo / ~/Downloads
│       ├── find-backup.ts           # findLatestBackup(), getBackupAge()
│       └── timestamp.ts             # generateTimestamp() → YYYYMMDDHHMMSS
│
├── tests/                           # 296 tests across 35 files
│   ├── collectors/                  # meta, ssh, npm, claude tests
│   ├── commands/                    # list fuzzy match tests
│   ├── registry/                    # entries validation, resolve, collector tests
│   ├── scan/                        # scanner, patterns, redactor tests
│   ├── restore/                     # plan, execute, snapshot tests
│   └── utils/                       # redact, timestamp tests
│
├── docs/                            # VitePress documentation site
│   ├── .vitepress/config.mts        # VitePress config with mermaid plugin
│   └── *.md                         # Documentation pages
│
├── PLAN.md                          # Master plan and roadmap
├── CLAUDE.md                        # Project-level Claude Code instructions
└── package.json                     # dothaven package definition
```

## Type System

### Core Types

```typescript
// === Snapshot Types (src/snapshot/types.ts) ===

interface Section {
  name: string;
  pairs: Record<string, string>;              // Key-value metadata
  items: { raw: string; columns: string[] }[]; // Tabular data
  content: string | null;                      // Full text content
}

type Snapshot = Record<string, Section>;        // section ID → section
type CollectorResult = Record<string, Section>; // collectors return the same shape
```

```typescript
// === Collector Types (src/collectors/types.ts) ===

interface CollectorContext {
  redact: boolean;    // true by default — controls sensitivity redaction
  home: string;       // $HOME — injected for testability
}

type Collector = (ctx: CollectorContext) => Promise<CollectorResult>;

// makeSection() helper creates Section objects
function makeSection(name: string, opts?: {
  pairs?: Record<string, string>;
  items?: { raw: string; columns: string[] }[];
  content?: string | null;
}): Section;
```

```typescript
// === Registry Types (src/registry/types.ts) ===

type Platform = "darwin" | "linux" | "win32";

type EntryKind =
  | { type: "file" }                          // Read file content
  | { type: "file"; metadata: true }          // Only existence + line count
  | { type: "dir" }                           // List directory contents
  | { type: "json-extract"; fields: string[] }; // Extract specific JSON fields as pairs

interface ConfigEntry {
  id: string;                                // Section name: "shell.zshrc"
  name: string;                              // Human label: ".zshrc"
  paths: Partial<Record<Platform, string>>;  // Per-OS paths (~ expanded at runtime)
  category: string;                          // Powers --only/--skip filtering
  kind: EntryKind;                           // How to collect this entry
  backupDest: string;                        // Relative path in backup directory
  sensitivity: "low" | "medium" | "high";    // Sensitivity classification hint
  redact?: (content: string) => string;      // Optional custom redaction function
}
```

```typescript
// === Backup Types (src/backup/types.ts) ===

interface BackupFile {
  type: "file";
  src: string;                               // Absolute source path
  dest: string;                              // Relative destination in backup
  redact?: (content: string) => string;      // Custom redaction
}

interface BackupDir {
  type: "dir";
  src: string;                               // Absolute source directory
  dest: string;                              // Relative destination in backup
}

type BackupEntry = BackupFile | BackupDir;

interface BackupSource {
  category: string;                          // Category name for filtering
  entries: (home: string) => BackupEntry[];  // Lazy — evaluated with $HOME at runtime
}
```

```typescript
// === Restore Types (src/restore/types.ts) ===

type FileStatus = "new" | "conflict" | "same" | "redacted";

interface RestoreEntry {
  backupPath: string;   // Relative path in backup dir
  targetPath: string;   // Absolute destination on machine
  category: string;     // Category for picker
  status: FileStatus;   // Computed status
}

interface RestorePlan {
  entries: RestoreEntry[];
  backupDir: string;
  categories: string[];  // Sorted unique categories
}

type ConflictAction = "overwrite" | "skip" | "diff";
type BatchConflictAction = ConflictAction | "overwrite-all" | "skip-all";
```

```typescript
// === Scan Types (src/scan/types.ts) ===

type Severity = "HIGH" | "MEDIUM" | "LOW";
type ScanAction = "skip" | "redact" | "include";

interface ScanPattern {
  id: string;                    // Pattern identifier
  label: string;                 // Human-readable label
  severity: Severity;
  regex: RegExp;
  defaultAction: ScanAction;
}

interface ScanFinding {
  pattern: ScanPattern;
  match: string;                 // Truncated to 40 chars
  line: number;                  // 1-based line number
}

interface ScanResult {
  filePath: string;
  action: ScanAction;            // Highest severity finding determines
  findings: ScanFinding[];
}

interface ScanSummary {
  skipped: number;
  redacted: number;
  included: number;
  results: ScanResult[];         // Only results with findings
}
```

## Data Flow

### Collect Flow

```
CLI args → parseArgs()
  → resolveOutputDir()
  → build CollectorContext { redact, home }
  → Promise.allSettled(collectors)
  → merge fulfilled results → CollectorResult
  → if redact: scanContent() each section → skip/redact/include
  → if slim: truncate content to 10 lines
  → serializeSnapshot(sections) via src/snapshot (native JSON.stringify)
  → Bun.write() timestamped .json file
```

### Backup Flow

```
CLI args → parseArgs()
  → resolveOutputDir()
  → filterSources(backupSources, only, skip)
  → for each source:
      → entries(home) → BackupEntry[]
      → file: Bun.file().text() → scanContent() → entry.redact?() → applyRedactions() → Bun.write()
      → dir: Bun.Glob('**/*').scan() → copy all files
  → if archive: tar czf → rm -rf dir
  → summarize() → formatReport()
```

### Restore Flow

```
CLI args → parseArgs()
  → buildRestorePlan(backupDir, home):
      → buildRestoreMap() from backupSources
      → Bun.Glob('**/*').scan(backupDir)
      → for each file: match to map → resolveFileStatus() (Bun.hash comparison)
      → handle dir entries, .local overrides
  → if --pick: pickCategories() → filter plan
  → if --dry-run: printPlan() → exit
  → createSnapshot() for conflicts
  → for each entry: prompt if conflict → Bun.write()
```

## Key Design Decisions

### Registry as Single Source of Truth

Before the registry, config paths were hardcoded in 3 places: 11 collector files, `backup/sources.ts`, and `restore/plan.ts`. Adding a new tool meant editing all three.

Now, `src/registry/entries.ts` is the only place paths are defined. The registry generates:
- **Collectors** via `registryCollector(entries)` — handles file, dir, json-extract, and metadata kinds
- **Backup sources** via `registryBackupSources(entries)` — groups by category, resolves paths

Hand-written collectors remain for complex cases: `meta` (dynamic data), `ssh` (structured parsing), `ollama` (command output), `apps` (macOS-specific checks), `homebrew` (command output).

### `Promise.allSettled` for Collectors

Collectors run in parallel. If Homebrew isn't installed or Ollama isn't running, those collectors fail silently while others succeed. The report includes whatever data was available.

### `Bun.hash()` for File Comparison

Restore uses `Bun.hash()` (xxHash) to compare backup content against live files. This is significantly faster than string equality for large files and avoids loading both strings into memory for comparison.

### Lazy `BackupSource.entries(home)`

Backup sources don't resolve paths at import time. The `entries()` function takes `home` as a parameter, making the entire backup pipeline testable with any directory.

### Sensitivity as Pipeline, Not Gate

The scan system doesn't just block or allow — it operates as a three-stage pipeline (detect → classify → act) where each finding gets its own action. A single file can have both redacted and included findings. The highest-severity action determines the file-level action.

## Dependencies

The CLI has **zero runtime dependencies**. Snapshot serialization, parsing, and diffing
are handled entirely in-tree by the `src/snapshot` module (native `JSON.stringify` /
`JSON.parse` plus an in-tree `compareSnapshots()` / `formatDiff()`).

| Package | Role |
|---------|------|
| `vitepress` | Documentation site (dev dependency) |
| `vitepress-plugin-mermaid` | Mermaid diagram support in docs (dev dependency) |
| `@types/bun` | TypeScript definitions for Bun APIs (dev dependency) |

The CLI uses only Bun built-ins (`Bun.file`, `Bun.$`, `Bun.Glob`, `Bun.hash`, `Bun.color`, `Bun.env`) and Node.js standard library modules (`path`, `os`, `fs/promises`).
