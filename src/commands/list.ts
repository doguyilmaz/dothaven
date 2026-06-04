import { join } from "node:path";
import { parseSnapshot } from "../snapshot";
import type { Section } from "../snapshot/types";

/** Human-readable single-section dump for `list` (the only display consumer of the old stringify). */
function formatSection(name: string, section: Section): string {
  const lines = [`[${name}]`];
  for (const [k, v] of Object.entries(section.pairs)) lines.push(`  ${k} = ${v}`);
  for (const item of section.items) {
    lines.push(`  ${item.columns.length > 1 ? item.columns.join("  ") : item.raw}`);
  }
  if (section.content != null) {
    lines.push("  ---");
    for (const line of section.content.split("\n")) lines.push(`  ${line}`);
  }
  return lines.join("\n");
}

function fuzzyMatch(query: string, sectionName: string): boolean {
  const q = query.toLowerCase();
  const s = sectionName.toLowerCase();
  return s.includes(q) || s.split(".").some((part) => part.includes(q));
}

async function getReportsDir(): Promise<string> {
  const cwd = process.cwd();
  return join(cwd, "reports");
}

export async function list(args: string[]) {
  if (!args.length) {
    console.log("Usage: dotfiles list <section>");
    console.log("Example: dotfiles list brew");
    return;
  }

  const query = args[0];
  const reportsDir = await getReportsDir();

  const glob = new Bun.Glob("*.json");
  const entries: { path: string; mtime: number }[] = [];

  for await (const path of glob.scan(reportsDir)) {
    const stat = await Bun.file(join(reportsDir, path)).stat();
    entries.push({ path: join(reportsDir, path), mtime: stat?.mtimeMs ?? 0 });
  }

  entries.sort((a, b) => b.mtime - a.mtime);

  if (!entries.length) {
    console.log("No .json reports found. Run 'dotfiles collect' first.");
    return;
  }

  const snapshot = parseSnapshot(await Bun.file(entries[0].path).text());

  const matches = Object.keys(snapshot).filter((name) => fuzzyMatch(query, name));

  if (!matches.length) {
    console.log(`No sections matching "${query}".`);
    console.log("Available sections:", Object.keys(snapshot).join(", "));
    return;
  }

  for (const name of matches) {
    console.log(formatSection(name, snapshot[name]));
  }
}
