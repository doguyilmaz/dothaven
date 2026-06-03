import type { Collector, CollectorResult } from "./types";
import { makeSection } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

/** Parse `brew list --formula` / `--cask` (one name per line). */
export function parseBrewList(text: string): string[] {
  return text
    .trim()
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .sort();
}

const items = (names: string[]) => names.map((n) => ({ raw: n, columns: [n] }));

export function makeHomebrewCollector(env: CommandEnv = defaultEnv): Collector {
  return async () => {
    const result: CollectorResult = {};

    try {
      const formulae = parseBrewList(await env.run(["brew", "list", "--formula"]));
      if (formulae.length) result["apps.brew.formulae"] = makeSection("apps.brew.formulae", { items: items(formulae) });
    } catch {}

    try {
      const casks = parseBrewList(await env.run(["brew", "list", "--cask"]));
      if (casks.length) result["apps.brew.casks"] = makeSection("apps.brew.casks", { items: items(casks) });
    } catch {}

    return result;
  };
}

export const collectHomebrew = makeHomebrewCollector();
