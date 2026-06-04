import { test, expect, describe } from "bun:test";
import {
  parseVersionToken,
  parseGoVersion,
  parseRustupToolchains,
  parseCargoCrates,
  parseSwiftVersion,
  parseXcodeVersion,
  parseZigVersion,
  parseAdbVersion,
  makeRuntimesCollector,
} from "../../src/collectors/runtimes";
import type { CollectorContext } from "../../src/collectors/types";
import { fakeEnv } from "../helpers/fake-env";

// ─── Pure parsers (real CLI output) ──────────────────────────────────────────

describe("parseVersionToken", () => {
  test("extracts the token after a word", () => {
    expect(parseVersionToken("rustc 1.96.0 (ac68faa20 2026-05-25)", "rustc")).toBe("1.96.0");
    expect(parseVersionToken("cargo 1.96.0 (30a34c682 2026-05-25)", "cargo")).toBe("1.96.0");
  });
  test("missing → ''", () => {
    expect(parseVersionToken("nope", "rustc")).toBe("");
  });
});

describe("parseGoVersion", () => {
  test("parses version + platform", () => {
    expect(parseGoVersion("go version go1.26.3 darwin/arm64")).toEqual({
      version: "go1.26.3",
      platform: "darwin/arm64",
    });
  });
  test("invalid → null", () => {
    expect(parseGoVersion("")).toBeNull();
    expect(parseGoVersion("command not found")).toBeNull();
  });
});

describe("parseRustupToolchains", () => {
  test("parses name + flags, plain toolchains, ignores prose lines", () => {
    const text = "stable-aarch64-apple-darwin (active, default)\nnightly-aarch64-apple-darwin";
    expect(parseRustupToolchains(text)).toEqual([
      { name: "stable-aarch64-apple-darwin", flags: "active, default" },
      { name: "nightly-aarch64-apple-darwin", flags: "" },
    ]);
  });
  test("ignores prose status lines (no default toolchain set)", () => {
    expect(parseRustupToolchains("no installed toolchains")).toEqual([]);
    expect(
      parseRustupToolchains("stable-aarch64-apple-darwin (default)\nerror: rustup could not choose a version"),
    ).toEqual([{ name: "stable-aarch64-apple-darwin", flags: "default" }]);
  });

  test("empty → []", () => {
    expect(parseRustupToolchains("")).toEqual([]);
  });
});

describe("parseCargoCrates", () => {
  test("parses 'name vX.Y.Z:' lines, ignores indented binaries", () => {
    expect(parseCargoCrates("ripgrep v14.1.0:\n    rg\nfd-find v10.2.0:\n    fd")).toEqual([
      { name: "ripgrep", version: "14.1.0" },
      { name: "fd-find", version: "10.2.0" },
    ]);
  });
  test("parses git- and path-source crates (source annotation before colon)", () => {
    expect(
      parseCargoCrates("cargo-watch v8.5.0 (https://github.com/watchexec/cargo-watch#a1b2c3d):\n    cargo-watch"),
    ).toEqual([{ name: "cargo-watch", version: "8.5.0" }]);
    expect(parseCargoCrates("localtool v0.2.0 (/Users/me/proj):\n    localtool")).toEqual([
      { name: "localtool", version: "0.2.0" },
    ]);
  });

  test("empty (no global crates) → []", () => {
    expect(parseCargoCrates("")).toEqual([]);
  });
});

describe("parseSwiftVersion", () => {
  test("extracts Apple Swift version", () => {
    const text = "swift-driver version: 1.148.6 Apple Swift version 6.3.1 (swiftlang-6.3.1)";
    expect(parseSwiftVersion(text)).toBe("6.3.1");
  });
  test("missing → ''", () => {
    expect(parseSwiftVersion("")).toBe("");
  });
});

describe("parseXcodeVersion", () => {
  test("parses version + build", () => {
    expect(parseXcodeVersion("Xcode 26.4.1\nBuild version 17E202")).toEqual({
      version: "26.4.1",
      build: "17E202",
    });
  });
  test("version without build line", () => {
    expect(parseXcodeVersion("Xcode 26.4.1")).toEqual({ version: "26.4.1", build: "" });
  });
  test("missing → null", () => {
    expect(parseXcodeVersion("")).toBeNull();
  });
});

describe("parseZigVersion", () => {
  test("single version line", () => {
    expect(parseZigVersion("0.13.0\n")).toBe("0.13.0");
  });
  test("non-version output → ''", () => {
    expect(parseZigVersion("command not found: zig")).toBe("");
    expect(parseZigVersion("")).toBe("");
  });
});

describe("parseAdbVersion", () => {
  test("extracts platform-tools Version line", () => {
    const text = "Android Debug Bridge version 1.0.41\nVersion 36.0.2-14143358\nInstalled as /x";
    expect(parseAdbVersion(text)).toBe("36.0.2-14143358");
  });
  test("missing → ''", () => {
    expect(parseAdbVersion("")).toBe("");
  });
});

// ─── Collector logic (mocked env) ────────────────────────────────────────────

const ctx: CollectorContext = { redact: true, home: "/fake/home" };

function runtimeEnv() {
  return fakeEnv({
    run: (cmd) => {
      const key = cmd.join(" ");
      const table: Record<string, string> = {
        "go version": "go version go1.26.3 darwin/arm64",
        "rustc --version": "rustc 1.96.0 (ac68faa20 2026-05-25)",
        "cargo --version": "cargo 1.96.0 (30a34c682 2026-05-25)",
        "cargo install --list": "ripgrep v14.1.0:\n    rg",
        "rustup toolchain list": "stable-aarch64-apple-darwin (active, default)",
        "swift --version": "swift-driver version: 1.148.6 Apple Swift version 6.3.1 (x)",
        "zig version": "0.13.0",
        "xcodebuild -version": "Xcode 26.4.1\nBuild version 17E202",
        "xcode-select -p": "/Applications/Xcode.app/Contents/Developer",
        "adb version": "Android Debug Bridge version 1.0.41\nVersion 36.0.2-14143358",
      };
      return table[key] ?? "";
    },
    files: ["/fake/home/Library/Android/sdk"],
    dirs: {
      "/fake/home/Library/Android/sdk/build-tools": ["35.0.0", "34.0.0"],
      "/fake/home/Library/Android/sdk/platforms": ["android-36", "android-33"],
    },
  });
}

describe("makeRuntimesCollector", () => {
  test("assembles all toolchain sections", async () => {
    const r = await makeRuntimesCollector(runtimeEnv())(ctx);

    expect(r["runtimes.go"]?.pairs).toEqual({ version: "go1.26.3", platform: "darwin/arm64" });
    expect(r["runtimes.rust"]?.pairs).toEqual({ rustc: "1.96.0", cargo: "1.96.0" });
    expect(r["runtimes.rust.toolchains"]?.items).toEqual([
      { raw: "stable-aarch64-apple-darwin (active, default)", columns: ["stable-aarch64-apple-darwin", "active, default"] },
    ]);
    expect(r["runtimes.rust.crates"]?.items).toEqual([
      { raw: "ripgrep@14.1.0", columns: ["ripgrep", "14.1.0"] },
    ]);
    expect(r["runtimes.swift"]?.pairs).toEqual({ version: "6.3.1" });
    expect(r["runtimes.zig"]?.pairs).toEqual({ version: "0.13.0" });
    expect(r["runtimes.xcode"]?.pairs).toEqual({
      version: "26.4.1",
      build: "17E202",
      path: "/Applications/Xcode.app/Contents/Developer",
    });
    expect(r["runtimes.android"]?.pairs).toEqual({
      sdk: "/fake/home/Library/Android/sdk",
      platformTools: "36.0.2-14143358",
    });
    expect(r["runtimes.android.buildTools"]?.items.map((i) => i.raw)).toEqual(["34.0.0", "35.0.0"]);
    expect(r["runtimes.android.platforms"]?.items.map((i) => i.raw)).toEqual(["android-33", "android-36"]);
  });

  test("no toolchains present → empty result", async () => {
    expect(await makeRuntimesCollector(fakeEnv())(ctx)).toEqual({});
  });

  test("honors ANDROID_HOME over the default macOS path", async () => {
    const env = fakeEnv({
      env: { ANDROID_HOME: "/opt/android-sdk" },
      files: ["/opt/android-sdk"],
      dirs: { "/opt/android-sdk/platforms": ["android-34"] },
      run: (cmd) => (cmd.join(" ") === "adb version" ? "Android Debug Bridge version 1.0.41\nVersion 35.0.0" : ""),
    });
    const r = await makeRuntimesCollector(env)(ctx);
    expect(r["runtimes.android"]?.pairs.sdk).toBe("/opt/android-sdk");
    expect(r["runtimes.android"]?.pairs.platformTools).toBe("35.0.0");
    expect(r["runtimes.android.platforms"]?.items.map((i) => i.raw)).toEqual(["android-34"]);
  });

  test("android section omitted when SDK dir absent", async () => {
    const r = await makeRuntimesCollector(fakeEnv({ run: (cmd) => (cmd[0] === "go" ? "go version go1.26.3 darwin/arm64" : "") }))(ctx);
    expect(r["runtimes.go"]).toBeDefined();
    expect(r["runtimes.android"]).toBeUndefined();
  });

  test("a throwing tool does not break the others", async () => {
    const r = await makeRuntimesCollector(
      fakeEnv({
        run: (cmd) => {
          if (cmd[0] === "go") throw new Error("boom");
          if (cmd.join(" ") === "swift --version") return "Apple Swift version 6.0";
          return "";
        },
      }),
    )(ctx);
    expect(r["runtimes.go"]).toBeUndefined();
    expect(r["runtimes.swift"]?.pairs).toEqual({ version: "6.0" });
  });
});
