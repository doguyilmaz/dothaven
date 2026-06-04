import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { registryCollector } from "../../src/registry/collector";
import { registryEntries } from "../../src/registry/entries";
import type { CollectorContext } from "../../src/collectors/types";

const npmEntries = registryEntries.filter((e) => e.id === "npm.config");
const collectNpm = registryCollector(npmEntries);

let tempHome: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "dotfiles-test-"));
  await Bun.write(
    join(tempHome, ".npmrc"),
    `//registry.npmjs.org/:_authToken=npm_SuperSecretToken123\nregistry=https://registry.npmjs.org/\n`,
  );
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
});

describe("collectNpm (registry)", () => {
  test("redacts auth token by default", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectNpm(ctx);
    expect(result["npm.config"]).toBeDefined();
    expect(result["npm.config"].content).toContain("_authToken=[REDACTED]");
    expect(result["npm.config"].content).not.toContain("SuperSecretToken");
  });

  test("shows real token with --no-redact", async () => {
    const ctx: CollectorContext = { redact: false, home: tempHome };
    const result = await collectNpm(ctx);
    expect(result["npm.config"].content).toContain("npm_SuperSecretToken123");
  });

  test("returns empty for missing file", async () => {
    const ctx: CollectorContext = { redact: true, home: "/tmp/nonexistent" };
    const result = await collectNpm(ctx);
    expect(result).toEqual({});
  });
});
