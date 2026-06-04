import type { ScanSummary, Severity, ScanResult, ScanFinding } from "./types";

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

function topFinding(r: ScanResult): ScanFinding {
  return r.findings.toSorted((a, b) => SEVERITY_RANK[b.pattern.severity] - SEVERITY_RANK[a.pattern.severity])[0];
}

const ACTION_LABEL: Record<ScanResult["action"], string> = {
  redact: "redact",
  skip: "skip (private key)",
  include: "keep",
};

/** Standalone Markdown security report grouping scanned files by severity. */
export function formatSecurityReport(results: ScanResult[]): string {
  const withFindings = results.filter((r) => r.findings.length > 0);
  const redacted = withFindings.filter((r) => r.action === "redact").length;
  const skipped = withFindings.filter((r) => r.action === "skip").length;

  const lines = [
    "# Security Report",
    "",
    `${results.length} file(s) scanned · ${withFindings.length} with findings · ${redacted} to redact · ${skipped} to skip.`,
    "",
  ];

  if (withFindings.length === 0) {
    lines.push("No sensitive data found. ✅", "");
    return `${lines.join("\n").trimEnd()}\n`;
  }

  const groups: [Severity, string][] = [
    ["HIGH", "## 🔴 HIGH — secrets (masked or skipped before sync)"],
    ["MEDIUM", "## 🟡 MEDIUM"],
    ["LOW", "## ⚪ LOW"],
  ];

  for (const [severity, heading] of groups) {
    const group = withFindings
      .filter((r) => topFinding(r).pattern.severity === severity)
      .toSorted((a, b) => a.filePath.localeCompare(b.filePath));
    if (group.length === 0) continue;
    lines.push(heading);
    for (const r of group) {
      const top = topFinding(r);
      lines.push(`- \`${r.filePath}\` — ${top.pattern.label} · ${ACTION_LABEL[r.action]} · L${top.line}`);
    }
    lines.push("");
  }

  return `${lines.join("\n").trimEnd()}\n`;
}
