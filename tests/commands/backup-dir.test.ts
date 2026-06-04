import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm, readdir } from "node:fs/promises";
import { tmpdir } from "node:os";
import { copyDir } from "../../src/commands/backup";
import type { ScanResult } from "../../src/scan";

let src: string;

beforeAll(async () => {
  src = await mkdtemp(join(tmpdir(), "bdir-src-"));
  await Bun.write(
    join(src, "config_default"),
    "[core]\nproject = myproj\nclient_secret = GOCSPX-SuperSecretValue123\n",
  );
  await Bun.write(
    join(src, "id_ed25519"),
    "-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaA\n-----END OPENSSH PRIVATE KEY-----\n",
  );
  await Bun.write(join(src, "readme.txt"), "just docs\n");
});

afterAll(async () => {
  await rm(src, { recursive: true, force: true });
});

describe("copyDir redaction parity with copyFile", () => {
  test("redacts secrets and skips private-key files when redact is on", async () => {
    const dest = await mkdtemp(join(tmpdir(), "bdir-dest-"));
    const scanResults: ScanResult[] = [];
    await copyDir({ type: "dir", src, dest: "out" }, dest, true, scanResults);
    const outDir = join(dest, "out");
    const files = await readdir(outDir);

    expect(files).not.toContain("id_ed25519"); // private key skipped, never written
    expect(files).toContain("config_default");
    const cfg = await Bun.file(join(outDir, "config_default")).text();
    expect(cfg).toContain("[REDACTED]");
    expect(cfg).not.toContain("GOCSPX-SuperSecretValue123");
    expect(cfg).toContain("project = myproj"); // benign line kept
    expect(files).toContain("readme.txt");

    await rm(dest, { recursive: true, force: true });
  });

  test("with redact off, copies everything verbatim", async () => {
    const dest = await mkdtemp(join(tmpdir(), "bdir-dest2-"));
    await copyDir({ type: "dir", src, dest: "out" }, dest, false, []);
    const files = await readdir(join(dest, "out"));
    expect(files).toContain("id_ed25519");
    const cfg = await Bun.file(join(dest, "out", "config_default")).text();
    expect(cfg).toContain("GOCSPX-SuperSecretValue123");
    await rm(dest, { recursive: true, force: true });
  });
});
