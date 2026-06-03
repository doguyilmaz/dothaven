import type { Collector, CollectorResult } from "./types";
import { makeSection } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

/** Parse `code --list-extensions` / `cursor --list-extensions` (one id per line). */
export function parseExtensions(text: string): string[] {
  return text
    .trim()
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .sort();
}

const items = (exts: string[]) => exts.map((e) => ({ raw: e, columns: [e] }));

export function makeEditorsExtCollector(env: CommandEnv = defaultEnv): Collector {
  return async () => {
    const result: CollectorResult = {};

    try {
      const code = parseExtensions(await env.run(["code", "--list-extensions"]));
      if (code.length) result["editor.vscode.extensions"] = makeSection("editor.vscode.extensions", { items: items(code) });
    } catch {}

    try {
      const cursor = parseExtensions(await env.run(["cursor", "--list-extensions"]));
      if (cursor.length) result["editor.cursor.extensions"] = makeSection("editor.cursor.extensions", { items: items(cursor) });
    } catch {}

    return result;
  };
}

export const collectEditorsExt = makeEditorsExtCollector();
