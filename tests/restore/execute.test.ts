import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { buildRestorePlan } from "../../src/restore/plan";
import { executeRestore, createSnapshot } from "../../src/restore/execute";

let tempHome: string;
let tempBackup: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "restore-exec-home-"));
  tempBackup = await mkdtemp(join(tmpdir(), "restore-exec-backup-"));

  await Bun.$`mkdir -p ${join(tempBackup, "shell")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "editor/zed")}`.quiet();
  await Bun.$`mkdir -p ${join(tempBackup, "ssh")}`.quiet();

  await Bun.write(join(tempBackup, "shell/.zshrc"), "new zshrc content");
  await Bun.write(join(tempBackup, "editor/zed/settings.json"), '{"theme":"One Dark"}');
  await Bun.write(join(tempBackup, "ssh/config"), "Host test\n  HostName [REDACTED]");
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
  await rm(tempBackup, { recursive: true, force: true });
});

describe("executeRestore", () => {
  test("dry-run does not write files", async () => {
    const plan = await buildRestorePlan(tempBackup, tempHome);
    await executeRestore(plan, { dryRun: true });

    const zshrc = Bun.file(join(tempHome, ".zshrc"));
    expect(await zshrc.exists()).toBe(false);
  });

  test("restores new files", async () => {
    const restoreHome = await mkdtemp(join(tmpdir(), "restore-new-"));
    const plan = await buildRestorePlan(tempBackup, restoreHome);

    const newEntries = plan.entries.filter((e) => e.status === "new");
    const newPlan = { ...plan, entries: newEntries };
    await executeRestore(newPlan, { dryRun: false });

    const zedFile = Bun.file(join(restoreHome, ".config/zed/settings.json"));
    expect(await zedFile.exists()).toBe(true);
    const content = await zedFile.text();
    expect(content).toBe('{"theme":"One Dark"}');

    await rm(restoreHome, { recursive: true, force: true });
  });

  test("creates pre-restore snapshot for conflicts", async () => {
    const restoreHome = await mkdtemp(join(tmpdir(), "restore-snapshot-"));
    await Bun.write(join(restoreHome, ".zshrc"), "original zshrc content");

    const plan = await buildRestorePlan(tempBackup, restoreHome);
    const conflictEntries = plan.entries.filter((e) => e.status === "conflict");
    expect(conflictEntries.length).toBeGreaterThan(0);

    const conflictPlan = { ...plan, entries: conflictEntries };
    const snapshotDir = await createSnapshot(conflictPlan);

    expect(snapshotDir).not.toBeNull();
    const snapshotFile = Bun.file(join(snapshotDir!, "shell/.zshrc"));
    expect(await snapshotFile.exists()).toBe(true);
    const content = await snapshotFile.text();
    expect(content).toBe("original zshrc content");

    await rm(snapshotDir!, { recursive: true, force: true });
    await rm(restoreHome, { recursive: true, force: true });
  });

  test("no snapshot when no conflicts", async () => {
    const restoreHome = await mkdtemp(join(tmpdir(), "restore-no-snap-"));
    const plan = await buildRestorePlan(tempBackup, restoreHome);

    const newEntries = plan.entries.filter((e) => e.status === "new");
    const newPlan = { ...plan, entries: newEntries };
    const snapshotDir = await createSnapshot(newPlan);

    expect(snapshotDir).toBeNull();
    await rm(restoreHome, { recursive: true, force: true });
  });

  test("skips redacted files", async () => {
    const restoreHome = await mkdtemp(join(tmpdir(), "restore-redacted-"));

    await Bun.$`mkdir -p ${join(restoreHome, ".ssh")}`.quiet();
    await Bun.write(join(restoreHome, ".ssh/config"), "Host real\n  HostName 10.0.0.1");

    const plan = await buildRestorePlan(tempBackup, restoreHome);
    const redactedEntries = plan.entries.filter((e) => e.status === "redacted");
    const redactedPlan = { ...plan, entries: redactedEntries };
    await executeRestore(redactedPlan, { dryRun: false });

    const sshContent = await Bun.file(join(restoreHome, ".ssh/config")).text();
    expect(sshContent).toContain("10.0.0.1");
    expect(sshContent).not.toContain("[REDACTED]");

    await rm(restoreHome, { recursive: true, force: true });
  });
});
