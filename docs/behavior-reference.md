# Behavior Reference

This reference documents default behaviors, edge cases, error handling, and non-obvious design decisions across all commands.

## Output Path Resolution

Order of precedence:

| Priority | Condition | Result |
|----------|-----------|--------|
| 1 | `-o <path>` flag provided | Exact path specified |
| 2 | `.git/HEAD` exists in current working directory | `<cwd>/reports/` |
| 3 | Otherwise | `~/Downloads` |

Detection uses `Bun.file(join(cwd, ".git/HEAD")).exists()` — it checks for the HEAD file specifically, not just a `.git` directory.

### Timestamped Outputs

| Type | Pattern | Example |
|------|---------|---------|
| Collect report | `<hostname>-YYYYMMDDHHMMSS.json` | `MacBook-Pro-20260407143022.json` |
| Backup directory | `backup-<hostname>-YYYYMMDDHHMMSS` | `backup-MacBook-Pro-20260407143022` |
| Archive | `backup-<hostname>-YYYYMMDDHHMMSS.tar.gz` | `backup-MacBook-Pro-20260407143022.tar.gz` |
| Pre-restore snapshot | `pre-restore-YYYYMMDDHHMMSS` | `pre-restore-20260407143500` |

Timestamps use `YYYYMMDDHHMMSS` format (no separators) for filesystem-safe, sortable names.

---

## Command-Specific Behaviors

### `collect`

| Behavior | Detail |
|----------|--------|
| Collector parallelism | All collectors run via `Promise.allSettled` — one failing doesn't block others |
| Missing tools | Collectors for `ollama`, `brew`, apps return `{}` — no error output |
| Section ordering | Determined by collector return order and `Object.assign` merge |
| Redaction default | **On** — disable with `--no-redact` |
| Slim truncation | Content sections cut to 10 lines, suffix `... (N more lines)` added |
| Sensitivity report | Printed after report file path — only shown when findings exist |
| Output directory creation | `mkdir -p` is called on the output directory before writing |

### `backup`

| Behavior | Detail |
|----------|--------|
| File scan | Each file is scanned individually before writing |
| Skip action | File with private key → **not backed up** (not even with redaction) |
| Custom redaction order | `entry.redact()` runs first, then `applyRedactions()` (pattern-based) |
| Directory copy | `Bun.Glob('**/*')` with `{ onlyFiles: true, dot: true }` — includes dotfiles, files only |
| Empty backup | If no files were copied (all missing), prints "No files found to backup." |
| Archive creation | `tar czf` runs on backup dir → dir is then `rm -rf`'d |
| Category filtering | `--only` filters first, then `--skip` — both can be used together |
| Category count | Summary shows per-category file counts: `ai (7), shell (1)` |

### `restore`

| Behavior | Detail |
|----------|--------|
| File comparison | `Bun.hash()` (xxHash64) for fast content comparison |
| Pre-restore snapshot | Only includes conflicting files — not `new` or `same` files |
| Redacted skip | Files containing `[REDACTED]` string are automatically skipped |
| `.local` mapping | `shell/.zshrc.local` → `~/.zshrc.local` (base entry path + `.local` suffix) |
| Directory entries | Files under a `dir` backup entry are matched by prefix and restored to the correct subdirectory |
| Conflict persistence | `a` (overwrite-all) and `l` (skip-all) persist for the rest of the session |
| Exit behavior | If no files restored, prints "No files restored." |
| Category picker | Shows categories with file counts, filters plan after selection |

### `diff`

| Behavior | Detail |
|----------|--------|
| Auto-find backup | Searches output dir for `backup-*` directories, sorted descending by name |
| TTY detection | `process.stdout.isTTY ?? false` — no colors in pipes |
| Color method | `Bun.color(name, "ansi-256")` — not raw ANSI escape codes |
| Grouping | Output is grouped by category with category headers |
| Scope limitation | Only compares files **in backup** — does not discover new files on machine |
| Section filter | `--section` matches exact category name (not fuzzy) |

### `status`

| Behavior | Detail |
|----------|--------|
| Backup detection | Same as `diff` — finds latest `backup-*` directory |
| Age calculation | Directory `mtime` → human-readable (`Nm ago`, `Nh ago`, `Nd ago`) |
| Age fallback | If stat fails, shows "unknown" |
| All-clear message | "Everything up to date." when no modified or new files |
| Modified list | Only shown when modified files exist |

### `compare`

| Behavior | Detail |
|----------|--------|
| Auto-detect | Finds two newest `.json` files in `<cwd>/reports/` by modification time |
| Search scope | Only searches `<cwd>/reports/` — **not** `~/Downloads` |
| Minimum files | Requires at least 2 `.json` files when auto-detecting |
| Diff labels | Derived from filename without `.json` extension |
| Color | Always enabled (color: true passed to formatter) |
| No diff output | Prints "No differences found." when reports are identical |

### `list`

| Behavior | Detail |
|----------|--------|
| Fuzzy matching | Substring match on full name + dot-segment match on parts |
| Search scope | Latest `.json` file in `<cwd>/reports/` only |
| Multiple matches | All matching sections are printed |
| No match | Lists all available section names |
| Output format | Human-readable dump of the matched section's `pairs`, `items`, and `content` |

### `scan`

| Behavior | Detail |
|----------|--------|
| File mode | Single file scanned directly |
| Directory mode | Recursive with `Bun.Glob('**/*')` and `{ dot: true }` |
| Exclusions | `node_modules/` and `.git/` paths skipped |
| Size limit | Files over 1 MiB skipped |
| Severity sorting | Findings sorted HIGH → MEDIUM → LOW in detailed output |
| Match display | Each finding shows: `L{line} [{severity}] {label}: {match}` |
| Clean output | "No sensitive data found." when no findings |

---

## Error Handling

### Fatal Errors

| Condition | Behavior |
|-----------|----------|
| `$HOME` not set | `console.error` + `process.exit(1)` |
| Bun runtime not available | Error message + `process.exit(1)` (checked in `bin/dothaven.ts`) |
| No backup path for `restore` | Prints usage message, exits normally |

### Graceful Degradation

| Condition | Behavior |
|-----------|----------|
| Missing config file | Registry collector skips entry — no error |
| Missing directory for `dir` kind | Try/catch → skipped silently |
| `ollama` not installed | `collectOllama` catches error → returns `{}` |
| `brew` not installed | `collectHomebrew` catches error → returns `{}` |
| Non-macOS for apps/brew | Platform guard → returns `{}` |
| JSON parse error | Registry collector catches → skips entry |
| `stat()` fails in age calculation | Returns "unknown" |
| No backup found for `diff`/`status` | "No backup found. Run 'dothaven backup' first." |
| No reports for `compare`/`list` | Appropriate message with usage hint |

### Bun API Usage

| API | Where Used | Why |
|-----|-----------|-----|
| `Bun.file()` | Everywhere | File existence checks, reading content |
| `Bun.$` | Collectors, backup | Shell commands (uname, brew, ollama, tar, mkdir, rm, ls) |
| `Bun.Glob` | Backup, scan, restore, compare, list | File pattern matching |
| `Bun.hash()` | Restore plan | Fast content comparison (xxHash64) |
| `Bun.color()` | Diff command | Terminal color output |
| `Bun.env` | Path resolution | Environment variable access |
| `Bun.write()` | Backup, collect | File writing |

---

## Constants

| Constant | Value | Location | Purpose |
|----------|-------|----------|---------|
| `REDACTION_MARKER` | `"[REDACTED]"` | `src/utils/constants.ts` | Marker used in redacted content |
| `SLIM_MAX_LINES` | `10` | `src/commands/collect.ts` | Max lines in slim mode |
| `MAX_FILE_SIZE` | `1048576` (1 MiB) | `src/commands/scan.ts` | Max file size for scan |

---

## TTY / Color Behavior

- `diff` command: checks `process.stdout.isTTY ?? false`
  - TTY present: uses `Bun.color()` for yellow/green/blue/gray output
  - Not a TTY (pipe, redirect): all color strings are empty, output is plain text
- `compare` command: always passes `color: true` to `formatDiff()` (color is handled by the in-tree `formatDiff` in `src/snapshot/compare.ts`)
- ANSI reset: `\x1b[0m` is used after each colored line (only when TTY)
