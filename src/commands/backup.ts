import { hostname } from "node:os";
import { join, dirname } from "node:path";
import { resolveOutputDir } from "../utils/resolve-output";
import { getHome } from "../utils/home";
import { generateTimestamp } from "../utils/timestamp";
import { backupSources } from "../backup/sources";
import { scanContent, summarize, formatReport, applyRedactions } from "../scan";
import type { ScanResult } from "../scan";
import type { BackupEntry, BackupSource } from "../backup/types";

function parseArgs(args: string[]) {
  let redact = true;
  let archive = false;
  let outputDir: string | null = null;
  let only: string[] = [];
  let skip: string[] = [];

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--no-redact") redact = false;
    if (args[i] === "--archive") archive = true;
    if (args[i] === "-o" && args[i + 1]) outputDir = args[++i];
    if (args[i] === "--only" && args[i + 1]) only = args[++i].split(",");
    if (args[i] === "--skip" && args[i + 1]) skip = args[++i].split(",");
  }

  return { redact, archive, outputDir, only, skip };
}

function filterSources(sources: BackupSource[], only: string[], skip: string[]): BackupSource[] {
  let filtered = sources;
  if (only.length) filtered = filtered.filter((s) => only.includes(s.category));
  if (skip.length) filtered = filtered.filter((s) => !skip.includes(s.category));
  return filtered;
}

async function copyFile(
  entry: BackupEntry & { type: "file" },
  destRoot: string,
  redact: boolean,
  scanResults: ScanResult[],
): Promise<boolean> {
  const file = Bun.file(entry.src);
  if (!(await file.exists())) return false;

  let content = await file.text();
  const scanResult = scanContent(entry.dest, content);

  if (redact && scanResult.action === "skip") {
    scanResults.push(scanResult);
    return false;
  }

  if (redact && entry.redact) content = entry.redact(content);
  if (redact) content = applyRedactions(content, scanResult);

  scanResults.push(scanResult);

  const destPath = join(destRoot, entry.dest);
  await Bun.$`mkdir -p ${dirname(destPath)}`.quiet();
  await Bun.write(destPath, content);
  return true;
}

async function copyDir(entry: BackupEntry & { type: "dir" }, destRoot: string): Promise<number> {
  const glob = new Bun.Glob("**/*");
  let count = 0;

  try {
    for await (const relative of glob.scan({ cwd: entry.src, onlyFiles: true, dot: true })) {
      const srcPath = join(entry.src, relative);
      const destPath = join(destRoot, entry.dest, relative);
      await Bun.$`mkdir -p ${dirname(destPath)}`.quiet();
      const content = await Bun.file(srcPath).text();
      await Bun.write(destPath, content);
      count++;
    }
  } catch {
    // Directory doesn't exist or is unreadable — skip silently (tool may not be installed)
  }

  return count;
}

export async function backup(args: string[]) {
  const { redact, archive, outputDir, only, skip } = parseArgs(args);
  const resolvedOutput = await resolveOutputDir(outputDir);

  const ts = generateTimestamp();
  const backupDir = join(resolvedOutput, `backup-${hostname()}-${ts}`);

  const sources = filterSources(backupSources, only, skip);
  let totalFiles = 0;
  const backedUpCategories: string[] = [];
  const scanResults: ScanResult[] = [];

  for (const source of sources) {
    const entries = source.entries(getHome());
    let categoryCount = 0;

    for (const entry of entries) {
      if (entry.type === "file") {
        const copied = await copyFile(entry, backupDir, redact, scanResults);
        if (copied) categoryCount++;
      } else {
        categoryCount += await copyDir(entry, backupDir);
      }
    }

    if (categoryCount > 0) {
      backedUpCategories.push(`${source.category} (${categoryCount})`);
      totalFiles += categoryCount;
    }
  }

  if (totalFiles === 0) {
    console.log("No files found to backup.");
    return;
  }

  if (archive) {
    const archivePath = `${backupDir}.tar.gz`;
    const backupName = backupDir.split("/").pop()!;
    await Bun.$`tar czf ${archivePath} -C ${resolvedOutput} ${backupName}`.quiet();
    await Bun.$`rm -rf ${backupDir}`.quiet();
    console.log(`Archive saved to: ${archivePath}`);
    console.log(`  ${totalFiles} files across: ${backedUpCategories.join(", ")}`);
  } else {
    console.log(`Backup saved to: ${backupDir}`);
    console.log(`  ${totalFiles} files across: ${backedUpCategories.join(", ")}`);
  }

  if (redact) {
    const summary = summarize(scanResults);
    const report = formatReport(summary);
    if (report) console.log(report);
  }
}
