---
title: Encryption & chezmoi
weight: 9
---

dothaven never encrypts or stores anything itself. It does **discovery, audit, and export** — finding your configs, classifying which ones hold secrets, and building a plan. [chezmoi](https://www.chezmoi.io/) does **storage, age-encryption, and apply** on the target machine. The two halves meet in one command: `dothaven chezmoi-export` translates the plan into `chezmoi add` and `chezmoi add --encrypt` calls, plus a `run_onchange` install script that rebuilds your packages on `chezmoi apply`.

[age](https://age-encryption.org/) is the encryption backend. chezmoi reads its age key from `~/.config/chezmoi/key.txt` (configured in `chezmoi.toml`) and uses it to encrypt every file dothaven marks as a secret. dothaven decides *what* to encrypt; chezmoi *performs* the encryption.

{{< callout type="warning" >}}
Losing the age key makes every encrypted file **unrecoverable**. There is no recovery path and no backdoor. Back the key up offline (a password manager works), and **never commit it** to the chezmoi source repo. dothaven's `init` check repeats this warning for the same reason.
{{< /callout >}}

## The init prerequisite check

Before exporting, run `dothaven init` to confirm the three prerequisites for a working chezmoi + age setup. It probes the machine and prints each step as done (`✓`) or with the exact command to fix it (`→`):

1. **chezmoi installed** — detected by running `chezmoi --version`.
2. **age encryption key configured** — detected by reading `~/.config/chezmoi/chezmoi.toml` and matching `encryption = "age"`.
3. **chezmoi source (private dotfiles repo) initialized** — detected when `chezmoi source-path` reports a directory that is a git repo (it has a `.git/HEAD`).

```text
$ dothaven init
dothaven init — chezmoi + age bootstrap

  ✓ chezmoi installed
  → age encryption key configured
      age-keygen -o ~/.config/chezmoi/key.txt
      ⚠ Back this key up offline (password manager). Lose it and encrypted files are unrecoverable.
  → chezmoi source (private dotfiles repo) initialized
      chezmoi init git@github.com:you/dotfiles.git

Run the commands above, then re-run `dothaven init`.
```

The suggested repo URL uses your GitHub login (resolved with `gh api user --jq .login`) and the `dotfiles` name, matching chezmoi's default convention. When all three steps pass, the check prints the next commands instead:

```text
✓ Setup complete. Next:
  dothaven chezmoi-export          # dry-run — review the plan
  dothaven chezmoi-export --apply  # execute
```

`init` only reports status — it never installs anything or writes the key for you. Run the printed commands yourself.

## chezmoi-export: the hybrid export

`chezmoi-export` is dry-run by default. It prints the plan it *would* execute and stops; `--apply` is required to actually call chezmoi.

```text
$ dothaven chezmoi-export
chezmoi-export plan — 6 path(s), 3 encrypted:

     add            /Users/you/.zshrc  (plain)
     add            /Users/you/.gitconfig  (plain)
  🔒 add --encrypt  /Users/you/.ssh/config  (has redact rule)
  🔒 add --encrypt  /Users/you/.ssh/id_ed25519  (ssh private key)
  🔒 add --encrypt  /Users/you/.gnupg  (sensitivity:high)
  + run_onchange install script (brew, packages)

Dry-run. Re-run with --apply to execute (requires chezmoi + a configured age key).
```

Each line shows the verb chezmoi will use (`add` plain, or `add --encrypt`), the resolved source path, and the reason for the decision. The header counts total paths and how many are encrypted.

### The encrypt decision, per entry

For every registry entry that exists on this machine, dothaven decides plain vs. `--encrypt`. The rule is exact — an entry is encrypted if **any** of the following holds:

- its **sensitivity is `high`** — reason `sensitivity:high`; or
- it **has a redact rule** — reason `has redact rule`; or
- the scanner finds a **HIGH-severity secret** in it — reason `secret detected`.

The first two are decided from the registry metadata alone, with no file read. The third actually opens the file: a HIGH-severity secret forces encryption even if the entry was otherwise considered plain. For a directory entry, the scan walks the directory — a single HIGH-severity secret in **any** file inside it encrypts the whole directory.

The secret check is deliberately **HIGH-only**. A benign IP address or email — which the scanner flags at a lower severity — never forces encryption. Only credentials the scanner classifies as HIGH (private keys, tokens, and similar) tip an entry into the encrypted set. Anything that isn't high-sensitivity, doesn't carry a redact rule, and contains no HIGH secret is added **plain** (reason `plain`).

### The SSH private-key sweep

In addition to the registry entries, when the `ssh` category is selected dothaven sweeps `~/.ssh` for private keys. The sweep is **by content, not by filename**: it scans each file (skipping anything ending in `.pub`) and keeps it only if the scanner matches a private-key header — `private-key-pem` (the `-----BEGIN ... PRIVATE KEY-----` family, which covers OpenSSH, RSA, and others) or `pgp-private-key`.

Because the match is on the key header, the sweep catches `id_ed25519`, `id_rsa`, custom `*.key` files, and anything else holding real key material, regardless of how it's named. Every discovered key is added to the plan as `add --encrypt` with reason `ssh private key`. Keys already in the plan (matched by source path) are not added twice.

### The gnupg gate

`~/.gnupg` is a high-sensitivity directory entry, but it is only carried if it actually holds secret keys. dothaven checks `~/.gnupg/private-keys-v1.d` for any `*.key` file. If none exist, the `secrets.gnupg` entry is dropped from the plan entirely — carrying it would otherwise capture only runtime cruft and no real secret.

When `~/.gnupg` *does* hold secret keys and is part of the plan, `--apply` also writes a `.chezmoiignore` into the chezmoi source so that runtime cruft is not tracked:

```text
# gnupg runtime cruft (managed by dothaven chezmoi-export)
.gnupg/S.*
.gnupg/*.lock
.gnupg/.#*
.gnupg/random_seed
.gnupg/public-keys.d/*.lock
```

These globs exclude sockets, lock files, and the RNG seed. **Key material is intentionally not ignored** — it is what you want carried (encrypted). The patterns are merged idempotently: if `.chezmoiignore` already exists, only the missing lines are appended under the labeled header, and patterns already present are left untouched.

## The run_onchange install script

When the `brew` or `packages` group is selected, `--apply` writes `run_onchange_install-packages.sh` into the chezmoi source. chezmoi re-runs any `run_onchange_` script on `chezmoi apply` whenever the script's contents change — so on a fresh machine it reinstalls your toolchain, and on an existing one it runs again only when the package set shifts.

The script is built to be safe to run anywhere:

- **Every block is command-guarded.** Each manager's block is wrapped in `if command -v <tool> >/dev/null 2>&1; then ... fi`, so a manager that isn't installed is simply skipped.
- **Every install is `|| true`.** A single failed package never aborts the rest.
- **The script ends in `exit 0`.** Combined with `set -uo pipefail`, a missing tool or failed step never fails the whole `chezmoi apply`.

```bash
#!/bin/bash
# Generated by `dothaven chezmoi-export`. chezmoi re-runs this on apply when it changes.
set -uo pipefail

if command -v brew >/dev/null 2>&1; then
  brew bundle --file=/dev/stdin <<'BREWFILE' || true
brew "git"
cask "ghostty"
BREWFILE
fi

if command -v fnm >/dev/null 2>&1; then
  fnm install 20.11.0 || true
fi

if command -v bun >/dev/null 2>&1; then
  bun add -g typescript || true
fi

exit 0
```

It covers Homebrew formulae and casks (via `brew bundle`), Node versions (via `fnm install`), and global packages for bun, pnpm, npm, and cargo crates. The Brewfile is embedded into this **unencrypted** script, so dothaven redacts inline credentials (for example a private tap's `https://user:pass@host`) before embedding. Node versions are always kept exact; the `system` pseudo-version is skipped.

Deno global bins are a special case: the original module URL can't be recovered from a bin name, so they're recorded as a comment for manual reinstall rather than emitted as a broken `deno install`. If more than one JS global manager installs the same package name, `--apply` prints a PATH-shadowing warning but keeps the entry in each manager's block.

## Flags

| Flag | Effect |
| --- | --- |
| `--apply` | Execute the plan. Without it, `chezmoi-export` is a dry-run that prints the plan and stops. `--apply` requires chezmoi and a configured age key. |
| `--pin` | Pin global packages to their captured version in the install script (e.g. `name@1.2.3`). Without it, the script installs the bare name so a fresh machine gets the current release. Node versions are always pinned regardless. |
| `--only` | Restrict the export to these categories/groups (comma-separated). A non-empty `--only` list excludes everything not named. |
| `--skip` | Exclude these categories/groups (comma-separated). `--skip` always wins over `--only`. For the install script, a skipped group's lines are stripped from the Brewfile too. |

`--only` and `--skip` apply to both registry categories (e.g. `shell`, `git`, `ssh`, `secrets`) and the install-script groups (`brew`, `packages`). Selection logic: skip wins; a non-empty only-list restricts to its members.

```bash
# Plan everything, see what would be encrypted
dothaven chezmoi-export

# Export shell + git configs only, no install script
dothaven chezmoi-export --only shell,git --apply

# Export everything except SSH, pinning package versions
dothaven chezmoi-export --skip ssh --pin --apply
```

## The apply path

With `--apply`, after re-confirming chezmoi is installed dothaven:

1. resolves the chezmoi source with `chezmoi source-path`;
2. writes the gnupg `.chezmoiignore` if the plan still carries `secrets.gnupg`;
3. runs `chezmoi add` (plain) or `chezmoi add --encrypt` for each planned path, reporting `✔` per success and `✗` per failure (a failed add is skipped, not fatal);
4. writes `run_onchange_install-packages.sh` if a brew/packages group was selected and there is anything installable.

```text
$ dothaven chezmoi-export --skip ssh --apply
chezmoi-export plan — 4 path(s), 1 encrypted:

     add            /Users/you/.zshrc  (plain)
     add            /Users/you/.gitconfig  (plain)
     add            /Users/you/.config/zed/settings.json  (plain)
  🔒 add --encrypt  /Users/you/.gnupg  (sensitivity:high)
  + run_onchange install script (brew, packages)

  ✔ .chezmoiignore (gnupg runtime cruft)

  ✔ /Users/you/.zshrc
  ✔ /Users/you/.gitconfig
  ✔ /Users/you/.config/zed/settings.json
  ✔ encrypted /Users/you/.gnupg
  ✔ run_onchange_install-packages.sh

Done. Review with `chezmoi diff`, then commit your private chezmoi source repo.
```

dothaven hands off here. Review the staged changes with `chezmoi diff`, then commit and push your **private** chezmoi source repo. The encrypted files are safe to commit; the age key is not.

## The age key lifecycle

1. **Generate.** `age-keygen -o ~/.config/chezmoi/key.txt` creates the key. `init` prints this command when the key is missing.
2. **Declare.** Tell chezmoi to use age in `~/.config/chezmoi/chezmoi.toml` (`encryption = "age"` plus the recipient). `init` treats the key as configured once it sees `encryption = "age"` there.
3. **Back up.** Store a copy offline — a password manager entry is enough. This copy is your only recovery path.
4. **Encrypt.** `chezmoi-export --apply` calls `chezmoi add --encrypt`; chezmoi encrypts to the configured recipient and commits ciphertext to the source.
5. **Apply elsewhere.** On a new machine, place the same `key.txt`, run `chezmoi init` + `chezmoi apply`, and chezmoi decrypts every file back into place.

The key never leaves your control and is never part of what dothaven exports. If it is lost, the encrypted files cannot be decrypted by anyone — including you.

{{< cards >}}
  {{< card link="../security" title="Security & redaction" >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../installation" title="Installation" >}}
{{< /cards >}}
