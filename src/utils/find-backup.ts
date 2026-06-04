import { join } from "node:path";
import { readdir, stat } from "node:fs/promises";
import { resolveOutputDir } from "./resolve-output";

export async function findLatestBackup(): Promise<string | null> {
  const outputDir = await resolveOutputDir(null);
  let entries: string[];
  try {
    entries = await readdir(outputDir);
  } catch {
    return null;
  }

  const backups = entries
    .filter((e) => e.startsWith("backup-"))
    .sort()
    .reverse();

  return backups.length > 0 ? join(outputDir, backups[0]) : null;
}

export async function getBackupAge(backupDir: string): Promise<string> {
  try {
    const stats = await stat(backupDir);
    const ageMs = Date.now() - stats.mtimeMs;
    const mins = Math.floor(ageMs / 60000);
    if (mins < 60) return `${mins}m ago`;
    const hours = Math.floor(mins / 60);
    if (hours < 24) return `${hours}h ago`;
    const days = Math.floor(hours / 24);
    return `${days}d ago`;
  } catch {
    return "unknown";
  }
}
