# Sensitivity and Redaction

The sensitivity system ensures sensitive data is never silently included in reports or backups. It runs automatically on every `collect` and `backup` — you don't need to think about it unless you want to override it.

## Pipeline

Every file processed goes through a three-stage pipeline:

```
Content → Detection (regex per line) → Classification (HIGH/MEDIUM/LOW) → Action (skip/redact/include)
```

1. **Detection**: each line is tested against 27+ regex patterns
2. **Classification**: each match is tagged with a severity level
3. **Action**: the highest-severity finding determines the file-level action

## Severity Levels

| Level | Meaning | Default Action |
|-------|---------|----------------|
| **HIGH** | Credentials that grant access — private keys, API keys, tokens, passwords, database URLs | `skip` for private keys, `redact` for everything else |
| **MEDIUM** | Potentially identifying information — IP addresses, email addresses | `redact` for IPs, `include` for emails |
| **LOW** | Machine-specific paths — home directory with username | `include` |

## Actions

| Action | What Happens |
|--------|-------------|
| `skip` | Entire file is dropped from output — not included in report or backup |
| `redact` | Matched values are replaced with `[REDACTED]` — file structure preserved |
| `include` | File included as-is — finding is logged in the sensitivity report |

The **highest-severity action wins** per file. If a file has both a `skip` finding (private key) and a `redact` finding (auth token), the file is skipped entirely.

## Complete Pattern Reference

### HIGH Severity — Private Keys (action: `skip`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `private-key-pem` | private key | `/-----BEGIN.*PRIVATE KEY-----/` | `-----BEGIN RSA PRIVATE KEY-----` |
| `pgp-private-key` | PGP private key | `/-----BEGIN PGP PRIVATE KEY BLOCK-----/` | PGP private key block header |

These patterns cause the **entire file to be skipped** — private key files should never appear in reports or backups.

### HIGH Severity — Generic Secrets (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `generic-secret` | secret value | `/(PASSWORD\|SECRET_KEY\|API_SECRET\|PRIVATE_KEY\|AUTH_TOKEN\|ACCESS_TOKEN\|SECRET)\s*[=:]\s*\S+/i` | `APP_SECRET=my-secret-value` |
| `generic-api-key` | API key | `/(API_KEY\|APIKEY)\s*[=:]\s*\S+/i` | `SOME_API_KEY=abc123def456` |

::: info No leading `\b`
These patterns intentionally omit `\b` before the keyword because `_` is a word character — `APP_SECRET` would not match `\bSECRET` since the boundary falls between `_` and `S`.
:::

### HIGH Severity — Auth Tokens (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `auth-token-npm` | npm auth token | `/_authToken=.+/` | `_authToken=secret-token-123` |
| `bearer-token` | bearer token | `/Bearer\s+[A-Za-z0-9\-._~+/]+=*/` | `Authorization: Bearer eyJhbG...` |
| `github-token` | GitHub token | `/\b(ghp_\|gho_\|ghu_\|ghs_\|github_pat_)...\b/` | `ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZab...` |
| `npm-token` | npm token | `/\bnpm_[A-Za-z0-9]{36,}\b/` | `npm_aBcDeFgHiJkLmNoPqRsTuVwXyZ` |

### HIGH Severity — AI Provider Keys (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `openai-key` | OpenAI key | `/\bsk-(proj-)?[A-Za-z0-9]{20,}\b/` | `sk-proj-1234567890abcdefghij...` |
| `anthropic-key` | Anthropic key | `/\bsk-ant-[A-Za-z0-9\-]{20,}\b/` | `sk-ant-api03-abcdefghijklmno...` |

### HIGH Severity — Cloud Provider Keys (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `aws-access-key` | AWS access key | `/\bAKIA[0-9A-Z]{16}\b/` | `AKIAIOSFODNN7EXAMPLE` |
| `aws-secret-key` | AWS secret key | `/aws_secret_access_key\s*=\s*.+/i` | `aws_secret_access_key = wJalrXU...` |
| `google-api-key` | Google API key | `/\bAIza[A-Za-z0-9\-_]{35}\b/` | `AIzaSyBcDeFgHiJkLmNoPqRsTuV...` |
| `google-oauth-token` | Google OAuth token | `/\bya29\.[A-Za-z0-9\-_]+\b/` | `ya29.a0AfH6SMBx...` |
| `firebase-key` | Firebase key | `/\bAAAA[A-Za-z0-9\-_:]{100,}\b/` | Firebase server key |
| `cloudflare-token` | Cloudflare token | `/\bv1\.0-[A-Fa-f0-9]{24,}\b/` | Cloudflare API token |

### HIGH Severity — Payment & SaaS Keys (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `stripe-key` | Stripe key | `/\b(sk_live_\|sk_test_\|pk_live_\|pk_test_\|rk_live_\|rk_test_)...\b/` | `sk_live_abcdefghijklmnopqrstuv` |
| `mapbox-token` | Mapbox token | `/\b(pk\|sk)\.eyJ...\b/` | `pk.eyJhbGciOi.abcdef123456` |
| `twilio-key` | Twilio key | `/\bSK[0-9a-fA-F]{32}\b/` | Twilio API key |
| `sendgrid-key` | SendGrid key | `/\bSG\.[A-Za-z0-9\-_]{22,}\.[A-Za-z0-9\-_]{22,}\b/` | `SG.abcdef...wxyz123...` |

### HIGH Severity — Messaging Platforms (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `slack-token` | Slack token | `/\b(xoxb\|xoxp\|xoxs\|xoxa\|xoxr)-...\b/` | `xoxb-123456789012-abcdefghij` |
| `discord-token` | Discord token | `/\b[MN][A-Za-z0-9]{23,}\.…\b/` | Discord bot token |

### HIGH Severity — Database & Infrastructure (action: `redact`)

| Pattern ID | Label | Regex | Example Match |
|-----------|-------|-------|---------------|
| `database-url` | database connection string | `/\b(postgres\|postgresql\|mysql\|mongodb\|mongodb\+srv\|redis\|rediss):\/\/...\b/i` | `postgres://user:pass@host:5432/db` |
| `supabase-key` | Supabase key | `/\bsbp_[A-Za-z0-9]{40,}\b/` | Supabase project API key |
| `vercel-token` | Vercel token | `/\b(vc_prod_\|vc_test_)[A-Za-z0-9]{20,}\b/` | Vercel deployment token |
| `jwt-token` | JWT token | `/\beyJhbGciOi...\b/` | `eyJhbGciOiJIUzI1NiIs...` (3-part base64) |

### MEDIUM Severity

| Pattern ID | Label | Regex | Default Action | Example |
|-----------|-------|-------|----------------|---------|
| `ip-address` | IP address | `/\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b/` | `redact` | `192.168.1.100` |
| `email-address` | email address | `/\b[\w.+-]+@[\w-]+\.[\w.]+\b/` | `include` | `dev@example.com` |

### LOW Severity

| Pattern ID | Label | Regex | Default Action | Example |
|-----------|-------|-------|----------------|---------|
| `home-path` | home directory path | `/(Users\|home)/<username>/` | `include` | `/Users/dogu/...` |

::: tip Dynamic Pattern
The `home-path` pattern is generated at runtime using `os.userInfo().username`. If the username can't be determined, this pattern is omitted.
:::

## Pattern Caching

Patterns are generated once and cached in memory (`cachedPatterns`). The username lookup for `home-path` happens only on first access.

## Custom Redaction Functions

Some config entries have **custom redaction** that runs before pattern-based scanning:

| Entry | Function | What It Does |
|-------|----------|-------------|
| SSH config | `redactSshConfig()` | Replaces `HostName <value>` lines with `HostName [REDACTED]` |
| npm config | `redactNpmTokens()` | Replaces `_authToken=<value>` with `_authToken=[REDACTED]` |

Custom redaction preserves file structure while removing specific sensitive values. Pattern-based scanning then handles anything the custom function missed.

## Match Truncation

All matches in scan findings are truncated to **40 characters**. If the original match is longer, it's cut and `...` is appended (total max: 43 chars). This prevents sensitive data from appearing in scan report output.

## Sensitivity Report

After every `collect` and `backup` (with redaction enabled), a summary is printed:

```
⚠ Sensitivity report:
  HIGH   ~/.ssh/id_ed25519         private key — skipped
  HIGH   ~/.npmrc                  auth token — redacted
  MEDIUM ~/.gitconfig              email address — included

  2 items redacted, 1 skipped. Use --no-redact to include all.
```

Each line shows:
- Severity level of the top finding
- File path (padded to 30 chars)
- Pattern label and action taken

Files with no findings are omitted from the report.

## Overriding Redaction

```bash
dothaven collect --no-redact    # Include everything
dothaven backup --no-redact     # Backup without redaction
```

::: danger Use with caution
`--no-redact` disables **all** sensitivity handling. Private keys, API tokens, and database passwords will be included in plain text. Only use this when you fully control the storage destination (e.g., an encrypted, private repository).
:::

## How Redaction Works Internally

The `applyRedactions()` function:

1. Takes file content and a `ScanResult`
2. If the result action is not `redact`, returns content unchanged
3. For each finding, replaces the matched text with `[REDACTED]` using the pattern's regex
4. The `REDACTION_MARKER` constant (`"[REDACTED]"`) is defined in `src/utils/constants.ts` and used consistently across the codebase

The restore system checks for `REDACTION_MARKER` in backup files — any file containing it gets `redacted` status and is automatically skipped during restore to prevent writing masked values to the machine.
