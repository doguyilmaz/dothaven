---
title: "Backup & restore"
weight: 5
---

This page covers the two kinds of backup, what goes into them, and how restore decides what to write. For the step-by-step move, see [Moving to a new machine](../migration).

## Two kinds of backup

| | `dothaven backup` | `dothaven backup --encrypt` |
| --- | --- | --- |
| Form | A folder (or one `.tar.gz` with `--archive`) | One `.tar.gz.age` file |
| Secret values | Masked: the key name stays, the value becomes `[REDACTED]` | Kept |
| SSH keys, cloud logins, other credential files | Left out, and listed | Included |
| Installed apps list, macOS settings | Included | Included |
| Restores on a new machine | Everything except redacted and left-out files | Everything |
| Use it for | A local safety copy, `status` and `diff` | Moving to a new machine |

Both come from the same walk over the same files, with the same `MANIFEST.txt`. The only differences are the redaction gate and the encryption.

## backup

```bash
dothaven backup                                  # a folder in ~/.local/share/dothaven
dothaven backup --archive                        # one .tar.gz, still redacted
dothaven backup --encrypt -o /Volumes/MyDrive    # one encrypted file, everything in it
```

A backup holds:

- every config file dothaven tracks that exists on this machine (see [Registry](../registry)), plus anything you added with `dothaven include`;
- the list of installed apps and packages, a script that reinstalls them, and your crontab (`inventory`);
- on macOS, your system settings and the apps in your Dock (`macos`).

On a terminal, with no `--only` or `--skip`, it first shows a category picker (everything selected; categories holding credentials are marked), then offers once any config-looking paths that nothing covers yet. What you pick joins your include list; what you leave is remembered and not asked about again.

### Flags

| Flag | Meaning |
| --- | --- |
| `--encrypt` | One age-encrypted file with everything, credentials included (asks for a passphrase) |
| `--archive` | One `.tar.gz` file instead of a folder (still redacted, not encrypted) |
| `--no-redact` | Keep raw secret values in a plaintext backup (prefer `--encrypt`) |
| `-o`, `--output` | Where to write it, e.g. a USB drive (default: `~/.local/share/dothaven`) |
| `--only` | Only these categories (comma-separated) |
| `--skip` | Skip these categories, e.g. `--skip inventory,macos` |

The name is `backup-<host>-<UTC timestamp>`, with `.tar.gz` or `.tar.gz.age` for the one-file kinds. The output folder is created owner-only (`0700`), and every file in a backup is owner-only (`0600`, or `0700` if it was executable).

### Categories

`--only` and `--skip` take category names. `--skip` wins over `--only`. A name that is not a category is an error that lists the real ones, rather than a backup that quietly holds nothing:

```text
error: unknown category "shel" — choose from: ai, apps, build, bun, cloud, db, dev, devops, dothaven, editor, extra, git, inventory, lang, macos, mobile, net, npm, schedule, secrets, shell, ssh, terminal, vm
```

| Category | What is in it |
| --- | --- |
| `ai` | Claude, Codex, Cursor, Gemini… skills, agents, MCP, plugins |
| `shell` | zsh, bash, fish and their frameworks |
| `git` | git config, global hooks and ignores, gh/glab |
| `editor` | VS Code, Cursor, Zed, Neovim, Vim, Helix… |
| `terminal` | tmux, Ghostty, kitty, WezTerm, Starship… |
| `ssh` | ssh config, known_hosts and keys |
| `cloud` | AWS, GCP, Azure, kubectl, Docker, Vercel… |
| `devops` | helm, k9s, ansible, terraform… |
| `lang` | Ruby, Python, Rust, Go, PHP, .NET, JS tool config |
| `npm`, `bun` | npm and bun config |
| `db` | database client config and saved passwords |
| `secrets` | `.netrc`, Vault token, GnuPG keys |
| `vm` | version pins (`.tool-versions`, mise, `.nvmrc`) |
| `net` | curl and wget defaults |
| `dev` | direnv |
| `apps` | Karabiner, Hammerspoon, window managers… |
| `schedule` | launchd agents (macOS) |
| `build` | Maven and Gradle settings |
| `mobile` | Xcode user data, Android keystores |
| `dothaven` | dothaven's own settings (your include list) |
| `extra` | paths you added with `dothaven include` |
| `inventory` | list of installed apps & packages, to reinstall |
| `macos` | system settings: trackpad, keyboard, Dock, Finder… (macOS) |

`inventory` and `macos` are not files; they are selected with the same flags. `schedule` exists only on macOS, and macOS settings are only captured on a Mac.

### What a backup looks like inside

```text
backup-mymac-20260927232938/
├── MANIFEST.txt                   what is in it, and everything left out
├── ai/claude/settings.json
├── ai/claude/skills/…
├── git/.gitconfig
├── git/hooks/pre-commit           executable bit kept
├── shell/.zshrc
├── extra/.config/raycast/…        your includes, by path under $HOME
├── inventory/
│   ├── snapshot.json              installed apps, packages, runtimes, fonts
│   ├── Brewfile
│   ├── install-packages.sh
│   └── crontab
└── macos-defaults/prefs.json      system settings and Dock apps
```

### What is left out, and how you are told

A backup is judged by what it is missing, so dothaven lists everything that exists and did not go in, on screen and in `MANIFEST.txt`:

| Left out | When | Why |
| --- | --- | --- |
| Credential files (`~/.ssh`, `~/.aws/credentials`, kubeconfig, GnuPG, …) | Plain backups (unless `--no-redact`) | Masking cannot be trusted to catch every secret in them. They go in the encrypted backup. |
| Any file with a private key in it | Plain backups (unless `--no-redact`) | A private key cannot be partly masked into safety. |
| Files over 64 MiB | Every backup | Config files are kilobytes; this keeps a stray disk image out. |
| Text files over 8 MiB | Plain backups (unless `--no-redact`) | Too large to check for secrets. The encrypted backup carries them. |
| Files that exist but cannot be read | Every backup | Listed on stderr so you can fix permissions. |

A credential folder stays protected even when you include it yourself: `dothaven include ~/.aws` does not put `~/.aws/credentials` into a plaintext backup.

Some things are skipped without comment because they are not config: sockets, pipes and devices (such as `gpg-agent` sockets), `.DS_Store`, and per-tool clutter such as caches, logs, plugin downloads and shell history (each registry entry lists its own).

Symbolic links are followed, so dotfiles managed with GNU Stow or a bare repo are backed up by their content. A link loop cannot trap the walk.

```text
# Left out of this plaintext backup (credentials, high-sensitivity).
# Carry them encrypted: dothaven backup --encrypt  (or chezmoi-export --apply)
#   cloud/aws/credentials
#   ssh
```

### Redaction in plaintext backups

In a plain backup every file is scanned before it is written. Secret values are replaced with `[REDACTED]` and the rest of the file is kept, so you can still read it. A few formats keep their structure: `HostName` and `IdentityFile` in `~/.ssh/config`, and `_authToken` in `.npmrc`.

A redacted file is kept for reference, but restore never writes it over a real file: that would replace your working token with the word `[REDACTED]`. See [Security](../security) for what the scanner detects.

### `--no-redact`

Writes raw values into a plaintext backup, credential files and private keys included. If it wrote any private key, it says so in red:

```text
🔴 1 file holding private keys were written UNENCRYPTED:
    ssh/id_ed25519
  Treat this backup as secret, or use dothaven backup --encrypt instead.
```

There is rarely a reason to use it. `--encrypt` gives you the same completeness without the risk.

### Encrypted backups

`--encrypt` implies a single file. It asks for a passphrase twice; it must be at least 10 characters, because the file can hold your SSH keys and cloud logins. In scripts, set `DOTHAVEN_PASSPHRASE` instead.

- The files stream through tar, gzip and age encryption straight into the output file. Nothing is written in plaintext, not even temporarily.
- The file is written as `<name>.partial`, owner-only from the first byte, and renamed only when complete. An interrupted backup never looks like a finished one.
- It is a standard [age](https://age-encryption.org) file (passphrase mode), so `age -d` opens it too. dothaven uses the age library, so neither machine needs the `age` program.
- There is no recovery. Without the passphrase, nobody can open the file, including you.

More in [Encryption](../encryption).

## include

The registry cannot know every tool. `include` adds your own paths to every backup:

```bash
dothaven include ~/.config/raycast ~/bin   # add
dothaven include --remove ~/bin            # stop carrying one
dothaven include --list                    # what you added, and what nothing covers
dothaven include --review                  # pick interactively from what nothing covers
```

- Paths must be inside your home folder. `~/…`, `$HOME/…`, absolute paths and paths relative to where you are all work on the command line.
- A path that does not exist yet is added anyway, with a warning, and picked up once it does.
- The list is a text file at `~/.config/dothaven/include`, one path per line, `#` for comments. A line starting with `!` is a path you reviewed and declined, so `backup` does not ask about it again. You can edit the file by hand.
- Included paths go into the backup under `extra/` and restore puts them back in the same place under your home folder, **even on a new machine with no include list yet**. The include list itself is backed up too (category `dothaven`).
- They are scanned and redacted like everything else in a plaintext backup, and restored owner-only.

## restore

```bash
dothaven restore                                   # pick from the backups it finds
dothaven restore ~/.local/share/dothaven/backup-mymac-20260927232931
dothaven restore /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
dothaven restore github                            # from your private GitHub repo
```

It accepts a backup folder, a `.tar.gz`, an encrypted `.tar.gz.age`, or `github` (see [GitHub sync](../github#restoring-from-github)). The kind is recognised by content, so a file renamed on the way (`backup (1).age`) still opens. An encrypted one asks for its passphrase (three tries on a terminal) and needs no other tools.

With no path on a terminal, it lists the backups it can find in `~/.local/share/dothaven`, the current folder, Downloads, Desktop, Documents, and the top of each mounted drive (and one folder down), newest first, and lets you type a path instead. Off a terminal you must pass a path.

### How each file is classified

Restore compares each file in the backup with the one on this machine, and with what it remembers from earlier restores:

| Mark | Status | Meaning | What restore does |
| --- | --- | --- | --- |
| `+` | new | Not on this machine | Writes it |
| `↑` | updated | Newer in the backup; yours is exactly what an earlier restore wrote | Writes it (the old copy is saved) |
| `≠` | differs | Exists here and is different | Asks you, with a diff. Off a terminal: keeps yours, unless `--force` |
| `✎` | changed by you | Restore wrote it earlier, and you edited it since | Same as differs |
| `○` | skipped last time | You declined this exact copy before (answered *Skip*, or unpicked it from the file list) | Not offered again, unless you pick it by name or use `--force` |
| `✓` | applied | Identical | Nothing |
| `⊘` | redacted | The backup's copy had secrets masked | Never written, even with `--force` |

### Choosing what to restore

On a terminal, restore shows the counts and asks:

```text
What should be restored?
  Already-applied files are not listed again.
> Everything new or updated (12), ask about 2 that differ
  Choose categories
  Choose files
  Show me the list first
  Cancel
```

- **Everything new or updated** writes the new and updated files and asks about each one that differs.
- **Choose categories** picks, for example, only `ai`, `shell` and `git`. The categories you leave out stay on offer, so a restore can be done in phases: `shell` today, `ai` tomorrow.
- **Choose files** lists every file (type `/` to filter). Picking a file that differs is your approval to replace it; a file you unpick from that list is remembered as declined.
- **Show me the list first** prints the plan, then asks again.

For each file that differs you choose: *Overwrite with backup*, *Skip (keep live file)*, *Show diff*, *Overwrite all remaining*, or *Skip all remaining*.

### It remembers

Restore keeps a ledger at `~/.local/share/dothaven/state/applied.json` of what it wrote and what you declined. Running it again shows what is done, instead of offering every file as new work:

```text
  9 files: 1 skipped last time, 5 already applied, 3 redacted
✓ Nothing new to restore — everything here is applied or was left out on purpose.
```

The ledger holds file hashes, never file contents. Only a decision is remembered: a file you were shown and said no to. A category you did not pick, or a file kept off a terminal because nobody was there to ask, is still on offer next time. What you declined is remembered per backup: restoring a different (for example, newer) backup asks about those files again.

### A different home folder

A new Mac often comes with a different account name, and a move to Linux turns `/Users/you` into `/home/you`. Every backup records the home folder it was made under. When this machine's differs, restore writes the new one wherever a text file mentions the old one: a `PATH` entry, an editor setting, a Claude hook command, `includeIf "gitdir:/Users/you/work/"`. Only a whole path component is changed (`/Users/dogu`, never `/Users/doguyilmaz`), and binary files are left as they are. The plan says how many files this touches; `--keep-paths` turns it off.

### Files with no place here

A backup can hold config whose tool lives somewhere else on this OS, or nowhere: Karabiner or iTerm2 restored on Linux, or a tool that a newer dothaven knew about. Restore lists those files beside the plan (in `--dry-run` too) rather than dropping them silently, and says how to copy them out by hand.

### Nothing is lost

Before any file is replaced, its current version is copied to `~/.local/share/dothaven/pre-restore-<timestamp>/`, owner-only:

```text
  • the versions it replaced are in ~/.local/share/dothaven/pre-restore-20260927233012
```

Other safeguards:

- Files that were credentials or medium-sensitivity config are written owner-only (`0600`); other config gets `0644`. Executable files stay executable, so git hooks keep working. A file that is stricter on this machine (say, a `.zshrc` you made `0600`) keeps its permissions.
- If the current version of a file cannot be read, it cannot be copied aside, so it is not replaced either; restore says which.
- If the file on this machine is a symbolic link, restore skips it and says so, rather than writing through the link into whatever it points at.
- A backup entry that tries to leave its folder (`../`, absolute paths) is refused. Archives are unpacked into a private temporary folder (`0700`) that is deleted afterwards, even on a forced exit (a second Ctrl-C). A folder left behind by a crash or `kill -9` is removed on the next run. Symbolic links and device files inside an archive are not extracted.
- Commands that only read a backup's inventory or settings (`missing`, `reinstall`, `defaults import`) unpack just that part. The rest of an encrypted archive, keys included, is decrypted in memory and read past, never written.

### Flags

| Flag | Meaning |
| --- | --- |
| `--dry-run` | Show what would change without writing |
| `--force` | Overwrite differing files (a pre-restore snapshot is saved first) |
| `--yes` | Don't ask before writing |
| `--only` | Only these categories (comma-separated). A misspelled one is an error |
| `--skip` | Skip these categories (comma-separated) |
| `--keep-paths` | Don't rewrite the old machine's home folder path to this one's |

Off a terminal, or with `--yes`, there are no questions: new and updated files are written, files that differ are kept (and still offered next time), unless you add `--force`.

### Afterwards

When the files are done, restore offers the rest of the backup. On a terminal it asks whether to put back your macOS settings and whether to reinstall your apps now. Otherwise it prints the commands:

```text
Also in this backup:
  dothaven defaults import ~/Downloads/backup-mymac-….tar.gz.age  # macOS settings
  dothaven reinstall ~/Downloads/backup-mymac-….tar.gz.age        # apps & packages you had
  dothaven missing ~/Downloads/backup-mymac-….tar.gz.age          # what is still missing here
```

## reinstall and missing

Every backup records what was installed. Two commands use that list on the new machine:

```bash
dothaven reinstall <backup>   # installs only what is missing
dothaven missing <backup>     # lists what is missing, changes nothing
```

`reinstall` covers Homebrew formulae, casks, taps and App Store apps (through the Brewfile), VS Code extensions (through the Brewfile, or from the extension list where there is no Homebrew), Node versions (fnm), global npm/pnpm/bun packages, pipx apps, uv tools, Rust toolchains and cargo crates, Composer, Dart and .NET tools, Cursor extensions, and apt, dnf, pacman, snap and flatpak packages. It shows what is already installed, asks what to install (everything missing, some groups, or single packages), and runs attached to your terminal with no time limit. `--dry-run` prints the script instead. Off a terminal it needs `--yes`.

`missing` matches by name, ignoring versions, and exits with code 1 while anything is missing. Both accept a `collect` snapshot (`.json`) as well as a backup. See [Commands](../commands#reinstall).

## status and diff

```bash
dothaven status                    # newest backup folder vs this machine
dothaven diff                      # same, file by file
dothaven diff <backup>             # any backup: folder, archive, encrypted, or github
dothaven diff --section shell      # one category
```

`status` compares the newest backup *folder* in `~/.local/share/dothaven` with this machine. If you have no backup folder, only one-file backups (for example encrypted ones, anywhere dothaven looks), it names the newest and suggests `dothaven diff <file>`, since comparing an encrypted one needs the passphrase.

```text
Last backup: 2h ago (backup-mymac-20260927232931)
  9 files tracked: 1 modified, 5 unchanged
  3 redacted

Modified since backup:
  ~ shell/.zshrc

These differ from the backup. Run dothaven backup to capture them.
```

`diff` groups files by category and labels each `modified`, `unchanged`, `new in backup (missing on machine)` or `redacted`.

{{< cards >}}
  {{< card link="../migration" title="Moving to a new machine" >}}
  {{< card link="../registry" title="Registry" subtitle="Every tracked path" >}}
  {{< card link="../security" title="Security" >}}
{{< /cards >}}
