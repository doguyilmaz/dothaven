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

The menu fills the terminal and shows one short list at a time, so it fits any window and nothing scrolls. The home screen has five groups; each opens a list of three to six entries. The line at the bottom says what the selected entry does, and entries that change something say so.

```text
 dothaven                                        MacBook-Pro · GitHub: signed in

 Home › GitHub backup

 ▸ Save this Mac to GitHub
   Restore from GitHub
   Sign-in and status
   Sign out

 ────────────────────────────────────────────────────────────
 Private repository, encrypted by default. Signs you in if needed.
 ↑↓ move · enter open · esc back · q quit
```

| Key | Does |
| --- | --- |
| ↑ ↓ (or k j) | Move |
| Enter (or → l) | Open a group, or run an entry |
| 1 to 9 | Open or run the entry with that number |
| Esc (or ← h) | Back to the group above; on the home screen, leave |
| q | Leave |

An action runs in the normal terminal, so its output and questions read as they would from the command line. Press Enter afterwards and the menu comes back where you left it. Ctrl-C during an action stops that action and brings the menu back; a second Ctrl-C quits dothaven at once.

| Group | Entries |
| --- | --- |
| Move to a new Mac | Check nothing would be lost, Pack everything into one encrypted file, Restore a backup, Reinstall my apps and packages, What's still missing here?, Put macOS settings back |
| GitHub backup | Save this Mac to GitHub, Restore from GitHub, Sign-in and status, Sign out |
| Everyday | Quick backup, What changed since my last backup?, Choose what else to back up, Open the dashboard |
| Check this Mac | Scan my config for secrets, Are my config files valid?, See everything installed, Check dothaven itself |
| chezmoi sync (optional) | Check chezmoi and age setup, Export configs to chezmoi, Apply my chezmoi repo here |

Below the groups are "Not sure? Answer a few questions" and Quit. On Linux the menu says "machine" instead of "Mac", and the macOS settings entry is not shown.

Most entries run the command of the same name (`ready`, `restore`, `reinstall`, `missing`, `github push`, `github logout`, `backup`, `status`, `include --review`, `scan`, `check`, `collect`, `doctor`, `ui`, `defaults import`, `init`, `chezmoi-export`, `migrate`, `guide`), so the menu and the command line behave the same. Entries that need a backup first let you pick one from those dothaven can find.

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
