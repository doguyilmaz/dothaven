import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { registryCollector } from "../../src/registry/collector";
import { registryEntries } from "../../src/registry/entries";
import type { CollectorContext } from "../../src/collectors/types";

const claudeEntries = registryEntries.filter((e) => e.id.startsWith("ai.claude."));
const collectClaude = registryCollector(claudeEntries);

let tempHome: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "dotfiles-test-"));
  const claudeDir = join(tempHome, ".claude");
  await Bun.$`mkdir -p ${claudeDir}/skills`.quiet();
  await Bun.write(
    join(claudeDir, "settings.json"),
    JSON.stringify({
      permissions: { "Bash(git *)": "allow", Read: "allow" },
      enabledPlugins: { "vercel@claude-plugins-official": true },
    }),
  );
  await Bun.write(join(claudeDir, "CLAUDE.md"), "# My instructions\nBe helpful.");
  await Bun.write(join(claudeDir, "skills/my-skill.md"), "skill content");
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
});

describe("collectClaude (registry)", () => {
  test("collects settings (permissions + plugins)", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectClaude(ctx);
    expect(result["ai.claude.settings"]).toBeDefined();
    expect(result["ai.claude.settings"].pairs["Bash(git *)"]).toBe("allow");
    expect(result["ai.claude.settings"].pairs["vercel@claude-plugins-official"]).toBe("true");
  });

  test("collects skills", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectClaude(ctx);
    expect(result["ai.claude.skills"]).toBeDefined();
    expect(result["ai.claude.skills"].items.length).toBeGreaterThan(0);
  });

  test("collects CLAUDE.md as full content", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectClaude(ctx);
    expect(result["ai.claude.md"]).toBeDefined();
    expect(result["ai.claude.md"].content).toContain("Be helpful");
  });

  test("returns empty for missing directory", async () => {
    const ctx: CollectorContext = { redact: true, home: "/tmp/nonexistent" };
    const result = await collectClaude(ctx);
    expect(Object.keys(result)).toHaveLength(0);
  });
});
