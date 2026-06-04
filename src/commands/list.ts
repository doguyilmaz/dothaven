import { join } from "node:path";
import { parse, stringify } from "@dotformat/core";
import type { DotfDocument } from "@dotformat/core";

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

  const glob = new Bun.Glob("*.dotf");
  const entries: { path: string; mtime: number }[] = [];

  for await (const path of glob.scan(reportsDir)) {
    const stat = await Bun.file(join(reportsDir, path)).stat();
    entries.push({ path: join(reportsDir, path), mtime: stat?.mtimeMs ?? 0 });
  }

  entries.sort((a, b) => b.mtime - a.mtime);

  if (!entries.length) {
    console.log("No .dotf reports found. Run 'dotfiles collect' first.");
    return;
  }

  const content = await Bun.file(entries[0].path).text();
  const doc = parse(content);

  const matches = Object.keys(doc.sections).filter((name) => fuzzyMatch(query, name));

  if (!matches.length) {
    console.log(`No sections matching "${query}".`);
    console.log("Available sections:", Object.keys(doc.sections).join(", "));
    return;
  }

  for (const name of matches) {
    const sectionDoc: DotfDocument = {
      sections: { [name]: doc.sections[name] },
    };
    console.log(stringify(sectionDoc));
  }
}
