import type { ConfigEntry, Platform } from "./types";
import type { Collector, CollectorResult } from "../collectors/types";
import { makeSection } from "../collectors/types";

export function registryCollector(entries: ConfigEntry[]): Collector {
  return async (ctx) => {
    const platform = process.platform as Platform;
    const result: CollectorResult = {};

    for (const entry of entries) {
      const template = entry.paths[platform];
      if (!template) continue;

      const absPath = template.replace("~", ctx.home);

      try {
        switch (entry.kind.type) {
          case "file": {
            if ("metadata" in entry.kind && entry.kind.metadata) {
              const file = Bun.file(absPath);
              if (!(await file.exists())) break;
              const content = await file.text();
              const lineCount = content.split("\n").length;
              result[entry.id] = makeSection(entry.id, {
                pairs: { exists: "true", lines: String(lineCount) },
              });
            } else {
              const file = Bun.file(absPath);
              if (!(await file.exists())) break;
              let content = await file.text();
              if (ctx.redact && entry.redact) content = entry.redact(content);
              result[entry.id] = makeSection(entry.id, { content: content.trim() });
            }
            break;
          }

          case "dir": {
            const glob = new Bun.Glob("*");
            const items: { raw: string; columns: string[] }[] = [];
            for await (const name of glob.scan(absPath)) {
              items.push({ raw: name, columns: [name] });
            }
            if (items.length) {
              result[entry.id] = makeSection(entry.id, { items });
            }
            break;
          }

          case "json-extract": {
            const file = Bun.file(absPath);
            if (!(await file.exists())) break;
            const json = await file.json();
            const pairs: Record<string, string> = {};

            const fields = entry.kind.fields.length > 0 ? entry.kind.fields : Object.keys(json);
            for (const field of fields) {
              if (json[field] !== undefined) {
                if (typeof json[field] === "object") {
                  for (const [k, v] of Object.entries(json[field] as Record<string, unknown>)) {
                    pairs[k] = String(v);
                  }
                } else {
                  pairs[field] = typeof json[field] === "object" ? JSON.stringify(json[field]) : String(json[field]);
                }
              }
            }

            if (Object.keys(pairs).length) {
              result[entry.id] = makeSection(entry.id, { pairs });
            }
            break;
          }
        }
      } catch {
        // Entry not available (file missing, dir not found, JSON parse error) — skip
      }
    }

    return result;
  };
}
