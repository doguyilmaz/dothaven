package scan

import (
	"encoding/base64"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
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

// kw is mk for keyword rules (see Pattern.keyword).
func kw(id, label string, sev Severity, action Action, re string) Pattern {
	p := mk(id, label, sev, action, re)
	p.keyword = true
	return p
}

// needs records text every match of the rule contains; see Pattern.need.
// For a case-insensitive rule it is compared lowercased.
func (p Pattern) needs(lits ...string) Pattern {
	p.fold = strings.HasPrefix(p.re.String(), "(?i)")
	p.need = make([]string, len(lits))
	for i, l := range lits {
		if p.fold {
			l = strings.ToLower(l)
		}
		p.need[i] = l
	}
	return p
}

func (p Pattern) prefilter(f func(string) bool) Pattern {
	p.pre = f
	return p
}

// hasDottedQuad reports text with a digit, a dot and a digit in a row, which
// every IPv4 address has.
func hasDottedQuad(s string) bool {
	for i := 1; i+1 < len(s); i++ {
		if s[i] == '.' && s[i-1] >= '0' && s[i-1] <= '9' && s[i+1] >= '0' && s[i+1] <= '9' {
			return true
		}
	}
	return false
}

// atLeast is a prefilter for rules that need n of one character.
func atLeast(c byte, n int) func(string) bool {
	return func(s string) bool { return strings.Count(s, string(c)) >= n }
}

// checked attaches a vetting function to a rule.
func (p Pattern) inConfigOnly() Pattern {
	p.configOnly = true
	return p
}

// codeExts are source code and web assets: files a person does not keep
// settings in by hand.
var codeExts = map[string]bool{
	".js": true, ".mjs": true, ".cjs": true, ".jsx": true, ".ts": true, ".tsx": true, ".mts": true, ".cts": true,
	".map": true, ".svg": true, ".css": true, ".scss": true, ".sass": true, ".less": true, ".html": true, ".htm": true,
	".vue": true, ".svelte": true, ".astro": true, ".py": true, ".pyi": true, ".rb": true, ".go": true, ".rs": true,
	".java": true, ".kt": true, ".scala": true, ".swift": true, ".c": true, ".h": true, ".cc": true, ".cpp": true,
	".hpp": true, ".cs": true, ".php": true, ".dart": true, ".ex": true, ".exs": true, ".snap": true,
}

func isCode(path string) bool { return codeExts[strings.ToLower(filepath.Ext(path))] }

func checked(p Pattern, check func(string) bool) Pattern {
	p.check = check
	return p
}

// meaningfulIP drops the addresses every machine has: loopback, "any",
// broadcast and netmasks. Redacting `DOCKER_HOST=tcp://127.0.0.1:2375` hides
// nothing and, in a plaintext backup, makes the whole .zshrc unrestorable.
func meaningfulIP(m string) bool {
	return !strings.HasPrefix(m, "127.") && m != "0.0.0.0" && !strings.HasPrefix(m, "255.")
}

// base64PrivateKey keeps the base64 PEM rule to private keys: a CA
// certificate (certificate-authority-data) is encoded the same way and is
// public.
func base64PrivateKey(m string) bool {
	n := len(m) / 4 * 4
	if n > 64 {
		n = 64 // the header is all that is needed
	}
	b, err := base64.StdEncoding.DecodeString(m[:n])
	return err == nil && strings.Contains(string(b), "PRIVATE KEY")
}

func build() {
	patterns = []Pattern{
		// HIGH: private keys and certs (skip whole file)
		mk("private-key-pem", "private key", High, Skip, `-----BEGIN[A-Z0-9 ]*PRIVATE KEY-----`).needs("-----BEGIN"),
		mk("pgp-private-key", "PGP private key", High, Skip, `-----BEGIN PGP PRIVATE KEY BLOCK-----`).needs("-----BEGIN PGP"),
		// GnuPG agent key material is a binary Libgcrypt s-expression such as
		// "(21:protected-private-key", not PEM, so the PEM rule above misses it.
		mk("gpg-sexp-private-key", "GnuPG private key", High, Skip, `\(\d{1,3}:(protected-|shadowed-)?private-key`).needs("private-key"),
		// An age identity: what chezmoi and sops decrypt with. Losing it loses
		// every file encrypted to it; leaking it opens all of them.
		// The post-quantum identity (age-keygen -pq) is AGE-SECRET-KEY-PQ-1….
		mk("age-secret-key", "age private key", High, Skip, `AGE-SECRET-KEY-(PQ-)?1[0-9A-Z]{50,}`).needs("AGE-SECRET-KEY-"),
		// A PEM private key, base64-encoded once more: kubeconfig's
		// client-key-data, CI variables. "LS0tLS1CRUdJTi" is "-----BEGIN".
		checked(mk("private-key-pem-b64", "private key (base64)", High, Skip, `LS0tLS1CRUdJTi[A-Za-z0-9+/]{16,}`), base64PrivateKey).needs("LS0tLS1CRUdJTi"),

		// HIGH: generic env-style secrets. The `["']?` before the delimiter lets
		// these fire on JSON (`"token": "v"`) as well as shell/ini (`TOKEN=v`); a
		// quote between the keyword and the colon otherwise defeats the match.
		// Space around the delimiter is [ \t]*, never \s*: \s crosses a line
		// break, and `token =` above `secret = x` would then mask the second
		// line's key and leave its value in the file.
		kw("generic-secret", "secret value", High, Redact, `\b([A-Z0-9]+_)*(TOKEN|KEY|SECRET|PASSWORD|PASSWD|CREDENTIALS?)\b["']?[ \t]*[=:][ \t]*\S+`).needs("TOKEN", "KEY", "SECRET", "PASSWORD", "PASSWD", "CREDENTIAL"),
		kw("generic-api-key", "API key", High, Redact, `(?i)(API_KEY|APIKEY)["']?[ \t]*[=:][ \t]*\S+`).needs("api_key", "apikey"),
		kw("secret-keyword", "secret value", High, Redact, `(?i)\b(password|passwd|secret|token|client[_-]?secret|secret[_-]?key|api[_-]?key|apikey|api[_-]?secret|api[_-]?token|access[_-]?key|access[_-]?token|auth[_-]?token|refresh[_-]?token|session[_-]?token|personal[_-]?access[_-]?token|private[_-]?key)\b["']?[ \t]*[=:][ \t]*\S+`).needs("passw", "secret", "token", "key"),

		// HIGH: auth tokens and prefixed keys
		kw("auth-token-npm", "npm auth token", High, Redact, `(?i)\b_(authToken|auth|password)[ \t]*=[ \t]*\S+`).needs("_auth", "_password"),
		mk("bearer-token", "bearer token", High, Redact, `Bearer[ \t]+[A-Za-z0-9\-._~+/]{20,}=*`).needs("Bearer"),
		mk("github-token", "GitHub token", High, Redact, `\b(ghp_[A-Za-z0-9]{36,}|gho_[A-Za-z0-9]{36,}|ghu_[A-Za-z0-9]{36,}|ghs_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b`).needs("ghp_", "gho_", "ghu_", "ghs_", "github_pat_"),
		mk("npm-token", "npm token", High, Redact, `\bnpm_[A-Za-z0-9]{36,}\b`).needs("npm_"),

		// HIGH: AI provider keys
		mk("openai-key", "OpenAI key", High, Redact, `\bsk-(proj-)?[A-Za-z0-9]{20,}\b`).needs("sk-"),
		mk("anthropic-key", "Anthropic key", High, Redact, `\bsk-ant-[A-Za-z0-9-]{20,}\b`).needs("sk-ant-"),

		// HIGH: cloud provider keys
		mk("aws-access-key", "AWS access key", High, Redact, `\bAKIA[0-9A-Z]{16}\b`).needs("AKIA"),
		mk("aws-secret-key", "AWS secret key", High, Redact, `(?i)aws_secret_access_key[ \t]*=[ \t]*.+`).needs("aws_secret_access_key"),
		mk("aws-session-token", "AWS session token", High, Redact, `(?i)\b[a-z0-9_]*session[_-]?token[ \t]*=[ \t]*.+`).needs("session"),
		mk("google-api-key", "Google API key", High, Redact, `\bAIza[A-Za-z0-9\-_]{35}\b`).needs("AIza"),
		mk("google-oauth-token", "Google OAuth token", High, Redact, `\bya29\.[A-Za-z0-9\-_]+\b`).needs("ya29."),
		mk("firebase-key", "Firebase key", High, Redact, `\bAAAA[A-Za-z0-9\-_:]{100,}\b`).needs("AAAA"),
		mk("cloudflare-token", "Cloudflare token", High, Redact, `\bv1\.0-[A-Fa-f0-9]{24,}\b`).needs("v1.0-"),

		// HIGH: payment and SaaS keys
		mk("stripe-key", "Stripe key", High, Redact, `\b(sk_live_|sk_test_|pk_live_|pk_test_|rk_live_|rk_test_)[A-Za-z0-9]{20,}\b`).needs("_live_", "_test_"),
		mk("mapbox-token", "Mapbox token", High, Redact, `\b(pk|sk)\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\b`).needs(".eyJ"),
		mk("twilio-key", "Twilio key", High, Redact, `\bSK[0-9a-fA-F]{32}\b`).needs("SK"),
		mk("sendgrid-key", "SendGrid key", High, Redact, `\bSG\.[A-Za-z0-9\-_]{22,}\.[A-Za-z0-9\-_]{22,}\b`).needs("SG."),

		// HIGH: messaging platform tokens
		mk("slack-token", "Slack token", High, Redact, `\b(xoxb|xoxp|xoxs|xoxa|xoxr)-[A-Za-z0-9-]+\b`).needs("xox"),
		mk("discord-token", "Discord token", High, Redact, `\b[MN][A-Za-z0-9]{23,}\.[A-Za-z0-9\-_]{6}\.[A-Za-z0-9\-_]{27,}\b`).prefilter(atLeast('.', 2)),

		// HIGH: database and credentialed URLs
		mk("database-url", "database connection string", High, Redact, `(?i)\b(postgres|postgresql|mysql|mongodb|mongodb\+srv|redis|rediss)://[^\s"']+`).needs("postgres", "mysql", "mongodb", "redis"),
		mk("url-credentials", "URL with inline credentials", High, Redact, `(?i)\b[a-z][a-z0-9+.-]*://[^\s:@/]+:[^\s@/]+@`).needs("@"),

		// HIGH: Supabase, Vercel, JWT
		mk("supabase-key", "Supabase key", High, Redact, `\bsbp_[A-Za-z0-9]{40,}\b`).needs("sbp_"),
		mk("vercel-token", "Vercel token", High, Redact, `\b(vc_prod_|vc_test_)[A-Za-z0-9]{20,}\b`).needs("vc_prod_", "vc_test_"),
		mk("jwt-token", "JWT token", High, Redact, `\beyJhbGciOi[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\b`).needs("eyJhbGciOi"),

		// HIGH: infra/hosting provider tokens (distinctive prefixes)
		mk("digitalocean-token", "DigitalOcean token", High, Redact, `\bdop_v1_[a-f0-9]{64}\b`).needs("dop_v1_"),
		mk("vault-token", "Vault token", High, Redact, `\bhv[sb]\.[A-Za-z0-9._-]{20,}\b`).needs("hvs.", "hvb."),
		mk("pulumi-token", "Pulumi token", High, Redact, `\bpul-[a-f0-9]{40}\b`).needs("pul-"),
		mk("flyio-token", "Fly.io token", High, Redact, `\bfm[12]_[A-Za-z0-9+/=_-]{20,}\b`).needs("fm1_", "fm2_"),
		mk("azure-sas", "Azure SAS token", High, Redact, `(?i)\bsig=[A-Za-z0-9%]{40,}`).needs("sig="),
		// .pgpass line: host:port:db:user:password. (?m) so it matches per line
		// during a whole-file redact too; the digit/* port field keeps PATH
		// exports, IPv6, and /etc/passwd-style lines from false-matching.
		mk("pgpass-line", "pgpass credentials", High, Redact, `(?m)^[^:#\s]+:(?:\d+|\*):[^:]*:[^:]*:.+$`).prefilter(atLeast(':', 4)),

		// MEDIUM
		checked(mk("ip-address", "IP address", Medium, Redact, `\b(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)(\.(25[0-5]|2[0-4]\d|1\d\d|[1-9]?\d)){3}\b`), meaningfulIP).prefilter(hasDottedQuad).inConfigOnly(),
		mk("email-address", "email address", Medium, Include, `\b[\w.+-]+@[\w-]+\.[\w.]+\b`).needs("@").inConfigOnly(),
	}

	if u := username(); u != "" {
		patterns = append(patterns, mk("home-path", "home directory path", Low, Include,
			`/(Users|home)/`+regexp.QuoteMeta(u)+`/`).needs("/Users/", "/home/"))
	}
}

func username() string {
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}
