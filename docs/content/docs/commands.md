---
title: Commands
weight: 5
---

dothaven is a single static Go binary built on [Cobra](https://github.com/spf13/cobra). It ships the commands below — plus the interactive [`tui`](../interactive) launcher and Cobra's auto-generated `help` and `completion`. This page is the complete reference: purpose, synopsis, every flag with its default, argument rules, and an example for each.

dothaven covers the **discovery, audit, and export** half of the workflow; [chezmoi](https://www.chezmoi.io/) handles **storage, age-encryption, and apply** on the target machine. Several commands stop at planning by design — they print what they would do and leave execution to chezmoi.

## How output paths are resolved

Commands that write files (`collect`, `backup`, and the snapshot machinery behind `status` / `diff`) resolve their destination the same way:

1. An explicit `-o`/`--output` value always wins.
2. Otherwise, if the current directory is a git repository (a `.git/HEAD` exists), output goes to `<cwd>/reports`.
3. Otherwise, output goes to `~/Downloads`.

Snapshot files are named `<hostname>-<timestamp>.json`; backups are named `backup-<hostname>-<timestamp>`. The timestamp is UTC `YYYYMMDDHHMMSS`.

{{< callout type="info" >}}
`compare` and `list` read from the literal `reports/` directory under the current working directory — they do not use the resolution logic above. Run them from your repo root.
{{< /callout >}}

---

## Capture

These commands read your machine and write artifacts: a snapshot, a backup, or a security report.

### collect

Inventory this machine into a timestamped JSON snapshot.

```text
dothaven collect [flags]
```

Runs the full collector pipeline (host metadata, the declarative registry, SSH, Ollama, apps, Homebrew, packages, runtimes, editor extensions, fonts, and a dotfiles sweep), redacts secrets by default, and writes a single JSON snapshot. When redaction runs, a summary of what was redacted is printed after the save.

**Arguments:** none.

| Flag | Default | Description |
| --- | --- | --- |
| `--no-redact` | `false` | Keep raw values (skip secret redaction). |
| `--slim` | `false` | Truncate long file contents to 10 lines. |
| `-o`, `--output` | _(resolved)_ | Output directory. Default: `./reports` in a repo, else `~/Downloads`. |

```bash
$ dothaven collect
Report saved to: /Users/you/project/reports/macbook-20260604120000.json
```

{{< callout type="warning" >}}
`--no-redact` writes raw secret values into the snapshot. Only use it for a snapshot you keep local and never commit.
{{< /callout >}}

### scan

Scan a file or directory for sensitive data (console).

```text
dothaven scan [path]
```

Walks a path looking for secrets and prints findings line-by-line with severity, then a summary. Findings are sorted high severity first. If `[path]` is omitted, the current directory (`.`) is scanned. A missing path is an error.

**Arguments:** optional single `path` (file or directory). Defaults to `.`.

This command has no flags.

```bash
$ dothaven scan ~/.aws/credentials
~/.aws/credentials
  L3 [High] AWS access key: AKIA****************
```

### security

Write a Markdown security report (default `SECURITY.md`).

```text
dothaven security [path]
```

Scans the same way as `scan`, but writes the result as a Markdown report to disk instead of printing findings, then prints how many files were scanned and how many had findings. If `[path]` is omitted, the current directory (`.`) is scanned.

**Arguments:** optional single `path` (file or directory). Defaults to `.`.

| Flag | Default | Description |
| --- | --- | --- |
| `-o`, `--output` | `SECURITY.md` | Report output path. |

```bash
$ dothaven security ./reports -o audit.md
Security report written to: audit.md
  12 scanned, 2 with findings.
```

---

## Inspect

These commands read existing snapshots (or the live machine) and report — they never write.

### list

Print a section (fuzzy-matched) from the most recent report.

```text
dothaven list <section>
```

Loads the newest `.json` file in `reports/` (relative to the current directory) and prints every section whose name fuzzy-matches the query. Matching is case-insensitive and also matches against the dot-separated parts of a section id (so `brew` matches `apps.brew.bundle`).

**Arguments:** exactly one `section` query.

This command has no flags.

```bash
$ dothaven list packages
[packages.bun.global]
  typescript
  prettier
```

### compare

Diff two JSON snapshots (newest two in `reports/` if omitted).

```text
dothaven compare [file1] [file2]
```

Compares two snapshots and prints only the differences. With no arguments, it picks the two newest `.json` files in `reports/` (relative to the current directory); with fewer than two available it prints usage and exits cleanly. Both explicit files must exist.

**Arguments:** zero, or exactly two file paths. (One argument is accepted by the parser but falls through to the auto-pick path.)

This command has no flags.

```bash
$ dothaven compare reports/old.json reports/new.json
+ packages.bun.global: vitest
- packages.npm.global: eslint
```

### doctor

Compare a snapshot against this machine; list what's missing.

```text
dothaven doctor <snapshot.json>
```

Re-inventories the live machine and reports installable items present in the snapshot but missing locally — packages, runtimes, Homebrew formulae, macOS apps, fonts, and editor extensions. Parity is keyed on item name, so version drift is ignored: the question is "present?", not "same version?".

**Arguments:** exactly one snapshot file path.

This command has no flags.

```bash
$ dothaven doctor reports/macbook-20260604120000.json
Missing on this machine (present in the snapshot):

  packages.bun.global (1)
    - vitest

1 item(s) missing across 1 section(s).
```

{{< callout type="warning" >}}
`doctor` exits **non-zero** when anything is missing — it is built for CI. A clean machine prints a parity message and exits `0`; any drift exits `1` with the report still on stdout.
{{< /callout >}}

---

## Backup and restore

These commands copy tracked config files into a timestamped backup and bring them back. They share `--only` / `--skip` category filtering.

### backup

Copy tracked config files into a timestamped backup.

```text
dothaven backup [flags]
```

Collects the registry's backup targets from your home directory, redacts secrets by default, and copies them into a timestamped `backup-<host>-<timestamp>` directory (or a `.tar.gz` with `--archive`). Prints a per-category file count and, when redaction ran, a redaction summary.

**Arguments:** none.

| Flag | Default | Description |
| --- | --- | --- |
| `--no-redact` | `false` | Keep raw values (skip secret redaction). |
| `--archive` | `false` | Create a `.tar.gz` instead of a directory. |
| `-o`, `--output` | _(resolved)_ | Output directory. Default: `./reports` in a repo, else `~/Downloads`. |
| `--only` | _(none)_ | Only these categories (comma-separated). |
| `--skip` | _(none)_ | Skip these categories (comma-separated). |

```bash
$ dothaven backup --only shell,git
Backup saved to: /Users/you/project/reports/backup-macbook-20260604120000
  5 files across: git (2), shell (3)
```

### restore

Restore files from a backup into your home directory.

```text
dothaven restore <backup-path> [flags]
```

Builds a plan from a backup directory, mapping each backed-up file to its home-directory target, then applies it. New files are written; files that differ from what's on disk are treated as conflicts and **skipped unless `--force`** is given. With `--force`, a pre-restore snapshot of the files about to be overwritten is saved first. Redacted entries are never restored.

**Arguments:** exactly one `backup-path` (a backup directory).

| Flag | Default | Description |
| --- | --- | --- |
| `--dry-run` | `false` | Show what would change without writing. |
| `--force` | `false` | Overwrite differing files (a pre-restore snapshot is saved first). |
| `--only` | _(none)_ | Only these categories (comma-separated). |
| `--skip` | _(none)_ | Skip these categories (comma-separated). |

```bash
$ dothaven restore reports/backup-macbook-20260604120000 --dry-run

Dry run — no files will be changed:

  [NEW]      git/gitconfig → /Users/you/.gitconfig
  [CONFLICT] shell/zshrc → /Users/you/.zshrc

  2 files total: 1 new, 1 conflicts
```

### status

Summarize the latest backup against the live machine.

```text
dothaven status
```

Finds the newest `backup-*` directory in the resolved output directory and reports how it compares to the live machine: files tracked, modified (conflicts), unchanged, new in backup, and redacted. Modified files are listed by name. If no backup exists, it tells you to run `backup` first.

**Arguments:** none.

This command has no flags.

```bash
$ dothaven status
Last backup: 2h ago (backup-macbook-20260604120000)
  12 files tracked: 1 modified, 11 unchanged

Modified since backup:
  shell/zshrc
```

### diff

Compare a backup against the live machine, grouped by category.

```text
dothaven diff [backup-path] [flags]
```

Like `status`, but prints every entry grouped by category with a per-file status (modified, new, unchanged, redacted) and a colored summary when stdout is a terminal. With no argument it uses the latest backup; pass a `backup-path` to compare a specific one.

**Arguments:** optional single `backup-path`. Defaults to the latest backup.

| Flag | Default | Description |
| --- | --- | --- |
| `--section` | _(none)_ | Only show this category. |

```bash
$ dothaven diff --section shell

Comparing backup against live system:

  shell/
    shell/zshrc — modified
    shell/zprofile — unchanged

  2 files: 1 modified, 1 unchanged
```

---

## Migrate

These commands bridge to chezmoi: they plan (and optionally apply) bringing your configs under chezmoi management, and verify the prerequisites for doing so.

### chezmoi-export

Plan (or apply) adding configs to chezmoi, encrypting secrets.

```text
dothaven chezmoi-export [flags]
```

Builds a chezmoi-add plan — plain `add` for ordinary configs, `add --encrypt` for secrets, and `add --template` for host-varying configs (shell rc, gitconfig, editor settings), whose absolute home paths are rewritten to `{{ .chezmoi.homeDir }}` so they port across machines — plus a `run_onchange` install script for Homebrew and global packages. **Dry-run by default:** it prints the plan and stops. With `--apply`, it executes against chezmoi (which must be installed, with a configured age key). When the plan encrypts anything it warns that those files are recoverable only with your age key and, on a terminal, asks you to confirm the key is backed up. If your editor's built-in Settings Sync looks active, it warns that cloud sync and chezmoi will both rewrite those files. On apply it also merges `.chezmoiignore` patterns for GnuPG runtime cruft when relevant and writes `run_onchange_install-packages.sh` into the chezmoi source path.

**Arguments:** none.

| Flag | Default | Description |
| --- | --- | --- |
| `--apply` | `false` | Execute the plan (default: dry-run). |
| `--pin` | `false` | Pin global packages to their captured version. |
| `--only` | _(none)_ | Only these categories/groups (comma-separated). |
| `--skip` | _(none)_ | Skip these categories/groups (comma-separated). |

```bash
$ dothaven chezmoi-export
chezmoi-export plan — 3 path(s), 1 encrypted:

      add            /Users/you/.gitconfig  (git config)
   🔒 add --encrypt  /Users/you/.ssh/id_ed25519  (ssh private key)
  + run_onchange install script (brew, packages)

Dry-run. Re-run with --apply to execute (requires chezmoi + a configured age key).
```

{{< callout type="warning" >}}
age is the encryption backend. **Losing the age key means encrypted files are unrecoverable** — back the key up somewhere safe and separate from the chezmoi source repo. If chezmoi is not installed, `--apply` exits non-zero and points you at `brew install chezmoi`.
{{< /callout >}}

### init

Check the chezmoi + age prerequisites for export.

```text
dothaven init
```

A read-only bootstrap check. It probes whether chezmoi is installed, whether age encryption is configured in `~/.config/chezmoi/chezmoi.toml`, whether the chezmoi source is an initialized git repo, and your GitHub login (via `gh`), then prints each step as done (`✓`) or pending (`→`) with the exact command to run. When everything is ready it prints the next `chezmoi-export` steps. It never changes anything.

**Arguments:** none.

This command has no flags.

```bash
$ dothaven init
dothaven init — chezmoi + age bootstrap

  ✓ chezmoi installed
  → configure age encryption
      chezmoi age setup

Run the commands above, then re-run `dothaven init`.
```

### migrate

Set up this machine from your chezmoi source (prereqs → apply → verify).

```text
dothaven migrate
```

The clean-machine happy path — one command for the moment you're staring at an empty laptop. It verifies chezmoi is installed and the source repo is initialized (printing guidance and exiting non-zero if not), warns if age encryption isn't configured, asks for confirmation on a terminal, then runs `chezmoi apply` — which pulls your managed configs and runs the generated install script. It finishes by pointing at `chezmoi diff` and `dothaven doctor` to verify. On a non-terminal (CI) it skips the confirmation.

**Arguments:** none.

This command has no flags.

---

## macOS preferences & local services

Two paired commands for config that doesn't live in a `~/.dotfile`: macOS app preferences (binary plists managed by `cfprefsd`) and Homebrew service config (under the brew prefix). Both capture to a directory and replay onto a new machine; both are macOS-oriented and no-op gracefully where the underlying tool is absent.

### defaults

Capture and restore curated macOS app preferences.

```text
dothaven defaults export [-o dir]
dothaven defaults import <dir>
```

`export` runs `defaults export` for a curated allowlist of app domains (iTerm2, Terminal.app, Rectangle, Hammerspoon, AltTab), writing each to an owner-only `.plist` under `<dir>/macos-defaults/`; domains with no preferences are skipped. `import` replays them with `defaults import` — the safe round-trip for `cfprefsd`-managed prefs, since a raw file copy is silently ignored. System domains (Dock/Finder/keyboard) are intentionally excluded: they mix portable keys with host-specific ones (display/Spaces UUIDs, absolute paths) that would corrupt a new machine, and need per-key curation.

| Flag | Default | Description |
| --- | --- | --- |
| `-o`, `--output` (export) | _(repo `./reports`, else `~/Downloads`)_ | Output directory for the `macos-defaults/` plists. |

### services

Capture and restore Homebrew-managed local service config.

```text
dothaven services export [-o dir]
dothaven services import <dir>
```

`export` captures user-editable service config under `$(brew --prefix)/etc` (nginx, httpd, my.cnf, redis, dnsmasq), writes it owner-only, records the source brew prefix, and warns — without redacting, since it must round-trip — if a file looks secret-bearing (e.g. a password in `my.cnf`). `import` resolves *this* machine's `$(brew --prefix)` and re-points the old prefix to the new one in the content, so Intel/ARM/Linuxbrew paths resolve. The service binaries themselves come back via the Brewfile; databases and other data are out of scope.

| Flag | Default | Description |
| --- | --- | --- |
| `-o`, `--output` (export) | _(repo `./reports`, else `~/Downloads`)_ | Output directory for the `services/` tree. |

---

## See also

{{< cards >}}
  {{< card link="../installation" title="Installation" >}}
  {{< card link="../quick-start" title="Quick start" >}}
{{< /cards >}}
