import { join } from "node:path";
import { backupSources } from "../backup/sources";
import { REDACTION_MARKER } from "../utils/constants";
import type { RestoreEntry, RestorePlan, FileStatus } from "./types";

interface RestoreMapping {
  absolutePath: string;
  category: string;
  type: "file" | "dir";
}

export function buildRestoreMap(home: string): Map<string, RestoreMapping> {
  const map = new Map<string, RestoreMapping>();

  for (const source of backupSources) {
    for (const entry of source.entries(home)) {
      map.set(entry.dest, {
        absolutePath: entry.src,
        category: source.category,
        type: entry.type,
      });
    }
  }

  return map;
}

async function resolveFileStatus(backupContent: string, targetPath: string): Promise<FileStatus> {
  if (backupContent.includes(REDACTION_MARKER)) return "redacted";

  const targetFile = Bun.file(targetPath);
  if (!(await targetFile.exists())) return "new";

  const targetContent = await targetFile.text();
  return Bun.hash(backupContent) === Bun.hash(targetContent) ? "same" : "conflict";
}

export async function buildRestorePlan(backupDir: string, home: string): Promise<RestorePlan> {
  const map = buildRestoreMap(home);
  const entries: RestoreEntry[] = [];
  const categorySet = new Set<string>();

  const glob = new Bun.Glob("**/*");
  for await (const relative of glob.scan({ cwd: backupDir, onlyFiles: true, dot: true })) {
    let targetPath: string | null = null;
    let category = "unknown";

    const mapping = map.get(relative);
    if (mapping && mapping.type === "file") {
      targetPath = mapping.absolutePath;
      category = mapping.category;
    } else {
      for (const [dest, m] of map.entries()) {
        if (m.type === "dir" && relative.startsWith(`${dest}/`)) {
          const relativeSuffix = relative.slice(dest.length + 1);
          targetPath = join(m.absolutePath, relativeSuffix);
          category = m.category;
          break;
        }
      }
    }

    if (!targetPath) {
      const localMatch = relative.match(/^(.+)\.local$/);
      if (localMatch) {
        const baseDest = localMatch[1];
        const baseMapping = map.get(baseDest);
        if (baseMapping && baseMapping.type === "file") {
          targetPath = `${baseMapping.absolutePath}.local`;
          category = baseMapping.category;
        }
      }
    }

    if (!targetPath) continue;

    const backupContent = await Bun.file(join(backupDir, relative)).text();
    const status = await resolveFileStatus(backupContent, targetPath);

    categorySet.add(category);
    entries.push({
      backupPath: relative,
      targetPath,
      category,
      status,
    });
  }

  return {
    entries,
    backupDir,
    categories: [...categorySet].sort(),
  };
}
