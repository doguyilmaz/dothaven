import type { Collector, CollectorResult } from "./types";
import { makeSection, toItems } from "./types";
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

// Progress/noise lines `brew bundle dump` may emit (denylist). Everything else is kept,
// so first-class directives (go/npm/cargo/uv/whalebrew/vscode/mas/…) survive — an
// allowlist silently dropped go/npm and produced an incomplete "restorable" Brewfile.
const BREWFILE_NOISE = /^\s*(✔|✓|✗|⚠|ℹ|==>|Warning:|Error:)|JSON API/;

/** Clean `brew bundle dump` stdout into a restorable Brewfile (drop noise, keep every directive). */
export function parseBrewfile(text: string): string {
  return text
    .split("\n")
    .filter((l) => !BREWFILE_NOISE.test(l))
    .join("\n")
    .trim();
}

export function makeHomebrewCollector(env: CommandEnv = defaultEnv): Collector {
  return async () => {
    const result: CollectorResult = {};

    try {
      const formulae = parseBrewList(await env.run(["brew", "list", "--formula"]));
      if (formulae.length)
        result["apps.brew.formulae"] = makeSection("apps.brew.formulae", { items: toItems(formulae) });
    } catch {}

    try {
      const casks = parseBrewList(await env.run(["brew", "list", "--cask"]));
      if (casks.length) result["apps.brew.casks"] = makeSection("apps.brew.casks", { items: toItems(casks) });
    } catch {}

    // A restorable Brewfile (taps + brews + casks + mas) — superset used by `brew bundle`.
    try {
      const brewfile = parseBrewfile(await env.run(["brew", "bundle", "dump", "--file=-"]));
      if (brewfile) result["apps.brew.bundle"] = makeSection("apps.brew.bundle", { content: brewfile });
    } catch {}

    return result;
  };
}

export const collectHomebrew = makeHomebrewCollector();
