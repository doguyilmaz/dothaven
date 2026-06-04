import { test, expect, describe } from "bun:test";
import { planChezmoiExport } from "../../src/commands/chezmoi";
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
