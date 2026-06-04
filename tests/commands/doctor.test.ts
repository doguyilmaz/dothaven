import { test, expect, describe } from "bun:test";
import { findMissing } from "../../src/commands/doctor";
import { makeSection } from "../../src/collectors/types";

const items = (...names: string[]) => names.map((n) => ({ raw: n, columns: [n] }));

describe("findMissing", () => {
  test("reports installable items in snapshot but not on the machine", () => {
    const snapshot = {
      "packages.bun.global": makeSection("packages.bun.global", { items: items("a@1", "b@1", "c@1") }),
      "runtimes.android.platforms": makeSection("x", { items: items("android-34", "android-35") }),
    };
    const current = {
      "packages.bun.global": makeSection("packages.bun.global", { items: items("a@1") }),
      "runtimes.android.platforms": makeSection("x", { items: items("android-34", "android-35") }),
    };
    expect(findMissing(snapshot, current)).toEqual({ "packages.bun.global": ["b@1", "c@1"] });
  });

  test("ignores non-installable sections (configs, sweep, ssh hosts)", () => {
    const snapshot = {
      "home.dotfiles.review": makeSection("x", { items: items(".foo", ".bar") }),
      "ssh.hosts": makeSection("x", { items: items("h1", "h2") }),
      "runtimes.go": makeSection("x", { pairs: { version: "go1.26" } }),
    };
    expect(findMissing(snapshot, {})).toEqual({});
  });

  test("editor extensions are installable", () => {
    const snapshot = { "editor.vscode.extensions": makeSection("x", { items: items("biomejs.biome", "vscode.git") }) };
    const current = { "editor.vscode.extensions": makeSection("x", { items: items("vscode.git") }) };
    expect(findMissing(snapshot, current)).toEqual({ "editor.vscode.extensions": ["biomejs.biome"] });
  });

  test("missing section entirely → all its items reported", () => {
    const snapshot = { "fonts.user": makeSection("x", { items: items("A.ttf", "B.otf") }) };
    expect(findMissing(snapshot, {})).toEqual({ "fonts.user": ["A.ttf", "B.otf"] });
  });

  test("full parity → empty", () => {
    const snap = { "packages.npm.global": makeSection("x", { items: items("typescript@5") }) };
    expect(findMissing(snap, snap)).toEqual({});
  });

  test("matches by name across serialize/parse boundary, ignoring version drift", () => {
    // Snapshot loaded from .dotf: raw is "name | version"; live items use "name@version".
    const snapshot = {
      "packages.bun.global": makeSection("x", {
        items: [
          { raw: "pkg | 1.0.0", columns: ["pkg", "1.0.0"] },
          { raw: "gone | 1.0.0", columns: ["gone", "1.0.0"] },
        ],
      }),
    };
    const current = {
      "packages.bun.global": makeSection("x", { items: [{ raw: "pkg@2.0.0", columns: ["pkg", "2.0.0"] }] }),
    };
    expect(findMissing(snapshot, current)).toEqual({ "packages.bun.global": ["gone | 1.0.0"] });
  });

  test("falls back to raw's first segment when parsed columns are absent", () => {
    const snapshot = {
      "fonts.user": makeSection("x", {
        items: [
          { raw: "A.ttf", columns: [] },
          { raw: "B.otf", columns: [] },
        ],
      }),
    };
    const current = { "fonts.user": makeSection("x", { items: items("A.ttf") }) };
    expect(findMissing(snapshot, current)).toEqual({ "fonts.user": ["B.otf"] });
  });
});
