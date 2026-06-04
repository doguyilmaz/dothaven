import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { buildRestorePlan } from "../../src/restore/plan";

let tempHome: string;
let tempBackup: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "diff-home-"));
  tempBackup = await mkdtemp(join(tmpdir(), "diff-backup-"));

  await Bun.write(join(tempHome, ".zshrc"), "original content");
  await Bun.write(join(tempHome, ".gitconfig"), "[user]\n  name = Test");
  await Bun.$`mkdir -p ${join(tempHome, ".config/zed")}`.quiet();
  await Bun.write(join(tempHome, ".config/zed/settings.json"), '{"theme":"Dark"}');

  await Bun.$`mkdir -p ${join(tempBackup, "shell")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "git")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "editor/zed")}`.quiet();

  await Bun.write(join(tempBackup, "shell/.zshrc"), "modified content");
  await Bun.write(join(tempBackup, "git/.gitconfig"), "[user]\n  name = Test");
  await Bun.write(join(tempBackup, "editor/zed/settings.json"), '{"theme":"Light"}');
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
  await rm(tempBackup, { recursive: true, force: true });
});

describe("diff (via buildRestorePlan)", () => {
  test("detects modified files as conflict", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const zshrc = plan.entries.find((e) => e.backupPath === "shell/.zshrc");
    expect(zshrc).toBeDefined();
    expect(zshrc!.status).toBe("conflict");
  });

  test("detects unchanged files as same", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const gitconfig = plan.entries.find((e) => e.backupPath === "git/.gitconfig");
    expect(gitconfig).toBeDefined();
    expect(gitconfig!.status).toBe("same");
  });

  test("detects modified editor settings as conflict", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const zed = plan.entries.find((e) => e.backupPath === "editor/zed/settings.json");
    expect(zed).toBeDefined();
    expect(zed!.status).toBe("conflict");
  });

  test("section filtering works", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const shellEntries = plan.entries.filter((e) => e.category === "shell");
    expect(shellEntries).toHaveLength(1);
    expect(shellEntries[0].backupPath).toBe("shell/.zshrc");
  });

  test("returns correct categories", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    expect(plan.categories).toContain("shell");
    expect(plan.categories).toContain("git");
    expect(plan.categories).toContain("editor");
  });
});
