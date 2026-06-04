# dothaven

Discover, back up, and migrate your machine's dev config — feeding [chezmoi](https://chezmoi.io) with age-encrypted secrets.

dothaven inventories what's on your machine (shell, git, editors, SSH, cloud CLIs, Homebrew, global packages, runtimes, fonts, AI tooling), scans it for secrets, and prepares an encrypted hand-off to chezmoi so a clean-install machine comes back without losing a thing.

| | |
|---|---|
| **Binary** | Single static Go binary — no runtime to install |
| **Platforms** | macOS & Linux · amd64 & arm64 |
| **Backbone** | [chezmoi](https://chezmoi.io) + [age](https://age-encryption.org) for storage, encryption, and `apply` |
| **Docs** | **https://doguyilmaz.github.io/dothaven** |

## Install

```bash
# Homebrew
brew install doguyilmaz/tap/dothaven

# Go
go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest

# From source
git clone https://github.com/doguyilmaz/dothaven && cd dothaven && go build ./cmd/dothaven
```

Running dothaven needs nothing else. The `chezmoi-export` and `init` commands additionally use [chezmoi](https://chezmoi.io) and a configured age key — see [Encryption & chezmoi](https://doguyilmaz.github.io/dothaven/docs/encryption/).

## Quick start

```bash
dothaven collect                 # inventory the machine → timestamped JSON snapshot
dothaven scan ~                  # scan a path for secrets (console)
dothaven security ~              # write a Markdown security report
dothaven chezmoi-export          # dry-run: plan plain vs --encrypt per file
dothaven chezmoi-export --apply  # execute (needs chezmoi + age)
dothaven doctor snapshot.json    # on a new machine: what's still missing?
```

## The hybrid model

dothaven is the **discovery + audit + export** layer; chezmoi is the **storage + encryption + apply** backbone.

| Stage | Owner |
|---|---|
| Discover the machine, snapshot installed config | dothaven |
| Scan for secrets, redact by default | dothaven |
| Build a `chezmoi add` plan + a `run_onchange` install script | dothaven |
| Track files in a private source repo | chezmoi |
| Encrypt the files marked secret | chezmoi + age |
| Write files back into `$HOME` on a new machine | chezmoi |

dothaven decides *what* to encrypt; chezmoi *performs* it. It is not itself an encryption or sync engine.

## Commands

| | |
|---|---|
| `collect` | Inventory the machine into a JSON snapshot |
| `doctor` | Diff a snapshot against this machine (non-zero exit on drift) |
| `scan` / `security` | Find secrets (console / Markdown report) |
| `backup` / `restore` | Copy tracked config files out and back, with a redaction gate |
| `status` / `diff` | Compare a backup against the live machine |
| `compare` / `list` | Diff two snapshots / print a snapshot section |
| `chezmoi-export` | Plan (or `--apply`) adding configs to chezmoi, encrypting secrets |
| `init` | Check the chezmoi + age prerequisites |

Full reference, with every flag: **[Commands](https://doguyilmaz.github.io/dothaven/docs/commands/)**.

## Security

A pattern scanner classifies findings as HIGH/MEDIUM/LOW with an action of skip/redact/include. Secrets are redacted by default before anything is written, and a file containing a private key (a `skip`-action secret) is **never** written into a plaintext backup or snapshot. On export, high-sensitivity files are added with `chezmoi add --encrypt`. See [Security & redaction](https://doguyilmaz.github.io/dothaven/docs/security/).

> The age key is the only thing protecting your encrypted files. Lose it and they are unrecoverable — back it up offline and never commit it.

## Development

```bash
go build ./...      # build
go test ./...       # unit + testscript e2e
gofmt -l ./cmd ./internal   # formatting (CI gate)
```

Layout: `cmd/dothaven` (entry point) + `internal/{snapshot,scan,sys,collect,registry,backup,restore,chezmoi,cli}`. See [Architecture](https://doguyilmaz.github.io/dothaven/docs/architecture/).

The docs site is Hugo + [Hextra](https://github.com/imfing/hextra): `cd docs && hugo server`.

## License

[MIT](./LICENSE).
