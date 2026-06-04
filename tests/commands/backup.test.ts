import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { backupSources } from "../../src/backup/sources";
import type { BackupSource } from "../../src/backup/types";

let tempHome: string;
let tempOutput: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "dotfiles-backup-home-"));
  tempOutput = await mkdtemp(join(tmpdir(), "dotfiles-backup-out-"));

  await Bun.$`mkdir -p ${join(tempHome, ".claude/skills")}`.quiet();
  await Bun.write(join(tempHome, ".claude/settings.json"), '{"permissions":{}}');
  await Bun.write(join(tempHome, ".claude/CLAUDE.md"), "# Claude Config");
  await Bun.write(join(tempHome, ".claude/skills/skill1.md"), "skill content");

  await Bun.write(join(tempHome, ".zshrc"), "export PATH=$PATH");

  await Bun.$`mkdir -p ${join(tempHome, ".ssh")}`.quiet();
  await Bun.write(join(tempHome, ".ssh/config"), "Host github.com\n  HostName 10.0.0.1\n  IdentityFile ~/.ssh/id_ed");

  await Bun.write(join(tempHome, ".npmrc"), "registry=https://npm.example.com\n_authToken=secret-token-123");

  await Bun.$`mkdir -p ${join(tempHome, ".config/zed")}`.quiet();
  await Bun.write(join(tempHome, ".config/zed/settings.json"), '{"theme":"One Dark"}');

  await Bun.write(join(tempHome, ".gitconfig"), "[user]\n  name = Test");
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
  await rm(tempOutput, { recursive: true, force: true });
});

async function runBackup(sources: BackupSource[], home: string, destRoot: string, redact: boolean) {
  let totalFiles = 0;
  for (const source of sources) {
    const entries = source.entries(home);
    for (const entry of entries) {
      if (entry.type === "file") {
        const file = Bun.file(entry.src);
        if (!(await file.exists())) continue;
        let content = await file.text();
        if (redact && entry.redact) content = entry.redact(content);
        const destPath = join(destRoot, entry.dest);
        await Bun.$`mkdir -p ${join(destPath, "..")}`.quiet();
        await Bun.write(destPath, content);
        totalFiles++;
      } else {
        const glob = new Bun.Glob("**/*");
        try {
          for await (const relative of glob.scan({ cwd: entry.src, onlyFiles: true })) {
            const destPath = join(destRoot, entry.dest, relative);
            await Bun.$`mkdir -p ${join(destPath, "..")}`.quiet();
            await Bun.write(destPath, await Bun.file(join(entry.src, relative)).text());
            totalFiles++;
          }
        } catch {}
      }
    }
  }
  return totalFiles;
}

describe("backup", () => {
  test("copies files to correct structure", async () => {
    const dest = join(tempOutput, "full");
    const count = await runBackup(backupSources, tempHome, dest, true);

    expect(count).toBeGreaterThan(0);
    expect(await Bun.file(join(dest, "ai/claude/settings.json")).exists()).toBe(true);
    expect(await Bun.file(join(dest, "ai/claude/CLAUDE.md")).exists()).toBe(true);
    expect(await Bun.file(join(dest, "ai/claude/skills/skill1.md")).exists()).toBe(true);
    expect(await Bun.file(join(dest, "shell/.zshrc")).exists()).toBe(true);
    expect(await Bun.file(join(dest, "git/.gitconfig")).exists()).toBe(true);
    expect(await Bun.file(join(dest, "editor/zed/settings.json")).exists()).toBe(true);
  });

  test("skips missing files silently", async () => {
    const dest = join(tempOutput, "missing");
    const count = await runBackup(backupSources, tempHome, dest, true);

    expect(await Bun.file(join(dest, "bun/.bunfig.toml")).exists()).toBe(false);
    expect(await Bun.file(join(dest, "terminal/.p10k.zsh")).exists()).toBe(false);
  });

  test("redacts ssh config by default", async () => {
    const dest = join(tempOutput, "redacted");
    await runBackup(backupSources, tempHome, dest, true);

    const sshContent = await Bun.file(join(dest, "ssh/config")).text();
    expect(sshContent).toContain("[REDACTED]");
    expect(sshContent).not.toContain("10.0.0.1");
  });

  test("redacts npm tokens by default", async () => {
    const dest = join(tempOutput, "redacted-npm");
    await runBackup(backupSources, tempHome, dest, true);

    const npmContent = await Bun.file(join(dest, "npm/.npmrc")).text();
    expect(npmContent).toContain("[REDACTED]");
    expect(npmContent).not.toContain("secret-token-123");
  });

  test("copies raw files with --no-redact", async () => {
    const dest = join(tempOutput, "raw");
    await runBackup(backupSources, tempHome, dest, false);

    const sshContent = await Bun.file(join(dest, "ssh/config")).text();
    expect(sshContent).toContain("10.0.0.1");

    const npmContent = await Bun.file(join(dest, "npm/.npmrc")).text();
    expect(npmContent).toContain("secret-token-123");
  });

  test("--only filters by category", async () => {
    const aiOnly = backupSources.filter((s) => s.category === "ai");
    const dest = join(tempOutput, "only-ai");
    await runBackup(aiOnly, tempHome, dest, true);

    expect(await Bun.file(join(dest, "ai/claude/settings.json")).exists()).toBe(true);
    expect(await Bun.file(join(dest, "shell/.zshrc")).exists()).toBe(false);
    expect(await Bun.file(join(dest, "git/.gitconfig")).exists()).toBe(false);
  });

  test("--skip filters by category", async () => {
    const skipSsh = backupSources.filter((s) => s.category !== "ssh");
    const dest = join(tempOutput, "skip-ssh");
    await runBackup(skipSsh, tempHome, dest, true);

    expect(await Bun.file(join(dest, "ssh/config")).exists()).toBe(false);
    expect(await Bun.file(join(dest, "shell/.zshrc")).exists()).toBe(true);
  });

  test("copies directory contents recursively", async () => {
    const dest = join(tempOutput, "dirs");
    await runBackup(backupSources, tempHome, dest, true);

    const skillContent = await Bun.file(join(dest, "ai/claude/skills/skill1.md")).text();
    expect(skillContent).toBe("skill content");
  });

  test("does not create empty directories", async () => {
    const emptyHome = await mkdtemp(join(tmpdir(), "dotfiles-empty-"));
    const dest = join(tempOutput, "empty");
    const count = await runBackup(backupSources, emptyHome, dest, true);

    expect(count).toBe(0);
    const exists = await Bun.file(join(dest, "ai")).exists();
    expect(exists).toBe(false);

    await rm(emptyHome, { recursive: true, force: true });
  });
});
