---
title: GitHub sync
weight: 6
---

Instead of carrying a file, you can keep your backup in a private repository on your own GitHub account. Each machine gets its own folder, git history keeps every earlier push, and a new machine restores with one command. Neither machine needs git: dothaven talks to the GitHub API directly.

```bash
# Old machine
dothaven github login
dothaven github push        # creates <you>/dothaven-backup (private) the first time

# New machine
dothaven github login
dothaven restore github
```

## Sign in

```bash
dothaven github login
```

There are three ways to sign in. Without `--with-token`, `login` tries the first, then the second:

1. **Browser sign-in.** When your build of dothaven includes a GitHub app, `login` shows a one-time code, copies it to your clipboard and opens github.com. Approve it there and the terminal carries on by itself.
2. **The GitHub CLI.** Without a built-in app, dothaven uses your `gh` login (`gh auth login`), and stores nothing of its own.
3. **A token on stdin**, the most locked-down option:

   ```bash
   dothaven github login --with-token < token.txt
   ```

   First [create the private repository](https://github.com/new) yourself; it can be empty, and the first push starts it. Then create a [fine-grained token](https://github.com/settings/personal-access-tokens/new) limited to that one repository, with **Contents: read & write** (Metadata: read comes with every token). This is safer than the browser sign-in, whose `repo` permission covers every private repository you have. Push with `--repo you/that-name`, or it will look for `<you>/dothaven-backup`.

If none of these is available, `login` prints what to do instead.

### Where the token is kept

A token dothaven stores (from the browser or `--with-token`) goes to your system's credential store:

| System | Store |
| --- | --- |
| macOS | The macOS Keychain |
| Linux with a keyring | The Secret Service (GNOME Keyring, KWallet), through `secret-tool` |
| Neither available | An owner-only file in `~/.config/dothaven/credentials/` |

The file fallback works, but `dothaven doctor` flags it with a warning, because a keyring is safer. Secrets are passed to the keychain on stdin, never on a command line where other processes could read them.

When looking for a token, dothaven checks, in order: the `DOTHAVEN_GITHUB_TOKEN` environment variable, its own stored token, then `gh auth token`.

## Push

```bash
dothaven github push
```

`push` (also `sync` or `save`) builds the same backup `dothaven backup` makes and commits it to `machines/<this machine>/` in the repository, replacing the previous copy there. Other machines' folders are left alone.

- **The first push creates the repository** `<you>/dothaven-backup`, private, after asking. Off a terminal it needs `--yes`. It is deliberately not called `dotfiles`, so it stays clear of the dotfiles repo many people keep by hand. dothaven only creates repositories on your own account.
- **It refuses public repositories.** If the repository is public, `push` stops with an error and writes nothing. Even an encrypted backup reveals which services you use; a plain one is your config for anyone to read.
- **Nothing changes if nothing changed.** dothaven fingerprints what went into the backup (paths, content hashes, executable bits; not timestamps). If it matches the last push, you get `✓ Already up to date` and no new commit. If nothing changed but you are pushing with a different passphrase, the copy on GitHub is replaced anyway, so the passphrase you now know is the one that opens it.
- **Encrypted means encrypted.** Before uploading, dothaven checks that every `.age` file it is about to send really is age-encrypted, and refuses to upload otherwise.
- **Your choices are remembered.** The repository and mode are saved in `~/.config/dothaven/github.json`, so later pushes do not ask again.

```text
✓ Created https://github.com/you/dothaven-backup (private)

Uploading 2 files (13 KB) to you/dothaven-backup …
✓ Pushed 214 files as machines/mymac (encrypted, commit 3f9c2ab)
  https://github.com/you/dothaven-backup/tree/main/machines/mymac

On the new machine:
  dothaven github login
  dothaven restore github
  You will need the passphrase. Nothing can open the encrypted part without it.
```

### Storage modes

On a terminal, the first push asks how to store it. You can change it on any push with `--mode`.

| Mode | What is on GitHub | Passphrase | Choose it when |
| --- | --- | --- | --- |
| `encrypted` (default) | One `backup.tar.gz.age` with everything, keys included | Yes | You want the complete, private backup. Recommended. |
| `split` | Readable config files, plus `secrets.tar.gz.age` holding credentials, medium- and high-sensitivity config, and any file with a secret in it | Yes | You want to browse and diff your config on GitHub, and still carry your keys. |
| `plain` | Readable files, secrets redacted, credential files left out | No | You only want readable config, and will carry keys another way. |

In the readable modes, `.git` folders (and the `.git` files of submodules) inside your config, such as Claude plugin marketplaces, are left out, because GitHub refuses a path named `.git`. The encrypted mode carries them inside its archive.

**Age keys stay off GitHub in every mode.** The key chezmoi or sops decrypts with (`~/.config/chezmoi/key.txt`, sops' `keys.txt`, or any file containing `AGE-SECRET-KEY-1…`) opens every encrypted file in your dotfiles repo. Putting it in a second repository, even encrypted, would leave that passphrase as the only thing protecting all of them. The push lists what it kept back. Carry the key with `dothaven backup --encrypt`, or in your password manager.

After the upload, the push lists everything it left out, as a file backup does: credential files in `plain` mode, age keys, and files over the size cap or that could not be read.

GitHub accepts at most 100 MB per file. If the encrypted backup is larger, `push` stops and suggests leaving something large out with `--skip`, or using `--mode split`.

### The passphrase

The encrypted parts use a passphrase of at least 10 characters. On the first push, dothaven asks for it twice, then offers to remember it in your keychain so routine pushes do not ask. The remembered copy stays on this machine; it protects the copy on GitHub. It is read back right after saving and kept only if it comes back exactly as typed. `dothaven github logout` forgets it.

In scripts, `DOTHAVEN_PASSPHRASE` takes precedence. It is held to the same 10-character rule: set but empty (a `$PASS` that expanded to nothing) is an error, never "no encryption". dothaven reads it once at startup and removes it from the environment, so no tool it runs (brew, npm, chezmoi, git) inherits it.

Every `.age` file is checked for the age header before it is uploaded. If you change the passphrase, the next push notices that the copy on GitHub no longer opens with it, and replaces it, even when nothing else changed.

You still need the passphrase on the new machine. Keep it in your password manager.

### Push flags

| Flag | Meaning |
| --- | --- |
| `--mode` | `encrypted` (default), `split`, or `plain` |
| `--repo` | `owner/name` (default: `<you>/dothaven-backup`) |
| `--machine` | Folder name for this machine in the repo (default: hostname). Lowercase letters, digits, `.`, `-`, `_` |
| `--only` | Only these categories |
| `--skip` | Skip these categories |
| `--yes` | Create the repository without asking (required off a terminal) |

## What the repository looks like

```text
dothaven-backup/
├── README.md                       how to restore
└── machines/
    ├── mymac/                      encrypted mode
    │   ├── backup.tar.gz.age
    │   └── dothaven.json           machine, OS, mode, date, file count, fingerprint, age header
    └── laptop/                     split mode
        ├── dothaven.json
        ├── MANIFEST.txt
        ├── shell/.zshrc
        ├── …
        └── secrets.tar.gz.age
```

Each push is one commit with a message such as `dothaven: mymac — encrypted backup, 214 files`. The machine's folder is rebuilt each time, so a file you deleted locally disappears from the latest version too; earlier versions stay in git history.

Commits appear as your GitHub account. dothaven has no server of its own, so there is no bot identity to commit as.

## Restoring from GitHub

```bash
dothaven restore github
```

This downloads the repository as one archive (no git needed), unpacks it into a private temporary folder, opens your machine's backup (asking for the passphrase if it is encrypted) and continues exactly like a local restore: choose what to restore, then macOS settings and reinstalling. If the repository holds several machines, it asks which one; off a terminal, name it.

You can be explicit:

```bash
dothaven restore github:you/dothaven-backup          # a specific repository
dothaven github pull --machine laptop                # a specific machine
dothaven github pull --repo you/other-backup --machine laptop --dry-run
```

{{< callout type="warning" >}}
You can also write `dothaven restore 'github#laptop'`. Quote it in zsh: with the `extendedglob` option on, an unquoted `#` is a glob operator and zsh stops with "no matches found". `github pull --machine laptop` avoids the problem.
{{< /callout >}}

`github pull` (also `github restore`) takes:

| Flag | Meaning |
| --- | --- |
| `--repo` | `owner/name` (default: the one you pushed to, or `<you>/dothaven-backup`) |
| `--machine` | Which machine's backup (default: ask, or the only one) |
| `--dry-run` | Show what would change without writing |
| `--force` | Overwrite differing files (a pre-restore snapshot is saved first) |
| `--yes` | Don't ask before writing |

Other commands that read a backup accept `github` too:

```bash
dothaven reinstall github
dothaven missing github
dothaven defaults import github
dothaven diff github
dothaven list brew github
```

## Status and sign-out

```bash
dothaven github status      # also plain `dothaven github`
```

```text
Signed in as you (via the macOS Keychain)
Repository: https://github.com/you/dothaven-backup (private)
Machines:
  laptop                   split, darwin, 12 Sep 2026 09:14
  mymac                    encrypted, darwin, 27 Sep 2026 23:40 ← this one
```

```bash
dothaven github logout
```

This removes dothaven's stored token and remembered passphrase. If you are still signed in another way (the `gh` CLI or `DOTHAVEN_GITHUB_TOKEN`), it says so.

## Security model

- **Private only.** dothaven checks the repository's visibility before every push and refuses a public one. `dothaven doctor` flags a public backup repository as a problem.
- **Encrypted by default.** In `encrypted` and `split` modes, credentials are only ever uploaded inside age-encrypted archives, written in a private temporary folder that is removed afterwards.
- **The token never touches a command line or git.** It lives in your keychain (or `gh`, or the environment), and is sent only to the GitHub API over HTTPS.
- **Narrow access if you want it.** A fine-grained token can be limited to the one repository.
- **Your history is yours.** git history keeps every push. If you ever pushed something you regret, deleting it from the latest version is not enough; treat anything that was in a readable push as exposed to whoever can read the repository.

## Environment variables

| Variable | Meaning |
| --- | --- |
| `DOTHAVEN_GITHUB_TOKEN` | A token to use instead of the stored one or `gh` |
| `DOTHAVEN_PASSPHRASE` | The passphrase for the encrypted parts, for scripts |
| `DOTHAVEN_SECRET_STORE=file` | Force the owner-only file store instead of the keychain (headless machines) |
| `DOTHAVEN_GITHUB_API`, `DOTHAVEN_GITHUB_WEB` | Point at another GitHub, such as GitHub Enterprise. Must be `https` (plain `http` only on localhost) |
| `DOTHAVEN_GITHUB_CLIENT_ID` | The OAuth app used for browser sign-in, if your build has none |

{{< cards >}}
  {{< card link="../migration" title="Moving to a new machine" >}}
  {{< card link="../backup-restore" title="Backup & restore" >}}
  {{< card link="../troubleshooting" title="Troubleshooting" >}}
{{< /cards >}}
