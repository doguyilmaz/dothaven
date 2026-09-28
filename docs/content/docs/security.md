---
title: Security
weight: 11
---

A backup of your dev setup is, among other things, a collection of your keys and tokens. This page explains exactly where secrets can end up, what dothaven does to keep them safe in each place, and why.

The short version:

- **Plain backups never hold a credential** (unless you pass `--no-redact`). Secret values are masked, credential files and private keys are left out, and everything left out is listed.
- **The encrypted backup holds everything**, and is never written unencrypted, not even for a moment.
- **Nothing is replaced on restore without asking**, and what is replaced is kept.
- **Tokens live in your keychain.** GitHub repos must be private. The dashboard is local and read-only.

All scanning and encryption happen inside the dothaven binary. Nothing is sent anywhere for analysis.

## Where a secret can end up

| Output | Secret values in files | Credential files (`~/.ssh`, `~/.aws/credentials`, …) | Files with a private key |
| --- | --- | --- | --- |
| `backup`, `backup --archive` | Masked as `[REDACTED]` | Left out, listed | Left out, listed |
| `backup --no-redact` | Raw | Included | Included, with a loud warning |
| `backup --encrypt` | Raw, inside the encryption | Included, encrypted | Included, encrypted |
| `github push` (encrypted, default) | Same as `--encrypt` | | |
| `github push --mode split` | Any file with a secret goes into the encrypted bundle | Encrypted bundle | Encrypted bundle |
| `github push --mode plain` | Same as a plain backup | | |
| `collect` snapshot | Masked | Their text is included, with secret values masked | Dropped |
| `scan` output | A masked preview, never the value | | |
| `dothaven ui` | File names and kinds only | | |
| `chezmoi-export` | Files with a HIGH secret are added encrypted | Added encrypted | Added encrypted |

Everything dothaven writes for itself (backups, snapshots, the security report, exported settings, the restore ledger) is owner-only (`0600` files, `0700` folders). A `collect` snapshot records credential files as present, never their contents.

## Plain backups: the redaction gate

Every file that goes into a plain backup passes a gate first:

1. **Credential entries are left out.** A registry entry marked high-sensitivity with no dedicated redactor, such as `~/.ssh`, `~/.aws/credentials`, kubeconfig, `~/.gnupg`, or a CLI's login file, is not copied at all. Why not scan and mask them instead? Because pattern matching is best-effort: an opaque token or binary key material has no recognisable shape. For files whose whole purpose is a secret, leaving them out is the only safe choice.
2. **Credential folders stay protected however they are reached.** If you `include ~/.aws`, or a broader entry wraps a credential folder, the files inside it are still left out. Paths are compared as written and as resolved, so symbolic links do not get around it: a stow-managed `~/.kube/config` that points into an included `~/.dotfiles` is still a credential, and so is a link in an included folder that points at `~/.aws/credentials`.
3. **Files with a private key are left out.** This is decided by content, not by file name: a PEM or PGP private-key block (also base64-encoded), GnuPG's binary key format, or an age identity, anywhere in the file. A private key cannot be partly masked into safety.
4. **Everything else is scanned and masked.** Matched secret values are replaced with `[REDACTED]`, every occurrence in the file. The rest of the file is kept so you can read it.
5. **Text files over 8 MiB are left out**, because they are too large to check. The encrypted backup carries them.

Everything left out is printed at the end of the backup and written to its `MANIFEST.txt`, so the backup can be checked for completeness later:

```text
⚠ 2 paths with credentials left out of this plaintext backup:
    cloud/aws/credentials
    ssh
  For a complete copy, keys included: dothaven backup --encrypt
```

On the way back, restore never writes a file that contains `[REDACTED]`, even with `--force`, because that would replace a working token with the placeholder. It lists those files instead, so you know which ones to fill in by hand.

`--no-redact` turns the gate off. It exists, but `--encrypt` gives the same completeness without the risk.

## The scanner

### Severity and action

Every rule has a **severity** (how serious a match is) and an **action** (what happens to it):

| Severity | What it catches |
| --- | --- |
| `HIGH` | Credentials and private keys |
| `MEDIUM` | Details that leak context: IP addresses, email addresses |
| `LOW` | Your home folder path (it contains your username) |

| Action | What happens |
| --- | --- |
| `skip` | The whole file (or snapshot section) is dropped |
| `redact` | The matched value is replaced with `[REDACTED]`; the rest is kept |
| `include` | Kept as it is; only reported |

When a file triggers several rules, the strongest action wins: `skip`, then `redact`, then `include`.

### What it detects

- **Private keys** (`HIGH`, skip): PEM private keys (`-----BEGIN … PRIVATE KEY-----`, which covers OpenSSH and RSA keys), the same base64-encoded once more (kubeconfig's `client-key-data`; a base64 CA certificate is left alone), PGP private key blocks, GnuPG's binary key format, and age identities (`AGE-SECRET-KEY-1…`, the key chezmoi and sops decrypt with).
- **Provider tokens** (`HIGH`, redact): AWS access, secret and session keys; Google API keys and OAuth tokens; Firebase; Azure SAS tokens; Cloudflare; DigitalOcean; Fly.io; GitHub (`ghp_`, `gho_`, `github_pat_`, …); npm tokens and `_authToken`; OpenAI; Anthropic; Stripe; Twilio; SendGrid; Mapbox; Slack; Discord; Supabase; Vercel; Pulumi; Vault; JWTs and bearer tokens; database connection strings (`postgres://`, `mysql://`, `mongodb://`, `redis://`); `.pgpass` lines; URLs with `user:password@` in them.
- **Generic secrets** (`HIGH`, redact): assignments whose name looks like a secret (`TOKEN`, `API_KEY`, `SECRET`, `PASSWORD`, `client_secret`, `access_token`, `refresh_token`, …), in shell, ini and JSON forms. These rules check the value first, so shell code that merely mentions a word (`token=$tokens[1]`, `if [[ $token == … ]]`) and placeholders (`your_token`, `xxx`) are not flagged.
- **IP addresses** (`MEDIUM`, redact), except loopback, `0.0.0.0` and netmasks, which every machine has. **Email addresses** (`MEDIUM`, include).
- **Your home folder path** (`LOW`, include).

A few formats are redacted in a way that keeps them valid: `HostName` and `IdentityFile` in `~/.ssh/config`, and `_authToken`, `_auth` and `_password` in `.npmrc`.

Redaction works line by line, the way the scan finds matches: a rule never reaches from one line into the next. (A key with an empty value, such as `token =` above `secret = x`, cannot hide the next line's value.) One secret is one finding, reported under its most specific rule.

macOS preference values are scanned together with their key, so an opaque token stored under a name like `syncApiToken` is caught too. Such a value is masked and marked review-only, never written back.

{{< callout type="info" >}}
**age keys never go to GitHub.** Your chezmoi or sops age key opens every encrypted file in your dotfiles repo. An encrypted file backup carries it, a plaintext backup never does, and no `github push` takes it, not even an encrypted one. See [Encryption](../encryption#the-chezmoi-path).
{{< /callout >}}

### Safe on hostile input

The scanner uses Go's RE2 regular expressions, which run in time linear in the input: no rule can be made to backtrack forever on a crafted file. When scanning a folder, it skips `.git`, `node_modules`, `vendor`, caches, virtual environments, cloud-storage mounts and files over 1 MiB.

### Scan output never shows the value

`dothaven scan` prints where each secret is and what kind it is, with a masked preview: the setting's name, a token's prefix and its last two characters. Scan output ends up in terminal scrollback, CI logs and screen shares.

```text
~/.aws/credentials
  L2 [HIGH] AWS access key: AKIA••••LE
  L3 [HIGH] AWS secret key: aws_secret_access_key =••••..
```

## Encrypted backups

`dothaven backup --encrypt` (and a GitHub push in the default mode) produces a standard [age](https://age-encryption.org) file protected by your passphrase.

- **No plaintext on disk.** Files stream from their place in your home folder through tar, gzip and age into the output file. There is no temporary unencrypted copy.
- **No half-written backups.** The file is written as `<name>.partial`, owner-only from the first byte, and renamed only once complete.
- **A strong enough passphrase.** At least 10 characters. age's passphrase mode uses scrypt, which slows guessing, but it cannot rescue a short word.
- **No recovery.** If you lose the passphrase, nobody can open the file, including you. Store it in your password manager.
- **Standard format.** `age -d backup-….tar.gz.age | tar -xz` also opens it, which matters if you ever need your backup without dothaven. dothaven builds age in, so neither machine needs the `age` program.
- **Passphrase input.** The prompt reads from the terminal directly (`/dev/tty`) without echoing. `DOTHAVEN_PASSPHRASE` is supported for scripts, but the prompt is the default because an environment variable is visible to every program the shell starts. dothaven reads it (and `DOTHAVEN_GITHUB_TOKEN`) once at startup and removes it from its environment, so the tools it runs never inherit it. Set but empty, or shorter than 10 characters, it is an error, never "no encryption".
- **Encrypted is checked, not assumed.** Writing a plain and an encrypted archive are separate functions, and the encrypted one refuses an empty passphrase. Before a GitHub push uploads a `.age` file, it checks the file really starts with an age header.

When you restore an encrypted backup, it is decrypted into a private temporary folder (`0700`), the files are written to their places, and the folder is deleted, also on a forced exit (a second Ctrl-C). A folder left behind by a crash or `kill -9` is removed the next time dothaven runs. Commands that only need a backup's inventory or settings (`missing`, `reinstall`, `defaults import`) write only that part; the rest, keys included, is decrypted in memory and never written.

## Restoring safely

- **Ask before replacing.** A file that already exists and differs is asked about on a terminal, with a diff, and kept off a terminal unless you pass `--force`.
- **Keep what is replaced.** Every file restore replaces is first copied, owner-only, to `~/.local/share/dothaven/pre-restore-<timestamp>/`.
- **Right permissions.** Credentials and sensitive config are written `0600`; executable files stay executable; a file that is stricter on this machine keeps its permissions.
- **Nothing replaced blind.** A file whose current version cannot be read (so no copy of it can be kept) is not replaced.
- **No writing through links.** If the file on this machine is a symbolic link, it is skipped and reported, instead of changing whatever the link points to.
- **No escaping.** An archive entry with an absolute path or `..` is refused; symbolic links and device files in an archive are never extracted; a single entry over 256 MiB is refused.
- **The ledger holds hashes.** `state/applied.json`, which remembers what was applied and what you declined, stores SHA-256 hashes and paths, never file contents.

## Where tokens and passphrases are kept

The GitHub token and the optional remembered backup passphrase go to your system's credential store: the macOS Keychain, or the Secret Service (GNOME Keyring, KWallet) on Linux. Values are handed to the keychain tool on stdin, never on a command line where other processes could read them.

If neither is available, dothaven falls back to an owner-only file under `~/.config/dothaven/credentials/`, and `dothaven doctor` warns about it. `DOTHAVEN_SECRET_STORE=file` chooses the file on purpose, for a headless machine.

## GitHub

- dothaven only writes to **private** repositories. It checks before every push and stops with an error on a public one. Even an encrypted backup shows which services you use.
- Encrypted and split modes upload credentials only inside age-encrypted archives.
- The token is sent only to the GitHub API over HTTPS, and never passed to git or put on a command line. A fine-grained token can be limited to the single backup repository.
- Commits are made as your account; there is no dothaven server in between.
- git history keeps every push. Anything that was ever in a readable (`split` or `plain`) push should be considered readable by anyone with access to the repository.

Details: [GitHub sync](../github#security-model).

## The dashboard

`dothaven ui` listens on `127.0.0.1` only, needs a random per-run key (swapped on first visit for an `HttpOnly`, `SameSite=Strict` cookie), checks the `Host` header against DNS rebinding, answers only `GET` and `HEAD`, sends a strict Content Security Policy, and loads no outside assets. The secrets panel shows file names and kinds, never values. Details: [Dashboard](../dashboard#security-model).

## Key material at a glance

| Key material | What dothaven does |
| --- | --- |
| SSH keys, `known_hosts`, `authorized_keys` (`~/.ssh`) | Encrypted backup only. `~/.ssh/config` also goes in plain backups, with `HostName` and `IdentityFile` masked. |
| GnuPG (`~/.gnupg`) | Encrypted backup only (sockets, locks and the random seed are skipped). |
| Cloud and CLI logins (AWS, GCP, Azure, kubeconfig, `gh`, Codex, …) | Encrypted backup only. |
| `.npmrc` | Both kinds; tokens masked in plain backups. |
| macOS Keychain items (signing certificates, saved passwords, app logins stored there) | Never read or copied. |
| The chezmoi age key | Not in the registry. Keep it separately (see the warning above). |

{{< cards >}}
  {{< card link="../backup-restore" title="Backup & restore" >}}
  {{< card link="../encryption" title="Encryption & chezmoi" >}}
  {{< card link="../github" title="GitHub sync" >}}
{{< /cards >}}
