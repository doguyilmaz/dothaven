import type { CommandEnv } from "../../src/collectors/env";

export interface FakeEnvSpec {
  /** stdout keyed by the command name (cmd[0]). */
  outputs?: Record<string, string>;
  /** Full control over run — overrides `outputs`. May throw to simulate failures. */
  run?: (cmd: string[]) => string;
  /** listDir results keyed by absolute path. */
  dirs?: Record<string, string[]>;
  /** Paths that should report as existing. */
  files?: string[];
  /** Environment variables exposed via getEnv. */
  env?: Record<string, string>;
}

/** Build a deterministic CommandEnv for collector tests. */
export function fakeEnv(spec: FakeEnvSpec = {}): CommandEnv {
  return {
    run: async (cmd) => (spec.run ? spec.run(cmd) : (spec.outputs?.[cmd[0]] ?? "")),
    listDir: async (path) => spec.dirs?.[path] ?? [],
    fileExists: async (path) => spec.files?.includes(path) ?? false,
    getEnv: (name) => spec.env?.[name],
  };
}
