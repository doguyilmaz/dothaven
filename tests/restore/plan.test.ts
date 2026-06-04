import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { buildRestoreMap, buildRestorePlan } from "../../src/restore/plan";

let tempHome: string;
let tempBackup: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "restore-home-"));
  tempBackup = await mkdtemp(join(tmpdir(), "restore-backup-"));

  await Bun.$`mkdir -p ${join(tempHome, ".claude/skills")}`.quiet();
  await Bun.write(join(tempHome, ".claude/settings.json"), '{"permissions":{}}');
  await Bun.write(join(tempHome, ".zshrc"), "export PATH=$PATH");
  await Bun.write(join(tempHome, ".gitconfig"), "[user]\n  name = Test");

  await Bun.$`mkdir -p ${join(tempBackup, "ai/claude/skills")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "shell")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "git")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "ssh")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "editor/zed")}`.quiet();

  await Bun.write(join(tempBackup, "ai/claude/settings.json"), '{"permissions":{}}');
  await Bun.write(join(tempBackup, "ai/claude/skills/skill1.md"), "skill content");
  await Bun.write(join(tempBackup, "shell/.zshrc"), "export PATH=$PATH:/new");
  await Bun.write(join(tempBackup, "git/.gitconfig"), "[user]\n  name = Test");
  await Bun.write(join(tempBackup, "ssh/config"), "Host github.com\n  HostName [REDACTED]");
  await Bun.write(join(tempBackup, "editor/zed/settings.json"), '{"theme":"Dark"}');
  await Bun.write(join(tempBackup, "shell/.zshrc.local"), "# local overrides");
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
  await rm(tempBackup, { recursive: true, force: true });
});

describe("buildRestoreMap", () => {
  test("maps dest paths to absolute source paths", () => {
    const map = buildRestoreMap(tempHome);
    const claude = map.get("ai/claude/settings.json");
    expect(claude).toBeDefined();
    expect(claude!.absolutePath).toBe(join(tempHome, ".claude/settings.json"));
    expect(claude!.category).toBe("ai");
    expect(claude!.type).toBe("file");
  });

  test("includes dir entries", () => {
    const map = buildRestoreMap(tempHome);
    const skills = map.get("ai/claude/skills");
    expect(skills).toBeDefined();
    expect(skills!.type).toBe("dir");
  });

  test("maps all categories", () => {
    const map = buildRestoreMap(tempHome);
    const categories = new Set([...map.values()].map((v) => v.category));
    expect(categories.has("ai")).toBe(true);
    expect(categories.has("shell")).toBe(true);
    expect(categories.has("git")).toBe(true);
    expect(categories.has("ssh")).toBe(true);
  });
});

describe("buildRestorePlan", () => {
  test("detects same files", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const gitconfig = plan.entries.find((e) => e.backupPath === "git/.gitconfig");
    expect(gitconfig).toBeDefined();
    expect(gitconfig!.status).toBe("same");
  });

  test("detects conflicts", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const zshrc = plan.entries.find((e) => e.backupPath === "shell/.zshrc");
    expect(zshrc).toBeDefined();
    expect(zshrc!.status).toBe("conflict");
  });

  test("detects new files", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const zed = plan.entries.find((e) => e.backupPath === "editor/zed/settings.json");
    expect(zed).toBeDefined();
    expect(zed!.status).toBe("new");
  });

  test("detects redacted files", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const ssh = plan.entries.find((e) => e.backupPath === "ssh/config");
    expect(ssh).toBeDefined();
    expect(ssh!.status).toBe("redacted");
  });

  test("maps directory entries (skills)", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const skill = plan.entries.find((e) => e.backupPath === "ai/claude/skills/skill1.md");
    expect(skill).toBeDefined();
    expect(skill!.targetPath).toBe(join(tempHome, ".claude/skills/skill1.md"));
    expect(skill!.category).toBe("ai");
  });

  test("maps .local override files", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    const local = plan.entries.find((e) => e.backupPath === "shell/.zshrc.local");
    expect(local).toBeDefined();
    expect(local!.targetPath).toBe(join(tempHome, ".zshrc.local"));
    expect(local!.category).toBe("shell");
  });

  test("includes correct categories", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    expect(plan.categories).toContain("ai");
    expect(plan.categories).toContain("shell");
    expect(plan.categories).toContain("git");
  });
});
