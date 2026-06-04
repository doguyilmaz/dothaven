import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { registryCollector } from "../../src/registry/collector";
import { registryEntries } from "../../src/registry/entries";
import type { CollectorContext } from "../../src/collectors/types";

const shellEntries = registryEntries.filter((e) => e.category === "shell");
const collectShell = registryCollector(shellEntries);

let tempHome: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "dotfiles-shell-test-"));
  await Bun.write(join(tempHome, ".zshrc"), "export FOO=1\n");
  await Bun.write(join(tempHome, ".zprofile"), 'eval "$(/opt/homebrew/bin/brew shellenv)"\n');
  await Bun.write(join(tempHome, ".bash_profile"), "source ~/.bashrc\n");
  await Bun.write(join(tempHome, ".bashrc"), "alias ll='ls -la'\n");
  // .zshenv intentionally not written — verifies missing files are skipped
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
});

describe("shell registry entries", () => {
  test("registers every shell profile file (drift fix)", () => {
    const ids = shellEntries.map((e) => e.id);
    expect(ids).toContain("shell.zshrc");
    expect(ids).toContain("shell.zprofile");
    expect(ids).toContain("shell.zshenv");
    expect(ids).toContain("shell.bash_profile");
    expect(ids).toContain("shell.bashrc");
  });

  test("collects existing shell files", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectShell(ctx);
    expect(result["shell.zprofile"]?.content).toContain("brew shellenv");
    expect(result["shell.bash_profile"]?.content).toContain("bashrc");
    expect(result["shell.bashrc"]?.content).toContain("alias ll");
  });

  test("skips missing shell files", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectShell(ctx);
    expect(result["shell.zshenv"]).toBeUndefined();
  });
});
