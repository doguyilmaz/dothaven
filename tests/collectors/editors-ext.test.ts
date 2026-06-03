import { test, expect, describe } from "bun:test";
import { parseExtensions, makeEditorsExtCollector } from "../../src/collectors/editors-ext";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

describe("parseExtensions", () => {
  test("trims, drops blanks, sorts", () => {
    expect(parseExtensions("anthropic.claude-code\n\n  aaron-bond.better-comments \n")).toEqual([
      "aaron-bond.better-comments",
      "anthropic.claude-code",
    ]);
  });

  test("empty → []", () => {
    expect(parseExtensions("")).toEqual([]);
  });
});

describe("makeEditorsExtCollector", () => {
  test("collects VS Code and Cursor extensions into separate sections", async () => {
    const r = await makeEditorsExtCollector(
      fakeEnv({
        outputs: {
          code: "anthropic.claude-code\nadpyke.codesnap",
          cursor: "anthropic.claude-code",
        },
      }),
    )(ctx);
    expect(r["editor.vscode.extensions"]?.items).toEqual([
      { raw: "adpyke.codesnap", columns: ["adpyke.codesnap"] },
      { raw: "anthropic.claude-code", columns: ["anthropic.claude-code"] },
    ]);
    expect(r["editor.cursor.extensions"]?.items).toEqual([
      { raw: "anthropic.claude-code", columns: ["anthropic.claude-code"] },
    ]);
  });

  test("omits an editor that is not installed", async () => {
    const r = await makeEditorsExtCollector(fakeEnv({ outputs: { cursor: "some.ext" } }))(ctx);
    expect(r["editor.vscode.extensions"]).toBeUndefined();
    expect(r["editor.cursor.extensions"]).toBeDefined();
  });

  test("neither installed → {}", async () => {
    expect(await makeEditorsExtCollector(fakeEnv())(ctx)).toEqual({});
  });
});
