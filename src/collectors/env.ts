/**
 * Injectable side-effects shared by command-based collectors.
 *
 * Collectors take a `CommandEnv` (defaulting to `defaultEnv`) so their full
 * logic — command → parser → section, error isolation, empty-omit — can be
 * unit-tested with a fake env, without the real tools being installed.
 */
export interface CommandEnv {
  /** Run a command, returning stdout. Never throws on non-zero exit (e.g. `npm ls` exits 1 on peer warnings). */
  run: (cmd: string[]) => Promise<string>;
  /** List entries of a directory (files only); returns [] if it does not exist. */
  listDir: (path: string) => Promise<string[]>;
  /** Whether a path exists. */
  fileExists: (path: string) => Promise<boolean>;
}

export const defaultEnv: CommandEnv = {
  run: async (cmd) => (await Bun.$`${cmd}`.nothrow().quiet()).stdout.toString(),
  listDir: async (path) => {
    const entries: string[] = [];
    try {
      for await (const name of new Bun.Glob("*").scan(path)) entries.push(name);
    } catch {}
    return entries;
  },
  fileExists: (path) => Bun.file(path).exists(),
};
