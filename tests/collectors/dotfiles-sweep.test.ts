import { test, expect, describe } from "bun:test";
import {
  managedDotNames,
  parseLsA,
  classifyDotfiles,
  makeDotfilesSweepCollector,
} from "../../src/collectors/dotfiles-sweep";
import { registryEntries } from "../../src/registry/entries";
import type { ConfigEntry } from "../../src/registry/types";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

describe("managedDotNames", () => {
  test("derives top-level ~/.X names from the real registry", () => {
    const s = managedDotNames(registryEntries);
    expect(s.has(".zshrc")).toBe(true);
    expect(s.has(".gitconfig")).toBe(true);
    expect(s.has(".config")).toBe(true); // from ~/.config/gh/...
    expect(s.has(".ssh")).toBe(true); // from ~/.ssh/config
    expect(s.has(".bunfig.toml")).toBe(true);
  });

  test("takes the first path segment after ~/", () => {
    const entry: ConfigEntry = {
      id: "x",
      name: "x",
      paths: { darwin: "~/.foo/bar.json" },
      category: "x",
      kind: { type: "file" },
      backupDest: "x",
      sensitivity: "low",
    };
    expect([...managedDotNames([entry])]).toEqual([".foo"]);
  });
});

describe("parseLsA", () => {
  test("splits lines, trims, drops blanks", () => {
    expect(parseLsA(".zshrc\n.config\n\n  .ssh \n")).toEqual([".zshrc", ".config", ".ssh"]);
  });
});

describe("classifyDotfiles", () => {
  test("splits managed / review, drops noise and non-dot entries", () => {
    const r = classifyDotfiles(
      [".zshrc", ".app-store", ".DS_Store", ".aws", "Documents", ".."],
      new Set([".zshrc"]),
      new Set([".DS_Store"]),
    );
    expect(r.managed).toEqual([".zshrc"]);
    expect(r.review).toEqual([".app-store", ".aws"]);
  });

  test("empty → empty buckets", () => {
    expect(classifyDotfiles([], new Set(), new Set())).toEqual({ managed: [], review: [] });
  });
});

describe("makeDotfilesSweepCollector", () => {
  test("classifies home dotfiles against the real registry", async () => {
    const collect = makeDotfilesSweepCollector(
      fakeEnv({ run: () => ".zshrc\n.app-store\n.DS_Store\n.aws\nDocuments" }),
    );
    const r = await collect(ctx);
    expect(r["home.dotfiles.managed"]?.items.map((i) => i.raw)).toContain(".zshrc");
    const review = r["home.dotfiles.review"]?.items.map((i) => i.raw) ?? [];
    expect(review).toContain(".app-store");
    expect(review).toContain(".aws");
    expect(review).not.toContain(".DS_Store");
    expect(review).not.toContain("Documents");
  });

  test("empty home → {}", async () => {
    expect(await makeDotfilesSweepCollector(fakeEnv({ run: () => "" }))(ctx)).toEqual({});
  });

  test("ls failure → {}", async () => {
    const collect = makeDotfilesSweepCollector(
      fakeEnv({
        run: () => {
          throw new Error("boom");
        },
      }),
    );
    expect(await collect(ctx)).toEqual({});
  });
});
