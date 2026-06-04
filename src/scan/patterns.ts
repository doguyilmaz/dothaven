import { userInfo } from "node:os";
import type { ScanPattern } from "./types";

function getUsername(): string {
  try {
    return userInfo().username;
  } catch {
    return "";
  }
}

let cachedPatterns: ScanPattern[] | null = null;

export function getScanPatterns(): ScanPattern[] {
  if (cachedPatterns) return cachedPatterns;

  const username = getUsername();

  const patterns: ScanPattern[] = [
    // --- HIGH: Private keys & certificates (skip entire file) ---
    {
      id: "private-key-pem",
      label: "private key",
      severity: "HIGH",
      regex: /-----BEGIN.*PRIVATE KEY-----/,
      defaultAction: "skip",
    },
    {
      id: "pgp-private-key",
      label: "PGP private key",
      severity: "HIGH",
      regex: /-----BEGIN PGP PRIVATE KEY BLOCK-----/,
      defaultAction: "skip",
    },

    // --- HIGH: Generic secret patterns (env-style) ---
    {
      // UPPER_SNAKE env-style names ending in a secret word: GITHUB_TOKEN, GH_TOKEN,
      // NPM_TOKEN, TOKEN, API_TOKEN, MY_SERVICE_KEY, AWS_SECRET_ACCESS_KEY, SECRET, PASSWORD…
      // Case-sensitive so it won't false-positive on lowercase words like primary_key / monkey.
      id: "generic-secret",
      label: "secret value",
      severity: "HIGH",
      regex: /\b([A-Z0-9]+_)*(TOKEN|KEY|SECRET|PASSWORD|PASSWD|CREDENTIALS?)\b\s*[=:]\s*\S+/,
      defaultAction: "redact",
    },
    {
      id: "generic-api-key",
      label: "API key",
      severity: "HIGH",
      regex: /(API_KEY|APIKEY)\s*[=:]\s*\S+/i,
      defaultAction: "redact",
    },
    {
      // Lowercase-friendly known secret words (no arbitrary prefix → avoids primary_key/monkey).
      id: "secret-keyword",
      label: "secret value",
      severity: "HIGH",
      regex:
        /\b(password|passwd|secret|client[_-]?secret|secret[_-]?key|api[_-]?secret|access[_-]?token|auth[_-]?token|refresh[_-]?token|session[_-]?token|private[_-]?key)\b\s*[=:]\s*\S+/i,
      defaultAction: "redact",
    },

    // --- HIGH: Auth tokens & prefixed keys ---
    {
      id: "auth-token-npm",
      label: "npm auth token",
      severity: "HIGH",
      regex: /_authToken=.+/,
      defaultAction: "redact",
    },
    {
      id: "bearer-token",
      label: "bearer token",
      severity: "HIGH",
      regex: /Bearer\s+[A-Za-z0-9\-._~+/]+=*/,
      defaultAction: "redact",
    },
    {
      id: "github-token",
      label: "GitHub token",
      severity: "HIGH",
      regex:
        /\b(ghp_[A-Za-z0-9]{36,}|gho_[A-Za-z0-9]{36,}|ghu_[A-Za-z0-9]{36,}|ghs_[A-Za-z0-9]{36,}|github_pat_[A-Za-z0-9_]{22,})\b/,
      defaultAction: "redact",
    },
    {
      id: "npm-token",
      label: "npm token",
      severity: "HIGH",
      regex: /\bnpm_[A-Za-z0-9]{36,}\b/,
      defaultAction: "redact",
    },

    // --- HIGH: AI provider keys ---
    {
      id: "openai-key",
      label: "OpenAI key",
      severity: "HIGH",
      regex: /\bsk-(proj-)?[A-Za-z0-9]{20,}\b/,
      defaultAction: "redact",
    },
    {
      id: "anthropic-key",
      label: "Anthropic key",
      severity: "HIGH",
      regex: /\bsk-ant-[A-Za-z0-9-]{20,}\b/,
      defaultAction: "redact",
    },

    // --- HIGH: Cloud provider keys ---
    {
      id: "aws-access-key",
      label: "AWS access key",
      severity: "HIGH",
      regex: /\bAKIA[0-9A-Z]{16}\b/,
      defaultAction: "redact",
    },
    {
      id: "aws-secret-key",
      label: "AWS secret key",
      severity: "HIGH",
      regex: /aws_secret_access_key\s*=\s*.+/i,
      defaultAction: "redact",
    },
    {
      // STS temporary credentials — lowercase aws config form (also any *_session_token).
      id: "aws-session-token",
      label: "AWS session token",
      severity: "HIGH",
      regex: /\b[a-z0-9_]*session[_-]?token\s*=\s*.+/i,
      defaultAction: "redact",
    },
    {
      id: "google-api-key",
      label: "Google API key",
      severity: "HIGH",
      regex: /\bAIza[A-Za-z0-9\-_]{35}\b/,
      defaultAction: "redact",
    },
    {
      id: "google-oauth-token",
      label: "Google OAuth token",
      severity: "HIGH",
      regex: /\bya29\.[A-Za-z0-9\-_]+\b/,
      defaultAction: "redact",
    },
    {
      id: "firebase-key",
      label: "Firebase key",
      severity: "HIGH",
      regex: /\bAAAA[A-Za-z0-9\-_:]{100,}\b/,
      defaultAction: "redact",
    },
    {
      id: "cloudflare-token",
      label: "Cloudflare token",
      severity: "HIGH",
      regex: /\bv1\.0-[A-Fa-f0-9]{24,}\b/,
      defaultAction: "redact",
    },

    // --- HIGH: Payment & SaaS keys ---
    {
      id: "stripe-key",
      label: "Stripe key",
      severity: "HIGH",
      regex: /\b(sk_live_|sk_test_|pk_live_|pk_test_|rk_live_|rk_test_)[A-Za-z0-9]{20,}\b/,
      defaultAction: "redact",
    },
    {
      id: "mapbox-token",
      label: "Mapbox token",
      severity: "HIGH",
      regex: /\b(pk|sk)\.eyJ[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\b/,
      defaultAction: "redact",
    },
    {
      id: "twilio-key",
      label: "Twilio key",
      severity: "HIGH",
      regex: /\bSK[0-9a-fA-F]{32}\b/,
      defaultAction: "redact",
    },
    {
      id: "sendgrid-key",
      label: "SendGrid key",
      severity: "HIGH",
      regex: /\bSG\.[A-Za-z0-9\-_]{22,}\.[A-Za-z0-9\-_]{22,}\b/,
      defaultAction: "redact",
    },

    // --- HIGH: Messaging platform tokens ---
    {
      id: "slack-token",
      label: "Slack token",
      severity: "HIGH",
      regex: /\b(xoxb|xoxp|xoxs|xoxa|xoxr)-[A-Za-z0-9-]+\b/,
      defaultAction: "redact",
    },
    {
      id: "discord-token",
      label: "Discord token",
      severity: "HIGH",
      regex: /\b[MN][A-Za-z0-9]{23,}\.[A-Za-z0-9\-_]{6}\.[A-Za-z0-9\-_]{27,}\b/,
      defaultAction: "redact",
    },

    // --- HIGH: Database connection strings ---
    {
      id: "database-url",
      label: "database connection string",
      severity: "HIGH",
      regex: /\b(postgres|postgresql|mysql|mongodb|mongodb\+srv|redis|rediss):\/\/[^\s"']+/i,
      defaultAction: "redact",
    },
    {
      // Any scheme:// URL carrying inline user:password@ credentials (e.g. a private
      // Homebrew tap remote https://ci-user:pass@gitlab.example.com/...).
      id: "url-credentials",
      label: "URL with inline credentials",
      severity: "HIGH",
      regex: /\b[a-z][a-z0-9+.-]*:\/\/[^\s:@/]+:[^\s@/]+@/i,
      defaultAction: "redact",
    },

    // --- HIGH: Supabase / Vercel / generic JWT ---
    {
      id: "supabase-key",
      label: "Supabase key",
      severity: "HIGH",
      regex: /\bsbp_[A-Za-z0-9]{40,}\b/,
      defaultAction: "redact",
    },
    {
      id: "vercel-token",
      label: "Vercel token",
      severity: "HIGH",
      regex: /\b(vc_prod_|vc_test_)[A-Za-z0-9]{20,}\b/,
      defaultAction: "redact",
    },
    {
      id: "jwt-token",
      label: "JWT token",
      severity: "HIGH",
      regex: /\beyJhbGciOi[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\.[A-Za-z0-9\-_]+\b/,
      defaultAction: "redact",
    },

    // --- MEDIUM ---
    {
      id: "ip-address",
      label: "IP address",
      severity: "MEDIUM",
      regex: /\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b/,
      defaultAction: "redact",
    },
    {
      id: "email-address",
      label: "email address",
      severity: "MEDIUM",
      regex: /\b[\w.+-]+@[\w-]+\.[\w.]+\b/,
      defaultAction: "include",
    },
  ];

  if (username) {
    patterns.push({
      id: "home-path",
      label: "home directory path",
      severity: "LOW",
      regex: new RegExp(`/(Users|home)/${username}/`),
      defaultAction: "include",
    });
  }

  cachedPatterns = patterns;
  return patterns;
}
