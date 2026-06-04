# Encryption & the Secret Gate

This tool never stores secrets in plaintext. There are two complementary mechanisms:

1. **Redaction** (on `collect` / `backup`) — the sensitivity scanner masks or skips secret
   values in `.dotf` snapshots and structured backups.
2. **Encryption** (on `chezmoi-export`) — secrets are handed to chezmoi with `--encrypt`, which
   encrypts them with [age](https://age-encryption.org) so they can be carried — even committed to a
   private repo — and decrypted on `apply`.

Encryption is the right tool for migration: most keys are painful to regenerate, so you want to
**keep** them (encrypted), not just redact them away.

---

## age key lifecycle

```bash
brew install age
age-keygen -o ~/key.txt        # prints the public recipient: age1...
```

Configure chezmoi (`~/.config/chezmoi/chezmoi.toml`):

```toml
encryption = "age"
[age]
  identity = "~/key.txt"       # the PRIVATE key
  recipient = "age1..."        # the PUBLIC key
```

**Rules:**

- The private key (`key.txt`) is the one thing that must **never** enter any repo. The tool only ever
  emits `chezmoi add --encrypt`; it does not read, copy, or commit your identity.
- **Back it up** to a password manager or an offline medium. **Lose it → you cannot decrypt** anything.
- Optionally protect the key itself with a passphrase and store `key.txt.age`, decrypting it into place
  on a new machine before the first `chezmoi apply`.

---

## Secret gate

`chezmoi-export` decides plain vs encrypted per file:

| Condition | Action |
|-----------|--------|
| Registry entry marked `sensitivity: high` (ssh keys, `~/.gnupg`, `~/.aws/credentials`, `~/.npmrc`, kube/docker config) | `chezmoi add --encrypt` |
| Any file whose **content the scanner flags** as a secret (e.g. a `GITHUB_TOKEN=` in `.zshrc`) | `chezmoi add --encrypt` (reason: *secret detected*) |
| Everything else (no detected secret) | `chezmoi add` (plain) |

The consequence: **a file containing a secret is never added in plaintext.** Even a "low-sensitivity"
shell rc gets encrypted the moment it holds a token. Review the plan with a dry run before `--apply`:

```bash
dotfiles chezmoi-export          # dry run — shows 🔒 for each encrypted path + the reason
dotfiles chezmoi-export --apply
```

---

## What the scanner catches

Env-style secrets (`GITHUB_TOKEN`, `*_KEY`, `*_TOKEN`, `SECRET`, `PASSWORD`), vendor-prefixed keys
(GitHub `ghp_`, OpenAI `sk-`, AWS `AKIA…`, Stripe, Slack, …), private-key headers (PEM/PGP → whole
file skipped), database URLs, and URLs carrying inline `user:password@` credentials. Redaction is
global (every occurrence on a line), and matches are name-based so it won't false-positive on
`primary_key` or `monkey`. Run `dotfiles security <path>` for a standalone report.
