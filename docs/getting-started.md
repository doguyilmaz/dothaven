# Getting Started

## Prerequisites

`dothaven` requires the [Bun](https://bun.sh) runtime (>= 1.0). Install it:

```bash
curl -fsSL https://bun.sh/install | bash
```

Verify:

```bash
bun --version
```

::: tip Why Bun?
The CLI uses `Bun.file()`, `Bun.$`, `Bun.Glob`, `Bun.hash`, and `Bun.color` throughout. It won't run on Node.js. The entry point (`bin/dothaven.ts`) checks for Bun at startup and exits with a clear error if it's missing.
:::

## Installation

### Option A: Run directly (no clone)

```bash
bunx dothaven collect
bunx dothaven backup
bunx dothaven scan ~/.ssh/config
```

This downloads the package on first run and caches it. Good for quick one-off snapshots.

### Option B: Clone the repository

```bash
git clone https://github.com/doguyilmaz/dothaven.git
cd dothaven
bun install
```

Then run commands via:

```bash
bun bin/dothaven.ts collect
bun bin/dothaven.ts backup
# …every command runs the same way: bun bin/dothaven.ts <command>
```

### Option C: Global install

```bash
bun install -g dothaven
dothaven collect
```

## Output Directory

Where reports and backups land depends on context:

| Condition | Output Directory |
|-----------|-----------------|
| `-o /path` provided | That exact path |
| Running inside a git repo (`.git/HEAD` exists in cwd) | `<cwd>/reports/` |
| Running outside a git repo (global/standalone) | `~/Downloads` |

You can always override with the `-o` flag:

```bash
dothaven collect -o ~/my-reports
dothaven backup -o /tmp/backup-test
```

## Core Journeys

### Journey A: "What's on my machine?"

Generate a `.json` snapshot — a single parseable JSON file capturing your entire machine config.

```bash
dothaven collect                           # Full snapshot
dothaven collect --slim                    # AI-friendly (truncated content)
dothaven list brew                         # Query a section from latest report
dothaven compare home.json work.json       # Diff two snapshots
```

The `.json` snapshot is a flat map of section id → section, with each section carrying key-value `pairs`, structured `items`, and/or a `content` block. It's pretty-printed (2-space), human-readable, git-diffable, and parseable by any JSON tool. Serialization is native (`JSON.stringify` / `JSON.parse`) via the in-tree `src/snapshot` module — no runtime dependencies.

### Journey B: "Back up my configs"

Copy real files into a structured directory. Two tracks:

**Clone track** (power users): backup writes into repo → `git add . && git commit && git push` to your private repo.

**CLI-only track** (quick & portable): use `--archive` for a `.tar.gz` you can email, AirDrop, or store anywhere.

```bash
dothaven backup                            # Full structured backup
dothaven backup --only ai,shell            # Just AI + shell configs
dothaven backup --skip editor,npm          # Everything except editors and npm
dothaven backup --archive                  # Export as .tar.gz
dothaven backup --archive -o ~/Desktop     # Archive to specific location
```

### Journey C: "Set up a new machine"

Restore from a backup with full safety features — interactive picker, dry run preview, conflict resolution, and automatic rollback snapshots.

```bash
dothaven restore ./backup --dry-run        # Preview what would change
dothaven restore ./backup --pick           # Select categories interactively
dothaven restore ./backup                  # Restore everything
```

### Journey D: "What changed?"

Compare your backup against the current machine state. Track drift.

```bash
dothaven diff                              # Full diff (auto-finds latest backup)
dothaven diff --section ai                 # Just AI configs
dothaven status                            # Quick summary: modified count, age
```

## Sensitivity — Safe by Default

Every `collect` and `backup` run automatically scans for sensitive data. The CLI:

1. **Detects** 27+ patterns (API keys, tokens, private keys, IPs, passwords, DB URLs)
2. **Classifies** each finding as HIGH / MEDIUM / LOW severity
3. **Acts**: redacts values, skips entire files, or includes with warnings

After every run, you see a sensitivity report:

```
⚠ Sensitivity report:
  HIGH   ~/.ssh/id_ed25519         private key — skipped
  HIGH   ~/.npmrc                  auth token — redacted
  MEDIUM ~/.gitconfig              email address — included

  2 items redacted, 1 skipped. Use --no-redact to include all.
```

To bypass redaction (e.g., for a private encrypted repo):

```bash
dothaven collect --no-redact
dothaven backup --no-redact
```

See [Sensitivity and Redaction](/sensitivity) for the full pattern list and action rules.

## Timestamped Output

All outputs use timestamped names to prevent overwrites:

| Output Type | Naming Pattern |
|------------|----------------|
| Collect report | `<hostname>-YYYYMMDDHHMMSS.json` |
| Backup directory | `backup-<hostname>-YYYYMMDDHHMMSS/` |
| Archive | `backup-<hostname>-YYYYMMDDHHMMSS.tar.gz` |
| Pre-restore snapshot | `pre-restore-YYYYMMDDHHMMSS/` |

## What's Next

- [Commands](/commands) — full reference for every command and flag
- [Backup and Restore](/backup-restore) — safety features, conflict resolution, rollback
- [Sensitivity and Redaction](/sensitivity) — all 27+ detection patterns
- [Architecture](/architecture) — project structure, types, registry design
- [Execution Flows](/flows) — mermaid diagrams showing runtime behavior
- [Behavior Reference](/behavior-reference) — defaults, edge cases, caveats
