import { resolve } from "node:path";
import { stat } from "node:fs/promises";
import { scanFile, scanDirectory, formatSecurityReport } from "../scan";
import type { ScanResult } from "../scan";

function parseArgs(args: string[]) {
  let out = "SECURITY.md";
  const positional: string[] = [];
  for (let i = 0; i < args.length; i++) {
    if (args[i] === "-o" && args[i + 1]) out = args[++i];
    else if (!args[i].startsWith("-")) positional.push(args[i]);
  }
  return { target: resolve(positional[0] ?? "."), out };
}

export async function security(args: string[]) {
  const { target, out } = parseArgs(args);

  // stat() decides file vs dir — NOT file size. A 0-byte file is still a file; the old `size > 0`
  // check misrouted empty files into scanDirectory, which then threw ENOTDIR on a non-directory.
  let isFile: boolean;
  try {
    isFile = (await stat(target)).isFile();
  } catch {
    console.error(`Path not found: ${target}`);
    process.exitCode = 1;
    return;
  }

  let results: ScanResult[];
  if (isFile) {
    const result = await scanFile(target);
    results = result ? [result] : [];
  } else {
    results = await scanDirectory(target);
  }

  await Bun.write(out, formatSecurityReport(results));

  const withFindings = results.filter((r) => r.findings.length > 0).length;
  console.log(`Security report written to: ${out}`);
  console.log(`  ${results.length} scanned, ${withFindings} with findings.`);
}
