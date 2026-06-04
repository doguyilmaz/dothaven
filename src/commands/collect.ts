import { hostname } from "node:os";
import { join } from "node:path";
import { generateTimestamp } from "../utils/timestamp";
import { serializeSnapshot } from "../snapshot";
import type { CollectorContext, CollectorResult } from "../collectors/types";
import { resolveOutputDir } from "../utils/resolve-output";
import { getHome } from "../utils/home";
import { summarize, formatReport, redactSection } from "../scan";
import type { ScanResult } from "../scan";
import { registryEntries, registryCollector } from "../registry";
import { collectMeta } from "../collectors/meta";
import { collectSsh } from "../collectors/ssh";
import { collectOllama } from "../collectors/ollama";
import { collectApps } from "../collectors/apps";
import { collectHomebrew } from "../collectors/homebrew";
import { collectPackages } from "../collectors/packages";
import { collectRuntimes } from "../collectors/runtimes";
import { collectEditorsExt } from "../collectors/editors-ext";
import { collectFonts } from "../collectors/fonts";
import { collectDotfilesSweep } from "../collectors/dotfiles-sweep";

const collectors = [
  collectMeta,
  registryCollector(registryEntries),
  collectSsh,
  collectOllama,
  collectApps,
  collectHomebrew,
  collectPackages,
  collectRuntimes,
  collectEditorsExt,
  collectFonts,
  collectDotfilesSweep,
];

/** Run all collectors and merge their sections (no redaction — that happens in collect). */
export async function runCollectors(ctx: CollectorContext): Promise<CollectorResult> {
  const results = await Promise.allSettled(collectors.map((c) => c(ctx)));
  const sections: CollectorResult = {};
  for (const result of results) {
    if (result.status === "fulfilled") Object.assign(sections, result.value);
  }
  return sections;
}

function parseArgs(args: string[]) {
  let redact = true;
  let slim = false;
  let outputDir: string | null = null;

  for (let i = 0; i < args.length; i++) {
    if (args[i] === "--no-redact") redact = false;
    if (args[i] === "--slim") slim = true;
    if (args[i] === "-o" && args[i + 1]) outputDir = args[++i];
  }

  return { redact, slim, outputDir };
}

const SLIM_MAX_LINES = 10;

function slimSections(sections: CollectorResult) {
  for (const section of Object.values(sections)) {
    if (!section.content) continue;
    const lines = section.content.split("\n");
    if (lines.length > SLIM_MAX_LINES) {
      section.content = `${lines.slice(0, SLIM_MAX_LINES).join("\n")}\n... (${lines.length - SLIM_MAX_LINES} more lines)`;
    }
  }
}

export async function collect(args: string[]) {
  const { redact, slim, outputDir } = parseArgs(args);
  const resolvedOutput = await resolveOutputDir(outputDir);

  await Bun.$`mkdir -p ${resolvedOutput}`.quiet();

  const ctx: CollectorContext = {
    redact,
    home: getHome(),
  };

  const sections = await runCollectors(ctx);

  const scanResults: ScanResult[] = [];
  if (redact) {
    for (const [name, section] of Object.entries(sections)) {
      if (!redactSection(name, section, scanResults)) delete sections[name];
    }
  }

  if (slim) slimSections(sections);

  const output = serializeSnapshot(sections);

  const ts = generateTimestamp();
  const filename = `${hostname()}-${ts}.json`;
  const filepath = join(resolvedOutput, filename);
  await Bun.write(filepath, output);

  console.log(`Report saved to: ${filepath}`);

  if (redact) {
    const summary = summarize(scanResults);
    const report = formatReport(summary);
    if (report) console.log(report);
  }
}
