import { test, expect, describe } from "bun:test";
import { redactIPs, redactNpmTokens, redactSshConfig, redactAll } from "../../src/utils/redact";

describe("redactIPs", () => {
  test("redacts IPv4 addresses", () => {
    expect(redactIPs("HostName 192.168.1.100")).toBe("HostName [REDACTED]");
  });

  test("redacts multiple IPs", () => {
    expect(redactIPs("from 10.0.0.1 to 172.16.0.1")).toBe("from [REDACTED] to [REDACTED]");
  });

  test("leaves non-IP text unchanged", () => {
    expect(redactIPs("hello world")).toBe("hello world");
  });
});

describe("redactNpmTokens", () => {
  test("redacts auth tokens", () => {
    expect(redactNpmTokens("//registry.npmjs.org/:_authToken=npm_abc123xyz")).toBe(
      "//registry.npmjs.org/:_authToken=[REDACTED]",
    );
  });

  test("leaves non-token lines unchanged", () => {
    expect(redactNpmTokens("registry=https://registry.npmjs.org/")).toBe("registry=https://registry.npmjs.org/");
  });
});

describe("redactSshConfig", () => {
  test("redacts HostName", () => {
    expect(redactSshConfig("  HostName 192.168.1.1")).toBe("  HostName [REDACTED]");
  });

  test("redacts IdentityFile", () => {
    expect(redactSshConfig("  IdentityFile ~/.ssh/id_rsa")).toBe("  IdentityFile [REDACTED]");
  });

  test("redacts both in multiline", () => {
    const input = "Host github\n  HostName github.com\n  IdentityFile ~/.ssh/gh";
    const expected = "Host github\n  HostName [REDACTED]\n  IdentityFile [REDACTED]";
    expect(redactSshConfig(input)).toBe(expected);
  });
});

describe("redactAll", () => {
  test("applies all redactions", () => {
    const input = "HostName 192.168.1.1\nIdentityFile ~/.ssh/key\n_authToken=secret";
    const result = redactAll(input);
    expect(result).toContain("[REDACTED]");
    expect(result).not.toContain("192.168.1.1");
    expect(result).not.toContain("~/.ssh/key");
    expect(result).not.toContain("secret");
  });
});
