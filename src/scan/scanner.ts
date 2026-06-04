import { resolve } from "node:path";
import type { ScanFinding, ScanResult, ScanSummary, Action } from "./types";
import { getScanPatterns } from "./patterns";

const ACTION_PRIORITY: Record<Action, number> = { skip: 3, redact: 2, include: 1 };
const MAX_FILE_SIZE = 1024 * 1024;

export function scanContent(filePath: string, content: string): ScanResult {
  const patterns = getScanPatterns();
  const findings: ScanFinding[] = [];
  const lines = content.split("\n");

  for (let i = 0; i < lines.length; i++) {
    for (const pattern of patterns) {
      const match = lines[i].match(pattern.regex);
      if (match) {
        findings.push({
          pattern,
          line: i + 1,
          match: match[0].length > 40 ? `${match[0].slice(0, 40)}...` : match[0],
        });
      }
    }
  }

  let action: Action = "include";
  for (const f of findings) {
    if (ACTION_PRIORITY[f.pattern.defaultAction] > ACTION_PRIORITY[action]) {
      action = f.pattern.defaultAction;
    }
  }

  return { filePath, findings, action };
}

export async function scanFile(filePath: string): Promise<ScanResult | null> {
  const file = Bun.file(filePath);
  if (!(await file.exists())) return null;
  const content = await file.text();
  return scanContent(filePath, content);
}

export function summarize(results: ScanResult[]): ScanSummary {
  const withFindings = results.filter((r) => r.findings.length > 0);
  return {
    results: withFindings,
    redacted: withFindings.filter((r) => r.action === "redact").length,
    skipped: withFindings.filter((r) => r.action === "skip").length,
    included: withFindings.filter((r) => r.action === "include").length,
  };
}

/** Recursively scan a directory, skipping node_modules/.git and files larger than 1MB. */
export async function scanDirectory(dirPath: string): Promise<ScanResult[]> {
  const results: ScanResult[] = [];
  const glob = new Bun.Glob("**/*");

  for await (const relative of glob.scan({ cwd: dirPath, onlyFiles: true, dot: true })) {
    if (relative.includes("node_modules/") || relative.includes(".git/")) continue;
    const fullPath = resolve(dirPath, relative);
    if (Bun.file(fullPath).size > MAX_FILE_SIZE) continue;
    const result = await scanFile(fullPath);
    if (result) results.push(result);
  }

  return results;
}
