import { test, expect, describe } from "bun:test";
import { splitList } from "../../src/utils/args";

describe("splitList", () => {
  test("trims tokens and drops empties / trailing commas", () => {
    expect(splitList("ai, ssh,")).toEqual(["ai", "ssh"]);
    expect(splitList("  editor  ")).toEqual(["editor"]);
    expect(splitList("a,,b")).toEqual(["a", "b"]);
  });

  test("empty string → []", () => {
    expect(splitList("")).toEqual([]);
    expect(splitList("  ,  ,")).toEqual([]);
  });
});
