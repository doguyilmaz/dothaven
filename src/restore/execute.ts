import { join, dirname } from "node:path";
import { resolveOutputDir } from "../utils/resolve-output";
import { generateTimestamp } from "../utils/timestamp";
import type { RestorePlan, FileStatus } from "./types";
import { promptConflict } from "./prompt";

interface ExecuteOptions {
  dryRun: boolean;
}

const STATUS_LABELS: Record<FileStatus, string> = {
  new: "[NEW]",
  conflict: "[CONFLICT]",
  same: "[SAME]",
  redacted: "[REDACTED]",
};

function printPlan(plan: RestorePlan) {
  const newCount = plan.entries.filter((e) => e.status === "new").length;
  const conflictCount = plan.entries.filter((e) => e.status === "conflict").length;
  const sameCount = plan.entries.filter((e) => e.status === "same").length;
  const redactedCount = plan.entries.filter((e) => e.status === "redacted").length;

  for (const entry of plan.entries) {
    const label = STATUS_LABELS[entry.status].padEnd(12);
    console.log(`  ${label} ${entry.backupPath} → ${entry.targetPath}`);
  }

  const parts: string[] = [];
  if (newCount > 0) parts.push(`${newCount} new`);
  if (conflictCount > 0) parts.push(`${conflictCount} conflicts`);
  if (sameCount > 0) parts.push(`${sameCount} unchanged`);
  if (redactedCount > 0) parts.push(`${redactedCount} redacted (skipped)`);

  console.log(`\n  ${plan.entries.length} files total: ${parts.join(", ")}`);
}

export async function createSnapshot(plan: RestorePlan): Promise<string | null> {
  const conflicts = plan.entries.filter((e) => e.status === "conflict");
  if (conflicts.length === 0) return null;

  const resolvedOutput = await resolveOutputDir(null);
  const ts = generateTimestamp();
  const snapshotDir = join(resolvedOutput, `pre-restore-${ts}`);

  for (const entry of conflicts) {
    const file = Bun.file(entry.targetPath);
    if (!(await file.exists())) continue;

    const content = await file.text();
    const destPath = join(snapshotDir, entry.backupPath);
    await Bun.$`mkdir -p ${dirname(destPath)}`.quiet();
    await Bun.write(destPath, content);
  }

  return snapshotDir;
}

export async function executeRestore(plan: RestorePlan, options: ExecuteOptions) {
  if (options.dryRun) {
    console.log("\nDry run — no files will be changed:\n");
    printPlan(plan);
    return;
  }

  const snapshotDir = await createSnapshot(plan);
  if (snapshotDir) {
    console.log(`\nPre-restore snapshot saved to: ${snapshotDir}`);
  }

  let overwriteAll = false;
  let skipAll = false;
  let restored = 0;
  let skipped = 0;
  const restoredCategories: Record<string, number> = {};

  for (const entry of plan.entries) {
    if (entry.status === "same") {
      skipped++;
      continue;
    }

    if (entry.status === "redacted") {
      console.log(`  Skipping ${entry.backupPath} (contains [REDACTED] values)`);
      skipped++;
      continue;
    }

    const backupContent = await Bun.file(join(plan.backupDir, entry.backupPath)).text();

    if (entry.status === "conflict") {
      if (skipAll) {
        skipped++;
        continue;
      }

      if (!overwriteAll) {
        const targetContent = await Bun.file(entry.targetPath).text();
        const action = await promptConflict(entry, backupContent, targetContent);

        if (action === "skip-all") {
          skipAll = true;
          skipped++;
          continue;
        }
        if (action === "skip") {
          skipped++;
          continue;
        }
        if (action === "overwrite-all") {
          overwriteAll = true;
        }
      }
    }

    await Bun.$`mkdir -p ${dirname(entry.targetPath)}`.quiet();
    await Bun.write(entry.targetPath, backupContent);
    restored++;
    restoredCategories[entry.category] = (restoredCategories[entry.category] ?? 0) + 1;
  }

  if (restored === 0) {
    console.log("No files restored.");
    return;
  }

  const categoryParts = Object.entries(restoredCategories)
    .map(([cat, count]) => `${cat} (${count})`)
    .join(", ");

  console.log(`\nRestored ${restored} files across: ${categoryParts}`);
  if (skipped > 0) console.log(`  ${skipped} files skipped`);
}
