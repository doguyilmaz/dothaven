---
title: Security & redaction
weight: 9
---

dothaven audits dotfiles for secrets before they ever leave your machine. Every file and snapshot section is run through a built-in scanner that classifies what it finds, then either masks the value, drops the file entirely, or keeps it with a warning. This page describes how the scanner decides, what it detects, and how that protection wires into backups and exports.

The scanner is pure Go — it ships inside the single static `dothaven` binary with no external service, network call, or runtime dependency. Nothing is uploaded for analysis.

## Severity and action

Every detection rule carries two independent dimensions: a **severity** (how alarming the match is) and an **action** (what the scanner does about it).

Severity is informational — it ranks findings in reports so the worst offenders sort first:

| Severity | Meaning |
| --- | --- |
| `HIGH` | Credentials and private keys — tokens, API keys, connection strings, certificates. |
| `MEDIUM` | Identifiers that leak context — IP addresses, email addresses. |
| `LOW` | Local environment leakage — your home directory path. |

Action is what actually happens to the data:

| Action | Behavior |
| --- | --- |
| `skip` | Drop the whole file or section. Nothing from it is written. |
| `redact` | Replace the matched value with a marker, keep the rest. |
| `include` | Keep the value as-is, only surface it in the report. |

### Action priority

A single file can trigger many rules at once. The scanner resolves them to **one** action per file using a strict priority:

```text
skip  >  redact  >  include
```

So a file containing both an email address (`include`) and an AWS access key (`redact`) is redacted. A file containing a private key (`skip`) and a dozen redactable tokens is skipped outright — the entire file is dropped, because once a `skip`-action secret is present, masking the rest no longer makes the file safe to write. A file with no findings defaults to `include`.

## What the scanner detects

The rule set is grouped by category. The summary below describes coverage rather than listing every regex.

**Private keys & certificates** (`HIGH`, `skip`) — PEM private-key blocks (`-----BEGIN ... PRIVATE KEY-----`) and PGP private-key blocks. These are the only rules with the `skip` action: a private key cannot be partially masked into safety, so its file is dropped.

**Cloud & provider tokens** (`HIGH`, `redact`) — AWS access/secret/session keys, Google API keys and OAuth tokens, Firebase and Cloudflare tokens, GitHub PATs (`ghp_`, `gho_`, `github_pat_`, …), npm tokens, OpenAI and Anthropic keys, Stripe, Twilio, SendGrid, Mapbox, Slack, Discord, Supabase, Vercel, JWTs, bearer tokens, npm `_authToken`, database connection strings (`postgres://`, `mysql://`, `mongodb://`, `redis://`), and URLs with inline `user:pass@` credentials.

**Generic secrets** (`HIGH`, `redact`) — key/value assignments whose name looks like a secret: `TOKEN`, `KEY`, `SECRET`, `PASSWORD`, `CREDENTIALS`, `api_key`, `client_secret`, `access_token`, `refresh_token`, and similar, matched against `=` or `:` assignment forms. This catches `.env`-style secrets that don't match a known provider format.

**IP & email** (`MEDIUM`) — IPv4 addresses are redacted; email addresses are `include` (kept, reported only) so ordinary config that mentions an email isn't mangled.

**Home directory path** (`LOW`, `include`) — paths under `/Users/<you>/` or `/home/<you>/`, detected for the current OS user and reported so you know your username appears in a file. This rule is only registered when the current user can be resolved.

{{< callout type="info" >}}
Email and home-path findings use the `include` action — they are surfaced in the report but never altered. They exist to inform, not to block.
{{< /callout >}}

## The `[REDACTED]` marker

Redacted values are replaced with the literal string `[REDACTED]`. When a file's resolved action is `redact`, each matching pattern runs a global replace over the content, so every occurrence of every triggered secret on every line is masked — not just the first.

A few targeted redactors preserve structure instead of blanking the line, so the file stays valid after masking:

- **SSH config** — `HostName` and `IdentityFile` values become `[REDACTED]`, keeping the keyword so the config still parses. Matching is case-insensitive, so lowercase `hostname` / `identityfile` are caught too.
- **npm `.npmrc`** — `_authToken=`, plus the legacy `_auth=` and `_password=` forms, keep their key and mask only the value.
- **IP addresses** — replaced in place (octets are bounded to 0–255 so version strings like `1.2.3.400` aren't mistaken for IPs).

The same marker is what `restore` looks for to recognize a file as already-redacted, so a masked backup is never mistaken for clean data on the way back in.

## The skip gate

This is the core safety guarantee:

{{< callout type="warning" >}}
A file whose content scans to the `skip` action — a private key — is **never** written into a plaintext backup or snapshot. It is dropped at the gate, before any bytes touch disk.
{{< /callout >}}

During backup, every file passes through a gate that scans its content first. If redaction is on and the action is `skip`, the gate returns "do not write" and the file is excluded from the backup directory entirely. Redactable files are masked and written; clean files are copied as-is.

The gate is content-based, not filename-based. A private key named `id_ed25519`, `id_rsa`, or `vault.key` is caught by its PEM/PGP header, not by a known filename. The same applies to snapshot sections: `RedactSection` scrubs section content, key/value pairs (both values **and** keys, since a token can be a JSON key), and list items, dropping the entire section if its content scans to `skip`. No section type can bypass the gate.

## RE2 safety

The scanner uses Go's standard `regexp` package, which is backed by the RE2 engine. Every pattern is written without lookaround or backreferences, so matching is guaranteed **linear time** in the size of the input. There is no catastrophic backtracking: a hostile or pathological file cannot wedge the scanner into exponential blowup. Patterns are compiled once on first use and reused for the rest of the run.

Files larger than 1 MiB are skipped during directory scans, and `node_modules` and `.git` subtrees are pruned, so scanning a real home directory stays fast.

## The sensitivity report

Two report formats are produced from the same findings.

**Inline summary** — printed automatically after `collect` and `backup` when there are findings. One line per file with its top severity, path, top label, and what happened to it, followed by a tally:

```text
⚠ Sensitivity report:
  HIGH   ~/.aws/credentials             AWS access key — redacted
  HIGH   ~/.ssh/id_ed25519              private key — skipped
  MEDIUM ~/.ssh/config                  IP address — redacted

  2 items redacted, 1 skipped. Use --no-redact to include all.
```

**Standalone scan** — the `scan` command prints a detailed, per-finding breakdown to the console:

```bash
dothaven scan ~/.config
```

```text
~/.aws/credentials
  L4 [HIGH] AWS access key: aws_access_key_id = AKIAIOSFODNN7EXAMPLE
  L5 [HIGH] AWS secret key: aws_secret_access_key = wJalrXUtnFEMI/K7MDE...

⚠ Sensitivity report:
  HIGH   ~/.aws/credentials             AWS access key — redacted

  1 items redacted. Use --no-redact to include all.
```

Run with no path to scan the current directory. Pass a file to scan just that file, or a directory to scan it recursively.

**Markdown report** — the `security` command writes a grouped Markdown report (`SECURITY.md` by default) for review or commit:

```bash
dothaven security ~/.config
dothaven security ~/.config -o reports/audit.md
```

The report groups files by top severity (HIGH / MEDIUM / LOW), each line showing the path, top label, the action (`redact`, `skip (private key)`, or `keep`), and the line number. A clean scan writes a short "No sensitive data found" report instead.

| Command | Output | Default destination |
| --- | --- | --- |
| `scan [path]` | Detailed console breakdown + summary | stdout |
| `security [path]` | Grouped Markdown report | `SECURITY.md` (override with `-o`) |

## How redaction interacts with backups

Redaction is **on by default** for `collect` and `backup`. Every file and section runs through the scanner: private keys are dropped at the skip gate, redactable secrets are masked to `[REDACTED]`, and benign findings are reported. The inline sensitivity report is printed at the end of the run.

To disable redaction and copy raw values, pass `--no-redact` to either command:

```bash
dothaven backup --no-redact
```

With `--no-redact`, the skip gate is bypassed too — raw private keys and unmasked secrets are written. Use it only when the destination is itself encrypted or otherwise trusted.

## How redaction interacts with export

The `chezmoi-export` command takes the opposite approach for `HIGH`-severity secrets: instead of masking them, it flags those files to be stored **encrypted** by chezmoi using age. Export uses the same scanner to detect a `HIGH` finding (only `HIGH` — a benign IP or email never forces encryption), and a content-detected private key is routed to encrypted storage rather than dropped. In the hybrid model, dothaven does discovery and audit; chezmoi does storage, age-encryption, and apply.

{{< callout type="warning" >}}
age is the encryption backend for exported secrets. Losing the age key means the encrypted files are unrecoverable. Back up the key separately and securely.
{{< /callout >}}

## Key-material policy

Private keys are the one thing you cannot reinstall with a package manager, so it's worth knowing exactly what dothaven moves, what it never touches, and what you have to carry yourself. The rule is: **dothaven migrates key material only through the encrypted `chezmoi-export` path, never into a plaintext backup — and only for the categories you select.**

| Key material | What dothaven does |
| --- | --- |
| **SSH private keys** (`~/.ssh/id_*`) | Detected by content (a key header, not a filename) and added age-encrypted on `chezmoi-export` — **only when the `ssh` category is selected**. Never written to a plaintext backup or snapshot. |
| **GnuPG** (`~/.gnupg`) | Carried age-encrypted on export when real secret keys exist (`private-keys-v1.d/*.key`); runtime cruft (sockets, locks, `random_seed`) is ignored. Never plaintext. |
| **Other credentials** (cloud creds, `kubeconfig`, `.npmrc`, `.netrc`, …) | `HIGH`-sensitivity, so age-encrypted on export and excluded from a plaintext backup unless a targeted redactor can mask them. |
| **The age key** (`~/.config/chezmoi/key.txt`) | **Never touched.** This is the one secret dothaven will not move for you. |
| **Not handled** — `known_hosts`, `authorized_keys`, `.git-credentials`, the `.pub` half of a key pair, the macOS Keychain | Out of scope today. Regenerate or carry them manually. |

{{< callout type="warning" >}}
**Two ways to lose data permanently — both avoidable:**

1. **Forgetting to select `ssh`.** If you don't tick the `ssh` category, your private keys are silently left behind. Select it before you wipe the source machine.
2. **Losing the age key.** It is never in your repo, so if you encrypt your whole secret estate against it and then lose the only copy, every exported secret is unrecoverable — strictly worse than migrating nothing. Back the key up offline *before* you run `chezmoi-export --apply`.
{{< /callout >}}

## Related

{{< cards >}}
  {{< card link="../backup-restore" title="Backup & restore" subtitle="Where the skip gate and --no-redact apply" >}}
  {{< card link="../encryption" title="Encryption & export" subtitle="age-encrypted storage via chezmoi" >}}
  {{< card link="../commands" title="Commands" subtitle="scan, security, backup, collect" >}}
{{< /cards >}}
