import { test, expect, describe } from "bun:test";
import { parseAppList, makeAppsCollector } from "../../src/collectors/apps";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

const ctx: CollectorContext = { redact: true, home: "/fake/home" };
const RAYCAST = "/Applications/Raycast.app/Contents/Info.plist";
const ALTTAB = "/Applications/AltTab.app/Contents/Info.plist";

describe("parseAppList", () => {
  test("trims, drops blanks, sorts", () => {
    expect(parseAppList("Safari.app\n  Zed.app \n\nArc.app\n")).toEqual([
      "Arc.app",
      "Safari.app",
      "Zed.app",
    ]);
  });

  test("empty → []", () => {
    expect(parseAppList("")).toEqual([]);
  });
});

// The apps collector is macOS-specific (guards on process.platform).
const onlyDarwin = process.platform === "darwin";

describe("makeAppsCollector", () => {
  test.skipIf(!onlyDarwin)("reports installed apps + /Applications listing", async () => {
    const collect = makeAppsCollector(
      fakeEnv({
        files: [RAYCAST, ALTTAB],
        run: (cmd) => {
          if (cmd[0] === "ls") return "Safari.app\nZed.app";
          if (cmd[0] === "defaults") return "{ some = prefs; }";
          return "";
        },
      }),
    );
    const r = await collect(ctx);
    expect(r["apps.raycast"]?.pairs).toEqual({ installed: "true" });
    expect(r["apps.alttab"]?.pairs).toEqual({ installed: "true", preferences: "exists" });
    expect(r["apps.macos"]?.items).toEqual([
      { raw: "Safari.app", columns: ["Safari.app"] },
      { raw: "Zed.app", columns: ["Zed.app"] },
    ]);
  });

  test.skipIf(!onlyDarwin)("reports not-installed when plists are absent", async () => {
    const collect = makeAppsCollector(fakeEnv({ run: () => "" }));
    const r = await collect(ctx);
    expect(r["apps.raycast"]?.pairs).toEqual({ installed: "false" });
    expect(r["apps.alttab"]?.pairs).toEqual({ installed: "false" });
    expect(r["apps.macos"]).toBeUndefined();
  });

  test.skipIf(!onlyDarwin)("alttab installed but no readable prefs → no preferences key", async () => {
    const collect = makeAppsCollector(
      fakeEnv({
        files: [ALTTAB],
        run: (cmd) => {
          if (cmd[0] === "defaults") throw new Error("domain does not exist");
          return "";
        },
      }),
    );
    const r = await collect(ctx);
    expect(r["apps.alttab"]?.pairs).toEqual({ installed: "true" });
  });

  test.skipIf(onlyDarwin)("returns {} on non-darwin platforms", async () => {
    expect(await makeAppsCollector(fakeEnv())(ctx)).toEqual({});
  });
});
