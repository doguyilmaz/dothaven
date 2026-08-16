# dothaven audit — 2026-08-17

Commit `ee1bc79`. All 11 packages pass. This file is not committed.

## What was actually broken

**1. `migrate` applied unprompted off a terminal.** The most destructive command
in the tool — it overwrites `$HOME` and runs your install script.

```go
if tui.Interactive() { ok, _ := tui.Confirm("Apply…"); if !ok { return } }
runShell(ctx, "chezmoi", "apply")   // reached whether or not the block ran
```

Piped, in a script, or over SSH without a TTY, the confirmation was skipped and
it applied. `restore` does the opposite and says so in a comment. `migrate` was
the outlier, and it was the one that mattered most.

**2. `defaults import` and `services import` wrote with no preview.**
`services import` replaces nginx/mysql/redis configs in `$(brew --prefix)/etc` —
outside `$HOME`, in Homebrew's tree — with no `--dry-run` and no confirmation.

**3. `scan` exited 0 with HIGH findings.** A secret scanner that always exits 0
can only be read by a human, which defeats scanning. `dothaven scan . && git commit`
passed with an AWS key sitting in the diff.

## What changed

**One safety gate, in one place** (`internal/cli/safety.go`). The five commands
that write had five different answers to the same question. Now: on a terminal,
ask; off a terminal, refuse unless `--yes`. A pipe cannot answer, and silence is
not consent.

| command | before | now |
|---|---|---|
| `migrate` | applied unprompted off-TTY | `--dry-run` (real `chezmoi diff`), `--yes`, refuses otherwise |
| `defaults import` | wrote silently | lists domains first, `--dry-run`, `--yes` |
| `services import` | wrote silently | lists files and marks **overwrites**, `--dry-run`, `--yes` |
| `scan` | exit 0 on HIGH | exit 2 on HIGH, `--no-fail` to opt out |
| `restore` | already correct | unchanged |

Both imports were restructured to plan-then-write, so `--dry-run` reports what
the write pass would actually do rather than a second guess at it.

## Why it felt complicated

Eighteen flat verbs, four of which compare things, and two artifact types
(snapshot vs backup) that the names never mentioned.

- **Commands are grouped** by the job you have: *Start here / Save this machine's
  config / Set up or repair a machine / See what would change / Secrets.*
- **Bare `dothaven` opens the menu** on a terminal. Someone who can't remember
  which verb they want is exactly who the menu is for, and help is what they used
  to get. Off a terminal it still prints help.
- **The comparisons say what they take.** `status` → "Latest backup vs this
  machine — one-screen summary", `doctor` → "Snapshot vs this machine — what is
  not installed", and so on. Same commands, no memorising.
- **Root help answers "which one do I use"** — `backup` (local, no setup) vs
  `chezmoi-export` (syncs machines, secrets encrypted). That choice was the main
  fork and nothing explained it.

## TUI

- Reordered least → most destructive. "Set up this machine (chezmoi apply)" was
  first with the cursor on it: two keypresses from launch to overwriting `$HOME`.
  It's now last, and the two that write say `WRITES to ~`.
- **Added "Scan for secrets"** — the tool's core capability wasn't in the menu.
- Fixed a latent nil dereference: `runTUIAction` called `sub.RunE` directly, so
  any parent command (`defaults`, `services`) would have panicked rather than
  errored.

## What was already good

Worth saying, because it's the part you'd worry about in a tool that handles
secrets.

- **The scanner works.** Seven planted secret shapes — AWS, OpenAI, GitHub PAT,
  OpenSSH private key, Postgres URL with inline credentials, Slack token — all
  caught, several twice, with redaction on by default.
- `restore` is exemplary: `--dry-run`, per-conflict resolution, a pre-restore
  snapshot, and a safe non-interactive default. It's the model the others now follow.
- `chezmoi-export` plans by default and confirms the age key before writing
  ciphertext you could otherwise lose access to.
- No TODOs, no `panic()` in non-test code, binary is Developer ID signed and
  notarized, `reports/` (which holds real ssh/cloud config) is gitignored and has
  **never** been in git history — checked, since the repo is public.

## Tests

Every fix is pinned by a test that failed before it:

- `migrate.txtar` — asserts refusal off-TTY, `--dry-run` writing nothing, `--yes` applying
- `scan.txtar` — asserts exit 2 on HIGH and `--no-fail` returning 0
- `macos-defaults.txtar`, `services.txtar` — assert refusal, then `--yes`
- `safety_test.go` — the gate itself

The migrate test failing on the first run *was* the bug reproducing.

## Left alone, deliberately

- **`compare`/`diff`/`doctor`/`status` still exist as four commands.** Merging them
  into `snapshot diff` / `backup diff` would read better but breaks every existing
  invocation. Grouping and clearer descriptions get most of the benefit at no cost.
  Worth doing on a major version.
- **`backup` has no `--dry-run`** — it only ever creates a new timestamped folder,
  never touches what's there.
- **Homebrew cask is correct as-is**: no `auto_updates`, deliberately. dothaven has
  no self-updater, so `brew upgrade` should own it — the opposite of the Sparkle apps.

## Chezmoi: is it pulling its weight?

Yes, and the division is right — dothaven discovers and audits, chezmoi stores,
encrypts, and applies. Reimplementing that would mean owning an encryption format,
which is the last thing this tool should do.

What was weak was the *handoff*, not the integration: nothing told you when to use
`backup` versus `chezmoi-export`, and `migrate` gave no way to see what `chezmoi apply`
would do before it did it. Root help now answers the first; `migrate --dry-run` shells
out to `chezmoi diff` for the second — the same comparison `apply` acts on, so the
preview can't disagree with the result.
