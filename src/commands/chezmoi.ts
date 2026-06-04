import { getHome } from "../utils/home";
import { splitList } from "../utils/args";
import { registryEntries } from "../registry/entries";
import { resolvePath } from "../registry/resolve";
import type { ConfigEntry } from "../registry/types";
import { scanFile, scanDirectory, scanContent, applyRedactions } from "../scan";
import type { ScanResult } from "../scan";
import { defaultEnv } from "../collectors/env";
import type { CollectorContext, CollectorResult } from "../collectors/types";
import { collectHomebrew } from "../collectors/homebrew";
import { collectPackages } from "../collectors/packages";
import { collectRuntimes } from "../collectors/runtimes";

export interface ChezmoiPlanItem {
  id: string;
  src: string;
  kind: "file" | "dir";
  encrypt: boolean;
  reason: string;
}

/**
 * Decide, per registry entry that exists on disk, whether chezmoi adds it plain or `--encrypt`.
 * Encrypts when the entry is high-sensitivity, declares a redact rule (its content is meant to be
 * scrubbed, e.g. ssh.config), OR the scanner finds a real secret inside it — including inside a
 * DIRECTORY entry (previously dirs bypassed the gate entirely). Pure given its injected probes.
 */
export async function planChezmoiExport(
  entries: ConfigEntry[],
  home: string,
  fileExists: (p: string) => Promise<boolean>,
  containsSecret: (path: string, isDir: boolean) => Promise<boolean>,
): Promise<ChezmoiPlanItem[]> {
  const items: ChezmoiPlanItem[] = [];

  for (const entry of entries) {
    const src = resolvePath(entry, home);
    if (!src || !(await fileExists(src))) continue;

    const isDir = entry.kind.type === "dir";
    let encrypt = entry.sensitivity === "high" || !!entry.redact;
    let reason = entry.sensitivity === "high" ? "sensitivity:high" : encrypt ? "has redact rule" : "plain";

    if (!encrypt && (await containsSecret(src, isDir))) {
      encrypt = true;
      reason = "secret detected";
    }

    items.push({ id: entry.id, src, kind: isDir ? "dir" : "file", encrypt, reason });
  }

  return items;
}

/** True if the scanner finds a HIGH-severity secret in a file (or any file in a directory).
 * HIGH-only so a benign MEDIUM hit (an IP/email in an otherwise-plain config) doesn't force encryption. */
function hasHighFinding(result: ScanResult | null): boolean {
  return !!result && result.findings.some((f) => f.pattern.severity === "HIGH");
}

async function containsSecret(path: string, isDir: boolean): Promise<boolean> {
  if (isDir) {
    return (await scanDirectory(path)).some((r) => hasHighFinding(r));
  }
  return hasHighFinding(await scanFile(path));
}

/**
 * SSH private keys in ~/.ssh, detected by content (a private-key header) rather than
 * filename — so it catches id_ed25519, id_rsa, custom *.key, etc. Skips .pub files.
 */
export async function findSshPrivateKeys(
  home: string,
  listDir: (p: string) => Promise<string[]>,
  isPrivateKey: (p: string) => Promise<boolean>,
): Promise<string[]> {
  const dir = `${home}/.ssh`;
  const keys: string[] = [];
  for (const name of await listDir(dir)) {
    if (name.endsWith(".pub")) continue;
    const path = `${dir}/${name}`;
    if (await isPrivateKey(path)) keys.push(path);
  }
  return keys.sort();
}

async function isSshPrivateKey(path: string): Promise<boolean> {
  const result = await scanFile(path);
  return (
    !!result && result.findings.some((f) => f.pattern.id === "private-key-pem" || f.pattern.id === "pgp-private-key")
  );
}

export interface InstallManifest {
  brewfile?: string;
  nodeVersions?: string[];
  bunGlobals?: string[];
  npmGlobals?: string[];
  pnpmGlobals?: string[];
  cargoCrates?: string[];
  denoBins?: string[];
}

const guarded = (tool: string, ...body: string[]) => [`if command -v ${tool} >/dev/null 2>&1; then`, ...body, "fi"];

/**
 * A chezmoi `run_onchange_` script that reinstalls packages on `chezmoi apply` — so a fresh
 * machine gets brew formulae/casks, node versions, and global packages, not just config files.
 *
 * Every step is command-guarded and `|| true`, and the script ends in `exit 0`, so a missing tool
 * or one failing cask never aborts `chezmoi apply` (the script's exit code is its last command's).
 * Returns `null` when there is nothing real to install — the caller then writes no file, instead of
 * a header-only no-op script.
 */
export function buildPackageInstallScript(m: InstallManifest): string | null {
  const blocks: string[] = [];

  if (m.brewfile?.trim()) {
    blocks.push(
      guarded("brew", "  brew bundle --file=/dev/stdin <<'BREWFILE' || true", m.brewfile.trim(), "BREWFILE").join("\n"),
    );
  }

  const versions = (m.nodeVersions ?? []).filter((v) => v && v !== "system");
  if (versions.length) {
    blocks.push(guarded("fnm", ...versions.map((v) => `  fnm install ${v} || true`)).join("\n"));
  }

  // One install per line (readable, and one failure can't block the rest). Names only — no version
  // pin — so a fresh machine gets the current release of each global CLI.
  const globalBlock = (tool: string, add: string, pkgs?: string[]) => {
    if (pkgs?.length) blocks.push(guarded(tool, ...pkgs.map((p) => `  ${add} ${p} || true`)).join("\n"));
  };
  globalBlock("bun", "bun add -g", m.bunGlobals);
  globalBlock("pnpm", "pnpm add -g", m.pnpmGlobals);
  globalBlock("npm", "npm install -g", m.npmGlobals);

  if (m.cargoCrates?.length) {
    blocks.push(guarded("cargo", ...m.cargoCrates.map((c) => `  cargo install ${c} || true`)).join("\n"));
  }

  if (!blocks.length) return null;

  // deno globals: the original module URL isn't recoverable from a bin-dir name, so we can't emit a
  // working `deno install`. Record the names as a comment (info preserved) — never a broken command.
  if (m.denoBins?.length) {
    blocks.push(
      [
        "# deno global bins (reinstall manually — original module URL not captured):",
        ...m.denoBins.map((b) => `#   ${b}`),
      ].join("\n"),
    );
  }

  const header = [
    "#!/bin/bash",
    "# Generated by `dotfiles chezmoi-export`. chezmoi re-runs this on apply when it changes.",
    "set -uo pipefail",
  ].join("\n");

  return `${header}\n\n${blocks.join("\n\n")}\n\nexit 0\n`;
}

/**
 * Names installed by more than one JS global manager (e.g. `argent` via both bun and npm). We still
 * include them in every manager's block — they're not removed — but the caller warns so the user can
 * decide whether the duplication (and PATH shadowing) is intended.
 */
export function crossManagerDuplicates(m: InstallManifest): string[] {
  const count = new Map<string, number>();
  for (const list of [m.bunGlobals, m.npmGlobals, m.pnpmGlobals]) {
    for (const name of new Set(list ?? [])) count.set(name, (count.get(name) ?? 0) + 1);
  }
  return [...count.entries()]
    .filter(([, n]) => n > 1)
    .map(([name]) => name)
    .sort();
}

/** gnupg is worth carrying only if it holds real secret keys (private-keys-v1.d/*.key); otherwise
 * `chezmoi add ~/.gnupg` just captures lock-file/runtime cruft. */
export async function gnupgHasSecretKeys(home: string, listDir: (p: string) => Promise<string[]>): Promise<boolean> {
  return (await listDir(`${home}/.gnupg/private-keys-v1.d`)).some((f) => f.endsWith(".key"));
}

/**
 * Drop Brewfile lines whose directive (first token) is in `skip`. `brew bundle dump` embeds
 * `vscode "ext-id"` entries (and `mas`, `cask`…); `--skip vscode` strips them — handy when
 * editor extensions are synced elsewhere (e.g. VS Code Settings Sync).
 */
export function filterBrewfile(brewfile: string, skip: string[]): string {
  if (!skip.length) return brewfile;
  return brewfile
    .split("\n")
    .filter((l) => !skip.includes(l.trim().split(/\s/)[0]))
    .join("\n")
    .trim();
}

/**
 * The token to install a package with. Default (`pin=false`) is the bare name → a fresh machine gets
 * the current release of each global tool. `pin=true` keeps the captured `name@version` for a
 * reproducible set (some setups deliberately freeze versions). Falls back to `raw` when there are no
 * columns (e.g. a deno bin name, which has no version anyway).
 */
export function pickInstallSpec(item: { raw: string; columns: string[] }, pin: boolean): string {
  return pin ? item.raw : (item.columns[0] ?? item.raw);
}

async function gatherInstallManifest(ctx: CollectorContext, pin = false): Promise<InstallManifest> {
  const brew = await collectHomebrew(ctx);
  const pkgs = await collectPackages(ctx);
  const runtimes = await collectRuntimes(ctx);
  const specs = (src: CollectorResult, id: string) =>
    (src[id]?.items ?? []).map((i) => pickInstallSpec(i, pin)).filter(Boolean);
  // The Brewfile is embedded verbatim into an UNENCRYPTED run_onchange script — redact any inline
  // credentials (e.g. a private tap's https://user:pass@host remote) before it can land there.
  const rawBrewfile = brew["apps.brew.bundle"]?.content;
  return {
    // Node runtimes always keep their exact version — you want the specific node, not "latest".
    brewfile: rawBrewfile ? applyRedactions(rawBrewfile, scanContent("Brewfile", rawBrewfile)) : undefined,
    nodeVersions: (pkgs["packages.node.fnm"]?.items ?? []).map((i) => i.columns[0]),
    bunGlobals: specs(pkgs, "packages.bun.global"),
    npmGlobals: specs(pkgs, "packages.npm.global"),
    pnpmGlobals: specs(pkgs, "packages.pnpm.global"),
    cargoCrates: specs(runtimes, "runtimes.rust.crates"),
    denoBins: specs(pkgs, "packages.deno.bin"),
  };
}

/** Category selection: skip wins; if `only` is non-empty, the category must be in it. */
export function isSelected(category: string, only: string[], skip: string[]): boolean {
  if (skip.includes(category)) return false;
  return only.length === 0 || only.includes(category);
}

export function parseExportArgs(args: string[]) {
  let apply = false;
  let pin = false;
  let only: string[] = [];
  let skip: string[] = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--apply") apply = true;
    else if (args[i] === "--pin") pin = true;
    else if (args[i] === "--only" && args[i + 1]) only = splitList(args[++i]);
    else if (args[i] === "--skip" && args[i + 1]) skip = splitList(args[++i]);
  }
  return { apply, pin, only, skip };
}

export async function chezmoiExport(args: string[]) {
  const { apply, pin, only, skip } = parseExportArgs(args);
  const home = getHome();

  // Registry entries filtered by their category (--only / --skip).
  const entries = registryEntries.filter((e) => isSelected(e.category, only, skip));
  const plan = await planChezmoiExport(entries, home, defaultEnv.fileExists, containsSecret);

  // SSH private keys aren't a single registry path (filenames vary) — sweep ~/.ssh by content.
  if (isSelected("ssh", only, skip)) {
    for (const key of await findSshPrivateKeys(home, defaultEnv.listDir, isSshPrivateKey)) {
      if (!plan.some((p) => p.src === key)) {
        plan.push({ id: "ssh.key", src: key, kind: "file", encrypt: true, reason: "ssh private key" });
      }
    }
  }

  // Drop the gnupg dir unless it has real secret keys — avoids carrying lock-file/runtime cruft.
  if (!(await gnupgHasSecretKeys(home, defaultEnv.listDir))) {
    const i = plan.findIndex((p) => p.id === "secrets.gnupg");
    if (i >= 0) plan.splice(i, 1);
  }

  if (plan.length === 0) {
    console.log("Nothing to export — no managed configs found on this machine.");
    return;
  }

  const encrypted = plan.filter((p) => p.encrypt).length;
  console.log(`chezmoi-export plan — ${plan.length} path(s), ${encrypted} encrypted:\n`);
  for (const item of plan) {
    console.log(`  ${item.encrypt ? "🔒 add --encrypt" : "   add          "}  ${item.src}  (${item.reason})`);
  }

  if (!apply) {
    console.log("\nDry-run. Re-run with --apply to execute (requires chezmoi + a configured age key).");
    return;
  }

  let chezmoiOk = false;
  try {
    chezmoiOk = !!(await defaultEnv.run(["chezmoi", "--version"])).trim();
  } catch {}
  if (!chezmoiOk) {
    console.error("\nchezmoi not found. Install it (brew install chezmoi) and configure age encryption first.");
    process.exitCode = 1;
    return;
  }

  console.log("");
  for (const item of plan) {
    const cmd = item.encrypt ? ["chezmoi", "add", "--encrypt", item.src] : ["chezmoi", "add", item.src];
    try {
      await defaultEnv.run(cmd);
      console.log(`  ✔ ${item.encrypt ? "encrypted " : ""}${item.src}`);
    } catch (error) {
      console.error(`  ✗ ${item.src}: ${error}`);
    }
  }

  // Generate a run_onchange install script (brew + node + globals), honoring --only/--skip.
  const wantBrew = isSelected("brew", only, skip);
  const wantPackages = isSelected("packages", only, skip);
  if (wantBrew || wantPackages) {
    try {
      const manifest = await gatherInstallManifest({ redact: false, home }, pin);
      const dupes = crossManagerDuplicates(manifest);
      if (dupes.length) {
        console.warn(
          `  ⚠ installed by multiple managers (kept in each — review for PATH shadowing): ${dupes.join(", ")}`,
        );
      }
      if (!wantBrew) manifest.brewfile = undefined;
      else if (manifest.brewfile) manifest.brewfile = filterBrewfile(manifest.brewfile, skip);
      if (!wantPackages) {
        manifest.nodeVersions = [];
        manifest.bunGlobals = [];
        manifest.npmGlobals = [];
        manifest.pnpmGlobals = [];
        manifest.cargoCrates = [];
        manifest.denoBins = [];
      }
      const script = buildPackageInstallScript(manifest);
      const sourcePath = script ? (await defaultEnv.run(["chezmoi", "source-path"])).trim() : "";
      if (script && sourcePath) {
        await Bun.write(`${sourcePath}/run_onchange_install-packages.sh`, script);
        console.log("  ✔ run_onchange_install-packages.sh");
      }
    } catch (error) {
      console.error(`  ✗ install script: ${error}`);
    }
  }

  console.log("\nDone. Review with `chezmoi diff`, then commit your private chezmoi source repo.");
}
