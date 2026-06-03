import type { Collector, CollectorResult } from "./types";
import { makeSection } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

const RAYCAST_PLIST = "/Applications/Raycast.app/Contents/Info.plist";
const ALTTAB_PLIST = "/Applications/AltTab.app/Contents/Info.plist";

/** Parse `ls /Applications` output (one entry per line). */
export function parseAppList(text: string): string[] {
  return text
    .trim()
    .split("\n")
    .map((a) => a.trim())
    .filter(Boolean)
    .sort();
}

export function makeAppsCollector(env: CommandEnv = defaultEnv): Collector {
  return async () => {
    if (process.platform !== "darwin") return {};
    const result: CollectorResult = {};

    result["apps.raycast"] = makeSection("apps.raycast", {
      pairs: { installed: (await env.fileExists(RAYCAST_PLIST)) ? "true" : "false" },
    });

    const alttabInstalled = await env.fileExists(ALTTAB_PLIST);
    const alttabPairs: Record<string, string> = {
      installed: alttabInstalled ? "true" : "false",
    };
    if (alttabInstalled) {
      try {
        const prefs = await env.run(["defaults", "read", "com.lwouis.alt-tab-macos"]);
        if (prefs.trim()) alttabPairs.preferences = "exists";
      } catch {}
    }
    result["apps.alttab"] = makeSection("apps.alttab", { pairs: alttabPairs });

    try {
      const apps = parseAppList(await env.run(["ls", "/Applications"]));
      if (apps.length) {
        result["apps.macos"] = makeSection("apps.macos", {
          items: apps.map((a) => ({ raw: a, columns: [a] })),
        });
      }
    } catch {}

    return result;
  };
}

export const collectApps = makeAppsCollector();
