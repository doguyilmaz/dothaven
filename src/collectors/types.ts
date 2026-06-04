import type { DotfSection } from "@dotformat/core";

export interface CollectorContext {
  redact: boolean;
  home: string;
}

export type CollectorResult = Record<string, DotfSection>;

export type Collector = (ctx: CollectorContext) => Promise<CollectorResult>;

export function makeSection(
  name: string,
  opts: {
    pairs?: Record<string, string>;
    items?: { raw: string; columns: string[] }[];
    content?: string | null;
  } = {}
): DotfSection {
  return {
    name,
    pairs: opts.pairs ?? {},
    items: opts.items ?? [],
    content: opts.content ?? null,
  };
}

/** Build single-column items from a list of names — the common collector item shape. */
export const toItems = (names: string[]) => names.map((n) => ({ raw: n, columns: [n] }));
