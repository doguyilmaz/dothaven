# dothaven — project guide

A single-binary Go CLI that inventories a machine's dev config, scans it for secrets, and feeds [chezmoi](https://chezmoi.io) (age-encrypted) for clean-install migration. dothaven is the **discovery + audit + export** layer; chezmoi owns **storage + encryption + apply**.

## Stack

- Go 1.26, module `github.com/doguyilmaz/dothaven`. CLI built with [Cobra](https://github.com/spf13/cobra).
- Layout: `cmd/dothaven` (entry) + `internal/{snapshot,scan,sys,collect,registry,backup,restore,chezmoi,cli}`.
- Distribution: GoReleaser → Homebrew tap (`doguyilmaz/homebrew-tap`). No npm/Bun/Node — that stack was fully removed.
- Docs: Hugo + Hextra in `docs/` (its own nested `go.mod`), deployed to GitHub Pages.

## Conventions

- **Pure core, thin shell.** Parsers, planners, classifiers are pure functions, table-tested against measured real output. Side effects (running commands, FS) go through the `sys.Env` seam (`sys.Fake` for tests). Commands in `internal/cli` stay thin.
- **Single source of truth.** `internal/registry` declares every config source once; both `collect` and `backup` project from it.
- **Collectors** are failure-isolated: goroutine fan-out + panic recovery; a missing CLI tool yields an empty section, never an abort.
- **Snapshots** are `map[string]Section` serialized with deterministic (alphabetical) JSON, 2-space indent, HTML escaping off.
- Regexes are RE2 (no catastrophic backtracking). Secret scanning has three severities (HIGH/MEDIUM/LOW) and three actions (skip > redact > include).
- Match surrounding code; keep comments for non-obvious logic only.

## Commands & cadence

```bash
go build ./...              # build
go test ./...              # unit + testscript e2e (cmd/dothaven/testdata/script)
gofmt -l ./cmd ./internal  # must be empty (CI gate); also: go vet ./...
cd docs && hugo server     # docs preview
```

Each logical slice lands as its own commit with **gofmt + vet + test green**. CI (`.github/workflows/ci.yml`) runs the same three checks; releases fire on `v*` tags.

## Testing

- Pure logic packages are well-covered (snapshot/scan/registry/backup/restore/chezmoi).
- e2e (testscript) covers the FS-deterministic commands: scan, security, compare, list, backup, restore, status, diff, chezmoi-export (dry-run).
- `collect`, `doctor`, `init` shell out to external tools, so they stay on unit + manual smoke — e2e would make CI depend on installed toolchains.

## Security constraints (non-negotiable)

- Secrets are redacted by default; a `skip`-action file (private key) is **never** written to a plaintext backup/snapshot.
- The age private key (`~/.config/chezmoi/key.txt`) must never enter any repo — loss means encrypted files are unrecoverable.
- The user's real dotfiles live in a **separate private** chezmoi-source repo; this public repo is code only.
- Don't push to remotes without explicit confirmation.

## Git

- Never add a `Co-Authored-By` trailer to commits.
- Commit when a slice is done; push only when asked.
