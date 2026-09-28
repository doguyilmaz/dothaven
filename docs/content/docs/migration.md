---
title: Moving to a new machine
weight: 4
---

This is the end-to-end guide: everything you do on the old machine before you wipe it or hand it back, and everything you do on the new one to get your setup back.

There are three ways to carry your setup. Pick one:

| Way | Best when | What you need |
| --- | --- | --- |
| [**One encrypted file**](#way-1-one-encrypted-file) (recommended) | You are moving once, from one machine to another | A USB drive, cloud storage, or any way to copy one file |
| [**Private GitHub repo**](#way-2-a-private-github-repo) | You want every machine backed up somewhere you can reach from anywhere | A GitHub account |
| [**chezmoi repo**](#way-3-a-chezmoi-repo) | You already use, or want, [chezmoi](https://www.chezmoi.io/) to keep machines in sync | chezmoi, an age key, a private git repo |

The first two carry the same backup: your config, your credentials, the list of apps you had, and your macOS settings. The chezmoi way carries config files and an install script, but not macOS settings.

{{< callout type="info" >}}
Prefer menus to commands? Run `dothaven` with no arguments. The first group, **Moving to a new Mac** ("machine" on Linux), walks through the same five steps as Way 1. Its **Pack everything into one encrypted file** entry does all of the old-machine part in one go, and checks that the file opens before you wipe anything. See [Interactive mode](../interactive#pack-everything-into-one-encrypted-file).
{{< /callout >}}

## Way 1: one encrypted file

### On the old machine

{{% steps %}}

### Check that nothing exists only here

```bash
dothaven ready
```

Config can be rebuilt. Uncommitted changes, unpushed commits and stashes cannot, and neither can the `.env` file a fresh `git clone` will not bring back. `ready` looks through your home folder (up to 5 folders deep) for git repositories and reports:

- repositories with **no remote at all**: every commit in them exists only on this machine;
- repositories with **uncommitted files, commits on no remote, or stashes**;
- **gitignored files a clone won't bring back**: `.env` files, keys and keystores, cloud credentials, Terraform state;
- how old your **newest backup** is.

```text
1 repository with no remote — these exist ONLY on this machine:
  ✗ ~/code/prototype                              12 commits, 1 file uncommitted

1 repository with gitignored files a fresh clone won't bring back:
  ⚠ ~/code/api                                    .env

❌ Not safe to wipe yet: 2 repositories hold work that exists nowhere else.
```

Fix what it lists: add a remote and push, commit and push (a stash is not pushed by pushing a branch), and copy ignored files you need. For a gitignored file you want in your backup, `dothaven include ~/code/api/.env` carries it in the encrypted backup.

Nothing is fetched, so `ready` is fast and works offline, but it judges "pushed" against the remote state git last saw. It exits with code 2 while anything is at risk, or when you have no backup newer than 7 days.

### Catch config dothaven does not know about

```bash
dothaven include --list
```

This lists files and folders in your home folder that look like config but are in no backup. Add the ones you want:

```bash
dothaven include ~/.config/raycast ~/bin
```

You can skip this step: on a terminal, `backup` offers these paths once anyway.

### Make the encrypted backup

Write it straight to where it needs to end up, such as a USB drive:

```bash
dothaven backup --encrypt -o /Volumes/MyDrive
```

On a terminal it first shows the category picker with everything selected (press Enter to keep it that way) and offers any config nothing covers yet. Then it asks for a passphrase twice (at least 10 characters). You will need it on the new machine, and nothing can open the file without it, so put it in your password manager now.

The result is one file, `backup-<host>-<timestamp>.tar.gz.age`, holding:

- every config file dothaven tracks, plus your includes;
- your credentials: `~/.ssh` keys, cloud logins, tokens, GnuPG keys;
- the list of installed apps and packages, with a script to reinstall them;
- your macOS settings: trackpad, keyboard, Finder, hot corners, keyboard shortcuts and layouts, language order, and the apps in your Dock.

```text
✓ Encrypted backup saved — 194 files, 3.1 MB
  /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
  ai (58), cloud (6), editor (97), git (9), npm (1), shell (14), ssh (5), terminal (4)
  + installed apps & packages list, 214 macOS settings

Next:
  Copy this file off this machine — a USB drive, cloud storage, another computer.
  It lives on the disk you are about to replace.
  On the new machine: dothaven restore backup-mymac-20260927232938.tar.gz.age
  You will need the passphrase. Nothing can open this file without it.
```

Why this is safe to carry: the file is encrypted as it is written. Your keys and tokens never touch the disk in plaintext, not even as a temporary file. It is a standard age file, so `age -d` can also open it; dothaven itself needs nothing extra installed.

{{< callout type="info" >}}
In a script, set `DOTHAVEN_PASSPHRASE` instead of typing it. It must also be at least 10 characters. The prompt is the default because an environment variable is visible to every program the shell starts.
{{< /callout >}}

### Get the file off the machine, and check it opens

If you did not write it to a drive, copy it to one, or to cloud storage, or to another computer. Then prove it opens with your passphrase without changing anything:

```bash
dothaven restore --dry-run /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
```

It asks for the passphrase and lists what it would restore. (The menu's **Pack everything** flow does this check for you.)

{{% /steps %}}

### On the new machine

{{% steps %}}

### Install dothaven

On a fresh Mac, the installer script is quickest. It does not need Homebrew:

```bash
curl -fsSL https://raw.githubusercontent.com/doguyilmaz/dothaven/main/scripts/install.sh | sh
```

See [Installation](../installation) for other ways.

### Put your files back

Plug in the drive, then:

```bash
dothaven restore
```

With no path on a terminal, `restore` looks for backups in `~/.local/share/dothaven`, the current folder, Downloads, Desktop, Documents and mounted drives, and lets you pick one (or type a path). You can also name the file:

```bash
dothaven restore /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
```

It asks for the passphrase (three tries on a terminal), then asks what to restore:

```text
What should be restored?
  Already-applied files are not listed again.
> Everything new or updated (194)
  Choose categories            e.g. only ai, shell and git
  Choose files                 type / to filter
  Show me the list first
  Cancel
```

On a new machine almost everything is new, so the first choice is usually right. If a file already exists and differs (say, a default `.zshrc`), you are asked about it on its own, with a diff, and whatever you replace is saved to a `pre-restore-<timestamp>` folder first. Details are in [Backup & restore](../backup-restore#restore).

### Put your macOS settings back

When the restore finishes on a Mac, it offers:

```text
Also put back your macOS settings (trackpad, keyboard, Dock, Finder)?
```

Say yes, or do it later with `dothaven defaults import <file>`. Settings already set on this Mac are shown as done, not offered again. The Dock is rebuilt from the apps installed here, so it is worth doing again after the next step.

### Reinstall your apps and packages

The restore then offers to reinstall. You can also run it yourself:

```bash
dothaven reinstall /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
```

It compares the backup's list with this machine, shows what is already installed, and installs only what is missing: everything, some groups, or packages you pick. It runs attached to your terminal with no time limit, so Homebrew and `sudo` can ask for your password and a long `brew bundle` is not cut off.

{{< callout type="warning" >}}
On a fresh Mac, install Homebrew first. If `brew` is missing, `reinstall` warns you, skips the Homebrew part, and prints the official install command:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Then run `dothaven reinstall` again. Each step skips itself if its tool is missing, so running it twice is safe.
{{< /callout >}}

### Check what is still missing

```bash
dothaven missing /Volumes/MyDrive/backup-mymac-20260927232938.tar.gz.age
```

This lists what the old machine had installed that this one does not, grouped by package manager, with the command that installs each group:

```text
Missing on this machine:

  npm globals (2)
    typescript@5.6.2, @biomejs/biome@1.9.4
    fix: npm install -g typescript @biomejs/biome

2 items missing across 1 group.
Install them (all, or the ones you pick): dothaven reinstall /Volumes/MyDrive/backup-….tar.gz.age
```

It matches on names, not versions. It exits with code 1 while anything is missing, and prints `✓ Nothing missing` when you are done.

### Sign back in to things

Some things cannot be copied and need you: see [What does not travel](#what-does-not-travel) below.

{{% /steps %}}

## Way 2: a private GitHub repo

Instead of a file, keep the backup in a private repository on your GitHub account. No git is needed on either machine.

On the old machine:

```bash
dothaven ready                # same check as above
dothaven github login         # once per machine
dothaven github push          # creates <you>/dothaven-backup (private) the first time
```

`push` makes the same backup as `backup --encrypt` and commits it to `machines/<this machine>/` in the repo. It is encrypted by default. Other modes keep config readable on GitHub; see [GitHub sync](../github#storage-modes).

On the new machine:

```bash
dothaven github login
dothaven restore github
```

If the repo holds several machines, it asks which one (or pass `dothaven github pull --machine <name>`). From there it is the same as Way 1: pick what to restore, then settings, `reinstall` and `missing`. Those commands accept `github` in place of a file, too:

```bash
dothaven reinstall github
dothaven missing github
```

## Way 3: a chezmoi repo

If you want [chezmoi](https://www.chezmoi.io/) to own your dotfiles and keep machines in sync, dothaven can hand them over. This path needs chezmoi and an age key set up for chezmoi, and it does not carry macOS settings.

On the old machine:

```bash
dothaven init                      # checks chezmoi, the age key and the source repo; prints what to fix
dothaven chezmoi-export            # dry run: shows what would be added, and what encrypted
dothaven chezmoi-export --apply    # adds the files to your chezmoi source
chezmoi cd                         # then commit and push the source repo (keep it private)
```

`chezmoi-export` adds files one by one. Files with a secret in them are added encrypted; config that names your home folder is added as a template so it works under a different username. It also writes an install script that chezmoi runs on the new machine to reinstall your packages.

On the new machine, put your age key back first (for example at `~/.config/chezmoi/key.txt`), then:

```bash
chezmoi init <your-private-repo>
dothaven migrate                   # checks the prerequisites, then runs chezmoi apply
```

{{< callout type="error" >}}
With chezmoi, the age key is the only way to decrypt your secrets. If you lose it, every encrypted file in the repo is unrecoverable. Back it up offline before you wipe the old machine, and never commit it.
{{< /callout >}}

Details are on [Encryption & chezmoi](../encryption#the-chezmoi-path).

## What does not travel

dothaven leaves these out on purpose. Deal with them by hand:

- [ ] **Data.** Databases, Docker volumes, anything a local service stores. Dump and copy what you need. (`dothaven services export` carries the *config* of Homebrew services such as nginx and MySQL, not their data.)
- [ ] **System config** outside your home folder, such as `/etc/hosts`.
- [ ] **Keychain items**: code-signing certificates, provisioning profiles, saved passwords. Re-import or re-download them.
- [ ] **Sign-ins**: browser sync, the App Store, licensed apps, anything behind two-factor login. (CLI logins in files, such as `~/.aws` or `gh`, do travel in an encrypted backup.)
- [ ] **Large downloads**: Ollama models, simulator runtimes, Android emulators. The inventory records their names; pull them again.
- [ ] **Project-level config** (a repo's `.claude/`, `.mcp.json`, `.vscode/`). It travels with the project's own repository. Make sure it is pushed (`dothaven ready`).
- [ ] **Unsaved work**: save and quit your editors before the final backup. A backup carries files on disk, not open buffers.

## If something goes wrong

- **Wrong passphrase**: dothaven cannot recover it, and neither can anyone else. Try the one in your password manager.
- **A redacted file was not restored**: it came from a plain backup, which masks secret values. Restore from an encrypted backup instead.
- **Anything else**: run `dothaven doctor`, then see [Doctor & troubleshooting](../troubleshooting).

{{< cards >}}
  {{< card link="../backup-restore" title="Backup & restore" subtitle="Every option, and what each restore status means" >}}
  {{< card link="../github" title="GitHub sync" subtitle="Modes, sign-in, and the security model" >}}
  {{< card link="../security" title="Security" subtitle="What is redacted, left out, and encrypted" >}}
{{< /cards >}}
