---
title: Commands
weight: 9
---

Every dothaven command, grouped the way `dothaven --help` groups them, with every flag. Flags are copied from each command's `--help`. Run `dothaven <command> --help` for the same text in your terminal.

```text
dothaven [flags]
dothaven [command]
```

| Global flag | Meaning |
| --- | --- |
| `-h`, `--help` | Help for dothaven or any command |
| `-v`, `--version` | Print the version |

With no command, on a terminal, `dothaven` opens the [menu](../interactive). Off a terminal it prints the help.

**Rule for anything that writes:** a command that would change files you already have asks first on a terminal, and refuses off a terminal unless you pass `--yes`. Most take `--dry-run` to show what would change.

---

## Start here

### tui

Interactive menu: pick what to do.

```text
dothaven tui
```

The same menu as plain `dothaven`: grouped actions, run one, press Enter to come back. It needs a terminal and exits with an error without one. See [Interactive mode](../interactive). No flags.

### ui

Open a live dashboard in your browser (local and read-only). Alias: `dashboard`.

```text
dothaven ui [flags]
```

Serves a dashboard from this machine only: what your backups cover and what they miss, the backups it can find, secrets sitting in plain files, repositories with unpushed work, installed software, what restore has applied, and your GitHub backup repo. It listens on `127.0.0.1` with a one-time key in the link, never writes anything, and loads nothing from the internet. Stop it with Enter or Ctrl-C. See [Dashboard](../dashboard).

| Flag | Meaning |
| --- | --- |
| `--no-open` | Print the link but don't open a browser |

### guide

Answer a few questions, get the exact commands to run.

```text
dothaven guide
```

Asks what you want to do (back up, set up a new computer from this one, reinstall or replace this computer, check your setup is healthy, compare two computers, put your config in a private repo, see everything you have) and, where it matters, what kind of work you do. It answers with an ordered list of commands, a reason for each, and notes about what does not travel for your kind of work, then offers to run step 1. It does not ask what it can detect, such as whether chezmoi is installed. Needs a terminal. No flags.

### ready

Before a wipe: is anything on this machine only here? (read-only)

```text
dothaven ready [flags]
```

Looks through your home folder for git repositories with uncommitted changes, commits that are on no remote, stashes, and gitignored files a fresh clone won't bring back (`.env` files, keys, Terraform state). Repositories with no remote at all are listed first and separately. Then it checks how old your newest backup is.

Nothing is fetched, so it is fast and works offline, which also means it judges against the remote state git last saw. Dependency and cache folders (`node_modules`, `vendor`, `Library`, …) are skipped.

| Flag | Meaning |
| --- | --- |
| `--depth int` | How many folders deep below each root a repository can be (default 5) |
| `--root strings` | Where to look (default: your whole home folder). Repeatable |

**Exit code:** 2 if anything is at risk, or if there is no backup newer than 7 days. So it can gate a wipe script: `dothaven ready && …`.

```text
1 repository with no remote. These exist ONLY on this machine:
  ✗ ~/code/prototype                              12 commits, 1 file uncommitted

1 repository with work not pushed anywhere:
  ⚠ ~/code/api                                    2 files uncommitted, 5 commits unpushed, 1 stash

  ✓ Newest backup is 2 hours old (encrypted, /Volumes/MyDrive/backup-mymac-….tar.gz.age).

❌ Not safe to wipe yet: 2 repositories hold work that exists nowhere else.
```

---

## Save this machine

### backup

Save your config: a folder here, or one encrypted file to carry.

```text
dothaven backup [flags]
```

Copies every config dothaven tracks (plus anything you added with `include`), a list of your installed apps and packages, and your macOS settings.

- `dothaven backup`: a folder on this machine. Secrets are redacted and credential files (SSH keys, cloud logins) left out, and listed.
- `dothaven backup --encrypt`: one age-encrypted file with everything, keys and tokens included. This is the one for a new machine. Nothing is written in plaintext, not even temporarily.

On a terminal with no `--only`/`--skip`, it shows a category picker and then offers config that nothing covers yet. See [Backup & restore](../backup-restore#backup).

| Flag | Meaning |
| --- | --- |
| `--archive` | One `.tar.gz` file instead of a folder (still redacted, not encrypted) |
| `--encrypt` | One age-encrypted file with everything, credentials included (asks for a passphrase) |
| `--no-redact` | Keep raw secret values in a plaintext backup (prefer `--encrypt`) |
| `-o`, `--output string` | Where to write it, e.g. a USB drive (default: `~/.local/share/dothaven`) |
| `--only strings` | Only these categories (comma-separated; see the [category list](../backup-restore#categories)) |
| `--skip strings` | Skip these categories, e.g. `--skip inventory,macos` |

The passphrase must be at least 10 characters. `DOTHAVEN_PASSPHRASE` supplies it in scripts. An unknown category name is an error that lists the valid ones.

```bash
dothaven backup --encrypt -o /Volumes/MyDrive
dothaven backup --only ai,shell,git
dothaven backup --archive --skip inventory,macos
```

### include

Add your own files and folders to every backup.

```text
dothaven include [path...] [flags]
```

dothaven knows a few hundred config locations. Everything else you care about (a tool nobody else uses, a scripts folder, an app's config dir) goes here. Paths must be inside your home folder; restore puts them back in the same place. The list lives in `~/.config/dothaven/include` and travels with your backups. With no arguments it behaves like `--list`.

| Flag | Meaning |
| --- | --- |
| `--list` | Show what you added and what nothing covers yet |
| `--remove` | Remove the given paths instead of adding them |
| `--review` | Pick from the untracked files and folders interactively |

```bash
dothaven include ~/.config/raycast ~/bin
dothaven include --remove ~/bin
```

### collect

Inventory this machine into a timestamped JSON snapshot.

```text
dothaven collect [flags]
```

Runs every collector (installed apps, Homebrew, global packages, runtimes, version managers, editor extensions, fonts, mobile toolchains, scheduled jobs, and the contents of tracked config files) and writes one JSON file named `<host>-<UTC timestamp>.json`. Secrets are redacted by default, with a summary printed afterwards. Credential files (🔑 in the [registry](../registry)) are recorded as present, with their size, and their contents are never copied into a snapshot, not even with `--no-redact`. The file is owner-only. See [Collectors](../collectors) and [Snapshot format](../snapshot-format).

A backup already includes the installed-software part of this, so you only need `collect` to compare machines or to inspect the inventory.

| Flag | Meaning |
| --- | --- |
| `--no-redact` | Keep raw values (skip secret redaction) |
| `-o`, `--output string` | Output directory (default: `~/.local/share/dothaven/snapshots`) |
| `--slim` | Truncate long file contents to 10 lines |

```text
Snapshot saved to: /Users/you/.local/share/dothaven/snapshots/mymac-20260927233425.json
  24 sections. Browse with `dothaven list <section>`, e.g. `dothaven list brew`.
```

### defaults

Capture and restore macOS preferences. macOS only; it does nothing useful without the `defaults` tool.

```text
dothaven defaults [command]
```

A backup already captures your settings. These commands are for doing it on its own, or for putting settings back from a backup later.

#### defaults export

Export curated macOS defaults to plist files.

```text
dothaven defaults export [flags]
```

Writes into `<output>/macos-defaults/`:

- whole preference domains, as plists, for a curated list of apps (iTerm2, Terminal, Rectangle, Rectangle Pro, Hammerspoon, AltTab);
- `prefs.json`: every preference domain on the machine read key by key and sorted into settings worth carrying (natural scrolling, key repeat, hot corners, Finder options), settings that point at this machine's files (listed for you to set by hand), and application state (ignored). A few settings that are lists or tables are kept whole: keyboard shortcuts, keyboard layouts (input sources), language order, and App Shortcuts you defined for any app. It also records the apps pinned in your Dock. Secret-looking values are redacted or dropped.

| Flag | Meaning |
| --- | --- |
| `-o`, `--output string` | Output directory (default: `~/.local/share/dothaven`) |

#### defaults import

Put saved macOS settings back (from a backup or an export).

```text
dothaven defaults import [backup-or-dir] [flags]
```

Takes a backup (folder, `.tar.gz`, encrypted `.age`, or `github`) or a `defaults export` folder. With no argument it uses dothaven's own export folder (`~/.local/share/dothaven/macos-defaults`).

- It compares each setting with this Mac and shows the ones already set as done, instead of offering them again.
- By default it writes only the core system domains: global settings, Finder, Dock, Spaces, Stage Manager, Control Center, the menu-bar clock, screenshots, keyboard shortcuts and input sources, accessibility, trackpad and mouse, Software Update and the crash reporter, plus App Shortcuts wherever they live. Everything else is mostly application state, held back unless you pass `--all`.
- On a terminal you pick which domains to apply.
- It rebuilds the Dock from the old machine's apps that are installed here, and names the ones that are not ("reinstall first, then import again"). An unchanged Dock is left alone.
- It restarts the Dock, Finder or SystemUIServer when their settings changed, and reloads keyboard shortcuts, so most changes show at once. Some still need you to log out and back in.
- A list or table setting (shortcuts, layouts, languages) is read back after writing; if macOS stored it as plain text instead, the previous value is put back and it is counted as not written.
- App plists from `defaults export` replace those apps' preference domains wholesale.

| Flag | Meaning |
| --- | --- |
| `--all` | Also write settings outside the core system domains (mostly application state) |
| `--dry-run` | List the domains that would be replaced, write nothing |
| `--yes` | Skip the confirmation (required off a terminal) |

### services

Capture and restore Homebrew-managed local service config.

```text
dothaven services [command]
```

Exports user-editable service config under `$(brew --prefix)/etc` (nginx, httpd, MySQL, Redis, dnsmasq) and re-imports it on a new machine, re-pointing the Homebrew prefix so Intel, Apple silicon and Linuxbrew paths resolve. The service programs come back through the Brewfile; databases and other data are out of scope.

#### services export

```text
dothaven services export [flags]
```

Writes `<output>/services/`, owner-only. A file that looks like it holds a secret (a password in `my.cnf`) is kept as it is, since it must round-trip, and you are warned to keep the export safe.

| Flag | Meaning |
| --- | --- |
| `-o`, `--output string` | Output directory (default: `~/.local/share/dothaven`) |

#### services import

```text
dothaven services import <dir> [flags]
```

Writes into this machine's `$(brew --prefix)/etc`, outside your home folder. It lists every file first and marks the ones it would overwrite.

| Flag | Meaning |
| --- | --- |
| `--dry-run` | List the files that would be written, write nothing |
| `--yes` | Skip the confirmation (required off a terminal) |

---

## Set up a machine

### restore

Put a backup's files back into your home folder.

```text
dothaven restore [backup] [flags]
```

Accepts a backup folder, a `.tar.gz`, an encrypted `.tar.gz.age` (asks for the passphrase; no other tools needed), or `github` for your [GitHub repo](../github#restoring-from-github). With no path on a terminal, it lists the backups it can find: dothaven's folder, the current folder, Downloads, Desktop, Documents and mounted drives.

New files are written. A file that already exists and differs is a conflict: on a terminal you choose per file, with a diff; otherwise it is kept, unless `--force`. Anything overwritten is saved to a pre-restore snapshot first. Restore remembers what it applied and what you skipped, so running it again shows what is done. Afterwards it offers your macOS settings and reinstalling apps. See [Backup & restore](../backup-restore#restore).

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Show what would change without writing |
| `--force` | Overwrite differing files (a pre-restore snapshot is saved first) |
| `--keep-paths` | Don't rewrite the old machine's home folder path to this one's ([why](../backup-restore#a-different-home-folder)) |
| `--only strings` | Only these categories (comma-separated) |
| `--skip strings` | Skip these categories (comma-separated) |
| `--yes` | Don't ask before writing |

```bash
dothaven restore
dothaven restore --dry-run /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
dothaven restore github --only ai,shell
```

### reinstall

Install the apps & packages a backup recorded (only what's missing).

```text
dothaven reinstall [backup] [flags]
```

Every backup records what was installed: Homebrew formulae, casks and App Store apps, global npm/pnpm/bun/pipx/uv/cargo packages, editor extensions, Linux packages. This compares that list with this machine, shows what is already installed, and installs the rest: all of it, or the groups and packages you pick. It runs on the terminal, with no time limit, so Homebrew and `sudo` can ask for your password. Each step skips itself if its tool is missing, so it is safe to run again.

The argument can be a backup (any kind, or `github`) or a `collect` snapshot. With none on a terminal, it lets you pick a backup. On a Mac without Homebrew it warns you, skips the Homebrew part, and prints the Homebrew install command.

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Show what would be installed and the script, run nothing |
| `--yes` | Install everything missing without asking (required off a terminal) |

---

## Look, without changing anything

### status

Latest backup vs this machine: a one-screen summary.

```text
dothaven status
```

Compares the newest backup folder in `~/.local/share/dothaven` with this machine: files tracked, modified, unchanged, not on this machine, redacted, and the names of modified files. If there is no backup folder but there is a one-file backup (`.tar.gz` or encrypted), it names the newest and suggests `dothaven diff <file>`. No flags.

### diff

Backup vs this machine, file by file.

```text
dothaven diff [backup-path] [flags]
```

Lists every file grouped by category as `modified`, `unchanged`, `new in backup (missing on machine)` or `redacted`, with a summary. With no argument it uses the newest backup folder; the argument can be any kind of backup, including an encrypted one or `github`.

| Flag | Meaning |
| --- | --- |
| `--section string` | Only show this category |

### missing

What the old machine had installed that this one doesn't.

```text
dothaven missing [backup-or-snapshot]
```

Compares the app & package list inside a backup (or a `collect` snapshot) with this machine and lists what is missing, with the command that installs each group. It matches names and ignores versions. Run it on the new machine after restoring; `dothaven reinstall <backup>` installs them for you. With no argument it uses your newest `collect` snapshot. No flags.

**Exit code:** 1 if anything is missing.

This used to be `dothaven doctor <backup>`. That spelling still works and says where it moved.

### check

Are my config files still valid? Parses each one.

```text
dothaven check [flags]
```

Checks the config files dothaven tracks with the parser that owns each format: Go's JSON parser for JSON, `zsh -n` and `bash -n` for shell files, `git config` and `ssh -G` for their own configs. Formats with no parser to hand, and JSON with comments (which editors allow), are reported as unchecked rather than assumed fine. A missing `zsh` or `ssh` makes those files unchecked, not broken.

| Flag | Meaning |
| --- | --- |
| `--all` | List files that passed and files nothing could check |

**Exit code:** 2 if anything is broken.

### doctor

Check dothaven itself: tools, folders, permissions, and what it can reach.

```text
dothaven doctor
```

Checks that dothaven can do its job on this machine, and says what to fix when it cannot: the platform, its folders and their permissions, free disk space, its own settings, the keychain it keeps tokens in, the tools each feature relies on, the files it backs up (unreadable, too large), your backups, and GitHub if you are signed in. Nothing is changed. No flags. See [Doctor & troubleshooting](../troubleshooting).

**Exit code:** 1 if anything is broken (warnings alone exit 0).

### compare

Snapshot vs snapshot: what changed between two.

```text
dothaven compare [file1] [file2]
```

Compares two `collect` snapshots and prints only the differences. Every line says which snapshot it is only in, and `~` marks a value that changed (`old → new`). No flags.

- **With no arguments** it compares your two newest snapshots as a timeline: `+` is what the newer one added, `-` what it no longer has.
- **With two files** it compares them side by side: `+` lines are only in the first file, `-` lines only in the second. Put this machine's snapshot first to see what the other one lacks.

```text
[packages.npm.global]
  + typescript  (only in mymac-20260927233425)
  - eslint  (only in mymac-20260101120000)
```

### list

Print sections of the latest snapshot (or of a backup's inventory).

```text
dothaven list [section] [snapshot-or-backup]
```

With no section, lists the section names. A section is fuzzy-matched: `list brew` shows formulae, casks and the Brewfile. Reads the newest snapshot from `dothaven collect`, or the inventory inside a backup you name (any kind, or `github`). No flags.

```bash
dothaven list
dothaven list brew
dothaven list npm /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
```

---

## Secrets

### scan

Find secrets in your config, or in any file or folder (exits 2 if any are HIGH).

```text
dothaven scan [path] [flags]
```

With no path, scans every config file dothaven tracks: the ones a backup would carry. With a path, scans that file or folder (skipping `.git`, `node_modules`, caches and files over 1 MiB). Each finding shows the file, line, severity and kind, and a masked preview (the setting's name, a token's prefix, its last two characters), never the value itself.

| Flag | Meaning |
| --- | --- |
| `--no-fail` | Always exit 0, even with HIGH findings |

**Exit code:** 2 when anything HIGH turns up, so this can gate a commit hook or a CI job. Without it, a finding would only be noticed by someone reading the output.

```text
~/.aws/credentials
  L2 [HIGH] AWS access key: AKIA••••LE
```

### security

Write a Markdown security report (default `SECURITY.md`).

```text
dothaven security [path] [flags]
```

Scans like `scan` (with no path: your tracked config), but writes a Markdown report grouped by severity, owner-only, and prints how many files were scanned and how many had findings. The report names files, rules and line numbers, never the values.

| Flag | Meaning |
| --- | --- |
| `-o`, `--output string` | Report output path (default `SECURITY.md`) |

---

## Keep it in a private GitHub repo

### github

Keep your backup in a private GitHub repository.

```text
dothaven github [command]
```

Pushes this machine's backup to a private repository on your GitHub account (created for you as `dothaven-backup`), and restores from it on another machine. No git is needed on either side. With no subcommand it shows the status. dothaven refuses to write to a public repository. See [GitHub sync](../github).

#### github login

Sign in to GitHub (browser, a token on stdin, or your gh login).

```text
dothaven github login [flags]
```

Opens github.com in your browser with a one-time code. Approve it there and the terminal carries on by itself. The token is kept in your system keychain. Most locked down: a fine-grained token limited to one repository (Contents: read and write; Administration: read and write to create it).

| Flag | Meaning |
| --- | --- |
| `--with-token` | Read a token from stdin instead of opening the browser |
| `--gh` | Use the GitHub CLI's login instead, and store no token of dothaven's own |

```bash
dothaven github login --with-token < token.txt
```

#### github logout

Forget the stored GitHub token and remembered passphrase. No flags.

#### github status

Who you're signed in as, which repo, and which machines are in it. No flags.

#### github push

Back this machine up to your private GitHub repo. Aliases: `sync`, `save`.

```text
dothaven github push [flags]
```

Builds a backup of this machine (the same one `dothaven backup` makes) and commits it to `machines/<this machine>/` in your private repository, replacing the previous one there. Other machines in the repo are left alone, and git history keeps every earlier push. Nothing changes if nothing changed.

| Flag | Meaning |
| --- | --- |
| `--machine string` | Folder name for this machine in the repo (default: hostname) |
| `--mode string` | `encrypted` (default), `split`, or `plain`; see [storage modes](../github#storage-modes) |
| `--only strings` | Only these categories |
| `--repo string` | `owner/name` (default: `<you>/dothaven-backup`) |
| `--skip strings` | Skip these categories |
| `--yes` | Create the repository without asking (required off a terminal) |

#### github pull

Restore from your GitHub repo (same as `dothaven restore github`). Alias: `restore`.

```text
dothaven github pull [flags]
```

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Show what would change without writing |
| `--force` | Overwrite differing files (a pre-restore snapshot is saved first) |
| `--machine string` | Which machine's backup (default: ask, or the only one) |
| `--repo string` | `owner/name` (default: the one you pushed to, or `<you>/dothaven-backup`) |
| `--only strings` | Only these categories (comma-separated) |
| `--skip strings` | Skip these categories (comma-separated) |
| `--keep-paths` | Don't rewrite the old machine's home folder path to this one's |
| `--yes` | Don't ask before writing |

---

## Sync through a chezmoi repo (optional)

These commands hand your config to [chezmoi](https://www.chezmoi.io/) instead of a dothaven backup. They need chezmoi, and an age key set up for chezmoi. See [Encryption & chezmoi](../encryption#the-chezmoi-path).

### init

Check the chezmoi + age prerequisites for export.

```text
dothaven init
```

Checks three things and prints each as done (`✓`) or with the command that fixes it (`→`): chezmoi is installed, age encryption is configured in `~/.config/chezmoi/chezmoi.toml`, and your chezmoi source is an initialized git repository. On a terminal it offers to run the safe steps for you (installing chezmoi with Homebrew, `chezmoi init <url>`). It never creates your age key: that is yours to make and back up. No flags.

```text
dothaven init: chezmoi + age bootstrap

  ✓ chezmoi installed
  → age encryption key configured
      age-keygen -o ~/.config/chezmoi/key.txt
      ⚠ Back this key up offline (password manager). Lose it and encrypted files are unrecoverable.
  → chezmoi source (private dotfiles repo) initialized
      chezmoi init git@github.com:you/dotfiles.git
```

### chezmoi-export

Plan (or apply) adding configs to chezmoi, encrypting secrets.

```text
dothaven chezmoi-export [flags]
```

Builds a plan of `chezmoi add` calls, one file at a time: plain for ordinary config, `--encrypt` for credentials and any file with a secret in it, `--template` for config that names your home folder. It also builds a `run_onchange` install script that reinstalls your packages when chezmoi applies. **Dry run by default**; `--apply` executes it (needs chezmoi, and age configured when anything is encrypted). On a terminal with no `--only`/`--skip`, it asks which categories and install groups (`brew`, `packages`) to export. Details: [Encryption & chezmoi](../encryption#chezmoi-export).

| Flag | Meaning |
| --- | --- |
| `--apply` | Execute the plan (default: dry-run) |
| `--only strings` | Only these categories/groups (comma-separated) |
| `--pin` | Pin global packages to their captured version |
| `--skip strings` | Skip these categories/groups (comma-separated) |

### migrate

Set up this machine from your chezmoi source (prereqs → apply → verify).

```text
dothaven migrate [flags]
```

On a clean machine: checks that chezmoi is installed and your source repo is initialized (and warns if age is not configured, since encrypted files will not decrypt), then runs `chezmoi apply`, which writes your configs and runs your install script. It runs on your terminal with no time limit, so a long `brew bundle` can finish and ask for passwords. It ends by pointing at `chezmoi diff` and `dothaven check`.

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Show what chezmoi would change, write nothing |
| `--yes` | Skip the confirmation (required off a terminal) |

---

## Additional commands

### upgrade

Update dothaven to the latest release. Alias: `update`.

```text
dothaven upgrade [flags]
```

Checks GitHub for the newest release, works out how this copy of dothaven was installed, and runs that installer's upgrade for you.

| Installed with | What `upgrade` runs |
| --- | --- |
| Homebrew | `brew update && brew upgrade --cask dothaven` |
| `go install` | `go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest` |
| Anything else | Nothing; it prints the release page to replace the binary from |

dothaven never overwrites its own binary. Homebrew tracks the version it installed, so replacing that file behind its back leaves `brew outdated` describing something that no longer exists, and the next `brew upgrade` would undo it anyway.

| Flag | Meaning |
| --- | --- |
| `--check` | Report what is available, change nothing |
| `--yes` | Skip the confirmation prompt |

### help and completion

```bash
dothaven help [command]
dothaven completion bash|zsh|fish|powershell
```

### The update notice

At most once a day, dothaven checks whether a newer release exists and prints one line on **stderr** when there is one:

```text
⇡ dothaven 0.5.0 is available (you have 0.4.0). Run `dothaven upgrade`
```

Nothing is added to stdout. The check is skipped when stderr is not a terminal, when `CI` is set, for development builds, during `upgrade` itself, and when `DOTHAVEN_NO_UPDATE_CHECK` is set to anything.

It requests one URL, `https://github.com/doguyilmaz/dothaven/releases/latest`, and reads the version from the redirect. It downloads no page body, sends nothing about your machine beyond a `dothaven/<version>` user agent, and caches the answer in `~/.cache/dothaven/update-check.json`. A failed check is silent.

---

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success |
| `1` | An error. Also: `missing` found something missing, `doctor` found something broken, or a command refused to change things off a terminal without `--yes` |
| `2` | `scan` found a HIGH secret, `check` found a broken config, or `ready` found work at risk (or no recent backup) |
| `130` | Cancelled with Ctrl-C (`143` for SIGTERM). A second Ctrl-C forces an immediate exit |

## Environment variables

| Variable | Meaning |
| --- | --- |
| `DOTHAVEN_PASSPHRASE` | Passphrase for encrypted backups, instead of the prompt (at least 10 characters for a new backup) |
| `DOTHAVEN_GITHUB_TOKEN` | GitHub token to use instead of the stored one or `gh` |
| `DOTHAVEN_SECRET_STORE=file` | Keep secrets in an owner-only file instead of the keychain |
| `DOTHAVEN_GITHUB_API`, `DOTHAVEN_GITHUB_WEB` | Another GitHub endpoint, such as GitHub Enterprise (`https` only) |
| `DOTHAVEN_GITHUB_CLIENT_ID` | OAuth app for browser sign-in |
| `DOTHAVEN_GITHUB_APP` | GitHub App whose bot authors each push |
| `DOTHAVEN_NO_UPDATE_CHECK` | Turn off the daily update notice |
| `XDG_DATA_HOME`, `XDG_CONFIG_HOME`, `XDG_CACHE_HOME` | Move dothaven's data (`~/.local/share`), settings (`~/.config`) and cache (`~/.cache`) folders |

{{< cards >}}
  {{< card link="../interactive" title="Interactive mode" >}}
  {{< card link="../backup-restore" title="Backup & restore" >}}
  {{< card link="../troubleshooting" title="Troubleshooting" >}}
{{< /cards >}}
