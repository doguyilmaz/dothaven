import { test, expect, describe } from "bun:test";
import { parseOllamaList, makeOllamaCollector } from "../../src/collectors/ollama";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

// `ollama list` columns: NAME  ID  SIZE  MODIFIED (2+ spaces between columns).
const real = `NAME              ID            SIZE      MODIFIED
llama3.2:latest   a80c4f17acd5  2.0 GB    2 weeks ago
qwen2.5:7b        845dbda0ea48  4.7 GB    3 months ago`;

describe("parseOllamaList", () => {
  test("parses rows, skips header, drops the ID column, keeps spaced values", () => {
    expect(parseOllamaList(real)).toEqual([
      { name: "llama3.2:latest", size: "2.0 GB", modified: "2 weeks ago" },
      { name: "qwen2.5:7b", size: "4.7 GB", modified: "3 months ago" },
    ]);
  });

  test("header only → []", () => {
    expect(parseOllamaList("NAME  ID  SIZE  MODIFIED")).toEqual([]);
  });

  test("empty → []", () => {
    expect(parseOllamaList("")).toEqual([]);
  });
});

describe("makeOllamaCollector", () => {
  test("builds ai.ollama.models with name | size | modified rows", async () => {
    const r = await makeOllamaCollector(fakeEnv({ outputs: { ollama: real } }))(ctx);
    expect(r["ai.ollama.models"]?.items).toEqual([
      { raw: "llama3.2:latest | 2.0 GB | 2 weeks ago", columns: ["llama3.2:latest", "2.0 GB", "2 weeks ago"] },
      { raw: "qwen2.5:7b | 4.7 GB | 3 months ago", columns: ["qwen2.5:7b", "4.7 GB", "3 months ago"] },
    ]);
  });

  test("ollama not installed (throws) → {}", async () => {
    const r = await makeOllamaCollector(
      fakeEnv({
        run: () => {
          throw new Error("command not found: ollama");
        },
      }),
    )(ctx);
    expect(r).toEqual({});
  });

  test("no models → {}", async () => {
    expect(await makeOllamaCollector(fakeEnv({ outputs: { ollama: "" } }))(ctx)).toEqual({});
  });
});
