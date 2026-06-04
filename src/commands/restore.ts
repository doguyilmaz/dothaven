import { resolve } from "node:path";
import { getHome } from "../utils/home";
import { buildRestorePlan, executeRestore, pickCategories } from "../restore";

function parseArgs(args: string[]) {
  let pick = false;
  let dryRun = false;
  let backupPath: string | null = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--pick") pick = true;
    else if (args[i] === "--dry-run") dryRun = true;
    else if (!args[i].startsWith("--")) backupPath = args[i];
  }

  return { pick, dryRun, backupPath };
}

export async function restore(args: string[]) {
  const { pick, dryRun, backupPath } = parseArgs(args);

  if (!backupPath) {
    console.log("Usage: dotfiles restore <backup-path> [--pick] [--dry-run]");
    return;
  }

  const resolvedPath = resolve(backupPath);
  const home = getHome();

  let plan = await buildRestorePlan(resolvedPath, home);

  if (plan.entries.length === 0) {
    console.log("No restorable files found in backup.");
    return;
  }

  if (pick) {
    const counts: Record<string, number> = {};
    for (const entry of plan.entries) {
      counts[entry.category] = (counts[entry.category] ?? 0) + 1;
    }

    const selected = await pickCategories(plan.categories, counts);
    if (selected.length === 0) {
      console.log("No categories selected.");
      return;
    }

    plan = {
      ...plan,
      entries: plan.entries.filter((e) => selected.includes(e.category)),
      categories: selected,
    };
  }

  await executeRestore(plan, { dryRun });
}
