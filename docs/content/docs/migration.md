---
title: Migration runbook
weight: 11
---

This is the end-to-end procedure for moving a development setup to a new machine.
It is built around the hybrid model: **dothaven** handles discovery, audit, and
export, while **chezmoi** handles storage, age-encryption, and applying configs
on the destination. dothaven never stores or transmits your files — it plans the
hand-off and `chezmoi add` does the rest.

The runbook has two halves: everything you do on the **old machine** before you
wipe or hand it back, and everything you do on the **new machine** to restore
parity. Each half is an ordered checklist — work top to bottom.

{{< callout type="info" >}}
dothaven is a single static Go binary with no runtime dependency. chezmoi and
age are separate tools you install alongside it; only the `chezmoi-export
--apply` path and the new-machine restore actually invoke them.
{{< /callout >}}

## Before you start

Install dothaven on the old machine if it is not already present.

{{< tabs >}}
  {{< tab name="Homebrew" >}}
```bash
brew install doguyilmaz/tap/dothaven
```
  {{< /tab >}}
  {{< tab name="Go" >}}
```bash
go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest
```
  {{< /tab >}}
  {{< tab name="Source" >}}
```bash
git clone https://github.com/doguyilmaz/dothaven
cd dothaven
go build ./cmd/dothaven
```
  {{< /tab >}}
{{< /tabs >}}

You will also need [chezmoi](https://www.chezmoi.io/) and
[age](https://github.com/FiloSottile/age). The next step checks for both.

## Part 1 — Old machine

### Step 1: Verify the chezmoi + age prerequisites

`dothaven init` is a read-only preflight. It probes whether chezmoi is
installed, whether your `~/.config/chezmoi/chezmoi.toml` declares
`encryption = "age"`, whether the chezmoi source directory is a git repo, and
your GitHub login (via `gh api user`, if available). It prints a checklist and
the exact command for each unmet step — it does **not** change anything.

```bash
dothaven init
```

```text
dothaven init — chezmoi + age bootstrap

  ✓ chezmoi installed
  ✓ age encryption configured
  → initialize a chezmoi source repo
      chezmoi init

Run the commands above, then re-run `dothaven init`.
```

Work through any `→` items and re-run until everything reads `✓`. When the
prerequisites are met, `init` prints the next two commands for you:

```text
✓ Setup complete. Next:
  dothaven chezmoi-export          # dry-run — review the plan
  dothaven chezmoi-export --apply  # execute
```

### Step 2: Collect a snapshot and audit it

Inventory the machine into a timestamped JSON snapshot. By default `collect`
redacts secrets before anything touches disk and prints an inline sensitivity
report.

```bash
dothaven collect
```

```text
Report saved to: /Users/you/projects/dotfiles/reports/old-host-20260604093000.json

⚠ Sensitivity report:
  HIGH   /Users/you/.npmrc                npm auth token — redacted
  HIGH   /Users/you/.ssh/id_ed25519       private key — skipped

  1 items redacted, 1 skipped.
```

The output directory is resolved in this order: an explicit `-o` / `--output`
wins; otherwise, if the working directory is a git repo the file lands in
`<cwd>/reports`; otherwise it falls back to `~/Downloads`. The filename is
`<hostname>-<UTC timestamp>.json`. **Keep this file** — you will copy it to the
new machine and feed it to `dothaven doctor` to verify parity in Part 2.

Before you export anything, review what is sensitive on disk with the standalone
scanners. `scan` prints findings to the console; `security` writes a grouped
Markdown report (default `SECURITY.md`).

```bash
dothaven scan ~          # console: L<line> [SEVERITY] <label>: <match>
dothaven security ~      # writes SECURITY.md
```

```text
Security report written to: SECURITY.md
  142 scanned, 3 with findings.
```

Read the report. Anything HIGH is what the export will encrypt or skip — confirm
nothing surprising is about to be carried over.

### Step 3: Plan the chezmoi export (dry-run)

`chezmoi-export` builds the hand-off plan: plain `chezmoi add` for ordinary
configs and `chezmoi add --encrypt` for anything high-sensitivity. It is a
**dry-run by default** — nothing changes until you pass `--apply`.

```bash
dothaven chezmoi-export
```

```text
chezmoi-export plan — 4 path(s), 1 encrypted:

     add            /Users/you/.config/ghostty/config  (plain)
     add            /Users/you/.gitconfig  (plain)
     add            /Users/you/.zshrc  (plain)
  🔒 add --encrypt  /Users/you/.ssh/id_ed25519  (ssh private key)
  + run_onchange install script (brew, packages)

Dry-run. Re-run with --apply to execute (requires chezmoi + a configured age key).
```

The plan also lists a `run_onchange install script` line when brew or package
groups are selected — that is the script chezmoi will run on the new machine to
reinstall everything. Useful flags:

- `--pin` — pin global packages to the version captured on this machine
  (otherwise the fresh machine installs the current release).
- `--only` / `--skip` — comma-separated categories or groups to include or
  exclude (for example `--only ssh,brew` or `--skip vscode`). Skip wins over
  only.

Iterate on the plan with these flags until it lists exactly what you want to
carry over.

### Step 4: Apply the export

When the plan looks right, run it. `--apply` requires `chezmoi` and a configured
age key; if chezmoi is not found it stops with exit code 1 and tells you to
install it.

```bash
dothaven chezmoi-export --apply
```

```text
  ✔ .chezmoiignore (gnupg runtime cruft)

  ✔ /Users/you/.config/ghostty/config
  ✔ /Users/you/.gitconfig
  ✔ /Users/you/.zshrc
  ✔ encrypted /Users/you/.ssh/id_ed25519
  ✔ run_onchange_install-packages.sh

Done. Review with `chezmoi diff`, then commit your private chezmoi source repo.
```

What `--apply` writes into your chezmoi source directory:

- One `chezmoi add` (or `add --encrypt`) per planned path.
- `run_onchange_install-packages.sh` — a command-guarded bash script that
  reinstalls brew formulae/casks, fnm node versions, and global packages
  (`bun`, `pnpm`, `npm`, `cargo`) on the next `chezmoi apply`. Every step is
  `|| true` and the script ends in `exit 0`, so a missing tool never aborts the
  apply. Deno global bins are recorded as a comment (the original module URL is
  not recoverable from a bin name) — reinstall those by hand.
- `.chezmoiignore` entries for GnuPG runtime cruft (sockets, locks, the RNG
  seed) when a real GnuPG key is present. Key material itself is **not** ignored.

{{< callout type="warning" >}}
The generated `run_onchange_install-packages.sh` is **unencrypted**: the Brewfile
is embedded verbatim. dothaven redacts inline credentials it can detect (such as
a private tap's `https://user:pass@host`), but review the script before you
commit it to make sure no secret slipped in.
{{< /callout >}}

### Step 5: Commit the private chezmoi source repo

Review the staged changes, then commit and push the chezmoi source repository.
This repo holds your configs and the age-encrypted secrets — keep it **private**.

```bash
chezmoi diff
chezmoi cd
git add -A
git commit -m "Sync configs from old-host"
git push
exit
```

At this point the old machine's work is done. Make sure you still have:

1. The snapshot JSON from Step 2 (for the parity check on the new machine).
2. A backup of your **age private key** — see the warning below.

{{< callout type="error" >}}
age is the encryption backend. **If you lose your age key, every encrypted file
in your chezmoi repository is unrecoverable** — there is no recovery path. Before
you wipe the old machine, back the key up somewhere safe and offline (a password
manager or hardware-backed store). The default key location is
`~/.config/age/`; check `~/.config/chezmoi/chezmoi.toml` for the exact path your
setup uses.
{{< /callout >}}

## Part 2 — New machine

### Step 6: Install chezmoi and restore the age key

On the fresh machine, install chezmoi and age, then **restore your age private
key first** — before any `chezmoi apply`. chezmoi needs the key to decrypt the
encrypted files in your source repo; without it the apply will fail on the first
encrypted entry.

```bash
brew install chezmoi age
mkdir -p ~/.config/age
# Restore the key you backed up in Step 5, e.g.:
cp /Volumes/backup/key.txt ~/.config/age/key.txt
chmod 600 ~/.config/age/key.txt
```

{{< callout type="warning" >}}
Restore the age key **before** initializing chezmoi. If chezmoi applies an
encrypted file and cannot find the key, the apply aborts. Put the key back at
the path your `chezmoi.toml` expects, then continue.
{{< /callout >}}

### Step 7: Initialize chezmoi from your private repo

Point chezmoi at the private source repo you pushed in Step 5 and apply it. This
clones the repo, decrypts the encrypted files with your age key, and writes every
config into place.

```bash
chezmoi init --apply git@github.com:you/dotfiles-private.git
```

### Step 8: Let the run_onchange script reinstall packages

Because the source repo contains `run_onchange_install-packages.sh`, chezmoi
runs it as part of the apply (and again whenever its contents change). The script
installs:

- Homebrew formulae and casks (via `brew bundle` from the embedded Brewfile)
- fnm node versions
- global `bun`, `pnpm`, `npm`, and `cargo` packages

Each block is guarded by `command -v <tool>` and every install is `|| true`, so
the script keeps going even if a manager is missing. The first run can take a
while as Homebrew downloads everything. Deno global bins, recorded only as a
comment, must be reinstalled manually.

To install dothaven itself on the new machine so you can verify parity:

```bash
brew install doguyilmaz/tap/dothaven
```

### Step 9: Verify parity with the snapshot

Copy the snapshot JSON from Step 2 to the new machine, then run `dothaven
doctor` against it. doctor re-inventories the live machine and lists everything
that was present in the snapshot but is **missing** here. It only checks
*installable* inventory — packages, runtimes, brew formulae/casks, macOS apps,
fonts, and editor extensions — and matches on name, so version drift is ignored.

```bash
dothaven doctor old-host-20260604093000.json
```

When everything lines up:

```text
✅ Parity — everything installable in the snapshot is present on this machine.
```

When something is still missing, doctor groups it by section, prints a count,
and **exits non-zero** (CI-friendly):

```text
Missing on this machine (present in the snapshot):

  apps.brew.formulae (2)
    - jq
    - ripgrep
  packages.npm.global (1)
    - typescript

3 item(s) missing across 1 section(s).
```

A non-zero exit here is a normal outcome, not an error — it is the to-do list of
what to install. Re-run `chezmoi apply` (to re-trigger the install script) or
install the listed items by hand, then run `doctor` again until it reports
parity.

## Manual checklist (out of scope)

dothaven and chezmoi cover discovery, secrets, configs, and reinstallable
packages. The following are deliberately **not** captured — handle them by hand
on the new machine:

- **System files outside `$HOME`** — `/etc/hosts` and other `/etc` edits are not
  in scope.
- **VPN configuration and certificates** — reconfigure your VPN client and
  re-import any profiles.
- **Browser sync** — sign in to your browser to pull bookmarks, extensions, and
  saved sessions.
- **Provisioning profiles and signing identities** — re-download Apple
  provisioning profiles and re-import code-signing certificates into the
  keychain.
- **Ollama models** — the snapshot records model *names* (`ai.ollama.models`)
  but not the weights. Re-pull each model, e.g. `ollama pull llama3`.
- **Anything requiring an interactive login** — App Store apps, licensed
  software, and 2FA-gated services need to be signed in again.

## Related pages

{{< cards >}}
  {{< card link="../quick-start" title="Quick start" subtitle="Collect, audit, and export in five steps" >}}
  {{< card link="../commands" title="Commands" subtitle="Full reference for every subcommand and flag" >}}
  {{< card link="../security" title="Security & redaction" subtitle="How secrets are detected and handled" >}}
{{< /cards >}}
