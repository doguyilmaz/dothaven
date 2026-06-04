import { stat } from "fs/promises";

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
  /** List entries of a directory (files and directories, non-recursive); returns [] if it does not exist. */
  listDir: (path: string) => Promise<string[]>;
  /** Whether a path exists (file or directory). */
  fileExists: (path: string) => Promise<boolean>;
  /** Read an environment variable (undefined if unset). */
  getEnv: (name: string) => string | undefined;
}

export const defaultEnv: CommandEnv = {
  run: async (cmd) => (await Bun.$`${cmd}`.nothrow().quiet()).stdout.toString(),
  listDir: async (path) => {
    const entries: string[] = [];
    try {
      for await (const name of new Bun.Glob("*").scan({ cwd: path, onlyFiles: false, dot: true })) entries.push(name);
    } catch {}
    return entries;
  },
  fileExists: async (path) => {
    try {
      await stat(path);
      return true;
    } catch {
      return false;
    }
  },
  getEnv: (name) => process.env[name],
};
