import type { ScanFinding, ScanResult, ScanSummary, Action } from "./types";
import { getScanPatterns } from "./patterns";

const ACTION_PRIORITY: Record<Action, number> = { skip: 3, redact: 2, include: 1 };

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
