import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { scanContent, scanDirectory, formatSecurityReport } from "../../src/scan";

describe("formatSecurityReport", () => {
  test("clean results → no-findings message + counts", () => {
    const report = formatSecurityReport([scanContent("a", "theme = dark"), scanContent("b", "font = mono")]);
    expect(report).toContain("2 file(s) scanned");
    expect(report).toContain("0 with findings");
    expect(report).toContain("No sensitive data found");
  });

  test("groups files by top severity with action + line", () => {
    const report = formatSecurityReport([
      scanContent("~/.ssh/id_rsa", "-----BEGIN RSA PRIVATE KEY-----"),
      scanContent("~/.zshrc", "export GITHUB_TOKEN=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"),
      scanContent("~/.gitconfig", "  email = dev@example.com"),
      scanContent("clean", "theme = dark"),
    ]);
    expect(report).toContain("4 file(s) scanned · 3 with findings");
    expect(report).toContain("## 🔴 HIGH");
    expect(report).toContain("`~/.ssh/id_rsa` —");
    expect(report).toContain("skip (private key)");
    expect(report).toContain("`~/.zshrc` —");
    expect(report).toContain("## 🟡 MEDIUM");
    expect(report).toContain("`~/.gitconfig` — email address · keep");
  });
});

describe("scanDirectory", () => {
  let dir: string;

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "dotfiles-scandir-"));
    await Bun.write(join(dir, "secret.env"), "AWS_SECRET_ACCESS_KEY=abc123def456ghijkl");
    await Bun.write(join(dir, "clean.txt"), "nothing sensitive here");
  });

  afterAll(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  test("scans recursively and flags secret files", async () => {
    const results = await scanDirectory(dir);
    const secret = results.find((r) => r.filePath.endsWith("secret.env"));
    expect(secret?.findings.length ?? 0).toBeGreaterThan(0);
    const clean = results.find((r) => r.filePath.endsWith("clean.txt"));
    expect(clean?.findings ?? []).toHaveLength(0);
  });
});
