# Dotfiles CLI — Master Plan

> The ultimate machine identity tool. Collect, backup, restore, compare, and sync configs across machines.

**Runtime:** Bun (required) — uses `Bun.file()`, `Bun.$`, `Bun.Glob` throughout
**Depends on:** native JSON (zero deps) — in-tree `src/snapshot` (parseSnapshot, serializeSnapshot, compareSnapshots, formatDiff)
**Package:** `dothaven`

---

## User Journeys

### Journey A: "What's on my machine?"
Single-file `.json` snapshot — parseable, diffable, queryable.

```bash
dothaven collect                    # → reports/<hostname>.json
dothaven list models                # → fuzzy query a section
dothaven compare home.json work.json # → structured diff
```

**Status: Done**

### Journey B: "Back up my configs"
Real file copies in structured directories. Two tracks:

```
                    ┌─────────────────┐
                    │  dotfiles CLI    │
                    └────────┬────────┘
                             │
              ┌──────────────┴──────────────┐
              │                             │
        Clone track                    CLI-only track
     (power users)                   (quick & portable)
              │                             │
    ┌─────────┴─────────┐          ┌───────┴───────┐
    │ Real files in repo │          │ Single .json  │
    │ shell/.zshrc       │          │ snapshot file │
    │ ai/claude/...      │          │ (carry/email) │
    │ git/.gitconfig     │          └───────────────┘
    └─────────┬──────────┘
              │
     git push to private repo
     (user's storage, not ours)
```

**Status: Done**

### Journey C: "Set up a new machine"
Restore from backup. Interactive picker. Conflict resolution.

```bash
dothaven restore ./backup --pick --dry-run
```

**Status: Done**

---

## Sensitivity Model

Never silent about sensitive data. Three layers:

1. **Detection** — regex patterns (tokens, keys, IPs, passwords, private keys)
2. **Classification** — HIGH (private keys, auth tokens) / MEDIUM (IPs, emails) / LOW (usernames, paths)
3. **Action** — per-finding: redact / skip / include / warn-only

Runs automatically during `backup` and `collect`. Summary at the end:

```
⚠ Sensitivity report:
  HIGH   ~/.ssh/id_ed25519         private key — skipped
  HIGH   ~/.npmrc                  auth token — redacted
  MEDIUM ~/.gitconfig              email address — included

  2 items redacted, 1 skipped. Use --no-redact to include all.
```

Also available standalone: `dothaven scan [path]`

---

## Output Path Logic

- `-o /path` → explicit custom directory
- Running from cloned repo (`.git` detected in cwd) → `reports/` under repo root
- Running as global CLI (no `.git`) → `~/Downloads`

---

## 1. CLI Rewrite (Done)

Rewrote bash script → Bun/TypeScript CLI outputting `.json` files.

### Commands

```bash
dothaven collect [--no-redact] [-o path]
dothaven compare [file1] [file2]
dothaven list <section>
```

### Collector Pattern

```typescript
interface CollectorContext {
  redact: boolean   // true by default
  home: string      // $HOME — injected for testability
}

interface Section {
  name: string
  pairs: Record<string, string>
  items: { raw: string; columns: string[] }[]
  content: string | null
}

type Snapshot = Record<string, Section>
type CollectorResult = Record<string, Section>
type Collector = (ctx: CollectorContext) => Promise<CollectorResult>
```

Snapshots serialize to plain JSON via the in-tree `src/snapshot` module
(`serializeSnapshot` / `parseSnapshot`). On disk, each section is keyed by its
id; empty `pairs` / `items` / `content` fields are omitted, pretty-printed with
2-space indent.

- Returns `{}` if tool/file not found (no errors for missing stuff)
- Uses `Bun.file()` for reads, `Bun.$` for shell commands
- Wraps `Bun.$` calls in try/catch (brew, ollama may not be installed)

### Section Mapping

| Section | Type | Source |
|---|---|---|
| `meta` | pairs | hostname, OS, date |
| `ai.claude.settings` | pairs | `~/.claude/settings.json` (permissions + enabledPlugins) |
| `ai.claude.skills` | items | `ls ~/.claude/skills/` |
| `ai.claude.md` | content | `~/.claude/CLAUDE.md` |
| `ai.cursor.mcp` | content | `~/.cursor/mcp.json` |
| `ai.cursor.skills` | items | `ls ~/.cursor/skills/` |
| `ai.gemini.settings` | pairs | `~/.gemini/settings.json` |
| `ai.gemini.skills` | items | `ls ~/.gemini/skills/` |
| `ai.gemini.md` | content | `~/.gemini/GEMINI.md` |
| `ai.windsurf.mcp` | content | `~/.codeium/windsurf/mcp_config.json` |
| `ai.windsurf.skills` | items | `ls ~/.codeium/windsurf/skills/` |
| `ai.ollama.models` | items (piped) | `ollama list` parsed |
| `shell.zshrc` | content | `~/.zshrc` |
| `git.config` | content | `~/.gitconfig` |
| `gh.config` | content | `~/.config/gh/config.yml` |
| `editor.zed` | content | `~/.config/zed/settings.json` |
| `editor.cursor` | content | `~/Library/.../Cursor/User/settings.json` |
| `terminal.p10k` | pairs | exists + line count |
| `ssh.hosts` | items (piped) | `~/.ssh/config` parsed, redacted |
| `npm.config` | content | `~/.npmrc` redacted |
| `bun.config` | content | `~/.bunfig.toml` |
| `apps.raycast` | pairs | installed check |
| `apps.alttab` | pairs | installed + prefs |
| `apps.macos` | items | `ls /Applications/` |
| `apps.brew.formulae` | items | `brew list --formula` |
| `apps.brew.casks` | items | `brew list --cask` |
| `terminal.tmux` | content | `~/.tmux.conf` |
| `editor.nvim` | content | `~/.config/nvim/init.lua` or `~/.vimrc` |

### File Structure

```
dotfiles/
├── src/
│   ├── cli.ts                    # Entry point — command routing
│   ├── commands/
│   │   ├── collect.ts            # Orchestrates all collectors → .json file
│   │   ├── compare.ts            # Diffs two .json reports
│   │   └── list.ts               # Fuzzy section query
│   ├── snapshot/
│   │   ├── types.ts              # Section, Snapshot, CollectorResult
│   │   ├── serialize.ts          # serializeSnapshot / parseSnapshot (native JSON)
│   │   └── compare.ts            # compareSnapshots / formatDiff (in-tree)
│   ├── collectors/
│   │   ├── types.ts              # CollectorContext, CollectorResult, makeSection
│   │   ├── meta.ts, claude.ts, cursor.ts, gemini.ts, windsurf.ts
│   │   ├── ollama.ts, shell.ts, git.ts, gh.ts, editors.ts
│   │   ├── terminal.ts, ssh.ts, npm.ts, bun-config.ts
│   │   ├── apps.ts, homebrew.ts
│   └── utils/
│       └── redact.ts             # Redaction patterns
├── tests/
│   ├── collectors/               # meta, ssh, npm, claude tests
│   ├── commands/                 # list fuzzy match tests
│   └── utils/                    # redact tests
├── bin/
│   └── dotfiles.ts               # #!/usr/bin/env bun
└── package.json
```

---

## 2. Backup (Done)

`dothaven backup [-o path] [--only ai,shell] [--skip editors]`

Real files in real directory structure. Only creates what exists (no empty folders).

```
backup/
├── ai/
│   ├── claude/
│   │   ├── settings.json
│   │   ├── CLAUDE.md
│   │   └── skills/
│   ├── cursor/
│   │   └── mcp.json
│   ├── gemini/
│   │   ├── settings.json
│   │   └── GEMINI.md
│   └── windsurf/
│       └── mcp_config.json
├── shell/
│   └── .zshrc
├── git/
│   ├── .gitconfig
│   └── .gitignore_global
├── editor/
│   ├── zed/settings.json
│   └── cursor/settings.json
├── terminal/
│   └── .p10k.zsh
├── ssh/
│   └── config              # redacted by default
├── npm/
│   └── .npmrc              # redacted by default
└── bun/
    └── .bunfig.toml
```

### Key decisions

- Registry-driven — backup sources generated from `src/registry/entries.ts`
- Sensitivity scan runs before writing
- `--only` / `--skip` for selective backup
- Clone track: writes into repo structure → user commits
- CLI-only track: still uses `.json` single-file export

---

## 3. Sensitivity Scan (Done)

`dothaven scan [path]` — also runs automatically during backup/collect.

### Detection patterns

| Level | Pattern | Example |
|---|---|---|
| HIGH | Private key headers | `-----BEGIN.*PRIVATE KEY-----` |
| HIGH | Auth tokens | `_authToken=`, `Bearer `, `sk-`, `ghp_`, `npm_` |
| HIGH | AWS keys | `AKIA...`, `aws_secret_access_key` |
| HIGH | SaaS keys | Stripe, Mapbox, Slack, SendGrid, Discord, etc. |
| HIGH | AI keys | OpenAI `sk-`, Anthropic `sk-ant-` |
| HIGH | Database URLs | `postgres://`, `mongodb://`, `redis://` |
| HIGH | Generic secrets | `PASSWORD=`, `SECRET=`, `API_KEY=` |
| MEDIUM | IP addresses | `\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b` |
| MEDIUM | Email addresses | `\b\w+@\w+\.\w+\b` |
| LOW | Home directory paths | `/Users/<username>/`, `/home/<username>/` |

### Actions

- `redact` → replace value with `[REDACTED]` (default for HIGH)
- `skip` → exclude entire file from output (default for private keys)
- `include` → keep as-is with warning (default for LOW)
- `--no-redact` → bypass all, include everything

---

## 4. Restore (Done)

`dothaven restore <path> [--pick] [--dry-run]`

```bash
dothaven restore ./backup              # restore everything
dothaven restore ./backup --pick       # interactive section picker
dothaven restore ./backup --dry-run    # preview only, no changes
```

- `--pick` → checkbox UI: select which configs to restore
- `--dry-run` → shows what would change, doesn't touch anything
- Conflict handling: if target file differs, prompt overwrite / skip / show diff
- Pre-restore snapshot: before any overwrite, saves old files to `pre-restore-<timestamp>/` — same backup format, reversible with `dothaven restore`
- Supports `.local` override pattern: if `backup/shell/.zshrc.local` exists, restore it alongside `.zshrc`

---

## 5. Diff Against Live (Done)

`dothaven diff [--section ai]`

Compares repo backup against current machine state. Answers: "what changed since last backup?"

```bash
dothaven diff
# shell/.zshrc — modified (3 lines added)
# ai/claude/settings.json — modified (2 plugins added)
# editor/cursor/settings.json — unchanged
# ai/windsurf/mcp_config.json — new file (not in backup)
```

- Color-coded: green (unchanged), yellow (modified), blue (new), gray (redacted)
- `--section` flag for scoped diff
- Auto-finds latest backup if no path given

---

## 6. Config Registry (Done)

Extensible manifest of what configs exist and where they live on each OS.

```typescript
const registry: ConfigEntry[] = [
  {
    id: "shell.zshrc",
    name: ".zshrc",
    paths: { darwin: "~/.zshrc", linux: "~/.zshrc" },
    category: "shell",
    kind: { type: "file" },
    backupDest: "shell/.zshrc",
    sensitivity: "low",
  },
];
```

- Single source of truth — replaces 11 hardcoded collector files
- Generates collectors and backup sources automatically
- Users extend via `dotfiles add ~/.config/starship.toml` (future)
- Platform-aware paths enable Multi-OS support
- Categories power `--only` / `--skip` filtering

---

## 7. Multi-OS (Done)

- macOS: `~/Library/Application Support/...`
- Linux: `~/.config/...`
- Windows: `%APPDATA%/...`, `%USERPROFILE%/...`
- Registry entries have per-platform paths (`darwin`, `linux`, `win32`)
- `resolvePath()` handles `~`, `%APPDATA%`, `%USERPROFILE%` expansion
- OS-specific collectors have platform guards (`brew` only on macOS)
- Shell configs (zshrc, p10k, tmux) excluded on Windows (no path = auto-skip)

---

## 8. Init (GitHub template flow)

`dothaven init` → guided onboarding for new users.

### New user (no repo)

```bash
bunx dothaven init
  → "Create a private GitHub repo? (y/n)"
  → gh repo create my-dotfiles --private --template dotformat/template
  → cd my-dotfiles
  → dothaven backup
  → git add . && git commit -m "initial backup"
  → git push
  → "Done. Your configs are backed up to github.com/you/my-dotfiles"
```

### New machine (has repo)

```bash
git clone github.com/you/my-dotfiles
cd my-dotfiles
dothaven restore --pick
  → [ ] shell/.zshrc
  → [x] ai/claude/settings.json
  → [x] ai/claude/CLAUDE.md
  → [ ] git/.gitconfig
  → "Restore 2 selected configs? (y/n)"
```

### One-line remote install

```bash
curl -fsSL https://raw.githubusercontent.com/you/my-dotfiles/main/install.sh | bash
```

### Rules

- We never store user data — their GitHub is the storage
- Zero cloud, zero accounts beyond what they already have
- `gh` CLI required for repo creation (optional: manual git init fallback)

---

## 8. Quick Wins (from backlog)

Low-effort, high-impact improvements to daily usage.

### 8a. `dothaven status`
Quick summary: what's changed, what's backed up, what's new.
```bash
dothaven status
# Last backup: 2h ago (backup-doguyilmaz.local-20260407...)
# 3 modified since backup, 143 unchanged
# Modified: shell/.zshrc, ai/claude/settings.json, git/.gitconfig
```

### 8b. `--slim` flag for collect
AI token-efficient snapshots — strips verbose content, keeps structure + metadata only.
```bash
dothaven collect --slim    # smaller .json, good for feeding to AI
```

### 8c. Parallel collectors
`Promise.allSettled` for independent collectors (brew, ollama, file reads) — faster collect.

### 8d. Update README
Bun requirement, full CLI usage docs, all commands documented.

---

## Ideas Backlog

- [x] Timestamped report filenames — `<hostname>-YYYYMMDDHHMMSS.json`, no overwrites
- [ ] `.local` override pattern — separate shared vs machine-specific configs (inspired by gko/dotfiles)
- [ ] `--assume-unchanged` for sensitive template files in GitHub flow
- [ ] Profile switching — `dotfiles use work` / `dotfiles use personal`
- [ ] Encryption for sensitive files — encrypt with passphrase before storing, decrypt on restore
- [ ] Shallow clone + submodules for fast remote install
- [ ] Plugin system — community collectors for tools we don't cover
- [ ] Stream-based file copy — `Bun.file().stream()` for memory-safe large backup operations
- [x] Archive output — `--archive` flag for `.tar.gz` backup export (uses system tar, migrate to `Bun.Archiver` when available)
- [ ] Binary format — optional `--format binary` for `.json` snapshots
- [ ] Pluggable output format — `--format json|yaml|toml` via registry layer. Bun has native TOML/YAML parsers — use them when implementing
- [ ] `bun build --compile` — standalone binary distribution (no Bun install required)
- [ ] License — change to MIT when going public
- [ ] Init (GitHub template flow) — guided onboarding, `gh` repo create, one-line install

---

## Progress

| # | What | Status |
|---|---|---|
| 1 | CLI rewrite (collect, compare, list) | Done |
| 2 | Backup (structured file copy) | Done |
| 3 | Sensitivity scan | Done |
| 4 | Restore (with --pick, --dry-run, pre-restore snapshot) | Done |
| 5 | Diff against live system | Done |
| 6 | Config registry | Done |
| 7 | Multi-OS | Done |
| 8a | `dothaven status` | Next |
| 8b | `--slim` flag | Next |
| 8c | Parallel collectors | Next |
| 8d | README update | Next |
