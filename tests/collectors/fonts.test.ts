import { test, expect, describe } from "bun:test";
import { filterFonts, makeFontsCollector } from "../../src/collectors/fonts";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

describe("filterFonts", () => {
  test("keeps font files (case-insensitive), drops others, sorts", () => {
    expect(filterFonts(["b.ttf", "a.OTF", "readme.txt", ".DS_Store", "c.woff2", "d.ttc"])).toEqual([
      "a.OTF",
      "b.ttf",
      "c.woff2",
      "d.ttc",
    ]);
  });

  test("empty → []", () => {
    expect(filterFonts([])).toEqual([]);
  });
});

describe("makeFontsCollector", () => {
  test("collects user + system fonts, filtering non-fonts", async () => {
    const collect = makeFontsCollector(
      fakeEnv({
        dirs: {
          "/fake/home/Library/Fonts": ["FedraSansTogg-Bold.otf", "junk.txt"],
          "/Library/Fonts": ["Arial Unicode.ttf"],
        },
      }),
    );
    const r = await collect(ctx);
    expect(r["fonts.user"]?.items).toEqual([{ raw: "FedraSansTogg-Bold.otf", columns: ["FedraSansTogg-Bold.otf"] }]);
    expect(r["fonts.system"]?.items).toEqual([{ raw: "Arial Unicode.ttf", columns: ["Arial Unicode.ttf"] }]);
  });

  test("collects Linux user fonts (~/.local/share/fonts) and dedupes across dirs", async () => {
    const collect = makeFontsCollector(
      fakeEnv({
        dirs: {
          "/fake/home/Library/Fonts": ["Shared.ttf"],
          "/fake/home/.local/share/fonts": ["JetBrainsMono.ttf", "Shared.ttf"],
        },
      }),
    );
    const r = await collect(ctx);
    expect(r["fonts.user"]?.items.map((i) => i.raw)).toEqual(["JetBrainsMono.ttf", "Shared.ttf"]);
  });

  test("no font directories → {}", async () => {
    expect(await makeFontsCollector(fakeEnv())(ctx)).toEqual({});
  });

  test("directory with no fonts → omitted", async () => {
    const collect = makeFontsCollector(fakeEnv({ dirs: { "/fake/home/Library/Fonts": ["notes.txt"] } }));
    expect(await collect(ctx)).toEqual({});
  });
});
