# Commands

All commands are available via:

```bash
bunx @dotformat/cli <command>
# or from a clone:
bun bin/dotfiles.ts <command>
```

## Quick Reference

| Command | Purpose | Key Flags |
|---------|---------|-----------|
| `collect` | Machine snapshot → `.json` report | `--no-redact`, `--slim`, `-o` |
| `backup` | Copy config files → structured directory | `--archive`, `--only`, `--skip`, `--no-redact`, `-o` |
| `scan` | Standalone sensitivity scan | path argument |
| `security` | Markdown security report → file | path argument, `-o` |
| `chezmoi-export` | Plan/run `chezmoi add` (encrypt secrets) + install script | `--apply`, `--pin`, `--only`, `--skip` |
| `doctor` | New-machine parity check vs a snapshot | `<snapshot.json>` |
| `restore` | Restore backup → live locations | `--pick`, `--dry-run` |
| `diff` | Backup vs live system | `--section` |
| `status` | Quick backup summary | — |
| `compare` | Diff two `.json` files | positional file paths |
| `list` | Query section from latest report | fuzzy section name |

---

## `collect`

Generate a structured `.json` machine snapshot. This is the primary "what's on my machine?" command.

```bash
dotfiles collect [--no-redact] [--slim] [-o path]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-redact` | boolean | `false` | Include sensitive values as-is, skip all redaction |
| `--slim` | boolean | `false` | Truncate content sections to 10 lines. Appends `... (N more lines)` to truncated sections. Reduces output by ~65% — useful for feeding to AI tools |
| `-o <path>` | string | auto | Custom output directory |

### Behavior

1. Resolves output directory (see [Output Path Logic](#output-path-logic))
2. Runs **all collectors in parallel** via `Promise.allSettled` — one collector failing does not abort the report
3. Merges all fulfilled results into a single sections map
4. If redaction is enabled (default):
   - Scans each content section for sensitive patterns
   - Drops sections where action is `skip` (e.g., private keys)
   - Applies `[REDACTED]` replacements where action is `redact`
5. If `--slim` is enabled, truncates content sections to 10 lines
6. Serializes to JSON via `serializeSnapshot()` (native `JSON.stringify`, pretty-printed 2-space, empty fields omitted)
7. Writes to `<hostname>-YYYYMMDDHHMMSS.json`
8. Prints sensitivity report if any findings

### Collectors

The collect command runs these collectors in parallel:

| Collector | Source | Type |
|-----------|--------|------|
| `collectMeta` | hostname, OS, date | Hand-written |
| `registryCollector(registryEntries)` | All 23 registry entries | Generated from registry |
| `collectSsh` | `~/.ssh/config` parsed | Hand-written (structured items) |
| `collectOllama` | `ollama list` output | Hand-written (command) |
| `collectApps` | `/Applications`, Raycast, AltTab | Hand-written (macOS only) |
| `collectHomebrew` | `brew list --formula/--cask` | Hand-written (macOS only) |

### Example Output

```bash
$ dotfiles collect
Report saved to: reports/MacBook-Pro-20260407143022.json

⚠ Sensitivity report:
  HIGH   npm.config                     auth token — redacted
  MEDIUM ssh.config                     IP address — included

  1 items redacted. Use --no-redact to include all.
```

```bash
$ dotfiles collect --slim -o /tmp
Report saved to: /tmp/MacBook-Pro-20260407143022.json
```

---

## `backup`

Copy real config files into a structured directory tree. Unlike `collect` (which produces a single text file), backup creates actual file copies organized by category.

```bash
dotfiles backup [--no-redact] [--archive] [--only <categories>] [--skip <categories>] [-o path]
```

### Flags

| Flag | Type | Default | Description |
|------|------|---------|-------------|
| `--no-redact` | boolean | `false` | Skip sensitivity redaction |
| `--archive` | boolean | `false` | After creating backup dir, compress to `.tar.gz` via system `tar`, then remove the directory |
| `--only <list>` | comma-separated | all | Include **only** these categories (e.g., `--only ai,shell`) |
| `--skip <list>` | comma-separated | none | **Exclude** these categories (e.g., `--skip editor,npm`) |
| `-o <path>` | string | auto | Custom output directory |

### Available Categories

`ai`, `shell`, `git`, `editor`, `terminal`, `ssh`, `npm`, `bun`

### Behavior

1. Resolves output directory
2. Creates timestamped backup directory: `backup-<hostname>-YYYYMMDDHHMMSS/`
3. Filters backup sources by `--only` / `--skip`
4. For each source entry:
   - **File entries**: reads file → scans for sensitivity → applies custom redaction if defined → applies pattern redaction → writes to backup dest
   - **Dir entries**: copies all files recursively (using `Bun.Glob` with `dot: true` for dotfiles)
5. If `--archive`: runs `tar czf` on the backup dir, removes the original directory
6. Prints summary: file count per category
7. Prints sensitivity report

### Example Output

```bash
$ dotfiles backup --only ai,shell
Backup saved to: reports/backup-MacBook-Pro-20260407143200
  8 files across: ai (7), shell (1)

$ dotfiles backup --archive
Archive saved to: reports/backup-MacBook-Pro-20260407143200.tar.gz
  15 files across: ai (7), shell (1), git (3), editor (2), terminal (1), bun (1)
```

### Special Redaction

Some entries have **custom redaction functions** in addition to pattern-based scanning:

| Entry | Custom Redaction |
|-------|-----------------|
| `ssh.config` | `redactSshConfig()` — replaces HostName values with `[REDACTED]` |
| `npm.config` | `redactNpmTokens()` — replaces `_authToken=...` values with `[REDACTED]` |

---

## `scan`

Standalone sensitivity scanner. Useful for checking any file or directory for secrets before sharing.

```bash
dotfiles scan [path]
```

### Arguments

| Argument | Default | Description |
|----------|---------|-------------|
| `path` | `.` (current directory) | File or directory to scan |

### Behavior

- **Single file**: scans the file, reports findings
- **Directory**: recursively scans all files using `Bun.Glob('**/*')` with `dot: true`
  - Skips `node_modules/` and `.git/` paths
  - Skips files larger than **1 MiB**
- Findings are sorted by severity (HIGH → MEDIUM → LOW)
- Each finding shows: line number, severity, pattern label, and matched text (truncated to 40 chars)

### Example Output

```bash
$ dotfiles scan ~/.ssh/config

~/.ssh/config
  L3 [MEDIUM] IP address: 192.168.1.***...
  L7 [MEDIUM] email address: user@exam...

⚠ Sensitivity report:
  MEDIUM ~/.ssh/config                  IP address — redacted

  1 items redacted. Use --no-redact to include all.
```

---

## `security`

Scan a file or directory and write a **Markdown security report** grouping findings by severity. Same scanner engine as `scan`, but persisted to a file instead of console-only — handy as a reviewable artifact before sharing or committing.

```bash
dotfiles security [path] [-o file]
```

### Arguments & Flags

| Argument / Flag | Required | Default | Description |
|-----------------|----------|---------|-------------|
| `path` | no | `.` (cwd) | File or directory to scan |
| `-o <file>` | no | `SECURITY.md` | Output path for the report |

### Behavior

- Uses `stat()` to decide file vs directory (size-independent — a 0-byte file is scanned as a file, not mistaken for a directory).
- A file → scanned directly; a directory → scanned recursively (skips `node_modules/`, `.git/`, and files > 1 MB).
- A missing path prints a friendly error and exits `1`.
- Findings are grouped by top severity (🔴 HIGH / 🟡 MEDIUM / 🟢 LOW) with the matched pattern, action (skip / redact / keep), and line.

### Example

```bash
$ dotfiles security ~ -o ~/Desktop/home-audit.md
Security report written to: /Users/me/Desktop/home-audit.md
  412 scanned, 7 with findings.
```

---

## `restore`

Restore backed-up files to their original locations on the machine. Full safety features: dry run, interactive picker, pre-restore snapshots, conflict prompts.

```bash
dotfiles restore <backup-path> [--pick] [--dry-run]
```

### Arguments & Flags

| Argument/Flag | Type | Required | Description |
|---------------|------|----------|-------------|
| `<backup-path>` | string | **yes** | Path to a backup directory |
| `--pick` | boolean | no | Interactive category selection (checkbox UI) |
| `--dry-run` | boolean | no | Preview the restore plan without writing any files |

### Restore Plan

Before any writes, the CLI builds a **restore plan** by scanning the backup directory and mapping each file to its target location:

| Status | Meaning | Behavior |
|--------|---------|----------|
| `new` | File exists in backup but not on machine | Write directly |
| `same` | Backup content matches machine content (via `Bun.hash()`) | Skip silently |
| `conflict` | Both exist but differ | Prompt user |
| `redacted` | Backup file contains `[REDACTED]` | Skip with message |

### Conflict Prompt

When a file exists on both sides with different content:

| Key | Action |
|-----|--------|
| `o` | Overwrite this file |
| `s` | Skip this file |
| `d` | Show inline diff, then ask again |
| `a` | Overwrite **all** remaining conflicts |
| `l` | Skip **all** remaining conflicts |

### Pre-Restore Snapshot

Before overwriting any conflicting files, the CLI automatically saves the **current versions** of those files to a `pre-restore-YYYYMMDDHHMMSS/` directory. This snapshot uses the same backup format — you can restore from it with `dotfiles restore`.

### `.local` Override Pattern

If a backup contains `shell/.zshrc.local`, it restores to `~/.zshrc.local` — the `.local` suffix maps to the same target directory as the base file. This supports the common pattern of having machine-specific overrides alongside shared configs.

### Category Picker (`--pick`)

Shows a checkbox list of available categories with file counts. Select which categories to restore:

```bash
$ dotfiles restore ./backup --pick
? Select categories to restore:
  [x] ai (7 files)
  [ ] shell (1 file)
  [x] git (3 files)
  [ ] editor (2 files)
```

### Example Output

```bash
$ dotfiles restore ./backup --dry-run

Dry run — no files will be changed:

  [NEW]        editor/zed/settings.json → ~/.config/zed/settings.json
  [CONFLICT]   shell/.zshrc → ~/.zshrc
  [SAME]       git/.gitconfig → ~/.gitconfig
  [REDACTED]   ssh/config → ~/.ssh/config

  4 files total: 1 new, 1 conflicts, 1 unchanged, 1 redacted (skipped)
```

---

## `diff`

Compare the backup state against the current live system. Answers: "what changed since last backup?"

```bash
dotfiles diff [path] [--section <name>]
```

### Arguments & Flags

| Argument/Flag | Type | Default | Description |
|---------------|------|---------|-------------|
| `[path]` | string | auto-detect | Backup directory path. If omitted, finds the latest `backup-*` directory |
| `--section <name>` | string | all | Filter to a specific category (e.g., `ai`, `shell`, `git`) |

### Behavior

- Builds a restore plan (same as `restore`) to determine file statuses
- Groups entries by category
- Color-codes output (TTY-aware — no colors in pipes):
  - **yellow**: modified (content differs)
  - **green**: unchanged
  - **blue**: new (in backup, missing on machine)
  - **gray**: redacted
- Uses `Bun.color(name, "ansi-256")` for terminal colors

### Auto-Find Latest Backup

If no path is given, `diff` searches the resolved output directory for directories matching `backup-*`, sorted by name (descending), and uses the first match.

### Example Output

```bash
$ dotfiles diff

Comparing backup against live system:

  ai/
    ai/claude/settings.json — modified
    ai/claude/CLAUDE.md — unchanged
    ai/cursor/mcp.json — unchanged
  shell/
    shell/.zshrc — modified
  git/
    git/.gitconfig — unchanged

  5 files: 2 modified, 3 unchanged
```

```bash
$ dotfiles diff --section ai

Comparing backup against live system:

  ai/
    ai/claude/settings.json — modified
    ai/claude/CLAUDE.md — unchanged

  2 files: 1 modified, 1 unchanged
```

### Scope Note

`diff` compares only files **represented in the backup**. It does not discover new files that exist on the machine but were not captured in the backup. For full discovery, run `collect` and use `compare`.

---

## `status`

Quick summary of backup state — like `git status` for your configs.

```bash
dotfiles status
```

### Behavior

1. Finds the latest backup directory (same logic as `diff`)
2. Calculates backup age from directory modification time
3. Builds restore plan to count statuses
4. Prints summary

### Age Display

| Age | Display |
|-----|---------|
| < 60 minutes | `Nm ago` |
| < 24 hours | `Nh ago` |
| >= 24 hours | `Nd ago` |

### Example Output

```bash
$ dotfiles status
Last backup: 2h ago (backup-MacBook-Pro-20260407120000)
  15 files tracked: 2 modified, 13 unchanged

Modified since backup:
  shell/.zshrc
  ai/claude/settings.json
```

```bash
$ dotfiles status
Last backup: 5m ago (backup-MacBook-Pro-20260407143200)
  15 files tracked: 0 modified, 15 unchanged

Everything up to date.
```

---

## `compare`

Structured diff between two `.json` report files. Uses the in-tree `src/snapshot` module's `compareSnapshots()` and `formatDiff()`.

```bash
dotfiles compare [file1] [file2]
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `file1` | no | Path to first `.json` file |
| `file2` | no | Path to second `.json` file |

If both are omitted, `compare` finds the **two newest** `.json` files in `<cwd>/reports/` (sorted by modification time).

### Behavior

- Parses both files via `parseSnapshot()` (native `JSON.parse`)
- Computes structured diff via `compareSnapshots()`
- Formats with `formatDiff()` with color enabled (green `+`, red `-`, yellow `~`, dim `=`)
- Labels are derived from filenames (without `.json` extension)

### Example

```bash
$ dotfiles compare reports/MacBook-20260401.json reports/MacBook-20260407.json
```

::: warning
`compare` only looks in `<cwd>/reports/` when auto-detecting files. It does **not** search `~/Downloads` or other directories. Provide explicit paths if your reports are elsewhere.
:::

---

## `doctor`

New-machine parity check: compare a JSON snapshot against **this** machine and list what's installable in the snapshot but missing here. The "did everything come over?" guarantee after a migration — and CI-friendly via its exit code.

```bash
dotfiles doctor <snapshot.json>
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `snapshot.json` | **yes** | A snapshot produced by `collect` (typically from the old machine) |

### Behavior

- Parses the snapshot via `parseSnapshot()` (rejects non-snapshot JSON with a friendly error).
- Re-runs the collectors on the current machine and diffs against the snapshot.
- Only **installable inventory** is checked: `packages.*`, `runtimes.*`, `apps.brew.*`, `apps.macos`, `fonts.*`, and any `*.extensions` section.
- Items are keyed by **name** (`columns[0]`), so version drift is ignored — parity is "is it present", not "is it the same version".
- Prints what's missing per section; **exits `1`** if anything is missing (so it can gate CI), or prints a parity ✅ and exits `0`.

### Example

```bash
$ dotfiles doctor reports/old-machine-20260401.json
Missing on this machine (present in the snapshot):

  packages.bun.global (2)
    - eas-cli@16.19.2
    - vercel@39.0.0
  fonts.user (1)
    - JetBrainsMono-Regular.ttf

3 item(s) missing across 2 section(s).
```

---

## `list`

Print a section from the most recent `.json` report. Supports **fuzzy matching** on section names.

```bash
dotfiles list <section>
```

### Arguments

| Argument | Required | Description |
|----------|----------|-------------|
| `section` | **yes** | Section name or partial match |

### Fuzzy Matching

The query is matched against section names using two strategies:

1. **Substring match**: `brew` matches `apps.brew.formulae` and `apps.brew.casks`
2. **Dot-segment match**: `claude` matches `ai.claude.settings`, `ai.claude.skills`, `ai.claude.md`

All matching sections are printed.

### Available Sections

These are the section IDs used in `.json` reports:

| Section | Source |
|---------|--------|
| `meta` | Hostname, OS, date |
| `ai.claude.settings` | Claude permissions, plugins |
| `ai.claude.skills` | Claude skills directory listing |
| `ai.claude.md` | CLAUDE.md content |
| `ai.cursor.mcp` | Cursor MCP config |
| `ai.cursor.skills` | Cursor skills listing |
| `ai.gemini.settings` | Gemini preferences |
| `ai.gemini.skills` | Gemini skills listing |
| `ai.gemini.md` | GEMINI.md content |
| `ai.windsurf.mcp` | Windsurf MCP config |
| `ai.windsurf.skills` | Windsurf skills listing |
| `ai.ollama.models` | Ollama model list (name, size, modified) |
| `shell.zshrc` | .zshrc content |
| `git.config` | .gitconfig content |
| `git.ignore` | .gitignore_global content |
| `gh.config` | GitHub CLI config |
| `editor.zed` | Zed settings |
| `editor.cursor` | Cursor editor settings |
| `editor.nvim` | Neovim init.lua |
| `editor.vimrc` | .vimrc |
| `terminal.p10k` | P10k metadata (exists, line count) |
| `terminal.tmux` | tmux.conf content |
| `ssh.hosts` | SSH hosts table (host, hostname, identity) |
| `ssh.config` | SSH config content |
| `npm.config` | .npmrc content |
| `bun.config` | .bunfig.toml content |
| `apps.raycast` | Raycast installed status |
| `apps.alttab` | AltTab installed + prefs |
| `apps.macos` | macOS /Applications listing |
| `apps.brew.formulae` | Homebrew formulae list |
| `apps.brew.casks` | Homebrew casks list |

### Example

```bash
$ dotfiles list brew
[apps.brew.formulae]
bat
eza
fd
fzf
...

[apps.brew.casks]
alt-tab
firefox
raycast
...
```

```bash
$ dotfiles list claude
[ai.claude.settings]
enabledPlugins = ...
permissions = ...

[ai.claude.skills]
superskill.md
web-dev.md
```

If no sections match:

```bash
$ dotfiles list foo
No sections matching "foo".
Available sections: meta, ai.claude.settings, ai.claude.skills, ...
```

---

## `chezmoi-export`

Bridge to [chezmoi](https://www.chezmoi.io): plan (and optionally run) `chezmoi add` for your managed configs, encrypting the sensitive ones, and generate a `run_onchange_` script that reinstalls packages on `chezmoi apply`. See [chezmoi integration](/chezmoi) for the bigger picture.

```bash
dotfiles chezmoi-export [--apply] [--pin] [--only a,b] [--skip c,d]
```

### Flags

| Flag | Default | Description |
|------|---------|-------------|
| `--apply` | off (dry-run) | Actually run the `chezmoi add` commands and write the install script. Without it, the plan is printed and nothing changes. |
| `--pin` | off (latest) | Reinstall global packages at their **captured versions** (`name@version`). Default installs the **latest** of each. Node runtimes always keep their exact version. |
| `--only a,b` | all | Restrict to these categories / install groups (comma-separated, whitespace-tolerant). |
| `--skip c,d` | none | Exclude these categories / install groups. `--skip` wins over `--only`. |

`--only`/`--skip` accept registry categories (`ssh`, `git`, `ai`, `editor`, `cloud`, `shell`, …) **and** two install-group selectors: `brew` (the Brewfile) and `packages` (node/bun/pnpm/npm/cargo/deno). The latter drive the install script independently of the config plan — so `--only brew` exports just the Brewfile install step.

### Encryption (secret gate)

A path is added with `chezmoi add --encrypt` when **any** of these hold:

- its registry entry is `sensitivity: high`, or
- it declares a redact rule (e.g. `ssh.config`), or
- the scanner finds a **HIGH-severity secret** inside it — including inside a **directory** entry.

Everything else is added plain. A benign MEDIUM hit (an IP or email) does **not** force encryption.

### Extra behaviors

- **SSH private keys** in `~/.ssh` are detected by content (not filename) and added encrypted — `id_ed25519`, `id_rsa`, custom `*.key`, etc. (`.pub` files skipped).
- **GnuPG**: `~/.gnupg` is only carried if it holds real secret keys; before adding it, a `.chezmoiignore` is written so sockets (`S.*`), lock files, and `random_seed` are never committed — only key material.
- **Install script** (`run_onchange_install-packages.sh`): one install per line, command-guarded and `|| true`, ending in `exit 0` so a single failing cask can't abort `chezmoi apply`. Covers brew, fnm node versions, bun/pnpm/npm/cargo globals; deno bins are recorded as a comment (their original module URL isn't recoverable). The embedded Brewfile is redacted first (a private tap's inline credentials never land in the unencrypted script).
- **Cross-manager duplicates** (e.g. a package installed via both bun and npm) are kept in each but a warning is printed so you can resolve PATH shadowing.

### Example

```bash
# Review the plan (nothing changes)
$ dotfiles chezmoi-export
chezmoi-export plan — 9 path(s), 5 encrypted:
  🔒 add --encrypt  /Users/me/.ssh/config  (has redact rule)
  🔒 add --encrypt  /Users/me/.aws/credentials  (sensitivity:high)
     add            /Users/me/.gitconfig  (plain)
  + run_onchange install script (brew, packages)

Dry-run. Re-run with --apply to execute (requires chezmoi + a configured age key).

# Execute, keeping exact package versions, skipping editor extensions
$ dotfiles chezmoi-export --apply --pin --skip editor
```

::: warning
`--apply` requires `chezmoi` installed and an `age` key configured. The age private key is **never** added to the source repo — keep it in a password manager; losing it means encrypted files can't be decrypted. See [encryption](/encryption).
:::

---

## Output Path Logic

All commands that write files share the same output directory resolution:

| Priority | Condition | Output |
|----------|-----------|--------|
| 1 | `-o <path>` flag provided | Explicit path |
| 2 | `.git/HEAD` exists in current working directory | `<cwd>/reports/` |
| 3 | Otherwise (global/standalone run) | `~/Downloads` |

This means:

- **Inside a cloned repo**: reports and backups go to `reports/` — ideal for committing to git
- **Running globally** (via `bunx`): outputs go to `~/Downloads` — easy to find and share
- **Explicit `-o`**: always wins
