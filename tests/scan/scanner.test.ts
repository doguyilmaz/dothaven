import { test, expect, describe } from "bun:test";
import { join } from "node:path";
import { tmpdir } from "node:os";
import { scanContent, scanFile, summarize } from "../../src/scan/scanner";
import { applyRedactions } from "../../src/scan/redactor";

describe("scanContent", () => {
  test("detects private key headers → skip", () => {
    const content = "-----BEGIN RSA PRIVATE KEY-----\nMIIEow...\n-----END RSA PRIVATE KEY-----";
    const result = scanContent("~/.ssh/id_rsa", content);
    expect(result.action).toBe("skip");
    expect(result.findings.length).toBeGreaterThan(0);
    expect(result.findings[0].pattern.id).toBe("private-key-pem");
    expect(result.findings[0].line).toBe(1);
  });

  test("detects PGP private key → skip", () => {
    const content = "-----BEGIN PGP PRIVATE KEY BLOCK-----\nVersion: GnuPG v2";
    const result = scanContent("key.asc", content);
    expect(result.action).toBe("skip");
    expect(result.findings.some((f) => f.pattern.id === "pgp-private-key")).toBe(true);
  });

  test("detects npm auth tokens → redact", () => {
    const content = "registry=https://registry.npmjs.org/\n_authToken=npm_aBcDeFgHiJkLmNoPqRsTuVwXyZ";
    const result = scanContent("~/.npmrc", content);
    expect(result.action).toBe("redact");
    expect(result.findings.some((f) => f.pattern.id === "auth-token-npm")).toBe(true);
  });

  test("detects bearer tokens → redact", () => {
    const content = "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.abc123";
    const result = scanContent("config.yml", content);
    expect(result.action).toBe("redact");
    expect(result.findings.some((f) => f.pattern.id === "bearer-token")).toBe(true);
  });

  test("detects OpenAI keys → redact", () => {
    const content = "OPENAI_API_KEY=sk-proj-1234567890abcdefghijklmnop";
    const result = scanContent("config.json", content);
    expect(result.action).toBe("redact");
    expect(result.findings.some((f) => f.pattern.id === "openai-key")).toBe(true);
  });

  test("detects Anthropic keys → redact", () => {
    const content = "ANTHROPIC_API_KEY=sk-ant-api03-abcdefghijklmnopqrstuvwxyz";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "anthropic-key")).toBe(true);
  });

  test("detects GitHub tokens (ghp_, gho_, github_pat_)", () => {
    const content = "token: ghp_ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghij";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "github-token")).toBe(true);
  });

  test("detects AWS access keys → redact", () => {
    const content = "aws_access_key_id = AKIAIOSFODNN7EXAMPLE";
    const result = scanContent("~/.aws/credentials", content);
    expect(result.action).toBe("redact");
    expect(result.findings.some((f) => f.pattern.id === "aws-access-key")).toBe(true);
  });

  test("detects AWS secret keys → redact", () => {
    const content = "aws_secret_access_key = wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY";
    const result = scanContent("~/.aws/credentials", content);
    expect(result.findings.some((f) => f.pattern.id === "aws-secret-key")).toBe(true);
  });

  test("detects IP addresses → redact", () => {
    const content = "HostName 192.168.1.100\nPort 22";
    const result = scanContent("~/.ssh/config", content);
    expect(result.action).toBe("redact");
    expect(result.findings.some((f) => f.pattern.id === "ip-address")).toBe(true);
    expect(result.findings[0].line).toBe(1);
  });

  test("detects Google API key → redact", () => {
    const content = "GOOGLE_API_KEY=AIzaSyBcDeFgHiJkLmNoPqRsTuVwXyZ01234567";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "google-api-key")).toBe(true);
  });

  test("detects Stripe keys (sk_live_, pk_test_) → redact", () => {
    const content = "STRIPE_SECRET=sk_live_abcdefghijklmnopqrstuv";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "stripe-key")).toBe(true);
  });

  test("detects Mapbox token → redact", () => {
    const content = "MAPBOX_TOKEN=pk.eyJhbGciOi.abcdef123456";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "mapbox-token")).toBe(true);
  });

  test("detects Slack tokens → redact", () => {
    const content = "SLACK_BOT_TOKEN=xoxb-123456789012-abcdefghij";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "slack-token")).toBe(true);
  });

  test("detects SendGrid key → redact", () => {
    const content = "SENDGRID_API_KEY=SG.abcdefghijklmnopqrstuv.wxyz1234567890abcdefghij";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "sendgrid-key")).toBe(true);
  });

  test("detects database connection strings → redact", () => {
    const content = "DATABASE_URL=postgres://user:pass@host:5432/db";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "database-url")).toBe(true);
  });

  test("detects MongoDB connection strings → redact", () => {
    const content = "MONGO_URI=mongodb+srv://admin:secret@cluster0.abc123.mongodb.net/mydb";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "database-url")).toBe(true);
  });

  test("detects generic SECRET= patterns → redact", () => {
    const content = "APP_SECRET=my-super-secret-value-123";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "generic-secret")).toBe(true);
  });

  test("detects generic API_KEY= patterns → redact", () => {
    const content = "SOME_API_KEY=abc123def456";
    const result = scanContent(".env", content);
    expect(result.findings.some((f) => f.pattern.id === "generic-api-key")).toBe(true);
  });

  test("detects JWT tokens → redact", () => {
    const content =
      "token=eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIn0.dozjgNryP4J3jVmNHl0w5N_XgL0n3I9PlFUP0THsR8U";
    const result = scanContent("config", content);
    expect(result.findings.some((f) => f.pattern.id === "jwt-token")).toBe(true);
  });

  test("detects email addresses → include", () => {
    const content = "[user]\n  email = dev@example.com";
    const result = scanContent("~/.gitconfig", content);
    expect(result.action).toBe("include");
    expect(result.findings.some((f) => f.pattern.id === "email-address")).toBe(true);
  });

  test("highest severity finding determines file action", () => {
    const content = "email = dev@example.com\n_authToken=secret123";
    const result = scanContent("mixed.conf", content);
    expect(result.action).toBe("redact");
    expect(result.findings.length).toBe(2);
  });

  test("private key skip overrides redact", () => {
    const content = "-----BEGIN RSA PRIVATE KEY-----\n_authToken=secret\nemail@test.com";
    const result = scanContent("combo.pem", content);
    expect(result.action).toBe("skip");
  });

  test("returns include for clean content", () => {
    const content = "theme = dark\nfont_size = 14";
    const result = scanContent("settings.json", content);
    expect(result.action).toBe("include");
    expect(result.findings).toHaveLength(0);
  });

  test("truncates long matches to 40 chars", () => {
    const longToken = `Bearer ${"A".repeat(100)}`;
    const result = scanContent("config", longToken);
    expect(result.findings[0].match.length).toBeLessThanOrEqual(43);
    expect(result.findings[0].match.endsWith("...")).toBe(true);
  });
});

describe("summarize", () => {
  test("counts actions correctly", () => {
    const results = [
      scanContent("a", "-----BEGIN RSA PRIVATE KEY-----"),
      scanContent("b", "_authToken=secret"),
      scanContent("c", "email = dev@example.com"),
      scanContent("d", "theme = dark"),
    ];
    const summary = summarize(results);
    expect(summary.skipped).toBe(1);
    expect(summary.redacted).toBe(1);
    expect(summary.included).toBe(1);
    expect(summary.results).toHaveLength(3);
  });

  test("excludes files with no findings", () => {
    const results = [scanContent("clean", "no sensitive data here")];
    const summary = summarize(results);
    expect(summary.results).toHaveLength(0);
  });
});

describe("applyRedactions", () => {
  test("redacts matched patterns in content", () => {
    const content = "registry=https://npm.example.com\n_authToken=secret-token-123";
    const result = scanContent("~/.npmrc", content);
    const redacted = applyRedactions(content, result);
    expect(redacted).toContain("[REDACTED]");
    expect(redacted).not.toContain("secret-token-123");
  });

  test("returns content unchanged when action is not redact", () => {
    const content = "email = dev@example.com";
    const result = scanContent("~/.gitconfig", content);
    const redacted = applyRedactions(content, result);
    expect(redacted).toBe(content);
  });

  test("redacts IP addresses", () => {
    const content = "HostName 10.0.0.50";
    const result = scanContent("ssh/config", content);
    const redacted = applyRedactions(content, result);
    expect(redacted).toContain("[REDACTED]");
    expect(redacted).not.toContain("10.0.0.50");
  });
});

describe("scanFile", () => {
  test("scans an existing file", async () => {
    const tempFile = join(tmpdir(), "scan-test-file.txt");
    await Bun.write(tempFile, "_authToken=secret123");
    const result = await scanFile(tempFile);
    expect(result).not.toBeNull();
    expect(result!.action).toBe("redact");
    expect(result!.findings.length).toBeGreaterThan(0);
    await Bun.$`rm ${tempFile}`.quiet();
  });

  test("returns null for missing file", async () => {
    const result = await scanFile("/tmp/nonexistent-scan-file.txt");
    expect(result).toBeNull();
  });
});
