# Backup and Restore

## Two Output Tracks

The CLI offers two complementary ways to capture machine state:

| Track | Command | Output | Use Case |
|-------|---------|--------|----------|
| **Snapshot** | `collect` | Single `.json` file | Quick inspection, AI feeds, cross-machine compare |
| **File backup** | `backup` | Structured directory of real files | Git-committable, full restore capability |

Both tracks run sensitivity scanning by default.

---

## Backup

### How It Works

`dothaven backup` reads every config file defined in the [registry](/registry), scans it for sensitivity, applies redaction, and writes a copy to a structured directory.

```bash
dothaven backup                            # Everything
dothaven backup --only ai,shell            # Selective
dothaven backup --skip editor              # Exclusive
dothaven backup --archive                  # .tar.gz output
dothaven backup --archive -o ~/Desktop     # Archive to specific location
dothaven backup --no-redact                # Raw files, no redaction
```

### Backup Directory Layout

```
backup-<hostname>-YYYYMMDDHHMMSS/
├── ai/
│   ├── claude/
│   │   ├── settings.json        # json-extract → full file in backup
│   │   ├── CLAUDE.md
│   │   └── skills/
│   │       ├── superskill.md
│   │       └── web-dev.md
│   ├── cursor/
│   │   ├── mcp.json
│   │   └── skills/
│   ├── gemini/
│   │   ├── settings.json
│   │   ├── GEMINI.md
│   │   └── skills/
│   └── windsurf/
│       ├── mcp_config.json
│       └── skills/
├── shell/
│   └── .zshrc
├── git/
│   ├── .gitconfig
│   ├── .gitignore_global
│   └── gh/config.yml
├── editor/
│   ├── zed/settings.json
│   ├── cursor/settings.json
│   ├── nvim/init.lua
│   └── .vimrc
├── terminal/
│   ├── .p10k.zsh
│   └── .tmux.conf
├── ssh/
│   └── config                   # HostName values → [REDACTED]
├── npm/
│   └── .npmrc                   # _authToken → [REDACTED]
└── bun/
    └── .bunfig.toml
```

Only directories and files that **actually exist** on the machine are created. If you don't have Neovim installed, `editor/nvim/` won't appear.

### Category Filtering

Categories come from the registry's `category` field. Available categories:

| Category | Contents |
|----------|----------|
| `ai` | Claude, Cursor, Gemini, Windsurf (settings, skills, MCP configs, markdown files) |
| `shell` | `.zshrc` |
| `git` | `.gitconfig`, `.gitignore_global`, GitHub CLI config |
| `editor` | Zed, Cursor, Neovim, Vim settings |
| `terminal` | `.p10k.zsh`, `.tmux.conf` |
| `ssh` | SSH config (auto-redacted) |
| `npm` | `.npmrc` (auto-redacted) |
| `bun` | `.bunfig.toml` |

**`--only`** is inclusive: `--only ai,shell` backs up only AI and shell configs.
**`--skip`** is exclusive: `--skip editor,npm` backs up everything except editors and npm.

If both are provided, `--only` runs first, then `--skip` filters the result.

### Entry Processing

For each backup source entry:

**File entries:**
1. `Bun.file(src)` → check exists
2. Read content as text
3. `scanContent()` → determine action
4. If `redact` mode and action is `skip` → file is excluded entirely
5. If entry has custom `redact()` function → apply it (SSH config, npm tokens)
6. `applyRedactions()` → pattern-based redaction on remaining content
7. Write to backup destination

**Directory entries:**
1. `Bun.Glob('**/*')` with `{ cwd: src, onlyFiles: true, dot: true }`
2. Copy each file to the corresponding backup destination
3. Directories that don't exist or are unreadable are silently skipped

### Archive Export

With `--archive`:
1. Normal backup directory is created first
2. `tar czf <backup>.tar.gz -C <parent> <dirname>` compresses it
3. Original directory is removed
4. Only the `.tar.gz` file remains

Uses system `tar` — no additional dependencies.

### Sensitivity During Backup

The sensitivity scan runs per-file during backup:
- Files with `skip` action (private keys) are **not backed up**
- Files with `redact` action have their values replaced with `[REDACTED]`
- Custom redaction functions run **before** pattern-based redaction
- A sensitivity report is printed at the end

---

## Restore

### How It Works

`dothaven restore` reads a backup directory, maps each file to its original location on the machine, compares content, and writes files back.

```bash
dothaven restore ./backup --dry-run        # Preview only
dothaven restore ./backup --pick           # Select categories
dothaven restore ./backup                  # Restore everything
```

### Restore Plan

Before any files are written, the CLI builds a complete **restore plan**:

1. **Build restore map**: iterates all `BackupSource` entries to map backup paths → absolute target paths on the machine
2. **Scan backup directory**: `Bun.Glob('**/*').scan({ cwd: backupDir, dot: true })`
3. **Match each file**:
   - Direct match against restore map (file entries)
   - Prefix match for directory entries (e.g., `ai/claude/skills/foo.md` → matched via `ai/claude/skills` dir entry)
   - `.local` suffix match (e.g., `shell/.zshrc.local` → maps to `~/.zshrc.local` via the `shell/.zshrc` base entry)
4. **Determine status** for each matched file:

| Status | How Determined | Restore Behavior |
|--------|---------------|-----------------|
| `new` | Target file doesn't exist on machine | Write directly |
| `same` | `Bun.hash(backupContent) === Bun.hash(targetContent)` | Skip silently |
| `conflict` | Both exist, hashes differ | Prompt user |
| `redacted` | Backup content contains `[REDACTED]` | Skip with message |

### File Comparison

The plan uses `Bun.hash()` (xxHash64) for fast content comparison. This is a non-cryptographic hash — suitable for equality checks, much faster than comparing full string content.

### Pre-Restore Snapshot

**Before any conflicting files are overwritten**, the CLI saves the current machine versions to a snapshot directory:

```
pre-restore-YYYYMMDDHHMMSS/
├── shell/.zshrc          # Current version before overwrite
├── git/.gitconfig        # Current version before overwrite
└── ...
```

This snapshot:
- Uses the same directory structure as a regular backup
- Is stored in the resolved output directory
- Can be restored with `dothaven restore` — it's a valid backup

Only conflicting files are snapshot'd. New files (nothing to overwrite) and same files (no change) are not included.

### Conflict Resolution

When a file exists on both sides with different content, you're prompted:

```
CONFLICT: shell/.zshrc
  backup → ~/.zshrc (content differs)

  [o]verwrite  [s]kip  [d]iff  overwrite-[a]ll  skip-a[l]l
```

| Key | Behavior |
|-----|----------|
| `o` | Overwrite this one file |
| `s` | Skip this one file |
| `d` | Show inline diff between backup and machine content, then ask again |
| `a` | Overwrite **all remaining conflicts** without asking |
| `l` | Skip **all remaining conflicts** without asking |

The `a` and `l` options persist for the rest of the restore session.

### Redacted File Handling

Files containing `[REDACTED]` are **automatically skipped** during restore. You'll see a message:

```
Skipping ssh/config (contains [REDACTED] values)
```

This prevents writing masked values to the machine. To restore these files, re-run the backup with `--no-redact` first.

### Interactive Category Picker (`--pick`)

With `--pick`, a checkbox UI shows available categories with file counts:

```
? Select categories to restore:
  [x] ai (7 files)
  [ ] shell (1 file)
  [x] git (3 files)
  [ ] editor (2 files)
  [ ] terminal (1 file)
```

Only selected categories are processed. The plan is filtered before execution.

### Dry Run (`--dry-run`)

Preview the restore plan without writing anything:

```bash
$ dothaven restore ./backup --dry-run

Dry run — no files will be changed:

  [NEW]        editor/zed/settings.json → ~/.config/zed/settings.json
  [CONFLICT]   shell/.zshrc → ~/.zshrc
  [SAME]       git/.gitconfig → ~/.gitconfig
  [REDACTED]   ssh/config → ~/.ssh/config

  4 files total: 1 new, 1 conflicts, 1 unchanged, 1 redacted (skipped)
```

### `.local` Override Pattern

If your backup contains files with a `.local` suffix that correspond to a registered base file, they're mapped accordingly:

| Backup Path | Target Path | How It Maps |
|-------------|-------------|-------------|
| `shell/.zshrc` | `~/.zshrc` | Direct map |
| `shell/.zshrc.local` | `~/.zshrc.local` | `.local` suffix on base entry's target |

This supports the common dotfiles pattern of having `~/.zshrc` source a machine-specific `~/.zshrc.local` for local overrides.

---

## Recommended Workflow

### Initial Setup (New Repo)

```bash
git clone https://github.com/you/dothaven.git
cd dothaven
bun install

# Back up current machine
dothaven backup

# Review what was captured
dothaven diff

# Commit
git add . && git commit -m "initial backup"
git push
```

### Daily Use

```bash
# Quick check: what changed?
dothaven status

# Detailed diff
dothaven diff

# Re-backup if needed
dothaven backup
git add . && git commit -m "update configs"
```

### New Machine Setup

```bash
git clone https://github.com/you/dothaven.git
cd dothaven
bun install

# Preview what would be restored
dothaven restore reports/backup-* --dry-run

# Interactive restore
dothaven restore reports/backup-* --pick
```

### Comparing Two Machines

```bash
# On machine A:
dothaven collect -o /tmp

# On machine B:
dothaven collect -o /tmp

# Compare (copy both .json files to same location):
dothaven compare machineA.json machineB.json
```
