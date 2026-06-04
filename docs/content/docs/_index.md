---
title: Introduction
weight: 1
---

dothaven inventories your machine's development configuration, scans it for secrets, and hands the safe-to-share parts to [chezmoi](https://www.chezmoi.io/) for age-encrypted storage and migration across machines.

## The problem

Moving to a freshly installed machine means rebuilding the parts of your environment that never lived in a project repository: SSH keys, shell and editor configs, Homebrew formulae and casks, globally installed packages (Bun, npm, pnpm, Deno, Cargo), language runtimes, fonts, AI tooling, and the assorted secrets scattered through dotfiles. These live in your home directory, drift over time, and are easy to lose track of. You usually only notice what was missing after you needed it.

dothaven's job is to make that surface area visible and portable. It walks your machine, records what it finds in a timestamped JSON snapshot, flags anything that looks like a credential, and prepares an export that chezmoi can encrypt and replay on the next machine.

## The hybrid model

dothaven and chezmoi split the work along a clean line. dothaven is the discovery and audit layer; chezmoi is the storage and apply layer.

| Stage | Owner | What happens |
| --- | --- | --- |
| Discovery | dothaven | Walks the machine, builds a snapshot of installed config |
| Audit | dothaven | Scans snapshots and files for secrets, redacts by default |
| Export | dothaven | Builds a `chezmoi add` plan and a `run_onchange` install script |
| Storage | chezmoi | Tracks files in a private source repository |
| Encryption | chezmoi + age | Encrypts the files dothaven marked as secret |
| Apply | chezmoi | Writes files back into `$HOME` on a new machine |

The `chezmoi-export` command is the seam between the two tools. It produces a plan — plain `add` for ordinary configs, `add --encrypt` for anything containing a high-severity secret — and, when run with `--apply`, calls the real `chezmoi` binary to execute it. [age](https://github.com/FiloSottile/age) is the encryption backend chezmoi uses.

{{< callout type="warning" >}}
age is the only thing standing between an attacker and your encrypted files. If you lose the age key, the encrypted files are unrecoverable. Back the key up somewhere safe and separate from the source repository.
{{< /callout >}}

## What it captures

A `collect` run assembles a snapshot from a pipeline of collectors. Across them, dothaven inventories:

- **Host metadata** — hostname and OS, used to label the snapshot
- **SSH** — keys and config in `~/.ssh`
- **Homebrew** — formulae and casks (as a `Brewfile`)
- **macOS applications** — installed app inventory
- **Global packages** — Bun, npm, pnpm, and Deno globals
- **Runtimes** — language runtimes and Cargo crates
- **Node versions** — managed via fnm
- **Editor extensions** — installed editor/IDE extensions
- **Fonts** — installed font families
- **AI / Ollama tooling** — local model and AI tool config
- **Dotfiles** — a sweep of common configuration files in your home directory

Categories such as `ai`, `cloud`, `editor`, `git`, `shell`, `terminal`, `ssh`, and `secrets` come from a declarative registry, so a single source of truth drives both collection and backup.

Secret redaction is on by default. `collect` and `backup` redact matched credentials before anything is written, and print a summary of what was found. Pass `--no-redact` only when you deliberately want the raw values.

## What it does not do

dothaven is not an encryption or sync engine, and it does not replace chezmoi.

- It does **not** encrypt files itself — encryption is chezmoi + age.
- It does **not** store or version your configuration — that is your private chezmoi source repository.
- It does **not** apply files to a new machine — `chezmoi apply` does that.
- It does **not** manage the age key — generating and safeguarding the key is on you.

In dry-run mode (the default) `chezmoi-export` only prints a plan; nothing is written or encrypted until you add `--apply`, and `--apply` requires both `chezmoi` and a configured age key to be present.

## What it is

dothaven is a single static Go binary — a CLI built with [Cobra](https://github.com/spf13/cobra). It has no runtime dependency: no interpreter, no package manager, nothing to install alongside it. You drop the binary on a machine and run it.

The module path is `github.com/doguyilmaz/dothaven` and the main package is `./cmd/dothaven`.

{{< tabs >}}
  {{< tab name="Homebrew" >}}```bash
brew install doguyilmaz/tap/dothaven
```{{< /tab >}}
  {{< tab name="Go" >}}```bash
go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest
```{{< /tab >}}
  {{< tab name="Source" >}}```bash
git clone https://github.com/doguyilmaz/dothaven
cd dothaven
go build ./cmd/dothaven
```{{< /tab >}}
{{< /tabs >}}

## A first run

`collect` takes inventory and writes a timestamped JSON snapshot. The output location is resolved automatically: an explicit `-o` always wins, otherwise dothaven writes to `./reports` when the current directory is a git repository, and falls back to `~/Downloads` when it is not.

```bash
$ dothaven collect
Report saved to: /Users/you/project/reports/your-mac-20260604153000.json
```

From there, `doctor` compares a snapshot against the current machine and lists what is missing — a parity check that returns a non-zero exit code on drift, which makes it usable in CI:

```bash
$ dothaven doctor reports/your-mac-20260604153000.json
Missing on this machine (present in the snapshot):

  packages.bun.global (2)
    - @biomejs/biome
    - typescript

2 item(s) missing across 1 section(s).
```

## Where to next

{{< cards >}}
  {{< card link="installation" title="Installation" subtitle="Install dothaven via Homebrew, Go, or source." >}}
  {{< card link="quick-start" title="Quick start" subtitle="Collect, audit, and export in a few commands." >}}
  {{< card link="commands" title="Commands" subtitle="Every command, flag, and exit code." >}}
  {{< card link="migration" title="Migration" subtitle="Move a full setup to a clean-install machine." >}}
{{< /cards >}}
