---
title: "Doctor & troubleshooting"
linkTitle: Troubleshooting
weight: 13
---

## Start with doctor

```bash
dothaven doctor
```

`doctor` checks that dothaven can do its job on this machine and says what to fix when it cannot. It changes nothing, finishes in about a second, and exits with code 1 only if something is broken (warnings alone exit 0).

```text
dothaven doctor — can dothaven do its job here?

dothaven
  ✓ version        1.4.0 (installed with Homebrew)
  ✓ binary         /opt/homebrew/bin/dothaven

This machine
  ✓ platform       darwin arm64
  ✓ home folder    /Users/you
  · shell          /bin/zsh
  ✓ terminal       a terminal — menus and prompts available

Folders
  ⚠ data           /Users/you/.local/share/dothaven is readable by other users (0755) — it holds backups
      fix: chmod 700 /Users/you/.local/share/dothaven
  ✓ settings       /Users/you/.config/dothaven
  ✓ temporary      /var/folders/…/T/
  ✓ free space     212.4 GB available

Your settings
  ✓ include list   3 paths, 2 left out on purpose
  ✓ keychain       tokens and passphrases go to the macOS Keychain

What gets backed up
  ✓ tracked        1843 files from 61 sources on this machine (27 with credentials)
  · not covered    2 paths look like config but are in no backup
      dothaven include --list

Backups
  ✓ newest         encrypted, 2 days old — /Volumes/MyDrive/backup-mymac-20260925101200.tar.gz.age

Tools
  ✓ git            git version 2.46.0
  ✓ brew           Homebrew 4.4.0
  ✓ defaults       /usr/bin/defaults
  ✓ zsh            zsh 5.9 (arm64-apple-darwin24.0)
  ✓ ssh            OpenSSH_9.8p1, LibreSSL 3.3.6
  · gh             not found — needed for GitHub sign-in without a browser
  · chezmoi        not found — needed for the optional chezmoi sync
  ✓ age            built in — encrypted backups need no extra install

GitHub
  ✓ sign-in        you (via the macOS Keychain)
  ✓ repository     you/dothaven-backup (private)

⚠ Works, with 1 warning worth a look above.
```

| Mark | Meaning |
| --- | --- |
| `✓` | Fine |
| `·` | For your information; nothing to fix |
| `⚠` | Works, but worth a look; the `fix:` line says how |
| `✗` | Broken; dothaven cannot do part of its job until it is fixed |

What each section checks:

- **dothaven**: the version and how it was installed (this decides what `upgrade` runs).
- **This machine**: macOS or Linux (anything else is unsupported), your home folder, and whether this is a terminal. Off a terminal, commands that change files need `--yes`.
- **Folders**: dothaven's data folder (where backups and the restore ledger live) exists, is writable, and is private; the settings and temporary folders are writable; there is enough free space (a warning under 1 GB, a problem under 100 MB).
- **Your settings**: the include list parses (lines outside your home folder or duplicated are reported); the GitHub settings and restore ledger files are not damaged; where tokens are stored.
- **What gets backed up**: how many files a backup would carry, how many hold credentials, files that exist but cannot be read, files over the 64 MiB limit, and paths nothing covers.
- **Backups**: the newest backup it can find, anywhere it looks. A backup older than 7 days is a warning.
- **Tools**: the programs features rely on, and their versions. Missing `git`, `brew` or `defaults` is a warning (on macOS for the last two); the rest are optional.
- **GitHub**: if you are signed in, whether the token still works and whether your backup repository exists and is private. A public repository is a problem.

`doctor` used to compare a backup with this machine. That is now `dothaven missing <backup>`; `dothaven doctor <backup>` still works and forwards to it.

## Common problems

### "wrong passphrase"

```text
This backup is encrypted.
  ⚠ wrong passphrase, try again.
```

On a terminal you get three tries. The passphrase is the one you typed when you made the backup (or set in `DOTHAVEN_PASSPHRASE`); check your password manager. If `DOTHAVEN_PASSPHRASE` is set in your environment, it is used without asking, so an old value there will fail every time: `unset DOTHAVEN_PASSPHRASE`.

There is no recovery. The encryption is designed so that nobody can open the file without the passphrase.

### "DOTHAVEN_PASSPHRASE … shorter than 10 characters"

A new encrypted backup (or GitHub push) needs a passphrase of at least 10 characters, from the prompt or from the variable. A variable that is set but empty, such as a script's `$PASS` that expanded to nothing, is refused too: it is never taken to mean "don't encrypt". Choose a longer one. (Opening an existing backup accepts whatever passphrase it was made with.)

### "no terminal to ask for a passphrase on — set DOTHAVEN_PASSPHRASE"

You are running without a terminal (a script, CI, `ssh host cmd`). Set `DOTHAVEN_PASSPHRASE` for that command.

### "Refusing to continue without a terminal to confirm on."

A command that changes things you already have was run without a terminal to ask you. Re-run it with `--yes` if you meant it, or with `--dry-run` to see what it would change.

### "unknown category …"

```text
error: unknown category "shel" — choose from: ai, apps, build, …
```

The message lists every valid name. See [Categories](../backup-restore#categories).

### "nothing to back up — no tracked files found for this selection"

Your `--only`/`--skip` left nothing. Check the categories you named with `dothaven doctor` ("What gets backed up") or `dothaven include --list`.

### Some files were "redacted" and not restored

```text
⚠ 3 files had secrets redacted, so they were not restored:
    npm/.npmrc
```

The backup was a plain one, which masks secret values. Restoring the masked copy would break the real file, so restore skips it. Restore from an encrypted backup (`dothaven backup --encrypt` on the old machine), or copy those values by hand.

### A file was "skipped: the file here is a symlink"

The file on this machine is a symbolic link, often into a dotfiles repo managed by GNU Stow or similar. Writing through it would change the file it points to, so restore leaves it. Update the real file yourself, or remove the link and restore again.

### The same files keep being offered, or are never offered again

Restore remembers, in `~/.local/share/dothaven/state/applied.json`, what it applied and what you declined. A file you declined is not offered again from the same backup; pick it with **Choose files**, or use `--force`. To start over, delete `applied.json`: the only cost is that everything looks unapplied once.

### "this backup has no app & package list"

The backup was made with `--skip inventory`, or by an older dothaven. `reinstall` and `missing` need that list. Make a new backup on the old machine, or run `dothaven collect` there and pass the snapshot: `dothaven missing <snapshot.json>`.

### Homebrew is missing on a fresh Mac

`reinstall` warns that the Homebrew part will be skipped and prints the official installer:

```bash
/bin/bash -c "$(curl -fsSL https://raw.githubusercontent.com/Homebrew/install/HEAD/install.sh)"
```

Install it (it installs the Xcode command-line tools first, which takes a few minutes), then run `dothaven reinstall <backup>` again. To install dothaven itself without Homebrew, use the [installer script](../installation).

### GitHub: "… is PUBLIC — dothaven only writes to private repositories"

Make the repository private in its GitHub settings, or push somewhere else with `--repo owner/name`. dothaven will not write to a public repository in any mode.

### GitHub: "not signed in to GitHub" or "GitHub rejected the saved token"

Run `dothaven github login`. A token can expire or be revoked; `doctor` shows which source the rejected token came from. If `DOTHAVEN_GITHUB_TOKEN` is set, it is used before anything else.

### GitHub: "This build has no GitHub app configured for browser sign-in"

Either install the GitHub CLI and sign in (`gh auth login`, which dothaven then uses), or create a fine-grained token for one repository (Contents and Administration: read & write) and run `dothaven github login --with-token < token.txt`.

### GitHub: "holds several machines"

Name the machine: `dothaven github pull --machine laptop`. `dothaven github status` lists them.

### GitHub: the encrypted backup is over 100 MB

GitHub takes at most 100 MB per file. Leave out something large with `--skip` (for example a category holding big plugin folders), or use `--mode split`, which uploads readable config as separate files.

### zsh: "no matches found: github#laptop"

With zsh's `extendedglob` option, an unquoted `#` is a glob operator. Quote it, `dothaven restore 'github#laptop'`, or use `dothaven github pull --machine laptop`.

### doctor warns about the keychain on Linux

```text
⚠ keychain       tokens and passphrases go to ~/.config/dothaven/credentials/… (owner-only file; no keyring found)
```

dothaven uses the Secret Service through `secret-tool` when it is installed and a desktop session is running (`DBUS_SESSION_BUS_ADDRESS` is set). Install `libsecret-tools` (or your distribution's package with `secret-tool`) and a keyring such as GNOME Keyring or KWallet. On a headless machine the owner-only file is the fallback; set `DOTHAVEN_SECRET_STORE=file` to choose it on purpose.

### "could not remember it: the stored value did not read back the same"

dothaven reads a remembered passphrase back right after saving it, and keeps it only if it comes back exactly. A keychain that changed it would make later pushes encrypt with a passphrase you don't know. The push itself still works; you will be asked for the passphrase each time (or set `DOTHAVEN_PASSPHRASE`). Any character is fine in a passphrase: quotes, spaces and letters such as ş or ğ are all stored as typed.

### `ready` says "Not safe to wipe yet"

It found work that exists only on this machine: see [Moving to a new machine](../migration#check-that-nothing-exists-only-here). It also exits with code 2 when your newest backup is more than 7 days old, or there is none. Make a fresh one with `dothaven backup --encrypt`.

### `scan` exits with code 2

It found a HIGH secret. That is the point: it can block a commit. Use `--no-fail` for the report without the exit code.

### The dashboard says "Open the link dothaven printed" or "This link has been used already"

The link `dothaven ui` prints carries a key that works once: the browser that opens it gets a session, and reopening the link there keeps working. Anywhere else, the link no longer opens anything. That is the point, since the link sits in your terminal's scrollback and the browser's history. To use another browser, run `dothaven ui` again for a new link. "wrong host" means the address was not `127.0.0.1` or `localhost`.

### "the tui command needs an interactive terminal"

The menu needs a terminal for input and output. In a script, use the commands directly.

### Turning off the update notice

Set `DOTHAVEN_NO_UPDATE_CHECK=1`.

{{< cards >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../security" title="Security" >}}
  {{< card link="../github" title="GitHub sync" >}}
{{< /cards >}}
