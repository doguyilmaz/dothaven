import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { registryCollector } from "../../src/registry/collector";
import { registryEntries } from "../../src/registry/entries";
import type { CollectorContext } from "../../src/collectors/types";

let tempHome: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "collector-test-"));
  await Bun.write(join(tempHome, ".p10k.zsh"), "line1\nline2\nline3\nline4\nline5");
  await Bun.write(join(tempHome, ".bunfig.toml"), "[install]\noptional = true");
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
});

describe("registryCollector", () => {
  test("metadata kind returns exists + line count", async () => {
    const metadataEntries = registryEntries.filter((e) => e.id === "terminal.p10k");
    const collector = registryCollector(metadataEntries);
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collector(ctx);

    expect(result["terminal.p10k"]).toBeDefined();
    expect(result["terminal.p10k"].pairs.exists).toBe("true");
    expect(result["terminal.p10k"].pairs.lines).toBe("5");
    expect(result["terminal.p10k"].content).toBeNull();
  });

  test("file kind returns content", async () => {
    const bunEntries = registryEntries.filter((e) => e.id === "bun.config");
    const collector = registryCollector(bunEntries);
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collector(ctx);

    expect(result["bun.config"]).toBeDefined();
    expect(result["bun.config"].content).toContain("optional = true");
  });

  test("json-extract with scalar fields", async () => {
    const tempDir = await mkdtemp(join(tmpdir(), "json-scalar-"));
    await Bun.$`mkdir -p ${join(tempDir, ".gemini")}`.quiet();
    await Bun.write(join(tempDir, ".gemini/settings.json"), JSON.stringify({ theme: "dark", version: 2 }));

    const geminiEntries = registryEntries.filter((e) => e.id === "ai.gemini.settings");
    const collector = registryCollector(geminiEntries);
    const ctx: CollectorContext = { redact: true, home: tempDir };
    const result = await collector(ctx);

    expect(result["ai.gemini.settings"]).toBeDefined();
    expect(result["ai.gemini.settings"].pairs.theme).toBe("dark");
    expect(result["ai.gemini.settings"].pairs.version).toBe("2");

    await rm(tempDir, { recursive: true, force: true });
  });

  test("skips missing files", async () => {
    const collector = registryCollector(registryEntries);
    const ctx: CollectorContext = { redact: true, home: "/tmp/nonexistent" };
    const result = await collector(ctx);
    expect(Object.keys(result)).toHaveLength(0);
  });
});
