import { test, expect, describe } from "bun:test";
import {
  parseNpmGlobal,
  parseBunGlobal,
  parsePnpmGlobal,
  parseFnmList,
  makePackagesCollector,
} from "../../src/collectors/packages";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

// ─── Pure parsers ──────────────────────────────────────────────────────────

describe("parseNpmGlobal", () => {
  // Real `npm ls -g --depth=0 --json` shape captured from the machine.
  const real = JSON.stringify({
    name: "lib",
    dependencies: {
      "@swmansion/argent": { version: "0.9.0", overridden: false },
      corepack: { version: "0.35.0", overridden: false },
      npm: { version: "11.13.0", overridden: false },
    },
  });

  test("extracts name + version, sorted", () => {
    expect(parseNpmGlobal(real)).toEqual([
      { name: "@swmansion/argent", version: "0.9.0" },
      { name: "corepack", version: "0.35.0" },
      { name: "npm", version: "11.13.0" },
    ]);
  });

  test("missing version field → empty version string", () => {
    expect(parseNpmGlobal(JSON.stringify({ dependencies: { foo: {} } }))).toEqual([
      { name: "foo", version: "" },
    ]);
  });

  test("null / absent dependencies → []", () => {
    expect(parseNpmGlobal(JSON.stringify({ name: "lib" }))).toEqual([]);
    expect(parseNpmGlobal(JSON.stringify({ dependencies: null }))).toEqual([]);
  });

  test("invalid / empty json → [] (npm warnings or no output)", () => {
    expect(parseNpmGlobal("not json")).toEqual([]);
    expect(parseNpmGlobal("")).toEqual([]);
  });
});

describe("parseBunGlobal", () => {
  // Real `bun pm ls -g` shape: header line + tree rows.
  const real = `/Users/doguyilmaz/.bun/install/global node_modules (3520)
├── @anthropic-ai/claude-code@2.1.20
├── @google/gemini-cli@0.45.0
├── eas-cli@16.19.2
└── yo@5.1.0`;

  test("parses tree, skips header, handles scoped names + last (└──) row", () => {
    const pkgs = parseBunGlobal(real);
    expect(pkgs).toHaveLength(4);
    expect(pkgs).toContainEqual({ name: "@anthropic-ai/claude-code", version: "2.1.20" });
    expect(pkgs).toContainEqual({ name: "@google/gemini-cli", version: "0.45.0" });
    expect(pkgs).toContainEqual({ name: "eas-cli", version: "16.19.2" });
    expect(pkgs).toContainEqual({ name: "yo", version: "5.1.0" });
    expect(pkgs.some((p) => p.name.includes("node_modules"))).toBe(false);
  });

  test("row without a version → empty version string", () => {
    expect(parseBunGlobal("header\n└── lonelypkg")).toEqual([{ name: "lonelypkg", version: "" }]);
  });

  test("ignores blank lines and non-tree lines", () => {
    const messy = "header\n\n├── a@1.0.0\nrandom noise\n└── b@2.0.0\n";
    expect(parseBunGlobal(messy)).toEqual([
      { name: "a", version: "1.0.0" },
      { name: "b", version: "2.0.0" },
    ]);
  });

  test("empty → []", () => {
    expect(parseBunGlobal("")).toEqual([]);
  });
});

describe("parsePnpmGlobal", () => {
  test("array form (pnpm --json)", () => {
    const json = JSON.stringify([
      { path: "/x", dependencies: { tldr: { version: "3.3.0", from: "tldr" } } },
    ]);
    expect(parsePnpmGlobal(json)).toEqual([{ name: "tldr", version: "3.3.0" }]);
  });

  test("object form", () => {
    expect(parsePnpmGlobal(JSON.stringify({ dependencies: { tldr: { version: "3.3.0" } } }))).toEqual([
      { name: "tldr", version: "3.3.0" },
    ]);
  });

  test("invalid / empty → []", () => {
    expect(parsePnpmGlobal("x")).toEqual([]);
    expect(parsePnpmGlobal("")).toEqual([]);
    expect(parsePnpmGlobal("[]")).toEqual([]);
  });
});

describe("parseFnmList", () => {
  // Real `fnm ls` shape.
  const real = `* v20.20.2
* v24.16.0 default
* system`;

  test("parses versions and default flag", () => {
    expect(parseFnmList(real)).toEqual([
      { version: "v20.20.2", isDefault: false },
      { version: "v24.16.0", isDefault: true },
      { version: "system", isDefault: false },
    ]);
  });

  test("tolerates rows without the '*' marker and extra whitespace", () => {
    expect(parseFnmList("  v18.0.0  \n  v20.0.0 default ")).toEqual([
      { version: "v18.0.0", isDefault: false },
      { version: "v20.0.0", isDefault: true },
    ]);
  });

  test("isolates version when extra aliases follow (multi-alias / comma-separated)", () => {
    expect(parseFnmList("* v24.16.0 default, lts-latest")).toEqual([
      { version: "v24.16.0", isDefault: true },
    ]);
    expect(parseFnmList("* v22.0.0 lts-jod")).toEqual([{ version: "v22.0.0", isDefault: false }]);
  });

  test("empty → []", () => {
    expect(parseFnmList("")).toEqual([]);
  });
});

// ─── Collector logic (mocked env, deterministic) ─────────────────────────────

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

describe("makePackagesCollector", () => {
  test("assembles every section with correct ids and item shapes", async () => {
    const collect = makePackagesCollector(
      fakeEnv({
        outputs: {
          npm: JSON.stringify({ dependencies: { typescript: { version: "5.4.0" } } }),
          bun: "header\n└── eas-cli@16.19.2",
          pnpm: JSON.stringify({ dependencies: { tldr: { version: "3.3.0" } } }),
          fnm: "* v20.0.0\n* v22.0.0 default",
        },
        dirs: { "/fake/home/.deno/bin": ["deno-deploy"] },
      }),
    );
    const r = await collect(ctx);

    expect(r["packages.npm.global"]?.items).toEqual([
      { raw: "typescript@5.4.0", columns: ["typescript", "5.4.0"] },
    ]);
    expect(r["packages.bun.global"]?.items).toEqual([
      { raw: "eas-cli@16.19.2", columns: ["eas-cli", "16.19.2"] },
    ]);
    expect(r["packages.pnpm.global"]?.items).toEqual([
      { raw: "tldr@3.3.0", columns: ["tldr", "3.3.0"] },
    ]);
    expect(r["packages.node.fnm"]?.items).toEqual([
      { raw: "v20.0.0", columns: ["v20.0.0"] },
      { raw: "v22.0.0 (default)", columns: ["v22.0.0", "default"] },
    ]);
    expect(r["packages.deno.bin"]?.items).toEqual([
      { raw: "deno-deploy", columns: ["deno-deploy"] },
    ]);
  });

  test("omits sections whose tool produced no output", async () => {
    const collect = makePackagesCollector(fakeEnv({ outputs: { bun: "header\n└── only@1.0.0" } }));
    const r = await collect(ctx);
    expect(Object.keys(r)).toEqual(["packages.bun.global"]);
  });

  test("no tools present → empty result", async () => {
    expect(await makePackagesCollector(fakeEnv())(ctx)).toEqual({});
  });

  test("a throwing tool does not break the others (isolation)", async () => {
    const collect = makePackagesCollector(
      fakeEnv({
        run: (cmd) => {
          if (cmd[0] === "npm") throw new Error("npm exploded");
          if (cmd[0] === "bun") return "header\n└── ok@1.0.0";
          return "";
        },
      }),
    );
    const r = await collect(ctx);
    expect(r["packages.npm.global"]).toBeUndefined();
    expect(r["packages.bun.global"]?.items).toEqual([{ raw: "ok@1.0.0", columns: ["ok", "1.0.0"] }]);
  });

  test("deno bin entries are sorted", async () => {
    const collect = makePackagesCollector(fakeEnv({ dirs: { "/fake/home/.deno/bin": ["zed", "abc", "mid"] } }));
    const r = await collect(ctx);
    expect(r["packages.deno.bin"]?.items.map((i) => i.raw)).toEqual(["abc", "mid", "zed"]);
  });
});
