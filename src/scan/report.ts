import type { ScanSummary, Severity } from "./types";

const SEVERITY_RANK: Record<Severity, number> = { HIGH: 3, MEDIUM: 2, LOW: 1 };

export function formatReport(summary: ScanSummary): string {
  if (summary.results.length === 0) return "";

  const lines = ["\n⚠ Sensitivity report:"];

  for (const result of summary.results) {
    const top = result.findings.toSorted(
      (a, b) => SEVERITY_RANK[b.pattern.severity] - SEVERITY_RANK[a.pattern.severity],
    )[0];

    const level = top.pattern.severity.padEnd(6);
    const path = result.filePath.padEnd(30);
    const actionLabel = result.action === "redact" ? "redacted" : result.action === "skip" ? "skipped" : "included";
    lines.push(`  ${level} ${path} ${top.pattern.label} — ${actionLabel}`);
  }

  const parts: string[] = [];
  if (summary.redacted > 0) parts.push(`${summary.redacted} items redacted`);
  if (summary.skipped > 0) parts.push(`${summary.skipped} skipped`);

  if (parts.length > 0) {
    lines.push("");
    lines.push(`  ${parts.join(", ")}. Use --no-redact to include all.`);
  }

  return lines.join("\n");
}
