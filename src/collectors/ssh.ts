import { join } from "node:path";
import { REDACTION_MARKER } from "../utils/constants";
import type { Collector, CollectorResult } from "./types";
import { makeSection } from "./types";

interface SshHost {
  host: string;
  hostname: string;
  identityFile: string;
}

function parseSshConfig(content: string): SshHost[] {
  const hosts: SshHost[] = [];
  let current: Partial<SshHost> | null = null;

  for (const line of content.split("\n")) {
    const trimmed = line.trim();
    if (!trimmed || trimmed.startsWith("#")) continue;

    const hostMatch = trimmed.match(/^Host\s+(.+)/i);
    if (hostMatch) {
      if (current?.host) hosts.push(current as SshHost);
      current = { host: hostMatch[1].trim(), hostname: "", identityFile: "" };
      continue;
    }

    if (!current) continue;

    const hostnameMatch = trimmed.match(/^HostName\s+(.+)/i);
    if (hostnameMatch) current.hostname = hostnameMatch[1].trim();

    const identityMatch = trimmed.match(/^IdentityFile\s+(.+)/i);
    if (identityMatch) current.identityFile = identityMatch[1].trim();
  }

  if (current?.host) hosts.push(current as SshHost);
  return hosts;
}

export const collectSsh: Collector = async (ctx) => {
  const result: CollectorResult = {};
  const configPath = join(ctx.home, ".ssh/config");
  const file = Bun.file(configPath);

  if (!(await file.exists())) return result;

  const content = await file.text();
  const hosts = parseSshConfig(content);

  if (!hosts.length) return result;

  const items = hosts.map((h) => {
    const hn = ctx.redact ? REDACTION_MARKER : h.hostname;
    const id = ctx.redact ? REDACTION_MARKER : h.identityFile;
    const raw = `${h.host} | ${hn} | ${id}`;
    return { raw, columns: [h.host, hn, id] };
  });

  result["ssh.hosts"] = makeSection("ssh.hosts", { items });
  return result;
};
