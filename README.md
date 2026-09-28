<p align="center">
  <img src="docs/static/images/logo.svg" width="96" height="96" alt="dothaven logo: a dot and a tilde flowing out of an old window into a new green one">
</p>

<h1 align="center">dothaven</h1>

<p align="center">
  Move your dev setup to a new machine: dotfiles, AI tools, SSH keys, cloud logins,<br>
  installed apps and macOS settings. The backup is encrypted, and you choose what to restore.
</p>

<p align="center">
  <a href="https://doguyilmaz.github.io/dothaven"><b>Docs</b></a> ·
  <a href="https://doguyilmaz.github.io/dothaven/docs/migration/">Migration guide</a> ·
  <a href="https://doguyilmaz.github.io/dothaven/docs/commands/">Commands</a>
</p>

---

Migration Assistant copies a disk. A clean install gives you a fresh machine, and then a week of
finding out what you forgot: the zsh plugin config, the Claude Code skills and MCP servers, the
kubeconfig, the git hooks, the scroll direction. dothaven is for the clean install.

| | |
|---|---|
| **One file, everything in it** | `dothaven backup --encrypt` writes one age-encrypted file with your config, keys and tokens, the list of apps and packages you had, and your macOS settings. Nothing is ever written unencrypted, not even temporarily. |
| **Knows where things live** | Hundreds of config locations (shells, git, editors, terminals, cloud CLIs, SSH, databases, version managers), plus the global setup of Claude Code, Codex, Gemini, Cursor, Windsurf, VS Code, opencode, Copilot and more: skills, agents, commands, hooks, plugins, MCP servers. |
| **Not limited to that list** | Anything else that looks like config is offered to you once, and `dothaven include` adds any path you name. |
| **Restore what you choose** | Pick everything, some categories, or single files; see a diff before anything is replaced; the replaced copy is kept. It remembers what it applied, so a second run shows what is already done instead of offering everything again. |
| **Apps come back too** | `dothaven reinstall` installs only what is missing: Homebrew formulae, casks and App Store apps, global npm/pnpm/bun/pipx/uv/cargo packages, editor extensions. |
| **Or keep it on GitHub** | `dothaven github push` keeps each machine in a private repo, encrypted by default; `dothaven restore github` brings it back anywhere. |
| **Checks before you wipe** | `dothaven ready` finds uncommitted, unpushed and stashed work, repos with no remote, and gitignored `.env` files and keys that a fresh clone won't bring back. |

## Install

```bash
# On a fresh machine, without Homebrew (checks the release checksum, no sudo):
curl -fsSL https://raw.githubusercontent.com/doguyilmaz/dothaven/main/scripts/install.sh | sh

brew install --cask doguyilmaz/tap/dothaven                      # macOS (signed, notarized)
go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest    # macOS or Linux
```

One static binary with encryption built in; nothing else is needed. Binaries for every
platform are on the [releases page](https://github.com/doguyilmaz/dothaven/releases).

## Moving to a new machine

```bash
# On the old machine
dothaven ready                                   # anything that exists only here?
dothaven backup --encrypt -o /Volumes/MyDrive    # everything, in one encrypted file

# On the new machine
dothaven restore                                 # finds the backup (drives, Downloads…) and lets you pick
dothaven reinstall /Volumes/MyDrive/backup-….tar.gz.age    # apps & packages you had
dothaven missing   /Volumes/MyDrive/backup-….tar.gz.age    # anything still not here
```

Or run `dothaven` with no arguments: a menu walks you through the same steps, including a
**Pack everything** flow that checks for unpushed work, asks where to write the file, and
verifies that it decrypts before you wipe anything.

Prefer GitHub to a drive?

```bash
dothaven github push       # old machine: private repo <you>/dothaven-backup, encrypted
dothaven restore github    # new machine
```

## Everyday

```bash
dothaven status            # what changed since the last backup
dothaven include --list    # what nothing covers yet
dothaven scan              # secrets sitting in your config files (exits 2 on HIGH)
dothaven check             # do your config files still parse?
dothaven ui                # a local dashboard in your browser
dothaven doctor            # is dothaven itself set up right on this machine?
```

## Safety

- **Plaintext backups never hold a credential.** Secrets are redacted (the key name stays, the
  value goes), and SSH keys, cloud logins and other credential files are left out. Everything left
  out is listed in the output and in the backup's `MANIFEST.txt`. The encrypted backup carries them.
- **Encrypted means age.** Backups are standard [age](https://age-encryption.org) files: they
  open with `age -d` too, and old ones from earlier versions still restore.
- **Nothing you have is replaced without asking.** Restores preview, ask per differing file,
  and keep what they replace. Off a terminal, commands that change things need `--yes`.
- **Tokens live in your keychain** (macOS Keychain or the Secret Service), never on a command line,
  and never in the environment of a tool dothaven runs.
- **The GitHub repo must be private.** dothaven refuses to write to a public one. Every `.age`
  file is checked before upload, and your chezmoi/sops age key never goes to GitHub, not even encrypted.
- **Decrypted files don't linger.** Temporary folders are owner-only, removed even on a forced exit,
  and swept on the next run after a crash; read-only commands unpack only what they read.
- **The dashboard is local and read-only**: loopback only, one-time key, strict CSP, no outside assets.

What stays out on purpose: data (databases, Docker volumes), system config under `/etc`, app
bundles, caches and toolchains (they are reinstalled, not copied), and Keychain items such as
signing certificates. See [Security](https://doguyilmaz.github.io/dothaven/docs/security/).

## Also

- `dothaven defaults import <backup>` puts macOS settings back and shows which are already set.
- `dothaven services export` saves Homebrew service configs (nginx, mysql, redis…).
- `dothaven chezmoi-export` and `migrate` hand your config to a [chezmoi](https://chezmoi.io)
  repo instead, with secrets age-encrypted, if you want machines kept in sync.

Full reference: **[Commands](https://doguyilmaz.github.io/dothaven/docs/commands/)**.
Missing a tool? [Request it](https://github.com/doguyilmaz/dothaven/issues/new?template=config-request.yml),
or add it yourself with `dothaven include`.

## Development

```bash
go build ./...                 # build
go test ./...                  # unit + testscript e2e (a fake GitHub included)
gofmt -l ./cmd ./internal      # must be empty; also: go vet ./...
cd docs && hugo server         # docs preview
```

Layout: `cmd/dothaven` (entry) + `internal/…`. See [Architecture](https://doguyilmaz.github.io/dothaven/docs/architecture/).

## License

[MIT](./LICENSE).
