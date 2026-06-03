import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "path";
import { mkdtemp, rm } from "fs/promises";
import { tmpdir } from "os";
import { defaultEnv } from "../../src/collectors/env";

// Exercises the real IO boundary (Bun.$ / Bun.Glob / Bun.file) — the one place
// collectors touch the system. Verifies the assumptions the design relies on:
// array-interpolated commands run, and non-zero exits do NOT throw (the npm fix).
describe("defaultEnv (real IO)", () => {
  let dir: string;

  beforeAll(async () => {
    dir = await mkdtemp(join(tmpdir(), "dotfiles-env-test-"));
    await Bun.write(join(dir, "alpha"), "a");
    await Bun.write(join(dir, "beta"), "b");
  });

  afterAll(async () => {
    await rm(dir, { recursive: true, force: true });
  });

  test("run executes an array-interpolated command and returns stdout", async () => {
    expect((await defaultEnv.run(["echo", "hello"])).trim()).toBe("hello");
  });

  test("run does not throw on non-zero exit (npm ls warning case)", async () => {
    expect((await defaultEnv.run(["sh", "-c", "echo out; exit 3"])).trim()).toBe("out");
  });

  test("listDir lists files in a directory", async () => {
    expect((await defaultEnv.listDir(dir)).sort()).toEqual(["alpha", "beta"]);
  });

  test("listDir on a missing directory → []", async () => {
    expect(await defaultEnv.listDir(join(dir, "nope"))).toEqual([]);
  });

  test("fileExists reflects presence", async () => {
    expect(await defaultEnv.fileExists(join(dir, "alpha"))).toBe(true);
    expect(await defaultEnv.fileExists(join(dir, "missing"))).toBe(false);
  });
});
