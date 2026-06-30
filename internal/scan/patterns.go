package scan

import (
	"os/user"
	"regexp"
	"sync"
)

var (
	patternsOnce sync.Once
	patterns     []Pattern
)

// Patterns returns the secret-detection rule set (compiled once). All regexes
// are RE2-safe (no lookaround/backreferences), so matching is linear-time.
func Patterns() []Pattern {
	patternsOnce.Do(build)
	return patterns
}

func mk(id, label string, sev Severity, action Action, re string) Pattern {
	return Pattern{ID: id, Label: label, Severity: sev, Action: action, re: regexp.MustCompile(re)}
}

func build() {
	patterns = []Pattern{
		// HIGH — private keys & certs (skip whole file)
		mk("private-key-pem", "private key", High, Skip, `-----BEGIN.*PRIVATE KEY-----`),
		mk("pgp-private-key", "PGP private key", High, Skip, `-----BEGIN PGP PRIVATE KEY BLOCK-----`),
		// GnuPG agent key material is a binary Libgcrypt s-expression, not PEM —
		// e.g. "(21:protected-private-key" — so the PEM rule above misses it.
		mk("gpg-sexp-private-key", "GnuPG private key", High, Skip, `\(\d{1,3}:(protected-|shadowed-)?private-key`),

		// HIGH — generic env-style secrets. The `["']?` before the delimiter lets
		// these fire on JSON (`"token": "v"`) as well as shell/ini (`TOKEN=v`); a
		// quote between the keyword and the colon otherwise defeats the match.
		mk("generic-secret", "secret value", High, Redact, `\b([A-Z0-9]+_)*(TOKEN|KEY|SECRET|PASSWORD|PASSWD|CREDENTIALS?)\b["']?\s*[=:]\s*\S+`),
		mk("generic-api-key", "API key", High, Redact, `(?i)(API_KEY|APIKEY)["']?\s*[=:]\s*\S+`),
		mk("secret-keyword", "secret value", High, Redact, `(?i)\b(password|passwd|secret|token|client[_-]?secret|secret[_-]?key|api[_-]?key|apikey|api[_-]?secret|api[_-]?token|access[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|session[_-]?token|personal[_-]?access[_-]?token|private[_-]?key)\b["']?\s*[=:]\s*\S+`),

		// HIGH — auth tokens & prefixed keys
		mk("auth-token-npm", "npm auth token", High, Redact, `(?i)\b_(authToken|auth|password)\s*=\s*\S+`),
		mk("bearer-token", "bearer token", High, Redact, `Bearer\s+[A-Za-z0-9\-._~+/]{20,}=*`),
		mk("github-token", "GitHub token", High, Redact, `\b(ghp_[A-Za-z0-9]{36,}|gho_[A-Za-z0-9]{36,}|ghu_[A-Za-z0-9]{36,}|ghs_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`),
		mk("npm-token", "npm token", High, Redact, `\bnpm_[A-Za-z0-9]{36,}\b`),

		// HIGH — AI provider keys
		mk("openai-key", "OpenAI key", High, Redact, `\bsk-(proj-)?[A-Za-z0-9]{20,}\b`),
		mk("anthropic-key", "Anthropic key", High, Redact, `\bsk-ant-[A-Za-z0-9-]{20,}\b`),

		// HIGH — cloud provider keys
		mk("aws-access-key", "AWS access key", High, Redact, `\bAKIA[0-9A-Z]{16}\b`),
		mk("aws-secret-key", "AWS secret key", High, Redact, `(?i)aws_secret_access_key\s*=\s*.+`),
		mk("aws-session-token", "AWS session token", High, Redact, `(?i)\b[a-z0-9_]*session[_-]?token\s*=\s*.+`),
		mk("google-api-key", "Google API key", High, Redact, `\bAIza[A-Za-z0-9\-_]{35}\b`),
		mk("google-oauth-token", "Google OAuth token", High, Redact, `\bya29\.[A-Za-z0-9\-_]+\b`),
		mk("firebase-key", "Firebase key", High, Redact, `\bAAAA[A-Za-z0-9\-_:]{100,}\b`),
		mk("cloudflare-token", "Cloudflare token", High, Redact, `\bv1\.0-[A-Fa-f0-9]{24,}\b`),

		// HIGH — payment & SaaS keys
		mk("stripe-key", "Stripe key", High, Redact, `\b(sk_live_|sk_test_|pk_live_|pk_test_|rk_live_|rk_test_)[A-Za-z0-9]{20,}\b`),
		mk("mapbox-token", "Mapbox token", High, Redact, `\b(pk|sk)\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\b`),
		mk("twilio-key", "Twilio key", High, Redact, `\bSK[0-9a-fA-F]{32}\b`),
		mk("sendgrid-key", "SendGrid key", High, Redact, `\bSG\.[A-Za-z0-9\-_]{22,}\.[A-Za-z0-9\-_]{22,}\b`),

		// HIGH — messaging platform tokens
		mk("slack-token", "Slack token", High, Redact, `\b(xoxb|xoxp|xoxs|xoxa|xoxr)-[A-Za-z0-9-]+\b`),
		mk("discord-token", "Discord token", High, Redact, `\b[MN][A-Za-z0-9]{23,}\.[A-Za-z0-9\-_]{6}\.[A-Za-z0-9\-_]{27,}\b`),

		// HIGH — database & credentialed URLs
		mk("database-url", "database connection string", High, Redact, `(?i)\b(postgres|postgresql|mysql|mongodb|mongodb\+srv|redis|rediss)://[^\s"']+`),
		mk("url-credentials", "URL with inline credentials", High, Redact, `(?i)\b[a-z][a-z0-9+.-]*://[^\s:@/]+:[^\s@/]+@`),

		// HIGH — Supabase / Vercel / JWT
		mk("supabase-key", "Supabase key", High, Redact, `\bsbp_[A-Za-z0-9]{40,}\b`),
		mk("vercel-token", "Vercel token", High, Redact, `\b(vc_prod_|vc_test_)[A-Za-z0-9]{20,}\b`),
		mk("jwt-token", "JWT token", High, Redact, `\beyJhbGciOi[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\b`),

		// HIGH — infra/hosting provider tokens (distinctive prefixes)
		mk("digitalocean-token", "DigitalOcean token", High, Redact, `\bdop_v1_[a-f0-9]{64}\b`),
		mk("vault-token", "Vault token", High, Redact, `\bhv[sb]\.[A-Za-z0-9._-]{20,}\b`),
		mk("pulumi-token", "Pulumi token", High, Redact, `\bpul-[a-f0-9]{40}\b`),
		mk("flyio-token", "Fly.io token", High, Redact, `\bfm[12]_[A-Za-z0-9+/=_-]{20,}\b`),
		mk("azure-sas", "Azure SAS token", High, Redact, `(?i)\bsig=[A-Za-z0-9%]{40,}`),
		// .pgpass line: host:port:db:user:password. (?m) so it matches per line
		// during a whole-file redact too; the digit/* port field keeps PATH
		// exports, IPv6, and /etc/passwd-style lines from false-matching.
		mk("pgpass-line", "pgpass credentials", High, Redact, `(?m)^[^:#\s]+:(?:\d+|\*):[^:]*:[^:]*:.+$`),

		// MEDIUM
		mk("ip-address", "IP address", Medium, Redact, `\b(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`),
		mk("email-address", "email address", Medium, Include, `\b[\w.+-]+@[\w-]+\.[\w.]+\b`),
	}

	if u := username(); u != "" {
		patterns = append(patterns, mk("home-path", "home directory path", Low, Include,
			`/(Users|home)/`+regexp.QuoteMeta(u)+`/`))
	}
}

func username() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}
