import { parse } from "@dotformat/core";
import { runCollectors } from "./collect";
import { getHome } from "../utils/home";
import type { CollectorResult } from "../collectors/types";

/** Sections that represent installable inventory worth a parity check. */
function isInstallable(id: string): boolean {
  return (
    id.startsWith("packages.") ||
    id.startsWith("runtimes.") ||
    id.startsWith("apps.brew.") ||
    id === "apps.macos" ||
    id.startsWith("fonts.") ||
    id.endsWith(".extensions")
  );
}

/**
 * Primary identifier of an item, stable across the .dotf serialize/parse boundary
 * (stringify joins columns as "a | b"; live items use their own raw like "a@b").
 * Comparing by columns[0] (the name) also ignores version drift — parity is
 * "is it present", not "is it the same version".
 */
function keyOf(item: { raw: string; columns: string[] }): string {
  return item.columns?.[0] ?? item.raw.split(" | ")[0].trim();
}

/** Items present in the snapshot but absent from the current machine, per section. */
export function findMissing(snapshot: CollectorResult, current: CollectorResult): Record<string, string[]> {
  const missing: Record<string, string[]> = {};
  for (const [id, section] of Object.entries(snapshot)) {
    if (!isInstallable(id) || !section.items?.length) continue;
    const have = new Set((current[id]?.items ?? []).map(keyOf));
    const gone = section.items.filter((item) => !have.has(keyOf(item)));
    if (gone.length) missing[id] = gone.map((item) => item.raw);
  }
  return missing;
}

export async function doctor(args: string[]) {
  const snapshotPath = args.find((a) => !a.startsWith("-"));
  if (!snapshotPath) {
    console.error(
      "Usage: dotfiles doctor <snapshot.dotf>\n  Compares a .dotf snapshot against this machine and lists what's missing.",
    );
    process.exitCode = 1;
    return;
  }

  const file = Bun.file(snapshotPath);
  if (!(await file.exists())) {
    console.error(`Snapshot not found: ${snapshotPath}`);
    process.exitCode = 1;
    return;
  }

  const snapshot = parse(await file.text()).sections as CollectorResult;
  const current = await runCollectors({ redact: false, home: getHome() });
  const missing = findMissing(snapshot, current);

  const ids = Object.keys(missing).sort();
  if (ids.length === 0) {
    console.log("✅ Parity — everything installable in the snapshot is present on this machine.");
    return;
  }

  console.log("Missing on this machine (present in the snapshot):\n");
  for (const id of ids) {
    console.log(`  ${id} (${missing[id].length})`);
    for (const item of missing[id]) console.log(`    - ${item}`);
  }
  const total = ids.reduce((n, id) => n + missing[id].length, 0);
  console.log(`\n${total} item(s) missing across ${ids.length} section(s).`);
  process.exitCode = 1;
}
