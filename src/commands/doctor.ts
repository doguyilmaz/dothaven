import { parseSnapshot } from "../snapshot";
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
 * Primary identifier of an item: the name in columns[0], falling back to raw. Keying on the name
 * ignores version drift — parity is "is it present", not "is it the same version". JSON snapshots
 * persist raw and columns verbatim, so snapshot and live items key identically (no serialize quirk).
 */
function keyOf(item: { raw: string; columns: string[] }): string {
  return item.columns?.[0] ?? item.raw;
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
      "Usage: dotfiles doctor <snapshot.json>\n  Compares a .json snapshot against this machine and lists what's missing.",
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

  let snapshot: CollectorResult;
  try {
    snapshot = parseSnapshot(await file.text());
  } catch (error) {
    console.error(`Invalid snapshot: ${error instanceof Error ? error.message : error}`);
    process.exitCode = 1;
    return;
  }
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
