import type { Collector, CollectorResult } from "./types";
import { makeSection, toItems } from "./types";
import { type CommandEnv, defaultEnv } from "./env";
import { registryEntries } from "../registry/entries";
import type { ConfigEntry } from "../registry/types";

// Ephemeral, always-regenerated entries safe to ignore. Kept deliberately small:
// anything not clearly noise lands in "review" so nothing important is hidden.
const NOISE = new Set([
  ".DS_Store",
  ".CFUserTextEncoding",
  ".localized",
  ".Trash",
  ".cache",
  ".lesshst",
  ".node_repl_history",
  ".bash_history",
  ".zsh_history",
  ".zsh_sessions",
  ".zcompdump",
  ".cups",
  ".wget-hsts",
  ".sudo_as_admin_successful",
]);

/** Top-level `~/.X` names already covered by the registry (single source of truth). */
export function managedDotNames(entries: ConfigEntry[]): Set<string> {
  const set = new Set<string>();
  for (const e of entries) {
    const p = e.paths.darwin ?? e.paths.linux;
    const m = p?.match(/^~\/(\.[^/]+)/);
    if (m) set.add(m[1]);
  }
  return set;
}

/** Parse `ls -A` output into entry names. */
export function parseLsA(text: string): string[] {
  return text
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean);
}

export interface DotfileSweep {
  managed: string[];
  review: string[];
}

export function classifyDotfiles(entries: string[], managed: Set<string>, noise: Set<string>): DotfileSweep {
  const result: DotfileSweep = { managed: [], review: [] };
  for (const name of entries.filter((e) => e.startsWith(".") && e !== "." && e !== "..").sort()) {
    if (managed.has(name)) result.managed.push(name);
    else if (!noise.has(name)) result.review.push(name);
  }
  return result;
}

export function makeDotfilesSweepCollector(env: CommandEnv = defaultEnv): Collector {
  return async (ctx) => {
    let entries: string[];
    try {
      entries = parseLsA(await env.run(["ls", "-A", ctx.home]));
    } catch {
      return {};
    }
    if (!entries.length) return {};

    const { managed, review } = classifyDotfiles(entries, managedDotNames(registryEntries), NOISE);
    const result: CollectorResult = {};
    if (review.length) result["home.dotfiles.review"] = makeSection("home.dotfiles.review", { items: toItems(review) });
    if (managed.length)
      result["home.dotfiles.managed"] = makeSection("home.dotfiles.managed", { items: toItems(managed) });
    return result;
  };
}

export const collectDotfilesSweep = makeDotfilesSweepCollector();
