import type { Collector, CollectorResult } from "./types";
import { makeSection, toItems } from "./types";
import { type CommandEnv, defaultEnv } from "./env";

export interface GoInfo {
  version: string;
  platform: string;
}

export interface RustToolchain {
  name: string;
  flags: string;
}

export interface Crate {
  name: string;
  version: string;
}

export interface XcodeInfo {
  version: string;
  build: string;
}

/** Extract the token after a word: `rustc 1.96.0 (...)` with word "rustc" → "1.96.0". */
export function parseVersionToken(text: string, word: string): string {
  const m = text.match(new RegExp(`\\b${word}\\s+(\\S+)`));
  return m ? m[1] : "";
}

/** Parse `go version go1.26.3 darwin/arm64`. */
export function parseGoVersion(text: string): GoInfo | null {
  const m = text.trim().match(/^go version (\S+) (\S+)/);
  return m ? { version: m[1], platform: m[2] } : null;
}

/** Parse `rustup toolchain list` (`stable-aarch64-apple-darwin (active, default)`). */
export function parseRustupToolchains(text: string): RustToolchain[] {
  return text
    .trim()
    .split("\n")
    .map((l) => l.trim())
    .filter(Boolean)
    .map((l) => {
      const m = l.match(/^(\S+)(?:\s+\(([^)]*)\))?$/);
      return m ? { name: m[1], flags: m[2] ?? "" } : { name: l, flags: "" };
    })
    .filter((t) => !t.name.includes(" "));
}

/** Parse `cargo install --list` (`ripgrep v14.1.0:` followed by indented binaries). */
export function parseCargoCrates(text: string): Crate[] {
  const crates: Crate[] = [];
  for (const line of text.split("\n")) {
    // Registry: `ripgrep v14.1.0:` — git/path installs add a ` (source)` before the colon.
    const m = line.match(/^(\S+)\s+v(\S+?)(?:\s+\([^)]*\))?:\s*$/);
    if (m) crates.push({ name: m[1], version: m[2] });
  }
  return crates;
}

/** Parse `swift --version` (`... Apple Swift version 6.3.1 (...)`). */
export function parseSwiftVersion(text: string): string {
  const m = text.match(/Apple Swift version (\S+)/);
  return m ? m[1] : "";
}

/** Parse `xcodebuild -version` (`Xcode 26.4.1` / `Build version 17E202`). */
export function parseXcodeVersion(text: string): XcodeInfo | null {
  const v = text.match(/Xcode\s+(\S+)/);
  if (!v) return null;
  const b = text.match(/Build version\s+(\S+)/);
  return { version: v[1], build: b ? b[1] : "" };
}

/** Parse `zig version` (single line like `0.13.0`). */
export function parseZigVersion(text: string): string {
  const first = text.trim().split("\n")[0]?.trim() ?? "";
  return /^\d/.test(first) ? first : "";
}

/** Parse `adb version` — the `Version 36.0.2-...` line (platform-tools version). */
export function parseAdbVersion(text: string): string {
  const m = text.match(/^Version\s+(\S+)/m);
  return m ? m[1] : "";
}

export function makeRuntimesCollector(env: CommandEnv = defaultEnv): Collector {
  return async (ctx) => {
    const result: CollectorResult = {};

    try {
      const go = parseGoVersion(await env.run(["go", "version"]));
      if (go) result["runtimes.go"] = makeSection("runtimes.go", { pairs: { version: go.version, platform: go.platform } });
    } catch {}

    try {
      const pairs: Record<string, string> = {};
      const rustc = parseVersionToken(await env.run(["rustc", "--version"]), "rustc");
      if (rustc) pairs.rustc = rustc;
      const cargo = parseVersionToken(await env.run(["cargo", "--version"]), "cargo");
      if (cargo) pairs.cargo = cargo;
      if (Object.keys(pairs).length) result["runtimes.rust"] = makeSection("runtimes.rust", { pairs });
    } catch {}

    try {
      const toolchains = parseRustupToolchains(await env.run(["rustup", "toolchain", "list"]));
      if (toolchains.length) {
        result["runtimes.rust.toolchains"] = makeSection("runtimes.rust.toolchains", {
          items: toolchains.map((t) => ({
            raw: t.flags ? `${t.name} (${t.flags})` : t.name,
            columns: [t.name, t.flags].filter(Boolean),
          })),
        });
      }
    } catch {}

    try {
      const crates = parseCargoCrates(await env.run(["cargo", "install", "--list"]));
      if (crates.length) {
        result["runtimes.rust.crates"] = makeSection("runtimes.rust.crates", {
          items: crates.map((c) => ({ raw: `${c.name}@${c.version}`, columns: [c.name, c.version] })),
        });
      }
    } catch {}

    try {
      const swift = parseSwiftVersion(await env.run(["swift", "--version"]));
      if (swift) result["runtimes.swift"] = makeSection("runtimes.swift", { pairs: { version: swift } });
    } catch {}

    try {
      const zig = parseZigVersion(await env.run(["zig", "version"]));
      if (zig) result["runtimes.zig"] = makeSection("runtimes.zig", { pairs: { version: zig } });
    } catch {}

    try {
      const xcode = parseXcodeVersion(await env.run(["xcodebuild", "-version"]));
      if (xcode) {
        const pairs: Record<string, string> = { version: xcode.version };
        if (xcode.build) pairs.build = xcode.build;
        const path = (await env.run(["xcode-select", "-p"])).trim();
        if (path) pairs.path = path;
        result["runtimes.xcode"] = makeSection("runtimes.xcode", { pairs });
      }
    } catch {}

    try {
      const sdk =
        env.getEnv("ANDROID_HOME") ||
        env.getEnv("ANDROID_SDK_ROOT") ||
        `${ctx.home}/Library/Android/sdk`;
      if (await env.fileExists(sdk)) {
        const pairs: Record<string, string> = { sdk };
        const adb = parseAdbVersion(await env.run(["adb", "version"]));
        if (adb) pairs.platformTools = adb;
        result["runtimes.android"] = makeSection("runtimes.android", { pairs });

        const buildTools = [...(await env.listDir(`${sdk}/build-tools`))].sort();
        if (buildTools.length) result["runtimes.android.buildTools"] = makeSection("runtimes.android.buildTools", { items: toItems(buildTools) });

        const platforms = [...(await env.listDir(`${sdk}/platforms`))].sort();
        if (platforms.length) result["runtimes.android.platforms"] = makeSection("runtimes.android.platforms", { items: toItems(platforms) });
      }
    } catch {}

    return result;
  };
}

export const collectRuntimes = makeRuntimesCollector();
