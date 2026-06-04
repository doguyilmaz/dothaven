import { test, expect, describe } from "bun:test";
import {
  planChezmoiExport,
  findSshPrivateKeys,
  buildPackageInstallScript,
  gnupgHasSecretKeys,
  isSelected,
  filterBrewfile,
} from "../../src/commands/chezmoi";
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

const sshConfig: ConfigEntry = {
  id: "ssh.config",
  name: "SSH Config",
  paths: { darwin: "~/.ssh/config", linux: "~/.ssh/config" },
  category: "ssh",
  kind: { type: "file" },
  backupDest: "ssh/config",
  sensitivity: "medium",
  redact: (c) => c, // declares a redact rule → must be encrypted on export
};

const entries: ConfigEntry[] = [
  entry("aws.creds", "~/.aws/credentials", "high"),
  entry("zshrc", "~/.zshrc", "low"), // low, but the scanner finds a secret in it
  entry("gitconfig", "~/.gitconfig", "low"), // low + clean
  entry("gnupg", "~/.gnupg", "high", "dir"),
  entry("gcloud", "~/.config/gcloud/configurations", "medium", "dir"), // medium DIR, secret inside
  sshConfig, // medium + redact rule
  entry("missing", "~/.missing", "low"),
];

const fileExists = async (p: string) => !p.endsWith(".missing");
// secret in ~/.zshrc (file) and inside the gcloud configurations dir
const containsSecret = async (p: string, _isDir: boolean) => p.endsWith("/.zshrc") || p.endsWith("/configurations");

describe("planChezmoiExport (secret gate)", () => {
  test("encrypts high / redact-rule / secret-in-file / secret-in-dir; plain for clean; skips missing", async () => {
    const plan = await planChezmoiExport(entries, HOME, fileExists, containsSecret);
    const byId = Object.fromEntries(plan.map((p) => [p.id, p]));

    expect(byId["aws.creds"]).toMatchObject({ encrypt: true, reason: "sensitivity:high" });
    expect(byId["zshrc"]).toMatchObject({ encrypt: true, reason: "secret detected" });
    expect(byId["gitconfig"]).toMatchObject({ encrypt: false, reason: "plain" });
    expect(byId["gnupg"]).toMatchObject({ encrypt: true, kind: "dir" });
    expect(byId["gcloud"]).toMatchObject({ encrypt: true, reason: "secret detected", kind: "dir" }); // dir gate
    expect(byId["ssh.config"]).toMatchObject({ encrypt: true, reason: "has redact rule" }); // redact rule
    expect(byId["missing"]).toBeUndefined();
  });

  test("never plain-adds a file/dir the scanner flags as secret (the gate)", async () => {
    const plan = await planChezmoiExport(entries, HOME, fileExists, containsSecret);
    const plain = plan.filter((p) => !p.encrypt);
    expect(plain.every((p) => !p.src.endsWith("/.zshrc") && !p.src.endsWith("/configurations"))).toBe(true);
  });

  test("resolves src paths against the given home", async () => {
    const plan = await planChezmoiExport(entries, HOME, fileExists, containsSecret);
    expect(plan.find((p) => p.id === "aws.creds")?.src).toBe("/fake/home/.aws/credentials");
  });

  test("nothing on disk → empty plan", async () => {
    const plan = await planChezmoiExport(entries, HOME, async () => false, containsSecret);
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
  test("brew (|| true), fnm/global installs, cargo, deno comment, trailing exit 0", () => {
    const script = buildPackageInstallScript({
      brewfile: 'tap "x/y"\nbrew "git"',
      nodeVersions: ["v20.20.2", "v24.16.0", "system"],
      bunGlobals: ["eas-cli@16.19.2"],
      npmGlobals: ["typescript@5.4.0"],
      cargoCrates: ["ripgrep@14.1.0", "fd-find@10.2.0"],
      denoBins: ["deployctl"],
    })!;
    expect(script).not.toBeNull();
    expect(script.startsWith("#!/bin/bash")).toBe(true);
    expect(script).toContain("if command -v brew >/dev/null 2>&1; then");
    expect(script).toContain("brew bundle --file=/dev/stdin <<'BREWFILE' || true"); // failing cask won't abort apply
    expect(script).toContain('brew "git"');
    expect(script).toContain("fnm install v20.20.2 || true");
    expect(script).toContain("fnm install v24.16.0 || true");
    expect(script).not.toContain("fnm install system");
    expect(script).toContain("bun add -g eas-cli@16.19.2 || true");
    expect(script).toContain("npm install -g typescript@5.4.0 || true");
    expect(script).toContain("cargo install ripgrep@14.1.0 || true"); // version-pinned, replayable
    expect(script).toContain("cargo install fd-find@10.2.0 || true");
    expect(script).toContain("# deno global bins"); // recorded, not executed
    expect(script).toContain("#   deployctl");
    expect(script).not.toContain("deno install deployctl"); // never a broken command
    expect(script.trimEnd().endsWith("exit 0")).toBe(true); // last line can't fail the apply
  });

  test("empty manifest → null (no header-only no-op script written)", () => {
    expect(buildPackageInstallScript({})).toBeNull();
  });

  test("deno bins alone → null (nothing executable to reinstall)", () => {
    expect(buildPackageInstallScript({ denoBins: ["deployctl"] })).toBeNull();
  });
});

describe("gnupgHasSecretKeys", () => {
  test("true when private-keys-v1.d holds *.key files", async () => {
    const listDir = async (p: string) => (p.endsWith("/private-keys-v1.d") ? ["ABC123.key", "DEF456.key"] : []);
    expect(await gnupgHasSecretKeys("/h", listDir)).toBe(true);
  });

  test("false when empty or only non-key files (just cruft)", async () => {
    expect(await gnupgHasSecretKeys("/h", async () => [])).toBe(false);
    expect(await gnupgHasSecretKeys("/h", async () => ["README", "pubring.db"])).toBe(false);
  });
});

describe("isSelected (--only / --skip)", () => {
  test("skip excludes", () => {
    expect(isSelected("editor", [], ["editor"])).toBe(false);
  });
  test("only restricts to listed categories", () => {
    expect(isSelected("ai", ["ai", "ssh"], [])).toBe(true);
    expect(isSelected("editor", ["ai", "ssh"], [])).toBe(false);
  });
  test("default (no only/skip) includes everything", () => {
    expect(isSelected("anything", [], [])).toBe(true);
  });
  test("skip beats only", () => {
    expect(isSelected("ai", ["ai"], ["ai"])).toBe(false);
  });
});

describe("filterBrewfile", () => {
  const bf = [
    'tap "x/y"',
    'brew "git"',
    'cask "stats"',
    'vscode "biomejs.biome"',
    'vscode "golang.go"',
    'mas "Xcode", id: 497799835',
  ].join("\n");

  test("skip vscode strips extension lines, keeps the rest (Settings Sync case)", () => {
    const out = filterBrewfile(bf, ["vscode"]);
    expect(out).not.toContain("vscode ");
    expect(out).toContain('brew "git"');
    expect(out).toContain('cask "stats"');
    expect(out).toContain("mas ");
  });

  test("skip multiple directives", () => {
    const out = filterBrewfile(bf, ["mas", "cask"]);
    expect(out).not.toContain("cask ");
    expect(out).not.toContain("mas ");
    expect(out).toContain('vscode "biomejs.biome"');
  });

  test("no skip → unchanged", () => {
    expect(filterBrewfile(bf, [])).toBe(bf);
  });
});
