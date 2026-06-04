import { test, expect, describe } from "bun:test";
import { redactSection } from "../../src/scan";
import type { ScanResult } from "../../src/scan";
import { makeSection } from "../../src/collectors/types";

describe("redactSection (pairs + items + content)", () => {
  test("redacts secret values in json-extract pairs (was leaking to .dotf)", () => {
    const section = makeSection("ai.gemini.settings", {
      pairs: { apiKey: "sk-ant-api03-abcdefghijklmnopqrstuvwxyz", theme: "dark" },
    });
    const results: ScanResult[] = [];
    expect(redactSection("ai.gemini.settings", section, results)).toBe(true);
    expect(section.pairs.apiKey).toBe("[REDACTED]");
    expect(section.pairs.theme).toBe("dark"); // benign value untouched
  });

  test("redacts secret-bearing items", () => {
    const section = makeSection("x", {
      items: [
        { raw: "GITHUB_TOKEN=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", columns: ["GITHUB_TOKEN", "ghp_aaaa"] },
        { raw: "normal-file.txt", columns: ["normal-file.txt"] },
      ],
    });
    redactSection("x", section, []);
    expect(section.items[0].raw).toBe("[REDACTED]");
    expect(section.items[0].columns).toEqual(["[REDACTED]", "[REDACTED]"]);
    expect(section.items[1].raw).toBe("normal-file.txt");
  });

  test("content with a private key → drop the whole section (returns false)", () => {
    const section = makeSection("ssh.key", { content: "-----BEGIN OPENSSH PRIVATE KEY-----\nbase64" });
    expect(redactSection("ssh.key", section, [])).toBe(false);
  });

  test("content secrets still redacted", () => {
    const section = makeSection("npm", { content: "//registry/:_authToken=npm_secrettoken123456" });
    redactSection("npm", section, []);
    expect(section.content).toContain("[REDACTED]");
    expect(section.content).not.toContain("npm_secrettoken123456");
  });

  test("clean section is untouched", () => {
    const section = makeSection("x", { pairs: { theme: "dark" }, items: [{ raw: "a", columns: ["a"] }] });
    expect(redactSection("x", section, [])).toBe(true);
    expect(section.pairs.theme).toBe("dark");
    expect(section.items[0].raw).toBe("a");
  });
});
