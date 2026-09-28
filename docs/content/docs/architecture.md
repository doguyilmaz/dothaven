---
title: Architecture
weight: 16
---

This page is the contributor's map: how the packages divide the work, the seams that keep side effects testable, and how the pieces fit. dothaven is one static Go binary built on [Cobra](https://github.com/spf13/cobra), module `github.com/doguyilmaz/dothaven`, built from `./cmd/dothaven`.

## Principles

- **Pure core, thin shell.** Parsers, planners and classifiers are pure functions, table-tested against real tool output. Side effects go through small seams (`sys.Env`, `backup.Sink`, `gitwork.Runner`, the GitHub client's base URL) so tests can swap them. Commands in `internal/cli` stay thin.
- **One source of truth.** `internal/registry` declares every config location once. Backup, restore, scan, check, collect, the chezmoi export and the dashboard all project from it.
- **Failure isolation.** A missing tool yields an empty section, never an aborted run. Every external command has a deadline, except the ones that must run as long as they take (`reinstall`, `migrate`), which are attached to the terminal instead.
- **Safety in one place.** "Ask on a terminal, refuse off one unless `--yes`" is one function (`confirmWrite`). The plaintext redaction gate is one function (`backup.RunTo`).

## Package layout

```text
cmd/dothaven/            main (signals, exit codes) + testscript e2e harness
internal/
  cli/                   Cobra commands, grouped; composition root
  tui/                   interactive prompts (charmbracelet/huh)
  sys/                   the side-effect seam: commands, files, paths
  registry/              declared config locations; the user's include list
  collect/               concurrent collectors that inventory the machine
  snapshot/              the inventory model: JSON, compare, render
  scan/                  secret detection, redaction, previews
  backup/                the walk, the sinks (folder, tar, age), manifest, archives
  restore/               restore plan, classification, apply; the ledger
  chezmoi/               chezmoi export planner, templating, install script
  github/                GitHub API client: device flow, Git Data API commits
  github/githubtest/     in-memory fake GitHub for unit and e2e tests
  secretstore/           Keychain / Secret Service / owner-only file
  dashboard/             loopback, token-guarded, read-only web UI (embedded assets)
  gitwork/               find repositories and unsaved work (`ready`)
  health/                config validation by the owning parser (`check`)
  macprefs/              macOS preference capture, classification, Dock
  release/               update check and upgrade planning
```

Everything is under `internal/`: the only public surface is the CLI.

| Package | Responsibility |
| --- | --- |
| `cli` | Builds the command tree (grouped the way `--help` shows it) and wires each command to the packages below. Shared helpers: finding backups on disk, opening any kind of backup (folder, archive, encrypted, GitHub), passphrase prompts, `confirmWrite`. |
| `tui` | Menus, pickers, confirmations and the conflict prompt. Only called when stdin and stdout are terminals (`tui.Interactive()`). |
| `sys` | The `Env` interface and its real (`sys.OS`) and fake (`sys.Fake`) implementations; atomic, permission-aware file writes; the data, config and cache directories. |
| `registry` | `Entries`, the declared config sources (path per OS, kind, category, sensitivity, redactor, excludes); `BackupTargets`, the projection onto source and destination; the include list parser. |
| `collect` | Collectors for apps, Homebrew, packages, runtimes, version managers, editor extensions, fonts, mobile toolchains, scheduled jobs, and the sweep for uncovered config. |
| `snapshot` | `Snapshot` (`map[string]Section`), deterministic serialization, and structural compare. |
| `scan` | Pattern rules with severity and action, file and folder scanning, redaction, structure-preserving redactors, and `Preview` for safe output. |
| `backup` | `Walk` (symbolic links followed, excludes, the 64 MiB cap), `RunTo` (the redaction gate), the sinks, `Manifest`, and archive extraction and verification. |
| `restore` | `BuildPlanWith` (classify every file against the machine and the ledger), `Filter`, `Execute` (the only writer), and the `Ledger`. |
| `chezmoi` | `PlanExport` and `PlanFiles` (plain, encrypt, template), `Templatize`, SSH key and GnuPG detection, `BuildPackageInstallScript` (shared with backups' `install-packages.sh`), and the `init` checklist. |
| `github` | HTTPS client for the few endpoints needed: user, repository, create repository, Git Data API (blobs, trees, commits, refs), contents, tarball, device flow. |
| `secretstore` | `Get`/`Set`/`Delete` against the macOS Keychain (`security`), the Secret Service (`secret-tool`), or an owner-only file. |
| `dashboard` | The HTTP server, its security guard, and caching of panel data. Panels are functions supplied by `cli`. |
| `gitwork` | Walks roots for repositories and inspects each for dirty files, unpushed commits, stashes, missing remotes and ignored secret files, concurrently, with a timeout per git call. |
| `health` | Runs the owning parser for each tracked config file (`encoding/json`, `zsh -n`, `bash -n`, `git config`, `ssh -G`). |
| `macprefs` | Parses `defaults export` output, sorts keys into apply, review and skip, knows the core domains, and parses and builds Dock entries. |
| `release` | The once-a-day update check and how to upgrade each install method. |

## The `sys.Env` seam

Collectors and many commands take an `Env` instead of calling `os` and `exec` directly:

```go
type Env interface {
	Run(ctx context.Context, args ...string) (string, error)
	ReadFile(path string) ([]byte, error)
	ListDir(path string) ([]string, error)
	Exists(path string) bool
	Getenv(key string) string
	Home() string
}
```

`Run` tolerates a non-zero exit (it returns whatever reached stdout), because tools like `npm ls` exit 1 on harmless warnings. Only a failure to start the command is an error. Each command is bounded by a timeout and its output by a size cap.

`sys.Fake` serves files, folders, command output and environment variables from maps, so a collector's parsing is tested against fixed input with no machine involved.

## Collectors

A collector is `func(collect.Ctx) snapshot.Snapshot`. `RunCollectors` runs each in its own goroutine, writing to its own slot of a results slice, and merges after they all finish. A panic in one is recovered (and logged to stderr), so it contributes nothing instead of ending the run. See [Collectors](../collectors).

A backup's inventory is the same pipeline minus the registry reader: what is installed, not what the config files say, since the backup already carries those files.

## Backup: walk, gate, sink

```text
registry.BackupTargets ──> backup.Walk ──> backup.RunTo (gate) ──> Sink
                                                                    ├─ DirSink            a folder
                                                                    ├─ tar (+ gzip)       .tar.gz
                                                                    ├─ tar + gzip + age   .tar.gz.age, streamed
                                                                    ├─ SplitSink          readable files + encrypted bundle
                                                                    └─ DigestSink         fingerprint wrapper (GitHub push)
```

- `Walk` resolves one target into files: symbolic links followed (once per real folder, so a cycle cannot loop), sockets and devices skipped, excludes applied, files over the cap reported rather than dropped silently.
- `RunTo` is the plaintext gate: high-sensitivity entries without a redactor are skipped, files inside a credential root reached any other way are withheld, the scanner masks or withholds the rest. With encryption on, the gate is skipped: nothing is written in plaintext, so there is nothing to gate. Everything left out is recorded in the `Result` for the console and `MANIFEST.txt`.
- `Sink` is one interface for every destination, so a folder backup and an encrypted one cannot drift. `WriteArchive` streams tar into gzip into `age.Encrypt` into a `.partial` file, then renames it.
- `DigestSink` hashes paths, content and executable bits (never timestamps), so a GitHub push can tell an unchanged machine apart even though two encryptions of the same data share no bytes.

## Restore: plan, then apply

`restore.BuildPlanWith` walks the opened backup, maps each file back through the same `BackupTargets` projection (most specific folder first, refusing anything that would escape its target), reads the live file, and classifies it: `new`, `conflict`, `same` or `redacted`. The `Ledger` then refines that: a conflict whose live content is exactly what restore wrote before becomes `update`; one edited since becomes `changed`; a copy declined earlier becomes `skipped`.

Everything up to here is pure. `Execute` is the only function that writes: it honours the user's selection, asks through a callback for each file that differs, snapshots anything it replaces, refuses non-regular targets, and sets permissions by sensitivity. Its outcomes are folded back into the ledger (hashes only).

## GitHub

The client talks HTTPS to the API directly, so neither machine needs git and the token is never handed to a program that might store it. A push builds its tree with the Git Data API: small text files inline in the tree request, larger ones as blobs uploaded four at a time. It replaces only the `machines/<name>/` subtree, and moves the branch fast-forward only, rebuilding once on top if another push landed in between. A pull downloads the repository tarball and extracts it with the same zip-slip-safe extractor as local archives.

`DOTHAVEN_GITHUB_API` and `DOTHAVEN_GITHUB_WEB` point the client elsewhere (`https`, or `http` on loopback only), which is how the tests use `githubtest`, and how GitHub Enterprise would work.

Sign-in is the device flow of an OAuth app (scope `repo`): no installation step, and the first push can create the repository. The client also takes a GitHub App's device flow, whose user tokens last 8 hours: `resolveToken` renews them with the refresh token (no client secret is needed for device-flow tokens) and saves the new pair, since a refresh token works only once; read-only callers pass `renew=false` and report instead.

Commits name the GitHub App's `<slug>[bot]` as author and the account's noreply address as committer. When the user's git signs commits (`commit.gpgsign`), `cli.gitSigning` signs the exact object the API will build — `github.CommitPayload`, with the dates fixed and sent along — through `ssh-keygen -Y sign -n git` or `gpg -bsau`, and passes it as the commit's `signature`. GitHub checks it against the committer's signing keys, and its verdict comes back in the response's `verification`. The format is tested against `git verify-commit` itself. A bot-signed commit is not possible here: it needs an installation token, and so the app's private key, which a program on users' machines cannot hold.

### Setting up sign-in and the bot (maintainers)

Releases read two repository **variables** (Settings → Secrets and variables → Actions → Variables; both are public by design, so not secrets), baked in with `-ldflags` by `release.yml`. They come from two apps, as in lockstep:

1. **The OAuth app, for sign-in** → `DOTHAVEN_GITHUB_CLIENT_ID`. **Settings → Developer settings → OAuth Apps → New OAuth App**: name `dothaven`, homepage and authorization callback URL both the docs site (the callback is required but unused), then tick **Enable Device Flow**. Copy its **Client ID**. Never generate a client secret: the device flow needs none, and none may ship in the CLI.
2. **The GitHub App, for the bot** → `DOTHAVEN_GITHUB_APP`. **Settings → Developer settings → GitHub Apps → New GitHub App**: name `dothaven` (its URL name is the variable's value; the bot is `dothaven[bot]`), homepage the docs site, webhook off, **no permissions at all**, installable only on this account. It is never installed and never signs anyone in: GitHub creates the bot account with the app, and commits name it by its noreply address. Upload `docs/static/images/bot-1024.png` as its logo. No private key or client secret.

## Dashboard

`dashboard.Start` listens on `127.0.0.1:0` and serves the embedded page. Its guard runs before every handler: security headers, a `Host` check against the bound address, `GET`/`HEAD` only, and the key (from `?k=`, exchanged for a cookie) compared in constant time. Each panel is a `Source` function from `cli`, run detached from the request with a 90-second deadline, deduplicated while in flight, and cached for 20 seconds.

## Testing

- **Pure packages are table-tested** against measured real output: collectors' parsers, `scan`, `snapshot.Compare`, the restore classifier and ledger, the chezmoi planners, `macprefs`, `gitwork` with an injected runner, `github` against `githubtest`.
- **Deterministic output.** Snapshots serialize with sorted keys, two-space indent and HTML escaping off; renderers sort their output. Golden comparisons and git diffs stay stable.
- **End to end with testscript.** `cmd/dothaven/main_test.go` runs every `testdata/script/*.txtar` against the real binary in a temporary `HOME`. The harness registers fake `chezmoi`, `defaults`, `brew` and `killall` programs, starts a `githubtest` server for the GitHub scripts, sets `DOTHAVEN_SECRET_STORE=file`, and blanks any `gh` credentials, so a test never touches a real keychain, GitHub account or Dock.

```text
# encrypted.txtar (excerpt)
env DOTHAVEN_PASSPHRASE='correct horse battery staple'
exec dothaven backup --encrypt -o out --only ssh,git,cloud,npm,extra
stdout 'Encrypted backup saved'
! grep 'OPENSSH PRIVATE KEY' $ARCHIVE
rm .ssh .gitconfig .aws .npmrc .config
exec dothaven restore $ARCHIVE
grep 'BEGIN OPENSSH PRIVATE KEY' .ssh/id_ed25519
filemode 600 .ssh/id_ed25519
```

`collect`, `missing`, `reinstall` and `init` depend on installed toolchains, so they are covered by unit tests of their pure parts and by manual smoke tests rather than e2e scripts.

## Entry point and exit codes

`main` installs a two-stage signal handler (the first Ctrl-C cancels the command's context; a second one exits at once with 130, or 143 for SIGTERM), then runs `cli.Execute`, which also runs the update check alongside the command. A `cli.ExitError` carries an exit code without a message, which is how outcomes such as "something is missing" (1) or "a HIGH secret was found" (2) reach the shell without an error line. The version string defaults to `dev` and is set at release time with `-ldflags`.

{{< cards >}}
  {{< card link="../collectors" title="Collectors" >}}
  {{< card link="../registry" title="Registry" >}}
  {{< card link="../snapshot-format" title="Snapshot format" >}}
{{< /cards >}}
