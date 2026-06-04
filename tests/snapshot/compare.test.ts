import { test, expect, describe } from "bun:test";
import { compareSnapshots, formatDiff } from "../../src/snapshot";
import type { Snapshot } from "../../src/snapshot";

const sec = (name: string, extra: Partial<Snapshot[string]> = {}): Snapshot[string] => ({
  name,
  pairs: {},
  items: [],
  content: null,
  ...extra,
});
const item = (raw: string, ...columns: string[]) => ({ raw, columns: columns.length ? columns : [raw] });

describe("compareSnapshots (orientation: first arg = left = added)", () => {
  test("left-only section is 'added', right-only is 'removed'", () => {
    const diff = compareSnapshots({ onlyLeft: sec("onlyLeft") }, { onlyRight: sec("onlyRight") });
    expect(diff.sections.onlyLeft.status).toBe("added");
    expect(diff.sections.onlyRight.status).toBe("removed");
  });

  test("items diff keys on raw: added=left-only, removed=right-only, common=both", () => {
    const left = { s: sec("s", { items: [item("keep"), item("ladd")] }) };
    const right = { s: sec("s", { items: [item("keep"), item("radd")] }) };
    const d = compareSnapshots(left, right).sections.s;
    expect(d.status).toBe("changed");
    expect(d.items).toEqual({ added: ["ladd"], removed: ["radd"], common: ["keep"] });
  });

  test("pairs diff: added(left-only), removed(right-only), changed(both differ), common(both equal)", () => {
    const left = { s: sec("s", { pairs: { same: "1", chg: "L", onlyL: "l" } }) };
    const right = { s: sec("s", { pairs: { same: "1", chg: "R", onlyR: "r" } }) };
    const d = compareSnapshots(left, right).sections.s;
    expect(d.pairs.added).toEqual({ onlyL: "l" });
    expect(d.pairs.removed).toEqual({ onlyR: "r" });
    expect(d.pairs.changed).toEqual({ chg: { left: "L", right: "R" } });
    expect(d.pairs.common).toEqual({ same: "1" });
  });

  test("items in common alone do NOT mark a section changed", () => {
    const same = { s: sec("s", { items: [item("a")], pairs: { k: "v" }, content: "c" }) };
    expect(compareSnapshots(same, structuredClone(same)).sections.s.status).toBe("equal");
  });

  test("content diff is strict string|null inequality", () => {
    const d = compareSnapshots({ s: sec("s", { content: "x" }) }, { s: sec("s", { content: null }) }).sections.s;
    expect(d.content).toEqual({ left: "x", right: null, changed: true });
    expect(d.status).toBe("changed");
  });

  test("section name union is left-first", () => {
    const diff = compareSnapshots({ b: sec("b"), a: sec("a") }, { c: sec("c"), a: sec("a") });
    expect(Object.keys(diff.sections)).toEqual(["b", "a", "c"]);
  });
});

describe("formatDiff (verified byte-identical to @dotformat/core)", () => {
  test("color:false renders the fixed emission order with correct +/-/~/= and labels", () => {
    const left = {
      pkg: sec("pkg", { items: [item("hello"), item("world")], pairs: { a: "1", b: "L" }, content: "X" }),
    };
    const right = { pkg: sec("pkg", { items: [item("world")], pairs: { a: "1", b: "R", c: "2" }, content: "Y" }) };
    const out = formatDiff(compareSnapshots(left, right), { color: false });
    expect(out).toBe(
      [
        "[pkg]",
        "  + hello  (only in left)", // item left-only = added
        "  = world", // item common
        "  - c = 2  (only in right)", // pair right-only = removed
        "  ~ b = L → R", // pair changed (U+2192)
        "  = a = 1", // pair common
        "  ~ content changed",
      ].join("\n"),
    );
  });

  test("added/removed section headers use leftLabel/rightLabel", () => {
    const out = formatDiff(compareSnapshots({ x: sec("x", { items: [item("i")] }) }, {}), {
      leftLabel: "new",
      rightLabel: "old",
      color: false,
    });
    expect(out).toBe(["+ [x]  (only in new)", "  + i  (only in new)"].join("\n"));
  });

  test("color:true wraps in ANSI codes; no trailing newline", () => {
    const out = formatDiff(compareSnapshots({ x: sec("x", { items: [item("i")] }) }, { x: sec("x") }), { color: true });
    expect(out).toContain("\x1b[32m+ i\x1b[0m"); // green add
    expect(out.endsWith("\n")).toBe(false);
  });

  test("equal sections are still emitted (dim = lines) — no filtering", () => {
    const s = { x: sec("x", { items: [item("i")], pairs: { k: "v" } }) };
    const out = formatDiff(compareSnapshots(s, structuredClone(s)), { color: false });
    expect(out).toBe(["[x]", "  = i", "  = k = v"].join("\n"));
  });
});
