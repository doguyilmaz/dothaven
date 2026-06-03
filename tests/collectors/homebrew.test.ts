import { test, expect, describe } from "bun:test";
import { parseBrewList, parseBrewfile, makeHomebrewCollector } from "../../src/collectors/homebrew";
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

describe("parseBrewfile", () => {
  test("keeps Brewfile directives, drops cold-cache progress noise", () => {
    const raw = `✔︎ JSON API formula.jws.json\ntap "facebook/fb"\nbrew "glib"\ncask "stats"\nmas "Xcode", id: 497799835`;
    expect(parseBrewfile(raw)).toBe(`tap "facebook/fb"\nbrew "glib"\ncask "stats"\nmas "Xcode", id: 497799835`);
  });

  test("keeps comments, trims surrounding blank lines", () => {
    expect(parseBrewfile(`\n# Core lib\nbrew "glib"\n\n`)).toBe(`# Core lib\nbrew "glib"`);
  });

  test("empty → ''", () => {
    expect(parseBrewfile("")).toBe("");
  });
});

describe("makeHomebrewCollector", () => {
  const env = (table: Record<string, string>) => fakeEnv({ run: (cmd) => table[cmd.join(" ")] ?? "" });

  test("collects formulae, casks, and a restorable Brewfile", async () => {
    const r = await makeHomebrewCollector(
      env({
        "brew list --formula": "git\njq",
        "brew list --cask": "raycast\nzed",
        "brew bundle dump --file=-": `tap "x/y"\nbrew "git"\ncask "raycast"`,
      }),
    )(ctx);
    expect(r["apps.brew.formulae"]?.items).toEqual([
      { raw: "git", columns: ["git"] },
      { raw: "jq", columns: ["jq"] },
    ]);
    expect(r["apps.brew.casks"]?.items).toEqual([
      { raw: "raycast", columns: ["raycast"] },
      { raw: "zed", columns: ["zed"] },
    ]);
    expect(r["apps.brew.bundle"]?.content).toBe(`tap "x/y"\nbrew "git"\ncask "raycast"`);
  });

  test("omits empty sections", async () => {
    const r = await makeHomebrewCollector(env({ "brew list --formula": "git" }))(ctx);
    expect(Object.keys(r)).toEqual(["apps.brew.formulae"]);
  });

  test("brew not installed (throws) → {}", async () => {
    const r = await makeHomebrewCollector(
      fakeEnv({
        run: () => {
          throw new Error("command not found: brew");
        },
      }),
    )(ctx);
    expect(r).toEqual({});
  });
});
