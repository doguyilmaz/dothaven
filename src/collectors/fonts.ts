import type { Collector, CollectorResult } from "./types";
import { makeSection, toItems } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

const FONT_EXT = /\.(ttf|ttc|otf|otc|dfont|woff2?|pfb)$/i;

/** Keep only font files from a directory listing, sorted. */
export function filterFonts(names: string[]): string[] {
  return names.filter((n) => FONT_EXT.test(n)).sort();
}

/** Merge fonts found across several directories (missing dirs yield nothing). */
async function fontsIn(env: CommandEnv, dirs: string[]): Promise<string[]> {
  const names = new Set<string>();
  for (const dir of dirs) {
    for (const name of filterFonts(await env.listDir(dir))) names.add(name);
  }
  return [...names].sort();
}

export function makeFontsCollector(env: CommandEnv = defaultEnv): Collector {
  return async (ctx) => {
    const result: CollectorResult = {};

    const user = await fontsIn(env, [
      `${ctx.home}/Library/Fonts`, // macOS
      `${ctx.home}/.fonts`, // Linux
      `${ctx.home}/.local/share/fonts`, // Linux
    ]);
    if (user.length) result["fonts.user"] = makeSection("fonts.user", { items: toItems(user) });

    const system = await fontsIn(env, [
      "/Library/Fonts", // macOS
      "/usr/share/fonts", // Linux
      "/usr/local/share/fonts", // Linux
    ]);
    if (system.length) result["fonts.system"] = makeSection("fonts.system", { items: toItems(system) });

    return result;
  };
}

export const collectFonts = makeFontsCollector();
