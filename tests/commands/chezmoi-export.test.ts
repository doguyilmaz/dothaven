import { test, expect, describe } from "bun:test";
import { planChezmoiExport, findSshPrivateKeys, buildPackageInstallScript } from "../../src/commands/chezmoi";
import type { ConfigEntry } from "../../src/registry/types";

function entry(
  id: string,
  rel: string,
  sensitivity: "low" | "medium" | "high",
  kind: "file" | "dir" = "file",
): ConfigEntry {
  return {
    id,
    name: id,
    paths: { darwin: rel, linux: rel },
    category: "test",
    kind: { type: kind } as ConfigEntry["kind"],
    backupDest: id,
    sensitivity,
  };
}

const HOME = "/fake/home";

const entries: ConfigEntry[] = [
  entry("aws.creds", "~/.aws/credentials", "high"),
  entry("zshrc", "~/.zshrc", "low"), // low, but contains a secret
  entry("gitconfig", "~/.gitconfig", "low"), // low + clean
  entry("gnupg", "~/.gnupg", "high", "dir"),
  entry("missing", "~/.missing", "low"),
];

const fileExists = async (p: string) => !p.endsWith(".missing");
const hasSecret = async (p: string) => p.endsWith("/.zshrc");

describe("planChezmoiExport (secret gate)", () => {
  test("encrypts high-sensitivity, secret-detected, and high dirs; plain for clean; skips missing", async () => {
    const plan = await planChezmoiExport(entries, HOME, fileExists, hasSecret);
    const byId = Object.fromEntries(plan.map((p) => [p.id, p]));

    expect(byId["aws.creds"]).toMatchObject({ encrypt: true, reason: "sensitivity:high" });
    expect(byId["zshrc"]).toMatchObject({ encrypt: true, reason: "secret detected" });
    expect(byId["gitconfig"]).toMatchObject({ encrypt: false, reason: "plain" });
    expect(byId["gnupg"]).toMatchObject({ encrypt: true, kind: "dir" });
    expect(byId["missing"]).toBeUndefined();
  });

  test("never plain-adds a file the scanner flags as secret (the gate)", async () => {
    const plan = await planChezmoiExport(entries, HOME, fileExists, hasSecret);
    const plain = plan.filter((p) => !p.encrypt);
    expect(plain.every((p) => !p.src.endsWith("/.zshrc"))).toBe(true);
  });

  test("resolves src paths against the given home", async () => {
    const plan = await planChezmoiExport(entries, HOME, fileExists, hasSecret);
    expect(plan.find((p) => p.id === "aws.creds")?.src).toBe("/fake/home/.aws/credentials");
  });

  test("nothing on disk → empty plan", async () => {
    const plan = await planChezmoiExport(entries, HOME, async () => false, hasSecret);
    expect(plan).toEqual([]);
  });
});

describe("findSshPrivateKeys", () => {
  test("selects key files by content, skips .pub and non-keys", async () => {
    const listDir = async (p: string) =>
      p.endsWith("/.ssh") ? ["id_ed25519", "id_ed25519.pub", "config", "known_hosts", "work.key", "backup.zip"] : [];
    const isPriv = async (p: string) => p.endsWith("/id_ed25519") || p.endsWith("/work.key");
    expect(await findSshPrivateKeys("/home/u", listDir, isPriv)).toEqual([
      "/home/u/.ssh/id_ed25519",
      "/home/u/.ssh/work.key",
    ]);
  });

  test("no private keys → []", async () => {
    const listDir = async () => ["config", "known_hosts"];
    expect(await findSshPrivateKeys("/h", listDir, async () => false)).toEqual([]);
  });
});

describe("buildPackageInstallScript", () => {
  test("emits brew bundle, fnm installs (skipping 'system'), and guarded global installs", () => {
    const script = buildPackageInstallScript({
      brewfile: 'tap "x/y"\nbrew "git"',
      nodeVersions: ["v20.20.2", "v24.16.0", "system"],
      bunGlobals: ["eas-cli@16.19.2"],
      npmGlobals: ["typescript@5.4.0"],
    });
    expect(script.startsWith("#!/bin/bash")).toBe(true);
    expect(script).toContain("if command -v brew >/dev/null 2>&1; then");
    expect(script).toContain("brew bundle --file=/dev/stdin");
    expect(script).toContain('brew "git"');
    expect(script).toContain("fnm install v20.20.2 || true");
    expect(script).toContain("fnm install v24.16.0 || true");
    expect(script).not.toContain("fnm install system");
    expect(script).toContain("bun add -g eas-cli@16.19.2 || true");
    expect(script).toContain("npm install -g typescript@5.4.0 || true");
  });

  test("empty manifest → header only, no tool blocks", () => {
    const script = buildPackageInstallScript({});
    expect(script).toContain("#!/bin/bash");
    expect(script).not.toContain("brew bundle");
    expect(script).not.toContain("fnm install");
    expect(script).not.toContain("add -g");
  });
});
