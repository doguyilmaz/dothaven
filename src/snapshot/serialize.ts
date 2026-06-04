import type { Section, SectionItem, Snapshot } from "./types";

interface SerializedSection {
  pairs?: Record<string, string>;
  items?: SectionItem[];
  content?: string;
}

/**
 * Serialize a snapshot to pretty (2-space) JSON with a trailing newline. The redundant `name` is
 * dropped (it's the map key) and empty pairs/items / null content are omitted, so the file stays
 * compact and maps cleanly to a Go `map[string]Section` with `omitempty`.
 */
export function serializeSnapshot(snapshot: Snapshot): string {
  const out: Record<string, SerializedSection> = {};
  for (const [name, section] of Object.entries(snapshot)) {
    const entry: SerializedSection = {};
    if (section.pairs && Object.keys(section.pairs).length) entry.pairs = section.pairs;
    if (section.items && section.items.length) entry.items = section.items;
    if (section.content != null) entry.content = section.content;
    out[name] = entry;
  }
  return `${JSON.stringify(out, null, 2)}\n`;
}

function normalizeItem(value: unknown): SectionItem {
  const it = (value ?? {}) as Partial<SectionItem>;
  return {
    raw: typeof it.raw === "string" ? it.raw : "",
    columns: Array.isArray(it.columns) ? it.columns.map((c) => String(c)) : [],
  };
}

/**
 * Parse a JSON snapshot back into the in-memory model. Defensive — this reads arbitrary
 * user-supplied files (a system boundary for `doctor`/`compare`): it rejects non-object roots,
 * re-injects each section's `name` from its key, and coalesces missing fields to {}/[]/null so no
 * downstream consumer hits `undefined`.
 */
export function parseSnapshot(text: string): Snapshot {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    throw new Error("Not a valid JSON snapshot (failed to parse)");
  }
  if (!raw || typeof raw !== "object" || Array.isArray(raw)) {
    throw new Error("Snapshot must be a JSON object of sections");
  }

  const snapshot: Snapshot = {};
  for (const [name, value] of Object.entries(raw as Record<string, unknown>)) {
    const v = (value ?? {}) as Partial<Section>;
    snapshot[name] = {
      name,
      pairs:
        v.pairs && typeof v.pairs === "object" && !Array.isArray(v.pairs) ? (v.pairs as Record<string, string>) : {},
      items: Array.isArray(v.items) ? v.items.map(normalizeItem) : [],
      content: typeof v.content === "string" ? v.content : null,
    };
  }
  return snapshot;
}
