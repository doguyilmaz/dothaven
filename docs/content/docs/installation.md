---
title: Installation
weight: 2
---

dothaven is one static binary. It needs no interpreter and no runtime, and encryption is built in: you do not need to install age to make or open encrypted backups.

## Install

{{< tabs >}}
  {{< tab name="Fresh machine" >}}
The installer script is the fastest way onto a freshly wiped machine. It needs no Homebrew and no sudo:

```bash
curl -fsSL https://raw.githubusercontent.com/doguyilmaz/dothaven/main/scripts/install.sh | sh
```

It downloads the release for your OS and CPU, checks it against the release's published SHA-256 checksums (and refuses to install on a mismatch), and puts `dothaven` in `~/.local/bin`. If that folder is not on your `PATH` yet, it prints the line to add.

Why not Homebrew here? On a new Mac, Homebrew needs the Xcode command-line tools first, which takes several minutes before you can restore anything.

You can set these before running it:

| Variable | Default | Meaning |
| --- | --- | --- |
| `DOTHAVEN_VERSION` | latest release | A tag such as `v1.4.0` |
| `DOTHAVEN_BIN_DIR` | `~/.local/bin` | Where to install |
| `DOTHAVEN_BASE_URL` | the GitHub releases page | Download from a mirror |
  {{< /tab >}}
  {{< tab name="Homebrew (macOS)" >}}
```bash
brew install --cask doguyilmaz/tap/dothaven
```

The macOS binaries are signed and notarized.
  {{< /tab >}}
  {{< tab name="Go" >}}
```bash
go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest
```

This puts `dothaven` in `$(go env GOPATH)/bin`. Make sure that folder is on your `PATH`.
  {{< /tab >}}
  {{< tab name="Release binary" >}}
Download `dothaven_<os>_<arch>.tar.gz` from the [releases page](https://github.com/doguyilmaz/dothaven/releases), check it against `checksums.txt`, unpack it and move `dothaven` somewhere on your `PATH`.
  {{< /tab >}}
  {{< tab name="Source" >}}
```bash
git clone https://github.com/doguyilmaz/dothaven.git
cd dothaven
go build ./cmd/dothaven
```

The binary lands in the current folder. A source build reports its version as `dev`.
  {{< /tab >}}
{{< /tabs >}}

## Check it works

```bash
dothaven --version
dothaven doctor
```

`doctor` checks that dothaven can do its job on this machine: its folders and their permissions, free disk space, the keychain, the tools each feature uses, and GitHub if you are signed in. It changes nothing. See [Doctor & troubleshooting](../troubleshooting).

## Supported platforms

| OS | Architectures |
| --- | --- |
| macOS | `amd64`, `arm64` |
| Linux | `amd64`, `arm64` |

Windows is not supported.

## What else you might want

dothaven runs on its own. A few features call tools you may already have; `dothaven doctor` shows which are present.

| Tool | Used for | If it is missing |
| --- | --- | --- |
| `git` | `ready` (finding unpushed work) | `ready` cannot check repositories |
| Homebrew | Listing and reinstalling Homebrew apps (macOS) | The Homebrew part of the inventory and of `reinstall` is skipped |
| `defaults` | macOS settings (built into macOS) | — |
| `gh` | Signing in to GitHub without a browser | Use `github login --with-token` instead |
| `zsh`, `ssh` | `check` of zsh files and SSH config | Those files are reported as unchecked |
| `chezmoi` | The optional chezmoi sync | Only `init`, `chezmoi-export --apply` and `migrate` need it |

The chezmoi path also needs an age key for chezmoi (see [Encryption](../encryption#the-chezmoi-path)). Encrypted dothaven backups do not.

## Updating

```bash
dothaven upgrade          # also: dothaven update
dothaven upgrade --check  # see what is available, change nothing
```

`upgrade` works out how dothaven was installed and runs that installer's own upgrade. For Homebrew that is `brew update && brew upgrade --cask dothaven`; for `go install` it is `go install …@latest`. For anything else it prints the releases page. dothaven never overwrites its own binary, because Homebrew tracks the version it installed and would be left describing a file that no longer exists.

On a terminal, dothaven checks at most once a day for a newer release and prints one line on stderr when there is one. Set `DOTHAVEN_NO_UPDATE_CHECK=1` to turn it off. See [the update notice](../commands#the-update-notice).

## Where dothaven keeps things

| What | Where |
| --- | --- |
| Backups, snapshots, the restore ledger | `~/.local/share/dothaven` (or `$XDG_DATA_HOME/dothaven`) |
| Your include list, GitHub settings | `~/.config/dothaven` (or `$XDG_CONFIG_HOME/dothaven`) |
| The GitHub token and remembered passphrase | Your system keychain (see [GitHub sync](../github#where-the-token-is-kept)) |
| The update check cache | `~/.cache/dothaven` (or `$XDG_CACHE_HOME/dothaven`) |

## Shell completion

```bash
dothaven completion zsh      # also: bash, fish, powershell
source <(dothaven completion zsh)
```

Run `dothaven completion --help` for how to install it permanently.

## Next steps

{{< cards >}}
  {{< card link="../quick-start" title="Quick start" >}}
  {{< card link="../migration" title="Moving to a new machine" >}}
  {{< card link="../commands" title="Commands" >}}
{{< /cards >}}
