import { resolve } from "node:path";
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
  const file = Bun.file(target);
  const isFile = (await file.exists()) && file.size > 0;

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
