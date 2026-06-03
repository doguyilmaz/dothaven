import { test, expect, describe } from "bun:test";
import { parseBrewList, makeHomebrewCollector } from "../../src/collectors/homebrew";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

describe("parseBrewList", () => {
  test("trims, drops blanks, sorts", () => {
    expect(parseBrewList("git\n  jq \n\nbat\n")).toEqual(["bat", "git", "jq"]);
  });

  test("empty → []", () => {
    expect(parseBrewList("")).toEqual([]);
    expect(parseBrewList("   \n  ")).toEqual([]);
  });
});

describe("makeHomebrewCollector", () => {
  test("collects formulae and casks into separate sections", async () => {
    const collect = makeHomebrewCollector(
      fakeEnv({
        run: (cmd) => {
          const sub = cmd[cmd.length - 1];
          if (sub === "--formula") return "git\njq";
          if (sub === "--cask") return "raycast\nzed";
          return "";
        },
      }),
    );
    const r = await collect(ctx);
    expect(r["apps.brew.formulae"]?.items).toEqual([
      { raw: "git", columns: ["git"] },
      { raw: "jq", columns: ["jq"] },
    ]);
    expect(r["apps.brew.casks"]?.items).toEqual([
      { raw: "raycast", columns: ["raycast"] },
      { raw: "zed", columns: ["zed"] },
    ]);
  });

  test("omits empty sections", async () => {
    const collect = makeHomebrewCollector(
      fakeEnv({ run: (cmd) => (cmd[cmd.length - 1] === "--formula" ? "git" : "") }),
    );
    const r = await collect(ctx);
    expect(Object.keys(r)).toEqual(["apps.brew.formulae"]);
  });

  test("brew not installed (throws) → {}", async () => {
    const collect = makeHomebrewCollector(
      fakeEnv({
        run: () => {
          throw new Error("command not found: brew");
        },
      }),
    );
    expect(await collect(ctx)).toEqual({});
  });
});
