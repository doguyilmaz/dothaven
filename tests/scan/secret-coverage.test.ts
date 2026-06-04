import { test, expect, describe } from "bun:test";
import { scanContent } from "../../src/scan/scanner";
import { applyRedactions } from "../../src/scan/redactor";

// Regression tests for the adversarial review findings:
//  - shell secret exports (GITHUB_TOKEN/GH_TOKEN/NPM_TOKEN/bare TOKEN/*_KEY) leaked unredacted
//  - basic-auth URL credentials leaked
//  - non-global redaction leaked the 2nd+ same-pattern secret on a line

function redact(content: string): string {
  return applyRedactions(content, scanContent("shell.zshrc", content));
}

describe("shell secret export coverage (was leaking)", () => {
  const leaky = [
    "export GITHUB_TOKEN=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
    "export GH_TOKEN=0123456789abcdef0123456789abcdef01234567",
    "export NPM_TOKEN=0123456789abcdef0123456789abcdef01234567",
    "export TOKEN=0123456789abcdef0123456789abcdef01234567",
    "export HOMEBREW_GITHUB_API_TOKEN=0123456789abcdef0123456789abcdef01234567",
    "export CLOUDFLARE_API_TOKEN=0123456789abcdef0123456789abcdef01234567",
    "export MY_SERVICE_KEY=supersecretvalue123456",
    "password=hunter2supersecret",
    "client_secret: abcdef0123456789",
  ];

  for (const line of leaky) {
    test(`redacts: ${line.split(/[=:]/)[0].trim()}`, () => {
      const out = redact(line);
      expect(out).toContain("[REDACTED]");
      const value = line.split(/[=:]\s*/).slice(1).join("");
      expect(out).not.toContain(value);
    });
  }
});

describe("does NOT over-redact benign assignments", () => {
  for (const line of ["theme = dark", "font_size = 14", "monkey=1", "primary_key = id", "EDITOR=vim", "export PATH=/usr/bin"]) {
    test(`leaves alone: ${line}`, () => {
      const result = scanContent("settings", line);
      const redactFindings = result.findings.filter((f) => f.pattern.defaultAction === "redact");
      expect(redactFindings).toHaveLength(0);
    });
  }
});

describe("URL inline credentials", () => {
  test("redacts basic-auth tap URL password", () => {
    const line = 'tap "x/y", "https://ci-user:S3cr3tPassw0rd@gitlab.example.com/x/y"';
    const out = redact(line);
    expect(out).toContain("[REDACTED]");
    expect(out).not.toContain("S3cr3tPassw0rd");
  });

  test("does not flag a credential-free URL", () => {
    const result = scanContent("c", 'tap "homebrew/core", "https://github.com/Homebrew/homebrew-core"');
    expect(result.findings.filter((f) => f.pattern.defaultAction === "redact")).toHaveLength(0);
  });
});

describe("global redaction (multiple secrets per line)", () => {
  test("redacts ALL same-pattern tokens on one line, not just the first", () => {
    const a = "ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa";
    const b = "ghp_bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb";
    const out = redact(`export A=${a} B=${b}`);
    expect(out).not.toContain(a);
    expect(out).not.toContain(b);
  });

  test("redacts every IP on a single line", () => {
    const out = redact("hosts 10.0.0.1 10.0.0.2 10.0.0.3");
    expect(out).not.toContain("10.0.0.1");
    expect(out).not.toContain("10.0.0.2");
    expect(out).not.toContain("10.0.0.3");
  });
});
