/** Read a single line from stdin, trimmed. Writes `prompt` first (no newline). */
export async function readLine(prompt: string): Promise<string> {
  process.stdout.write(prompt);
  for await (const chunk of Bun.stdin.stream()) {
    return new TextDecoder().decode(chunk).trim();
  }
  return "";
}

/** Yes/no confirmation — defaults to No unless the answer is y/yes. */
export async function confirm(question: string): Promise<boolean> {
  const answer = await readLine(`${question} [y/N] `);
  return /^y(es)?$/i.test(answer);
}
