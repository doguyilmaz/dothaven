import { join } from "node:path";
import { parseSnapshot, compareSnapshots, formatDiff } from "../snapshot";

async function getReportsDir(): Promise<string> {
  const cwd = process.cwd();
  return join(cwd, "reports");
}

export async function compareCli(args: string[]) {
  const reportsDir = await getReportsDir();

  let files: string[];

  if (args.length >= 2) {
    files = args.slice(0, 2);
  } else {
    const glob = new Bun.Glob("*.json");
    const entries: { path: string; mtime: number }[] = [];

    for await (const path of glob.scan(reportsDir)) {
      const stat = await Bun.file(join(reportsDir, path)).stat();
      entries.push({ path: join(reportsDir, path), mtime: stat?.mtimeMs ?? 0 });
    }

    entries.sort((a, b) => b.mtime - a.mtime);

    if (entries.length < 2) {
      console.log("Need at least 2 .json reports in reports/ to compare.");
      console.log("Usage: dotfiles compare [file1] [file2]");
      return;
    }

    files = [entries[0].path, entries[1].path];
  }

  const [leftContent, rightContent] = await Promise.all([Bun.file(files[0]).text(), Bun.file(files[1]).text()]);

  const left = parseSnapshot(leftContent);
  const right = parseSnapshot(rightContent);
  const diff = compareSnapshots(left, right);

  const leftLabel =
    files[0]
      .split("/")
      .pop()
      ?.replace(/\.json$/, "") ?? "left";
  const rightLabel =
    files[1]
      .split("/")
      .pop()
      ?.replace(/\.json$/, "") ?? "right";

  // changesOnly: show only what differs (and so identical snapshots render empty → "No differences").
  const output = formatDiff(diff, { leftLabel, rightLabel, color: true, changesOnly: true });

  if (!output.trim()) {
    console.log("No differences found.");
  } else {
    console.log(output);
  }
}
