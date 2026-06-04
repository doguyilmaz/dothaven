import type { Collector } from "./types";
import { makeSection } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

export interface OllamaModel {
  name: string;
  size: string;
  modified: string;
}

/** Parse `ollama list` table output (header row + `NAME  ID  SIZE  MODIFIED`). */
export function parseOllamaList(text: string): OllamaModel[] {
  const lines = text.trim().split("\n");
  if (lines.length <= 1) return [];
  return lines
    .slice(1)
    .map((line) => {
      const parts = line
        .split(/\s{2,}/)
        .map((s) => s.trim())
        .filter(Boolean);
      const [name, _id, size, modified] = parts;
      return { name: name ?? "", size: size ?? "", modified: modified ?? "" };
    })
    .filter((m) => m.name);
}

export function makeOllamaCollector(env: CommandEnv = defaultEnv): Collector {
  return async () => {
    try {
      const models = parseOllamaList(await env.run(["ollama", "list"]));
      if (!models.length) return {};
      return {
        "ai.ollama.models": makeSection("ai.ollama.models", {
          items: models.map((m) => {
            const cols = [m.name, m.size, m.modified].filter(Boolean);
            return { raw: cols.join(" | "), columns: cols };
          }),
        }),
      };
    } catch {
      return {};
    }
  };
}

export const collectOllama = makeOllamaCollector();
