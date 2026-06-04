# Roadmap

## Completed

| Phase | Feature | What Was Done |
|-------|---------|---------------|
| 1 | CLI Rewrite | Rewrote bash script → Bun/TypeScript CLI with `.json` output |
| 2 | Backup | Real file copies in structured directories with `--archive` support |
| 3 | Sensitivity Scan | 27+ detection patterns, three-stage pipeline, auto-redaction |
| 4 | Restore | Interactive picker, dry-run, pre-restore snapshots, conflict resolution |
| 5 | Diff Against Live | Color-coded backup vs machine comparison, `--section` filter |
| 6 | Config Registry | Single source of truth for all config paths — replaced 11 collector files |
| 7 | Multi-OS | macOS, Linux, Windows path resolution with per-platform registry entries |
| — | Status command | Quick backup summary with age, modified count, file list |
| — | Slim mode | `--slim` flag for AI-friendly truncated snapshots |
| — | Parallel collectors | `Promise.allSettled` for independent collectors |
| — | Archive export | `--archive` flag for `.tar.gz` backup output |
| — | Timestamped reports | `<hostname>-YYYYMMDDHHMMSS.json` naming |
| — | JSON migration | Retired the custom `.dotf` format and `@dotformat/core` dependency in favour of native JSON — the CLI now has zero runtime dependencies |

## Current Stats

- **8 commands**: collect, backup, scan, restore, diff, status, compare, list
- **23 registry entries**: across 8 categories
- **27+ scan patterns**: HIGH, MEDIUM, LOW severity
- **296 tests**: 708 assertions, 0 failures
- **3 platforms**: macOS, Linux, Windows

## Next Up

### `dotfiles init` — GitHub Template Flow

Guided onboarding for new users:

```bash
bunx @dotformat/cli init
# → "Create a private GitHub repo? (y/n)"
# → gh repo create my-dotfiles --private --template dotformat/template
# → cd my-dotfiles && dotfiles backup
# → git add . && git commit -m "initial backup" && git push
```

**New machine flow:**

```bash
git clone github.com/you/my-dotfiles
cd my-dotfiles
dotfiles restore --pick
```

**One-line remote install:**

```bash
curl -fsSL https://raw.githubusercontent.com/you/my-dotfiles/main/install.sh | bash
```

Rules:
- We never store user data — their GitHub is the storage
- Zero cloud, zero accounts beyond what they already have
- `gh` CLI required for repo creation (manual git init fallback available)

### Plugin System

Safe extension points for community collectors and customization:

- Plugin adds registry entries and optional collector hooks
- Plugin declares category and destination mapping
- Plugin participates in scan/redaction pipeline
- Plugin works without changing core command surface

Example:

```bash
dotfiles add ~/.config/starship.toml   # Future: adds entry to local registry
```

## Ideas Backlog

| Idea | Description | Priority |
|------|-------------|----------|
| `.local` override pattern | Separate shared vs machine-specific configs | Low |
| `--assume-unchanged` | Skip sensitive template files in GitHub flow | Low |
| Profile switching | `dotfiles use work` / `dotfiles use personal` | Medium |
| Encryption | Encrypt sensitive files with passphrase before storing | Medium |
| Shallow clone + submodules | Fast remote install | Low |
| Stream-based file copy | `Bun.file().stream()` for large backups | Low |
| Binary format | Optional `--format binary` for snapshot files | Low |
| Pluggable output | `--format json\|yaml\|toml` via registry layer | Medium |
| `bun build --compile` | Standalone binary distribution (no Bun required) | Medium |
| License change | MIT when going public | — |

## Technical Debt

- `compare` always enables color output (should respect TTY)
- `diff` only compares files in backup — no reverse discovery for new-on-machine files
- `scan` command doesn't support `--no-redact` (always reports all findings)
- Windows platform paths defined but not tested on real Windows machines
