import { test, expect, describe, beforeAll, afterAll } from "bun:test";
import { join } from "node:path";
import { mkdtemp, rm } from "node:fs/promises";
import { tmpdir } from "node:os";
import { collectSsh } from "../../src/collectors/ssh";
import type { CollectorContext } from "../../src/collectors/types";

let tempHome: string;

beforeAll(async () => {
  tempHome = await mkdtemp(join(tmpdir(), "dotfiles-test-"));
  await Bun.$`mkdir -p ${join(tempHome, ".ssh")}`.quiet();
  await Bun.write(
    join(tempHome, ".ssh/config"),
    `Host github.com
  HostName github.com
  IdentityFile ~/.ssh/id_ed25519

Host work-server
  HostName 10.0.0.50
  IdentityFile ~/.ssh/work_key
`,
  );
});

afterAll(async () => {
  await rm(tempHome, { recursive: true, force: true });
});

describe("collectSsh", () => {
  test("parses hosts with redaction", async () => {
    const ctx: CollectorContext = { redact: true, home: tempHome };
    const result = await collectSsh(ctx);
    expect(result["ssh.hosts"]).toBeDefined();
    const items = result["ssh.hosts"].items;
    expect(items).toHaveLength(2);
    expect(items[0].columns[0]).toBe("github.com");
    expect(items[0].columns[1]).toBe("[REDACTED]");
    expect(items[0].columns[2]).toBe("[REDACTED]");
  });

  test("shows real values with --no-redact", async () => {
    const ctx: CollectorContext = { redact: false, home: tempHome };
    const result = await collectSsh(ctx);
    const items = result["ssh.hosts"].items;
    expect(items[1].columns[1]).toBe("10.0.0.50");
    expect(items[1].columns[2]).toBe("~/.ssh/work_key");
  });

  test("returns empty for missing file", async () => {
    const ctx: CollectorContext = { redact: true, home: "/tmp/nonexistent" };
    const result = await collectSsh(ctx);
    expect(result).toEqual({});
  });
});
