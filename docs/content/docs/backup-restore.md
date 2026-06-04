---
title: "Backup & restore"
weight: 10
---

`dothaven backup` copies your tracked config files into a timestamped tree, applying the
same secret-scrubbing gate as `collect` so a plaintext backup never carries a raw secret.
`dothaven restore` reads such a tree back, classifying each file against the live machine
and writing only what is safe to write. `status` and `diff` let you inspect a backup
without touching anything.

This is a local copy workflow — no encryption, no chezmoi, no remote. For the
encrypted-storage path, see the hybrid model below.

## backup

```bash
dothaven backup
```

`backup` walks the registry of tracked targets, reads each source, runs it through the
redaction gate, and writes the result into a fresh directory:

```text
backup-<hostname>-<UTC-timestamp>/
```

The timestamp is `YYYYMMDDhhmmss` in UTC, so directory names sort chronologically. If the
hostname can't be read it falls back to `machine`.

### Where backups land

The destination follows the standard output-directory resolution:

- an explicit `-o` / `--output` wins;
- otherwise `<cwd>/reports` when you're inside a git repository;
- otherwise `~/Downloads`.

```bash
dothaven backup -o ~/dotfiles-backups
```

Example run:

```text
$ dothaven backup
Backup saved to: /Users/you/project/reports/backup-mbp-20260604091233
  6 files across: git (2), shell (3), ssh (1)
```

(The per-category line is rendered from the actual category counts; `git (2), shell (3)`
means two files in the `git` category, three in `shell`.)

### The copy engine

Each registry target is either a single file or a directory:

- **Files** are read, gated, and written to their mapped destination.
- **Directories** are mirrored recursively (dotfiles included); every file inside passes
  through the gate independently.

Missing or unreadable sources are skipped silently — a tool you don't have installed
simply contributes nothing rather than erroring. If nothing matches at all, you get:

```text
No files found to backup.
```

### The redaction / skip gate

Before any file is written, its content goes through a two-tier gate:

1. **Skip (private keys).** If the scanner flags a *skip-action* finding — a PEM or PGP
   private key block — the file is **not copied at all**. A plaintext backup never carries
   a raw private key.
2. **Redact (secrets).** Otherwise, matched secrets (tokens, API keys, passwords, AWS/GCP
   credentials, and the like) are masked in place with a `[REDACTED]` marker before the
   file is written. The surrounding config is preserved; only the secret value is replaced.

After a redacting run, a sensitivity report is printed summarizing what was scrubbed or
skipped:

```text
⚠ Sensitivity report:
  High   ssh/config                     IP address — redacted
  High   ssh/id_ed25519                 private key — skipped

  1 items redacted, 1 skipped. Use --no-redact to include all.
```

### `--no-redact`

Disables the gate entirely — raw values are kept, private keys included. Use this only for
a backup you intend to keep private and unshared.

```bash
dothaven backup --no-redact
```

With `--no-redact`, no sensitivity report is printed (nothing was scrubbed).

### `--archive`

Writes a single `.tar.gz` instead of a directory and removes the intermediate tree:

```bash
dothaven backup --archive
```

```text
$ dothaven backup --archive
Archive saved to: /Users/you/project/reports/backup-mbp-20260604091233.tar.gz
  6 files across: git (2), shell (3), ssh (1)
```

{{< callout type="info" >}}
Archives are *not* picked up by `restore`, `status`, or `diff` — those operate on backup
directories. Extract a `.tar.gz` first if you need to diff or restore from it.
{{< /callout >}}

### `--only` / `--skip`

Both flags take comma-separated category names. `--skip` always wins; a non-empty `--only`
restricts the run to just those categories.

```bash
dothaven backup --only shell,git
dothaven backup --skip ssh
```

### backup flags

| Flag | Description |
| --- | --- |
| `-o`, `--output` | Output directory (default: `./reports` in a repo, else `~/Downloads`) |
| `--archive` | Create a `.tar.gz` instead of a directory |
| `--no-redact` | Keep raw values (skip secret redaction) |
| `--only` | Only these categories (comma-separated) |
| `--skip` | Skip these categories (comma-separated) |

## restore

```bash
dothaven restore <backup-path>
```

`restore` takes a backup directory and rebuilds a **plan**: it walks the backup, maps each
file back to its live target on the machine, reads that target's current content, and
assigns a status.

### Per-file status

| Status | Meaning |
| --- | --- |
| `new` | In the backup, absent on the machine |
| `conflict` | Present on the machine but differs from the backup |
| `same` | Identical — nothing to do |
| `redacted` | The backup holds a `[REDACTED]` marker — unrestorable |

### Default behavior

By default, `restore` is conservative:

- **`new`** files are written.
- **`conflict`** files are **skipped** — your existing file is left untouched.
- **`same`** and **`redacted`** files are never written.

```text
$ dothaven restore ~/backups/backup-mbp-20260604091233
Restored 2 file(s) across: shell (2)
  4 file(s) skipped
```

If nothing was restored because everything differs, you're told how to proceed:

```text
No files restored. 3 conflict(s) skipped — re-run with --force to overwrite.
```

If there was simply nothing to do:

```text
No files restored (everything already up to date).
```

{{< callout type="warning" >}}
**A `[REDACTED]` file is never restored.** When a backed-up file contains the `[REDACTED]`
marker, its real value was stripped during backup. Restoring it would overwrite a live
secret with the placeholder text, so the restore engine refuses — even under `--force`.
The redacted gate protects you on the way in *and* on the way out.
{{< /callout >}}

### `--force`

Overwrites `conflict` files. Before overwriting, the **prior content of each conflicting
file is snapshotted** into a `pre-restore-<timestamp>` directory (resolved the same way as
backup output), so an overwrite is always reversible.

```bash
dothaven restore <backup-path> --force
```

```text
$ dothaven restore ~/backups/backup-mbp-20260604091233 --force
Pre-restore snapshot saved to: /Users/you/project/reports/pre-restore-20260604093001
Restored 5 file(s) across: git (2), shell (3)
```

`--force` does **not** override the redacted guard — `redacted` (and `same`) entries are
still skipped.

### `--dry-run`

Prints the full plan without writing anything:

```bash
dothaven restore <backup-path> --dry-run
```

```text
$ dothaven restore ~/backups/backup-mbp-20260604091233 --dry-run

Dry run — no files will be changed:

  [NEW]      shell/.zshrc → /Users/you/.zshrc
  [CONFLICT] git/.gitconfig → /Users/you/.gitconfig
  [SAME]     shell/.bashrc → /Users/you/.bashrc
  [REDACTED] ssh/config → /Users/you/.ssh/config

  4 files total: 1 new, 1 conflicts, 1 unchanged, 1 redacted (skipped)
```

### `--only` / `--skip`

Same category filtering as backup — comma-separated, `--skip` wins over `--only`.

```bash
dothaven restore <backup-path> --only shell
dothaven restore <backup-path> --skip ssh
```

If filtering leaves nothing:

```text
No restorable files found in backup.
```

### restore flags

| Flag | Description |
| --- | --- |
| `--dry-run` | Show what would change without writing |
| `--force` | Overwrite differing files (a pre-restore snapshot is saved first) |
| `--only` | Only these categories (comma-separated) |
| `--skip` | Skip these categories (comma-separated) |

## status

```bash
dothaven status
```

`status` summarizes your **latest** backup directory against the live machine. It finds the
newest `backup-*` directory in the output location (archives are ignored — they can't be
diffed without extraction) and tallies the plan:

```text
$ dothaven status
Last backup: 2h ago (backup-mbp-20260604071205)
  6 files tracked: 1 modified, 4 unchanged
  1 not on machine (new in backup)

Modified since backup:
  git/.gitconfig
```

The age is rendered coarsely as minutes / hours / days ago from the directory's mtime. When
nothing has drifted:

```text
  Everything up to date.
```

If no backup exists yet:

```text
No backup found. Run 'dothaven backup' first.
```

## diff

```bash
dothaven diff [backup-path]
```

`diff` compares a backup against the live machine, **grouped by category**. With no
argument it uses the latest backup; pass a path to compare a specific one.

```text
$ dothaven diff

Comparing backup against live system:

  git/
    git/.gitconfig — modified
  shell/
    shell/.bashrc — unchanged
    shell/.zshrc — new in backup (missing on machine)
  ssh/
    ssh/config — redacted

  4 files: 1 modified, 2 unchanged, 1 new, 1 redacted
```

On a TTY the status labels are colored (yellow = modified, blue = new, green = unchanged,
gray = redacted); piped output is plain.

### `--section`

Limit the comparison to a single category:

```bash
dothaven diff --section git
```

```text
No entries found for section: git
```

is printed when that category has no entries in the backup.

### diff flags

| Flag | Description |
| --- | --- |
| `--section` | Only show this category |

## The hybrid model

`backup`/`restore` is the **local plaintext** path: a redacted copy you can inspect,
archive, and roll back yourself. For long-term, encrypted storage, dothaven hands off to
[chezmoi](https://www.chezmoi.io):

- **dothaven** does discovery, audit, and export.
- **chezmoi** does storage, age-encryption, and apply.

`age` is the encryption backend on the chezmoi side. Losing the age key means the encrypted
files are unrecoverable — back that key up separately. The two paths are complementary: use
`backup`/`restore` for quick local snapshots, and the chezmoi export for an encrypted source
of truth.

{{< cards >}}
  {{< card link="../installation" title="Installation" >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../chezmoi-export" title="chezmoi export" >}}
{{< /cards >}}
