import { getHome } from "../utils/home";
import { defaultEnv } from "../collectors/env";
import { confirm, readLine } from "../utils/prompt";

export interface InitState {
  chezmoiInstalled: boolean;
  ageKeyConfigured: boolean;
  sourceInitialized: boolean;
  user?: string;
}

export interface InitStep {
  id: "chezmoi" | "age-key" | "source";
  title: string;
  status: "done" | "todo";
  command?: string;
  note?: string;
}

const KEY_PATH = "~/.config/chezmoi/key.txt";

/** The private chezmoi-source repo URL — `dotfiles` matches chezmoi's default convention. */
export function repoUrl(user?: string): string {
  return `git@github.com:${user ?? "<you>"}/dotfiles.git`;
}

/** Pure: the ordered prerequisites for a working chezmoi + age setup, given the probed state. */
export function planInit(state: InitState): InitStep[] {
  return [
    {
      id: "chezmoi",
      title: "chezmoi installed",
      status: state.chezmoiInstalled ? "done" : "todo",
      command: state.chezmoiInstalled ? undefined : "brew install chezmoi",
    },
    {
      id: "age-key",
      title: "age encryption key configured",
      status: state.ageKeyConfigured ? "done" : "todo",
      command: state.ageKeyConfigured ? undefined : `age-keygen -o ${KEY_PATH}`,
      note: state.ageKeyConfigured
        ? undefined
        : "Back this key up offline (password manager). Lose it and encrypted files are unrecoverable.",
    },
    {
      id: "source",
      title: "chezmoi source (private dotfiles repo) initialized",
      status: state.sourceInitialized ? "done" : "todo",
      command: state.sourceInitialized ? undefined : `chezmoi init ${repoUrl(state.user)}`,
    },
  ];
}

export const isReady = (steps: InitStep[]): boolean => steps.every((s) => s.status === "done");

async function probeInitState(home: string): Promise<InitState> {
  const ok = async (cmd: string[]) => {
    try {
      return !!(await defaultEnv.run(cmd)).trim();
    } catch {
      return false;
    }
  };

  const chezmoiInstalled = await ok(["chezmoi", "--version"]);

  // age is configured if chezmoi.toml declares encryption and the identity file is present.
  let ageKeyConfigured = false;
  const toml = Bun.file(`${home}/.config/chezmoi/chezmoi.toml`);
  if (await toml.exists()) {
    const text = await toml.text();
    ageKeyConfigured = /encryption\s*=\s*"age"/.test(text);
  }

  // source is initialized if chezmoi reports a source path that exists and is a git repo.
  let sourceInitialized = false;
  if (chezmoiInstalled) {
    try {
      const src = (await defaultEnv.run(["chezmoi", "source-path"])).trim();
      sourceInitialized = !!src && (await Bun.file(`${src}/.git/HEAD`).exists());
    } catch {}
  }

  let user: string | undefined;
  try {
    user = (await defaultEnv.run(["gh", "api", "user", "--jq", ".login"])).trim() || undefined;
  } catch {}

  return { chezmoiInstalled, ageKeyConfigured, sourceInitialized, user };
}

export async function init(_args: string[]) {
  const home = getHome();
  const state = await probeInitState(home);
  const steps = planInit(state);

  console.log("dothaven init — chezmoi + age bootstrap\n");
  for (const step of steps) {
    console.log(`  ${step.status === "done" ? "✓" : "→"} ${step.title}`);
    if (step.status === "todo" && step.command) console.log(`      ${step.command}`);
    if (step.note) console.log(`      ⚠ ${step.note}`);
  }

  if (isReady(steps)) {
    console.log(
      "\n✓ Setup complete. Next:\n  dothaven chezmoi-export          # dry-run — review the plan\n  dothaven chezmoi-export --apply  # execute",
    );
    return;
  }

  // Non-interactive (piped/CI): just print the guidance above.
  if (!process.stdin.isTTY) {
    console.log("\nRun the commands above, then re-run `dothaven init`.");
    return;
  }

  console.log("");
  for (const step of steps) {
    if (step.status === "done") continue;

    if (step.id === "chezmoi") {
      if (await confirm("Install chezmoi via Homebrew now?")) await runShown(["brew", "install", "chezmoi"]);
    } else if (step.id === "age-key") {
      // Guided only — generating/placing key material is left to you on purpose.
      console.log("  → Generate your key yourself, then re-run init:");
      console.log(`      age-keygen -o ${KEY_PATH}`);
      console.log('    Then point chezmoi at it (chezmoi.toml: encryption = "age", [age] identity/recipient).');
      console.log("    ⚠ Back the key up offline — losing it means encrypted files can't be decrypted.");
    } else if (step.id === "source") {
      const fallback = repoUrl(state.user);
      const url = (await readLine(`  Private repo URL [${fallback}]: `)) || fallback;
      if (url.includes("<you>")) {
        console.log("    Set your repo URL and re-run, or run: chezmoi init <url>");
      } else if (await confirm(`Run \`chezmoi init ${url}\`?`)) {
        await runShown(["chezmoi", "init", url]);
      }
    }
  }

  console.log("\nWhen every step is ✓, run: dothaven chezmoi-export");
}

async function runShown(cmd: string[]): Promise<void> {
  console.log(`  $ ${cmd.join(" ")}`);
  try {
    const out = (await defaultEnv.run(cmd)).trim();
    if (out) console.log(out);
    console.log("  ✔ done");
  } catch (error) {
    console.error(`  ✗ ${error instanceof Error ? error.message : error}`);
  }
}
