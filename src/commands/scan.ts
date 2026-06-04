import { resolve } from "node:path";
import { scanFile, scanDirectory, summarize, formatReport } from "../scan";
import type { ScanResult, Severity } from "../scan";

const SEVERITY_RANK: Record<Severity, number> = { HIGH: 3, MEDIUM: 2, LOW: 1 };

function formatDetailed(results: ScanResult[]): string {
  const lines: string[] = [];

  for (const result of results) {
    if (result.findings.length === 0) continue;

    lines.push(`\n${result.filePath}`);
    const sorted = result.findings.toSorted(
      (a, b) => SEVERITY_RANK[b.pattern.severity] - SEVERITY_RANK[a.pattern.severity],
    );
    for (const finding of sorted) {
      lines.push(`  L${finding.line} [${finding.pattern.severity}] ${finding.pattern.label}: ${finding.match}`);
    }
  }

  return lines.join("\n");
}

export async function scan(args: string[]) {
  const target = resolve(args[0] ?? ".");
  const file = Bun.file(target);
  const isFile = (await file.exists()) && file.size > 0;

  let results: ScanResult[];

  if (isFile) {
    const result = await scanFile(target);
    results = result ? [result] : [];
  } else {
    results = await scanDirectory(target);
  }

  if (results.every((r) => r.findings.length === 0)) {
    console.log("No sensitive data found.");
    return;
  }

  console.log(formatDetailed(results));

  const summary = summarize(results);
  console.log(formatReport(summary));
}
