import type {
  ContentDiff,
  ItemsDiff,
  PairsDiff,
  Section,
  SectionDiff,
  SectionStatus,
  Snapshot,
  SnapshotDiff,
} from "./types";

/**
 * Compare two snapshots. ORIENTATION (preserved from the original .dotf comparator): the FIRST
 * argument is "left", and what exists ONLY in left is labeled `added` (+), while what exists only in
 * right is `removed` (-). `compare` is the diff engine; the caller chooses which snapshot is left.
 */

function diffItems(left: Section | null, right: Section | null): ItemsDiff {
  const leftRaws = (left?.items ?? []).map((i) => i.raw);
  const rightRaws = (right?.items ?? []).map((i) => i.raw);
  const leftSet = new Set(leftRaws);
  const rightSet = new Set(rightRaws);
  return {
    added: leftRaws.filter((r) => !rightSet.has(r)),
    removed: rightRaws.filter((r) => !leftSet.has(r)),
    common: leftRaws.filter((r) => rightSet.has(r)),
  };
}

function diffPairs(left: Section | null, right: Section | null): PairsDiff {
  const lp = left?.pairs ?? {};
  const rp = right?.pairs ?? {};
  const added: Record<string, string> = {};
  const removed: Record<string, string> = {};
  const changed: Record<string, { left: string; right: string }> = {};
  const common: Record<string, string> = {};
  for (const [k, v] of Object.entries(lp)) {
    if (!(k in rp)) added[k] = v;
    else if (rp[k] !== v) changed[k] = { left: v, right: rp[k] };
    else common[k] = v;
  }
  for (const [k, v] of Object.entries(rp)) {
    if (!(k in lp)) removed[k] = v;
  }
  return { added, removed, changed, common };
}

function diffContent(left: Section | null, right: Section | null): ContentDiff {
  const l = left?.content ?? null;
  const r = right?.content ?? null;
  return { left: l, right: r, changed: l !== r };
}

/** A both-present section is "changed" iff any item add/remove, any pair add/remove/change, or a
 * content change. Items in common alone do NOT mark a section changed. */
function computeStatus(items: ItemsDiff, pairs: PairsDiff, content: ContentDiff): SectionStatus {
  const changed =
    items.added.length > 0 ||
    items.removed.length > 0 ||
    Object.keys(pairs.added).length > 0 ||
    Object.keys(pairs.removed).length > 0 ||
    Object.keys(pairs.changed).length > 0 ||
    content.changed;
  return changed ? "changed" : "equal";
}

function sectionDiff(name: string, status: SectionStatus, left: Section | null, right: Section | null): SectionDiff {
  return {
    name,
    status,
    items: diffItems(left, right),
    pairs: diffPairs(left, right),
    content: diffContent(left, right),
  };
}

export function compareSnapshots(left: Snapshot, right: Snapshot): SnapshotDiff {
  const names = [...new Set([...Object.keys(left), ...Object.keys(right)])]; // left-first, de-duped
  const sections: Record<string, SectionDiff> = {};
  for (const name of names) {
    const l = left[name] ?? null;
    const r = right[name] ?? null;
    if (l && !r) {
      sections[name] = sectionDiff(name, "added", l, null);
    } else if (!l && r) {
      sections[name] = sectionDiff(name, "removed", null, r);
    } else {
      const d = sectionDiff(name, "equal", l, r);
      d.status = computeStatus(d.items, d.pairs, d.content);
      sections[name] = d;
    }
  }
  return { sections };
}

export interface FormatDiffOptions {
  leftLabel?: string;
  rightLabel?: string;
  color?: boolean;
}

/**
 * Render a diff to text. Emits EVERY section (including equal ones, as dim "=" lines) in the diff's
 * insertion order — callers that want changes-only must filter first. No trailing newline.
 */
export function formatDiff(diff: SnapshotDiff, options: FormatDiffOptions = {}): string {
  const leftLabel = options.leftLabel ?? "left";
  const rightLabel = options.rightLabel ?? "right";
  const c = options.color ?? true;
  const green = c ? "\x1b[32m" : "";
  const red = c ? "\x1b[31m" : "";
  const yellow = c ? "\x1b[33m" : "";
  const dim = c ? "\x1b[2m" : "";
  const reset = c ? "\x1b[0m" : "";

  const lines: string[] = [];
  for (const section of Object.values(diff.sections)) {
    const { name, status } = section;
    if (status === "added") lines.push(`${green}+ [${name}]${reset}  (only in ${leftLabel})`);
    else if (status === "removed") lines.push(`${red}- [${name}]${reset}  (only in ${rightLabel})`);
    else lines.push(`[${name}]`);

    for (const item of section.items.added) lines.push(`  ${green}+ ${item}${reset}  (only in ${leftLabel})`);
    for (const item of section.items.removed) lines.push(`  ${red}- ${item}${reset}  (only in ${rightLabel})`);
    for (const item of section.items.common) lines.push(`  ${dim}= ${item}${reset}`);

    for (const [k, v] of Object.entries(section.pairs.added)) {
      lines.push(`  ${green}+ ${k} = ${v}${reset}  (only in ${leftLabel})`);
    }
    for (const [k, v] of Object.entries(section.pairs.removed)) {
      lines.push(`  ${red}- ${k} = ${v}${reset}  (only in ${rightLabel})`);
    }
    for (const [k, ch] of Object.entries(section.pairs.changed)) {
      lines.push(`  ${yellow}~ ${k} = ${ch.left} → ${ch.right}${reset}`);
    }
    for (const [k, v] of Object.entries(section.pairs.common)) {
      lines.push(`  ${dim}= ${k} = ${v}${reset}`);
    }

    const content = section.content;
    if (content.changed) {
      if (content.left !== null && content.right !== null) lines.push(`  ${yellow}~ content changed${reset}`);
      else if (content.left !== null) lines.push(`  ${green}+ content${reset}  (only in ${leftLabel})`);
      else lines.push(`  ${red}- content${reset}  (only in ${rightLabel})`);
    }
  }
  return lines.join("\n");
}
