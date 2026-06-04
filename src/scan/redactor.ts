import type { Section } from "../snapshot/types";
import { REDACTION_MARKER } from "../utils/constants";
import type { ScanResult } from "./types";
import { scanContent } from "./scanner";

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
    const global = re.global ? re : new RegExp(re.source, `${re.flags}g`);
    redacted = redacted.replace(global, REDACTION_MARKER);
  }

  return redacted;
}

/**
 * Redact a whole section in place — content AND pairs AND items, so no section type bypasses the
 * gate (json-extract pairs and dir items previously leaked). Returns false when the section should
 * be dropped entirely (its content scanned to "skip", e.g. a private-key file).
 */
export function redactSection(name: string, section: Section, scanResults: ScanResult[]): boolean {
  if (section.content) {
    const r = scanContent(name, section.content);
    scanResults.push(r);
    if (r.action === "skip") return false;
    if (r.action === "redact") section.content = applyRedactions(section.content, r);
  }

  for (const key of Object.keys(section.pairs)) {
    // The KEY itself can carry a secret — e.g. a flattened JSON object whose key is a token
    // (json-extract with fields:[]). A key can't be masked in place without losing it, so drop the
    // whole pair when the key scans as sensitive.
    const keyScan = scanContent(name, key);
    if (keyScan.action !== "include") {
      scanResults.push(keyScan);
      delete section.pairs[key];
      continue;
    }
    const r = scanContent(`${name}.${key}`, section.pairs[key]);
    if (r.action !== "include") {
      scanResults.push(r);
      section.pairs[key] = REDACTION_MARKER;
    }
  }

  for (const item of section.items) {
    const r = scanContent(name, item.raw);
    if (r.action !== "include") {
      scanResults.push(r);
      item.raw = REDACTION_MARKER;
      item.columns = item.columns.map(() => REDACTION_MARKER);
    }
  }

  return true;
}
