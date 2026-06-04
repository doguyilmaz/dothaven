import { test, expect, describe } from "bun:test";
import { serializeSnapshot, parseSnapshot } from "../../src/snapshot";
import type { Snapshot } from "../../src/snapshot";

const sec = (name: string, extra: Partial<Snapshot[string]> = {}): Snapshot[string] => ({
  name,
  pairs: {},
  items: [],
  content: null,
  ...extra,
});

describe("serializeSnapshot", () => {
  test("omits name, empty pairs/items and null content; pretty + trailing newline", () => {
    const snap: Snapshot = {
      "runtimes.go": sec("runtimes.go", { pairs: { version: "go1.26.3" } }),
      "packages.bun.global": sec("packages.bun.global", { items: [{ raw: "eas-cli@1", columns: ["eas-cli", "1"] }] }),
      "empty.section": sec("empty.section"),
    };
    const out = serializeSnapshot(snap);
    expect(out.endsWith("\n")).toBe(true);
    expect(out).toContain('  "runtimes.go": {'); // 2-space indent
    const parsed = JSON.parse(out);
    expect(parsed["runtimes.go"]).toEqual({ pairs: { version: "go1.26.3" } }); // no name, no items/content
    expect(parsed["packages.bun.global"]).toEqual({ items: [{ raw: "eas-cli@1", columns: ["eas-cli", "1"] }] });
    expect(parsed["empty.section"]).toEqual({}); // everything omitted
  });

  test("content of empty string is kept (only null is omitted)", () => {
    const out = JSON.parse(serializeSnapshot({ s: sec("s", { content: "" }) }));
    expect(out.s).toEqual({ content: "" });
  });
});

describe("parseSnapshot", () => {
  test("re-injects name from key and defaults missing fields", () => {
    const snap = parseSnapshot('{ "runtimes.go": { "pairs": { "version": "go1.26.3" } }, "x": {} }');
    expect(snap["runtimes.go"]).toEqual({
      name: "runtimes.go",
      pairs: { version: "go1.26.3" },
      items: [],
      content: null,
    });
    expect(snap.x).toEqual({ name: "x", pairs: {}, items: [], content: null });
  });

  test("normalizes malformed items defensively", () => {
    const snap = parseSnapshot('{ "s": { "items": [{ "raw": "ok" }, { "columns": ["a","b"] }, 5] } }');
    expect(snap.s.items).toEqual([
      { raw: "ok", columns: [] },
      { raw: "", columns: ["a", "b"] },
      { raw: "", columns: [] },
    ]);
  });

  test("rejects non-object roots (system boundary)", () => {
    expect(() => parseSnapshot("not json")).toThrow();
    expect(() => parseSnapshot("[1,2,3]")).toThrow();
    expect(() => parseSnapshot("42")).toThrow();
  });
});

describe("round-trip", () => {
  test("serialize → parse preserves pairs / items{raw,columns} / content verbatim", () => {
    const snap: Snapshot = {
      "a.b": sec("a.b", {
        pairs: { k1: "v1", k2: "v2" },
        items: [
          { raw: "pkg@2.0.0", columns: ["pkg", "2.0.0"] }, // raw with '@' survives (was lost in .dotf)
          { raw: "file with spaces.txt", columns: ["file with spaces.txt"] },
        ],
        content: "line1\nline2\n",
      }),
    };
    const back = parseSnapshot(serializeSnapshot(snap));
    expect(back).toEqual(snap);
  });
});
