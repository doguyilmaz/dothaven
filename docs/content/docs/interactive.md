---
title: Interactive mode
weight: 4
---

dothaven is scriptable first, but on a terminal it gets out of your way with
interactive prompts. Every interactive flow is **opt-in by context**: it runs
only when both stdin and stdout are a terminal. Pipe the output, redirect it, or
run it in CI and the exact same command stays non-interactive — so automation is
never surprised by a prompt.

## The `tui` launcher

`dothaven tui` opens a menu and dispatches to the action you pick:

```text
  dothaven
  pick an action
  > Set up this machine from chezmoi (apply)
    Back up configs
    Export to chezmoi (age-encrypted)
    Restore from the latest backup
    Check setup (chezmoi + age)
    Status of the latest backup
    Quit
```

Each choice runs the matching command with its defaults, so the per-command
interactive flow below takes over. The first entry runs [`migrate`](../commands#migrate)
— the clean-machine path — so the menu leads with what a fresh laptop needs.
Off a terminal, `tui` exits with an error rather than hanging.

## Interactive-when-TTY commands

You don't need the `tui` menu — the commands themselves become interactive:

| Command | On a terminal, with no relevant flags |
| --- | --- |
| `backup` | Opens a **category picker** ("what to back up") |
| `chezmoi-export` | Opens the picker (config categories + the `brew`/`packages` groups) |
| `restore` | Resolves each **conflict** interactively (overwrite / skip / diff / all) |
| `init` | Offers to **run the safe setup steps** (install chezmoi, init the source repo) |

### Category picker

`backup` and `chezmoi-export` with no `--only`/`--skip` show a multi-select of
categories, with a count and a 🔒 marker on the ones that hold secrets:

```text
  What to back up
  space toggles · enter confirms
  > [x] shell      6
    [x] git        4
    [ ] cloud     18  🔒 encrypted
    [x] editor     9
    [ ] secrets    3  🔒 encrypted
```

Your selection becomes the `--only` filter. Pass `--only`/`--skip` explicitly to
skip the picker entirely.

### Restore conflict resolution

When a backed-up file differs from the one on disk, `restore` (without `--force`)
asks per file:

```text
  Conflict — /Users/you/.zshrc
  the live file differs from the backup
  > Overwrite with backup
    Skip (keep live file)
    Show diff
    Overwrite all remaining
    Skip all remaining
```

"Show diff" prints a red/green line diff and re-asks. Any overwrite is
snapshotted (owner-only) into a `pre-restore-…` directory first, so a wrong
choice is recoverable.

## Forcing non-interactive

- Pass the relevant flags (`--only`, `--skip`, `--force`, `--dry-run`) — they
  pre-answer the prompt, so it's skipped.
- Pipe or redirect (`dothaven backup | tee log`, CI) — no terminal, no prompt.

{{< cards >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../backup-restore" title="Backup & restore" >}}
{{< /cards >}}
