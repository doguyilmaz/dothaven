---
title: Interactive mode
weight: 8
---

Every dothaven command works from scripts. On a terminal, it also asks the questions that matter, so you do not need to remember flags. Interactive prompts appear only when both input and output are a terminal: pipe the output, redirect it, or run in CI, and the same command runs without asking.

## The menu

```bash
dothaven         # with no arguments, on a terminal
dothaven tui     # the same menu, by name
```

The menu is grouped by what you want to do. Each entry has a one-line hint, and the entries that change something say so. After an action finishes you press Enter to come back to the menu; Esc or Quit leaves.

```text
Moving to a new Mac
  1. Check nothing would be lost            unpushed work, .env files, backup age — read-only
  2. Pack everything into one encrypted file configs, keys, tokens, app list, settings
  3. Restore a backup onto this Mac         pick a backup, preview, then write
  4. Reinstall my apps & packages           from a backup's list — Homebrew, npm, …
  5. What's still missing here?             a backup's app list vs this Mac — read-only

Your private GitHub repo
  Save this Mac to GitHub                   private repo, encrypted by default
  Restore from GitHub                       pick a machine, preview, then write
  GitHub sign-in & status                   who, which repo, which machines

Everyday
  Quick backup to this Mac                  a folder here; secrets redacted
  What changed since my last backup?        read-only
  Choose what else to back up               files & folders dothaven doesn't know
  Scan my config for secrets                tokens and keys in plain files — read-only
  Are my config files valid?                parses each one — read-only
  See everything installed                  apps, packages, runtimes, fonts
  Open the dashboard in your browser        coverage, backups, secrets, risks — local, read-only
  Put macOS settings back from a backup     CHANGES system settings — asks first

Sync through a chezmoi repo (optional)
  Check chezmoi + age setup                 read-only
  Export configs to chezmoi                 preview first; secrets encrypted
  Apply my chezmoi repo here                WRITES to ~ and runs your install script

  Not sure? Answer a few questions          get the exact steps for your case
  Quit
```

On Linux the menu says "machine" instead of "Mac", and the macOS settings entry is not shown.

Most entries run the command of the same name (`ready`, `restore`, `reinstall`, `missing`, `github push`, `backup`, `status`, `include --review`, `scan`, `check`, `collect`, `ui`, `defaults import`, `init`, `chezmoi-export`, `migrate`, `guide`), so the menu and the command line behave the same. Entries that need a backup first let you pick one from those dothaven can find.

Off a terminal, `dothaven tui` exits with an error rather than waiting forever, and plain `dothaven` prints the help.

### Pack everything into one encrypted file

This entry is the whole old-machine job in one pass:

1. **Anything that exists only here?** Runs the `ready` check. If some work exists only on this machine, it asks whether to continue anyway.
2. **Anything else to take?** Offers the config-looking paths nothing covers yet.
3. **Where should the file go?** Mounted drives come first ("straight onto the drive — best"), then Desktop, dothaven's own folder, or a folder you type. Then it asks for a passphrase, twice.
4. **Packing.** Writes one encrypted backup with everything, then reads it back end to end with your passphrase, without writing anything, to prove it opens:

```text
✓ Checked: the file opens with your passphrase and holds all 196 entries.
```

If the file does not read back cleanly, it tells you not to rely on it and to pack again.

## Commands that ask on a terminal

You do not need the menu for the questions: the commands ask them themselves when run on a terminal.

| Command | What it asks |
| --- | --- |
| `backup` | Which categories to include (all selected; credential categories marked), then whether to add config nothing covers yet. Skipped when you pass `--only` or `--skip`. |
| `restore` | Which backup (when you give none); then everything new or updated, some categories, or some files; then, for each file that differs, overwrite or keep, with a diff. Afterwards: put back macOS settings? reinstall apps now? |
| `reinstall` | Which backup (when you give none); then everything missing, some groups, or some packages. |
| `defaults import` | Which settings domains to apply (all selected), and whether to rebuild the Dock. |
| `github push` | Whether to create the repository, how to store the backup (first time), the passphrase, and whether to remember it. |
| `github status` | Whether to sign in, if you are not. |
| `restore github` | Which machine, if the repository holds several. |
| `chezmoi-export` | Which categories and install groups to export; with `--apply`, whether your age key is backed up. |
| `init` | Whether to install chezmoi and run `chezmoi init` for you. |
| `guide` | What you want to do and what kind of work you do, then offers to run step 1. |
| `include --review` | Which uncovered paths to add. |

Encrypted backups ask for the passphrase on the terminal itself (`/dev/tty`), so this works even when output is piped: `dothaven restore backup.tar.gz.age | tee restore.log`.

### The category picker

```text
What to back up
Everything is selected. space toggles · a toggles all · enter continues
> [x] ai         Claude, Codex, Cursor, Gemini… skills, agents, MCP, plugins  🔑 credentials — left out unless --encrypt
  [x] apps       Karabiner, Hammerspoon, window managers…
  [x] build      Maven and Gradle settings  🔑 credentials — left out unless --encrypt
  [x] bun        bun config
  …
```

### A file that differs

```text
Conflict — ~/.zshrc
the live file differs from the backup
> Overwrite with backup
  Skip (keep live file)
  Show diff
  Overwrite all remaining
  Skip all remaining
```

"Show diff" prints a red and green line diff and asks again. Whatever you overwrite is first copied to a `pre-restore-<timestamp>` folder, so a wrong choice can be undone.

## Running without questions

- **Pass the flags** that answer the question: `--only`/`--skip` for categories, `--force` to overwrite files that differ, `--dry-run` to only look.
- **Pass `--yes`** where a command changes something (`restore`, `reinstall`, `defaults import`, `services import`, `migrate`, `github push` when creating the repository, `upgrade`).
- **Off a terminal**, a command that would change something you already have and was not given `--yes` refuses, rather than guessing:

```text
Refusing to continue without a terminal to confirm on.
Re-run with --yes if you meant it, or --dry-run to see what would change.
```

A pipe cannot answer a question, and silence is not consent. The exception is writing *new* files: `restore` off a terminal writes files that do not exist yet, and keeps any that differ.

- **Passphrases** come from `DOTHAVEN_PASSPHRASE` when it is set.

{{< cards >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../backup-restore" title="Backup & restore" >}}
{{< /cards >}}
