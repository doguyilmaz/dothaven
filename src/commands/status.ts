import { basename } from "node:path";
import { getHome } from "../utils/home";
import { findLatestBackup, getBackupAge } from "../utils/find-backup";
import { buildRestorePlan } from "../restore/plan";

export async function status() {
  const home = getHome();
  const backupDir = await findLatestBackup();

  if (!backupDir) {
    console.log("No backup found. Run 'dotfiles backup' first.");
    return;
  }

  const age = await getBackupAge(backupDir);
  const plan = await buildRestorePlan(backupDir, home);

  const modified = plan.entries.filter((e) => e.status === "conflict");
  const unchanged = plan.entries.filter((e) => e.status === "same");
  const newOnMachine = plan.entries.filter((e) => e.status === "new");
  const redacted = plan.entries.filter((e) => e.status === "redacted");

  console.log(`Last backup: ${age} (${basename(backupDir)})`);
  console.log(`  ${plan.entries.length} files tracked: ${modified.length} modified, ${unchanged.length} unchanged`);

  if (newOnMachine.length > 0) {
    console.log(`  ${newOnMachine.length} not on machine (new in backup)`);
  }
  if (redacted.length > 0) {
    console.log(`  ${redacted.length} redacted`);
  }

  if (modified.length > 0) {
    console.log("\nModified since backup:");
    for (const entry of modified) {
      console.log(`  ${entry.backupPath}`);
    }
  }

  if (modified.length === 0 && newOnMachine.length === 0) {
    console.log("\nEverything up to date.");
  }
}
