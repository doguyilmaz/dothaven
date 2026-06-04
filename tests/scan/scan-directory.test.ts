import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm, writeFile } from "node:fs/promises";
import { tmpdir } from "node:os";
import { scanFile, scanDirectory } from "../../src/scan";

let dir: string;
let emptyFile: string;
let secretFile: string;

beforeAll(async () => {
  dir = await mkdtemp(join(tmpdir(), "scandir-"));
  emptyFile = join(dir, "empty");
  secretFile = join(dir, "config");
  await writeFile(emptyFile, ""); // 0 bytes
  await writeFile(secretFile, "token = ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa\n");
});

afterAll(async () => {
  await rm(dir, { recursive: true, force: true });
});

describe("scanFile on a 0-byte file", () => {
  test("returns a result (not null), no findings — never misrouted as a dir", async () => {
    const r = await scanFile(emptyFile);
    expect(r).not.toBeNull();
    expect(r?.findings).toEqual([]);
  });
});

describe("scanDirectory hardening (no ENOTDIR crash)", () => {
  test("a regular file path → [] instead of throwing", async () => {
    expect(await scanDirectory(secretFile)).toEqual([]);
  });

  test("a 0-byte file path → [] instead of throwing", async () => {
    expect(await scanDirectory(emptyFile)).toEqual([]);
  });

  test("a missing path → []", async () => {
    expect(await scanDirectory(join(dir, "does-not-exist"))).toEqual([]);
  });

  test("an actual directory still scans its files", async () => {
    const results = await scanDirectory(dir);
    expect(results.some((r) => r.findings.length > 0)).toBe(true); // the secret file
  });
});
