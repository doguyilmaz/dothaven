---
title: Quick start
weight: 3
---

This walkthrough takes you from a fresh install to your first snapshot, a secrets
audit, and a diff between two machines. Every command here ships in the `dothaven`
binary — a single static Go executable with no runtime dependency.

## Install

dothaven is one self-contained binary. There is nothing to bootstrap and no
interpreter to keep around.

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

{{< callout type="info" >}}
dothaven handles discovery, audit, and export. Long-term storage,
age-encryption, and applying configs on a new machine are delegated to
[chezmoi](https://www.chezmoi.io/). The two are designed to work together.
{{< /callout >}}

## 1. Collect a snapshot

`dothaven collect` inventories the current machine — installed apps, Homebrew
formulae and casks, global packages, language runtimes, editor extensions,
fonts, SSH metadata, and tracked dotfiles — into a single timestamped JSON file.

```bash
dothaven collect
```

```text
Report saved to: /Users/you/projects/dotfiles/reports/your-host-20260604093000.json

⚠ Sensitivity report:
  HIGH   /Users/you/.npmrc                npm auth token — redacted
  HIGH   /Users/you/.ssh/id_ed25519       private key — skipped

  1 items redacted, 1 skipped. Use --no-redact to include all.
```

### Where the file lands

The output directory is resolved in this order:

1. An explicit `-o` / `--output` directory always wins.
2. Otherwise, if the working directory is a git repository, the file is written
   to `<cwd>/reports`.
3. Otherwise it falls back to `~/Downloads`.

The filename is `<hostname>-<UTC timestamp>.json`, where the timestamp is 14
digits in `YYYYMMDDHHMMSS` form.

### The sensitivity report

By default `collect` redacts secrets before they ever touch disk. As it builds
the snapshot it scans each section and prints the inline **Sensitivity report**
shown above. Each line is `SEVERITY  PATH  label — disposition`, where the
disposition is one of:

- **redacted** — the value is masked but the section is kept (tokens, API keys,
  passwords, connection strings).
- **skipped** — the whole section is dropped (private keys are never written
  out, even masked).
- **included** — kept as-is (low-risk matches such as a home-directory path).

A trailing summary tallies how many items were redacted or skipped.

To capture raw values instead, pass `--no-redact`. To keep long file contents
short (truncated to 10 lines), add `--slim`.

```bash
dothaven collect --no-redact          # keep raw values, skip redaction
dothaven collect --slim               # truncate long file bodies
dothaven collect -o ~/snapshots       # choose the output directory
```

{{< callout type="warning" >}}
`--no-redact` writes secrets to the snapshot in clear text. Only use it on files
you keep private, and never commit such a snapshot to a shared repository.
{{< /callout >}}

## 2. Scan for sensitive data

`collect` audits what goes into a snapshot. The standalone scanners let you point
the same secret detection at any file or directory.

### Console scan

`dothaven scan` walks a path and prints findings to the terminal. With no
argument it scans the current directory; directory walks skip `node_modules` and
`.git` and ignore files larger than 1 MiB.

```bash
dothaven scan ~
```

```text
/Users/you/.npmrc
  L1 [HIGH] npm auth token: //registry.npmjs.org/:_authToken=npm_xxxxxxx...

/Users/you/.aws/credentials
  L3 [HIGH] AWS secret key: aws_secret_access_key = wJalrXUtnF...

⚠ Sensitivity report:
  HIGH   /Users/you/.npmrc                npm auth token — redacted
  HIGH   /Users/you/.aws/credentials      AWS secret key — redacted

  2 items redacted. Use --no-redact to include all.
```

Each finding line is `L<line> [SEVERITY] <label>: <match>`, sorted with the
highest severity first. Severities are `HIGH`, `MEDIUM`, and `LOW`. If nothing
matches, `scan` prints `No sensitive data found.`

### Markdown report

`dothaven security` runs the same scan but writes a grouped Markdown report
instead of printing detail to the console. It defaults to `SECURITY.md` in the
current directory; override with `-o` / `--output`.

```bash
dothaven security ~
```

```text
Security report written to: SECURITY.md
  142 scanned, 3 with findings.
```

The report groups files by their top severity (HIGH / MEDIUM / LOW) and notes
the disposition for each:

```markdown
# Security Report

142 file(s) scanned · 3 with findings · 2 to redact · 1 to skip.

## 🔴 HIGH — secrets (masked or skipped before sync)
- `/Users/you/.aws/credentials` — AWS secret key · redact · L3
- `/Users/you/.npmrc` — npm auth token · redact · L1
- `/Users/you/.ssh/id_ed25519` — private key · skip (private key) · L1
```

## 3. Inspect a section

`dothaven list <section>` prints one section from the most recent snapshot in
`./reports`. The section name is fuzzy-matched, so a short query expands to any
section whose name (or any dot-delimited part of it) contains it.

```bash
dothaven list brew
```

```text
[apps.brew.casks]
  ghostty
  raycast
  visual-studio-code

[apps.brew.formulae]
  fzf
  jq
  ripgrep
```

Section names follow a dotted convention — for example `meta`, `apps.macos`,
`apps.brew.formulae`, `apps.brew.casks`, `packages.npm.global`, `runtimes.go`,
`runtimes.rust`, `fonts.user`, and `ai.ollama.models`. Because the match is
fuzzy, `list runtimes` prints every `runtimes.*` section and `list ollama` finds
`ai.ollama.models`.

If no snapshot exists yet, `list` reminds you to run `dothaven collect` first.

## 4. Compare two snapshots

`dothaven compare` diffs two JSON snapshots and prints only what changed. Give it
two files explicitly, or omit the arguments to compare the two newest reports in
`./reports`.

```bash
dothaven compare reports/old-host-20260101120000.json reports/new-host-20260604093000.json
```

```text
+ [apps.brew.casks]  (only in old-host-20260101120000)
  + orbstack  (only in old-host-20260101120000)

[meta]
  ~ date = 2026-01-01 → 2026-06-04

[packages.npm.global]
  + typescript  (only in old-host-20260101120000)
  - eslint  (only in old-host-20260604093000)
```

The labels are the file basenames (without `.json`). The prefixes read:

- `+` — present only in the first (left) file.
- `-` — present only in the second (right) file.
- `~` — a key-value pair whose value changed (`old → new`).

A section header wrapped in `+ [...]` or `- [...]` means the whole section is
new or gone. When the two snapshots are identical, `compare` prints
`No differences found.` Color is used automatically when stdout is a terminal.

```bash
dothaven compare      # diff the two newest reports in ./reports
```

## 5. Migrate with chezmoi

Once a snapshot looks right, hand the actual files off to chezmoi for storage,
age-encryption, and applying on another machine. `dothaven chezmoi-export` builds
the plan: plain `chezmoi add` for ordinary configs, `chezmoi add --encrypt` for
anything containing a high-severity secret, and `chezmoi add --template` for
host-varying configs (shell rc, gitconfig, editor settings) — whose absolute home
paths are rewritten to `{{ .chezmoi.homeDir }}` so they port across machines.

It is a dry run by default — nothing is changed until you pass `--apply`.

```bash
dothaven chezmoi-export
```

```text
chezmoi-export plan — 4 path(s), 1 encrypted:

     add            /Users/you/.gemini/GEMINI.md  (plain)
  📝 add --template  /Users/you/.gitconfig  (templated (host paths))
  📝 add --template  /Users/you/.zshrc  (templated (host paths))
  🔒 add --encrypt  /Users/you/.ssh/id_ed25519  (ssh private key)
```

On the fresh machine, `dothaven migrate` runs the other half: it checks the
prerequisites and applies your chezmoi source (pulling configs and running the
install script). See [Commands](../commands#migrate).

Useful flags:

- `--apply` — execute the plan (requires `chezmoi` and `age` to be installed).
- `--pin` — pin global packages to the version captured in the snapshot.
- `--only` / `--skip` — comma-separated categories to include or exclude
  (for example `--only ssh,brew`).

{{< callout type="warning" >}}
age is the encryption backend. If you lose your age key, the encrypted files in
your chezmoi repository are unrecoverable. Back the key up somewhere safe before
you rely on `--apply`.
{{< /callout >}}

## Where to go next

{{< cards >}}
  {{< card link="../commands" title="Commands" subtitle="Full reference for every subcommand and flag" >}}
  {{< card link="../installation" title="Installation" subtitle="All install methods and requirements" >}}
{{< /cards >}}
