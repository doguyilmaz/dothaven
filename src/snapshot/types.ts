/**
 * Snapshot data model — the in-memory shape every collector produces and the JSON written to disk.
 *
 * The on-disk JSON is a flat map `{ [sectionName]: { pairs?, items?, content? } }` (empty fields
 * omitted), which maps 1:1 to a Go `map[string]Section` with `omitempty` for a future port. The
 * redundant `name` is dropped on write and re-injected from the key on read.
 */

export interface SectionItem {
  raw: string;
  columns: string[];
}

export interface Section {
  name: string;
  pairs: Record<string, string>;
  items: SectionItem[];
  content: string | null;
}

/** A machine snapshot: section id → section. */
export type Snapshot = Record<string, Section>;

// --- diff shapes (compare / formatDiff) ---

export interface ItemsDiff {
  added: string[];
  removed: string[];
  common: string[];
}

export interface PairsDiff {
  added: Record<string, string>;
  removed: Record<string, string>;
  changed: Record<string, { left: string; right: string }>;
  common: Record<string, string>;
}

export interface ContentDiff {
  left: string | null;
  right: string | null;
  changed: boolean;
}

export type SectionStatus = "added" | "removed" | "changed" | "equal";

export interface SectionDiff {
  name: string;
  status: SectionStatus;
  items: ItemsDiff;
  pairs: PairsDiff;
  content: ContentDiff;
}

export interface SnapshotDiff {
  sections: Record<string, SectionDiff>;
}
