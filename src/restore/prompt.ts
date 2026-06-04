import type { RestoreEntry, BatchConflictAction } from "./types";
import { readLine } from "../utils/prompt";

export { readLine };

export async function pickCategories(categories: string[], counts: Record<string, number>): Promise<string[]> {
  console.log("\nAvailable categories:");
  for (let i = 0; i < categories.length; i++) {
    console.log(`  ${i + 1}) ${categories[i]} (${counts[categories[i]] ?? 0} files)`);
  }

  const input = await readLine("\nSelect categories (comma-separated numbers, or 'all'): ");

  if (input.toLowerCase() === "all") return categories;

  const indices = input
    .split(",")
    .map((s) => parseInt(s.trim(), 10) - 1)
    .filter((i) => i >= 0 && i < categories.length);

  return indices.map((i) => categories[i]);
}

export function showDiff(targetContent: string, backupContent: string, maxLines = 20): string {
  const targetLines = targetContent.split("\n");
  const backupLines = backupContent.split("\n");
  const lines: string[] = [];
  const max = Math.max(targetLines.length, backupLines.length);

  let shown = 0;
  for (let i = 0; i < max && shown < maxLines; i++) {
    const t = targetLines[i] ?? "";
    const b = backupLines[i] ?? "";
    if (t !== b) {
      if (t) lines.push(`  - ${t}`);
      if (b) lines.push(`  + ${b}`);
      shown++;
    }
  }

  if (shown >= maxLines) {
    lines.push(`  ... (${max - maxLines} more lines differ)`);
  }

  return lines.join("\n");
}

export async function promptConflict(
  entry: RestoreEntry,
  backupContent: string,
  targetContent: string,
): Promise<BatchConflictAction> {
  console.log(`\nConflict: ${entry.targetPath}`);
  const input = await readLine("  [o]verwrite / [s]kip / [d]iff / overwrite-[a]ll / skip-a[l]l: ");

  switch (input.toLowerCase()) {
    case "o":
      return "overwrite";
    case "s":
      return "skip";
    case "d":
      console.log(showDiff(targetContent, backupContent));
      return promptConflict(entry, backupContent, targetContent);
    case "a":
      return "overwrite-all";
    case "l":
      return "skip-all";
    default:
      return "skip";
  }
}
