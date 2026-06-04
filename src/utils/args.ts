/** Split a comma-separated flag value into trimmed, non-empty tokens.
 * `--only "ai, ssh,"` → ["ai", "ssh"]. Tolerates stray spaces and trailing commas. */
export function splitList(value: string): string[] {
  return value
    .split(",")
    .map((v) => v.trim())
    .filter(Boolean);
}
