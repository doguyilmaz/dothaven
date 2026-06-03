import type { Collector, CollectorResult } from "./types";
import { makeSection } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

const FONT_EXT = /\.(ttf|ttc|otf|otc|dfont|woff2?|pfb)$/i;

/** Keep only font files from a directory listing, sorted. */
export function filterFonts(names: string[]): string[] {
  return names.filter((n) => FONT_EXT.test(n)).sort();
}

const items = (names: string[]) => names.map((n) => ({ raw: n, columns: [n] }));

export function makeFontsCollector(env: CommandEnv = defaultEnv): Collector {
  return async (ctx) => {
    const result: CollectorResult = {};

    const user = filterFonts(await env.listDir(`${ctx.home}/Library/Fonts`));
    if (user.length) result["fonts.user"] = makeSection("fonts.user", { items: items(user) });

    const system = filterFonts(await env.listDir("/Library/Fonts"));
    if (system.length) result["fonts.system"] = makeSection("fonts.system", { items: items(system) });

    return result;
  };
}

export const collectFonts = makeFontsCollector();
