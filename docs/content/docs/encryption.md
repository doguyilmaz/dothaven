---
title: "Encryption & chezmoi"
weight: 12
---

dothaven can encrypt your setup in two different ways. Both use [age](https://age-encryption.org), but they solve different problems.

| | `backup --encrypt` (and `github push`) | `chezmoi-export` |
| --- | --- | --- |
| What you get | One encrypted file (or one encrypted file per machine on GitHub) | A [chezmoi](https://www.chezmoi.io/) source repo: one file per config, secrets encrypted individually |
| Protected by | A passphrase you choose | An age key file that chezmoi uses |
| What it carries | Config, credentials, installed-apps list, macOS settings | Config and credentials, plus an install script for your packages |
| Needs installed | Nothing: age is built into dothaven | chezmoi, and an age key |
| Restore with | `dothaven restore` | `chezmoi init` + `dothaven migrate` |
| Best for | Moving from one machine to another | Keeping several machines in sync over time |

If you are not sure, use `backup --encrypt`. It needs nothing else and carries more.

## backup --encrypt

```bash
dothaven backup --encrypt -o /Volumes/MyDrive
```

How it works:

- dothaven walks your config, adds the installed-apps inventory and macOS settings, and streams all of it through tar and gzip into age's passphrase (scrypt) encryption, straight into the output file. **At no point is an unencrypted copy written to disk**, not even a temporary one.
- The output is written as `<name>.partial`, owner-only, and renamed to `backup-<host>-<timestamp>.tar.gz.age` only once it is complete.
- The passphrase is asked twice and must be at least 10 characters. For scripts, `DOTHAVEN_PASSPHRASE` supplies it.
- dothaven uses the age library, so neither the old nor the new machine needs the `age` program. The file is still a standard age file:

  ```bash
  age -d backup-mymac-20260927232938.tar.gz.age | tar -tz   # list what is inside
  ```

{{< callout type="error" >}}
There is no way to recover a lost passphrase. Nothing, including dothaven, can open the file without it. Put it in your password manager before you wipe the old machine.
{{< /callout >}}

To prove a backup opens before you rely on it, run `dothaven restore --dry-run <file>`: it decrypts, lists what it would restore, and writes nothing. The menu's **Pack everything into one encrypted file** reads the new file back end to end automatically.

A [GitHub push](../github) in the default `encrypted` mode uploads exactly this kind of file. In `split` mode, readable config sits beside a `secrets.tar.gz.age` made the same way, holding the credentials and sensitive files.

## The chezmoi path

[chezmoi](https://www.chezmoi.io/) manages dotfiles from a git repository (the "source") and applies them to each machine. dothaven can fill that source for you: it knows where your config is, which files hold secrets, and which ones name your home folder.

### 1. Check the prerequisites

```bash
dothaven init
```

`init` checks that chezmoi is installed, that `~/.config/chezmoi/chezmoi.toml` declares `encryption = "age"`, and that your chezmoi source is an initialized git repository. For each missing step it prints the command, and on a terminal offers to run the safe ones (installing chezmoi with Homebrew, `chezmoi init <url>`).

It never creates your age key. That key is the only way to decrypt your secrets, so generating it and backing it up is yours to do:

```bash
age-keygen -o ~/.config/chezmoi/key.txt    # or: chezmoi age-keygen
```

Then add the age settings (`encryption = "age"`, the identity file and your recipient) to `chezmoi.toml`, as described in [chezmoi's age guide](https://www.chezmoi.io/user-guide/encryption/age/).

{{< callout type="error" >}}
**If you lose the age key, every encrypted file in your chezmoi repo is unrecoverable.** Keep a copy offline, in your password manager, and never commit it.

dothaven treats the key as the most sensitive file you have:

- **An encrypted file backup carries it.** `dothaven backup --encrypt` includes `~/.config/chezmoi/key.txt` (and sops' `keys.txt`), so the new machine gets it back with everything else.
- **A plaintext backup never does.** The key is a credential file, and the scanner recognises `AGE-SECRET-KEY-1…` under any name. A file holding one is left out of a plaintext backup even if you `include` its folder.
- **GitHub never gets it, not even encrypted.** The key opens the encrypted files in your chezmoi repo, and those files already sit in a repository. Putting the key in another repository, even behind a passphrase, would make that passphrase the only thing protecting all of them. Every `github push` leaves out any file containing an age identity, and says so.
{{< /callout >}}

### 2. Review the plan

```bash
dothaven chezmoi-export
```

This is a dry run. On a terminal with no `--only`/`--skip` it first asks which categories to export, plus two install groups: `brew` (Homebrew formulae and casks) and `packages` (global npm, pnpm, bun, pipx, cargo and other packages).

```text
chezmoi-export plan — 13 files, 5 encrypted:

     add            ~/.claude/settings.json  (plain)
  🔒 add --encrypt  ~/.claude.json  (secret detected)
     add            ~/.claude/skills  (folder, 1 file)
  📝 add --template  ~/.zshrc  (templated (home path))
     add            ~/.gitconfig  (plain)
  🔒 add --encrypt  ~/.ssh/config  (has redact rule)
  🔒 add --encrypt  ~/.ssh  (folder, 2 files)
  🔒 add --encrypt  ~/.aws/credentials  (sensitivity:high)
  🔒 add --encrypt  ~/.ssh/id_ed25519  (ssh private key)
  + run_onchange install script (brew)

🔒 Encrypted paths are recoverable only with your age key (~/.config/chezmoi/key.txt).
   Back it up offline before you rely on this — a lost key means those files are gone for good.

Dry-run. Re-run with --apply to execute.
```

### chezmoi-export

What decides each line:

**Encrypted (`🔒 add --encrypt`)** when any of these holds:

- the entry is a credential (`sensitivity:high`);
- the entry has a dedicated redactor, such as `~/.ssh/config` (`has redact rule`);
- the file contains a HIGH-severity secret (`secret detected`). An IP address or an email address never forces encryption.

Folders are judged **file by file**: one token in one file of your Neovim config encrypts that file, not the whole folder. The plan line then says, for example, `folder, 40 files, 1 encrypted: secret detected`. A credential folder (`~/.ssh`, `~/.gnupg`) is encrypted as a whole.

**Templated (`📝 add --template`)** for a single config file in the `shell`, `git`, `terminal`, `editor`, `dev` or `vm` categories that actually contains your home folder path. After adding it, dothaven rewrites every `/Users/you/` (or `/home/you/`) in the chezmoi copy to `{{ .chezmoi.homeDir }}/`, so the file works on a machine with a different username. Only that exact prefix is replaced, so a path such as `/Users/youtube` is left alone. Anything in the file that looks like template syntax (`{{` and `}}`, as in WezTerm key tables or Go format strings in a git alias) is escaped first, so `chezmoi apply` on the new machine does not fail on it. Files that do not mention your home folder are added as plain copies, since a template that did not need to be one is just one more thing that can fail to parse.

**Plain (`add`)** for everything else.

Other rules:

- Files are added one by one, with the same walk as a backup (symbolic links followed, each entry's clutter skipped), plus two rules of its own: **nested `.git` folders are skipped** (the chezmoi source is itself a git repository), and **files over 25 MB are left out and listed** (it is a repo you will push; GitHub refuses files over 100 MB).
- When `ssh` is selected, `~/.ssh` is also searched for private keys **by content** (any file whose text is a private-key block, whatever its name, excluding `.pub` files). Each one is added encrypted (`ssh private key`).
- `~/.gnupg` is only exported if it holds real secret keys (`private-keys-v1.d/*.key`). When it is, `--apply` adds GnuPG's runtime files (sockets, locks, `random_seed`) to the source's `.chezmoiignore`, merging with what is already there.
- If VS Code's or Cursor's own Settings Sync looks active, you are warned: chezmoi and the editor's cloud sync would keep overwriting each other's copy.
- Your `include` paths are exported too.

### 3. Apply it

```bash
dothaven chezmoi-export --apply
```

`--apply` needs chezmoi. If the plan encrypts anything, age must be configured in `chezmoi.toml`, and on a terminal you are asked to confirm that your age key is backed up. It then runs `chezmoi add` in batches (and retries a failed batch file by file, so the report names the file that failed), templatizes the files marked for it, and writes the install script. If anything fails, it lists the failures, says how many were encrypted secrets that were **not** carried, and exits with code 1.

It finishes by telling you where the source repo is and what is left: review with `chezmoi diff`, commit, and push. Until you push, nothing has left the machine.

```bash
chezmoi cd
git add -A && git commit -m 'update dotfiles'
git push        # keep this repository private
```

### The install script

When `brew` or `packages` is selected, `--apply` writes `run_onchange_install-packages.sh` into the source. chezmoi runs it on `chezmoi apply` whenever its contents change, which reinstalls your toolchain on a new machine. It covers Homebrew (`brew bundle` from your Brewfile, including VS Code extensions), Node versions (fnm), global bun, pnpm and npm packages, cargo crates and Rust toolchains, pipx, uv, Composer, Dart and .NET tools, Cursor extensions, and apt, dnf, pacman, snap and flatpak packages.

- Every block runs only if its tool exists (`command -v …`), every install ends in `|| true`, and the script ends with `exit 0`. A missing tool or a failed package never breaks `chezmoi apply`.
- `--pin` pins global packages to the version you have now; without it, the new machine gets the current release. Node versions are always exact.
- The Brewfile is embedded unencrypted, so credentials in it (such as a private tap's `https://user:pass@host`) are redacted first.
- Deno global commands are listed as a comment to reinstall by hand, since the module URL they came from is not recorded.
- If the same package is installed by more than one manager, you are warned, so you can check which copy wins on your `PATH`.

A dothaven backup carries the same script, as `inventory/install-packages.sh`; `dothaven reinstall` uses it.

### 4. On the new machine

Put your age key back first, at the path your `chezmoi.toml` expects. Without it, chezmoi cannot decrypt anything.

```bash
mkdir -p ~/.config/chezmoi
cp /Volumes/MyDrive/key.txt ~/.config/chezmoi/key.txt
chmod 600 ~/.config/chezmoi/key.txt
chezmoi init git@github.com:you/dotfiles.git
dothaven migrate
```

`migrate` checks that chezmoi is installed and the source is initialized (and warns if age is not configured), asks, then runs `chezmoi apply` attached to your terminal, with no time limit, so the install script can take as long as `brew bundle` needs and ask for passwords. `migrate --dry-run` shows `chezmoi diff` instead. Off a terminal it needs `--yes`.

Afterwards, `dothaven check` confirms the configs that landed still parse. For what the old machine had installed that this one lacks, run `dothaven collect` on the old machine and `dothaven missing <snapshot.json>` on the new one.

{{< cards >}}
  {{< card link="../security" title="Security" >}}
  {{< card link="../migration" title="Moving to a new machine" >}}
  {{< card link="../commands" title="Commands" >}}
{{< /cards >}}
