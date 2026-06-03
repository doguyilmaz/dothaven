import type { Collector, CollectorResult } from "./types";
import { makeSection } from "./types";

export interface PkgItem {
  name: string;
  version: string;
}

export interface NodeVersion {
  version: string;
  isDefault: boolean;
}

/** Split a package spec into name + version, preserving scoped names (@scope/pkg@1.2.3). */
function splitSpec(spec: string): PkgItem {
  const at = spec.lastIndexOf("@");
  if (at <= 0) return { name: spec, version: "" };
  return { name: spec.slice(0, at), version: spec.slice(at + 1) };
}

const byName = (a: PkgItem, b: PkgItem) => a.name.localeCompare(b.name);

/** Parse `npm ls -g --depth=0 --json`. */
export function parseNpmGlobal(jsonText: string): PkgItem[] {
  try {
    const data = JSON.parse(jsonText) as {
      dependencies?: Record<string, { version?: string }> | null;
    };
    return Object.entries(data.dependencies ?? {})
      .map(([name, info]) => ({ name, version: String(info?.version ?? "") }))
      .filter((p) => p.name)
      .sort(byName);
  } catch {
    return [];
  }
}

/** Parse `pnpm ls -g --depth=0 --json` (array or object form — defensive, format unverified). */
export function parsePnpmGlobal(jsonText: string): PkgItem[] {
  try {
    const data = JSON.parse(jsonText);
    const node = Array.isArray(data) ? data[0] : data;
    const deps = (node?.dependencies ?? {}) as Record<string, { version?: string }>;
    return Object.entries(deps)
      .map(([name, info]) => ({ name, version: String(info?.version ?? "") }))
      .filter((p) => p.name)
      .sort(byName);
  } catch {
    return [];
  }
}

/** Parse `bun pm ls -g` tree output (first line is a header, rows are `├──`/`└──`). */
export function parseBunGlobal(text: string): PkgItem[] {
  const items: PkgItem[] = [];
  for (const line of text.split("\n")) {
    const m = line.match(/^\s*[├└]──\s*(.+?)\s*$/);
    if (m) items.push(splitSpec(m[1]));
  }
  return items.filter((p) => p.name).sort(byName);
}

/** Parse `fnm ls` output (`* v20.20.2`, `* v24.16.0 default`, `* system`). */
export function parseFnmList(text: string): NodeVersion[] {
  return text
    .split("\n")
    .map((l) => l.replace(/^\s*\*/, "").trim())
    .filter(Boolean)
    .map((l) => ({
      version: l.replace(/\bdefault\b/, "").trim(),
      isDefault: /\bdefault\b/.test(l),
    }))
    .filter((v) => v.version);
}

const pkgItems = (pkgs: PkgItem[]) =>
  pkgs.map((p) => ({
    raw: p.version ? `${p.name}@${p.version}` : p.name,
    columns: [p.name, p.version].filter(Boolean),
  }));

/** Injectable side-effects so the collector is fully testable without real tools installed. */
export interface PkgEnv {
  /** Run a command, returning stdout. Must not throw on non-zero exit (npm ls exits 1 on warnings). */
  run: (cmd: string[]) => Promise<string>;
  /** List entries of a directory; returns [] if it does not exist. */
  listDir: (path: string) => Promise<string[]>;
}

export const defaultPkgEnv: PkgEnv = {
  run: async (cmd) => (await Bun.$`${cmd}`.nothrow().quiet()).stdout.toString(),
  listDir: async (path) => {
    const entries: string[] = [];
    try {
      for await (const name of new Bun.Glob("*").scan(path)) entries.push(name);
    } catch {}
    return entries;
  },
};

export function makePackagesCollector(env: PkgEnv = defaultPkgEnv): Collector {
  return async (ctx) => {
    const result: CollectorResult = {};

    try {
      const npm = parseNpmGlobal(await env.run(["npm", "ls", "-g", "--depth=0", "--json"]));
      if (npm.length) result["packages.npm.global"] = makeSection("packages.npm.global", { items: pkgItems(npm) });
    } catch {}

    try {
      const bun = parseBunGlobal(await env.run(["bun", "pm", "ls", "-g"]));
      if (bun.length) result["packages.bun.global"] = makeSection("packages.bun.global", { items: pkgItems(bun) });
    } catch {}

    try {
      const pnpm = parsePnpmGlobal(await env.run(["pnpm", "ls", "-g", "--depth=0", "--json"]));
      if (pnpm.length) result["packages.pnpm.global"] = makeSection("packages.pnpm.global", { items: pkgItems(pnpm) });
    } catch {}

    try {
      const versions = parseFnmList(await env.run(["fnm", "ls"]));
      if (versions.length) {
        result["packages.node.fnm"] = makeSection("packages.node.fnm", {
          items: versions.map((v) => ({
            raw: v.isDefault ? `${v.version} (default)` : v.version,
            columns: [v.version, v.isDefault ? "default" : ""].filter(Boolean),
          })),
        });
      }
    } catch {}

    try {
      const bins = await env.listDir(`${ctx.home}/.deno/bin`);
      if (bins.length) {
        const items = [...bins].sort((a, b) => a.localeCompare(b)).map((n) => ({ raw: n, columns: [n] }));
        result["packages.deno.bin"] = makeSection("packages.deno.bin", { items });
      }
    } catch {}

    return result;
  };
}

export const collectPackages = makePackagesCollector();
