import { getHome } from "../utils/home";
import { registryEntries } from "../registry/entries";
import { resolvePath } from "../registry/resolve";
import type { ConfigEntry } from "../registry/types";
import { scanFile } from "../scan";
import { defaultEnv } from "../collectors/env";

export interface ChezmoiPlanItem {
  id: string;
  src: string;
  kind: "file" | "dir";
  encrypt: boolean;
  reason: string;
}

/**
 * Decide, per registry entry that exists on disk, whether chezmoi should add it
 * plain or `--encrypt`. Encrypts when the entry is high-sensitivity OR when the
 * file's content is detected as containing secrets — so a secret can never be
 * added in plaintext (the secret gate). Pure given its injected probes.
 */
export async function planChezmoiExport(
  entries: ConfigEntry[],
  home: string,
  fileExists: (p: string) => Promise<boolean>,
  hasSecret: (p: string) => Promise<boolean>,
): Promise<ChezmoiPlanItem[]> {
  const items: ChezmoiPlanItem[] = [];

  for (const entry of entries) {
    const src = resolvePath(entry, home);
    if (!src || !(await fileExists(src))) continue;

    const isDir = entry.kind.type === "dir";
    let encrypt = entry.sensitivity === "high";
    let reason = encrypt ? "sensitivity:high" : "plain";

    if (!encrypt && !isDir && (await hasSecret(src))) {
      encrypt = true;
      reason = "secret detected";
    }

    items.push({ id: entry.id, src, kind: isDir ? "dir" : "file", encrypt, reason });
  }

  return items;
}

/** A file is treated as secret-bearing if the scanner would redact or skip it. */
async function fileHasSecret(path: string): Promise<boolean> {
  const result = await scanFile(path);
  return !!result && (result.action === "redact" || result.action === "skip");
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

export async function chezmoiExport(args: string[]) {
  const apply = args.includes("--apply");
  const home = getHome();
  const plan = await planChezmoiExport(registryEntries, home, defaultEnv.fileExists, fileHasSecret);

  // SSH private keys aren't a single registry path (filenames vary) — sweep ~/.ssh by content.
  for (const key of await findSshPrivateKeys(home, defaultEnv.listDir, isSshPrivateKey)) {
    if (!plan.some((p) => p.src === key)) {
      plan.push({ id: "ssh.key", src: key, kind: "file", encrypt: true, reason: "ssh private key" });
    }
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
  console.log("\nDone. Review with `chezmoi diff`, then commit your private chezmoi source repo.");
}
