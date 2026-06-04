import { REDACTION_MARKER } from "../utils/constants";
import type { ScanResult } from "./types";

export function applyRedactions(content: string, result: ScanResult): string {
  if (result.action !== "redact") return content;

  let redacted = content;
  const applied = new Set<string>();

  for (const finding of result.findings) {
    if (finding.pattern.defaultAction !== "redact") continue;
    // Dedupe by pattern: one global pass per pattern masks every occurrence,
    // including multiple same-pattern secrets on a single line.
    if (applied.has(finding.pattern.id)) continue;
    applied.add(finding.pattern.id);

    const re = finding.pattern.regex;
    const global = re.global ? re : new RegExp(re.source, re.flags + "g");
    redacted = redacted.replace(global, REDACTION_MARKER);
  }

  return redacted;
}
