---
title: Collectors
weight: 6
---

A **collector** is a small unit that inventories one area of your machine and
returns a set of snapshot sections. The `collect` command (and `doctor`) runs the
full collector pipeline against the live machine and merges the results into a
single timestamped JSON snapshot.

Collectors are the discovery half of dothaven's hybrid model: dothaven does
discovery, audit, and export; chezmoi does storage, age-encryption, and apply.
Nothing here writes to your machine — collectors only read.

## How the pipeline runs

Every collector implements the same signature:

```go
type Collector func(c Ctx) snapshot.Snapshot
```

The `Ctx` carries the shared inputs for a run: a `context.Context`, the
environment seam (`Env`), the resolved `Home` directory, and a `Redact` flag.

The pipeline has two important properties, both defined in
`internal/collect/collect.go`:

- **Concurrent.** `RunCollectors` launches every collector in its own goroutine
  and waits for all of them. The wall-clock cost of a run is roughly the slowest
  single collector, not the sum.
- **Failure-isolated.** Each goroutine recovers from panics, and a collector that
  fails simply returns whatever it could (an empty map is fine). One collector
  crashing or returning nothing never aborts the run or affects the others.

Results merge in collector order, so if two collectors emit the same section id,
the later one wins. In practice the section ids are disjoint.

{{< callout type="info" >}}
Collectors shell out to external CLIs (`brew`, `npm`, `go`, `ollama`, …) through
the `Env.Run` seam. A missing tool is tolerated: the spawn fails, the collector
gets empty output, and that section is simply omitted. You never need every tool
installed — you get sections for the tools you have. A non-zero exit (for example
`npm ls` exiting 1 on peer warnings) is also tolerated; stdout is still parsed.
{{< /callout >}}

## The default pipeline

The canonical order, from `defaultCollectors()` in
`internal/cli/collect.go`:

1. `MetaCollector` — labels the snapshot with host / OS / date.
2. The declarative **registry** collector (see [Registry](../registry)).
3. `SSHCollector`
4. `OllamaCollector`
5. `AppsCollector`
6. `HomebrewCollector`
7. `PackagesCollector`
8. `VersionManagersCollector`
9. `RuntimesCollector`
10. `EditorsExtCollector`
11. `FontsCollector`
12. `DotfilesSweepCollector`

Meta runs first because it labels the snapshot with host and OS. The registry
collector is part of the same pipeline; it is declarative (a list of entries)
rather than command-backed, and it honours the same `Redact` flag so the one list
serves both `collect` (redacting) and `doctor` (raw). Its sections are documented
on the [Registry](../registry) page.

## Collector reference

The command-backed collectors and the sections they produce:

| Collector | Section ids | External tools | Notes |
|-----------|-------------|----------------|-------|
| Meta | `meta` | none (Go runtime only) | host, os (`<os> <arch>`), date (`YYYY-MM-DD`) |
| SSH | `ssh.hosts` | none (reads `~/.ssh/config`) | columns `[host, hostname, identity]`; hostname + identity redacted when redaction is on |
| Ollama | `ai.ollama.models` | `ollama` | name / size / modified per model |
| Apps | `apps.raycast`, `apps.alttab`, `apps.macos` | `ls`, `defaults` | macOS app inventory |
| Homebrew | `apps.brew.formulae`, `apps.brew.casks`, `apps.brew.bundle` | `brew` | installed formulae, casks, and a restorable Brewfile |
| Packages | `packages.npm.global`, `packages.bun.global`, `packages.pnpm.global`, `packages.node.fnm`, `packages.deno.bin`, `packages.pipx`, `packages.go.bin` | `npm`, `bun`, `pnpm`, `fnm`, `pipx` (reads `~/.deno/bin`, `~/go/bin`) | global package managers, node versions, pipx + `go install` binaries |
| VersionManagers | `vm.asdf.versions`, `vm.pyenv.versions`, `vm.rbenv.versions` | `asdf`, `pyenv`, `rbenv` | versions installed via each version manager |
| Runtimes | `runtimes.go`, `runtimes.rust`, `runtimes.rust.toolchains`, `runtimes.rust.crates`, `runtimes.swift`, `runtimes.zig`, `runtimes.xcode`, `runtimes.android`, `runtimes.android.buildTools`, `runtimes.android.platforms` | `go`, `rustc`, `cargo`, `rustup`, `swift`, `zig`, `xcodebuild`, `xcode-select`, `adb` | language / SDK toolchains |
| EditorsExt | `editor.vscode.extensions`, `editor.cursor.extensions` | `code`, `cursor` | installed editor extensions |
| Fonts | `fonts.user`, `fonts.system` | none (reads font directories) | user + system installed font files |
| DotfilesSweep | `home.dotfiles.review`, `home.dotfiles.managed` | `ls` | classifies `~/.X` entries against the registry |

Every section is emitted only when it has content. If a tool is absent or returns
nothing parseable, its section never appears in the snapshot.

### Meta

Records basic machine identity from the Go runtime — no subprocess. Emits a single
`meta` section with three pairs: `host` (from `os.Hostname()`), `os` (joined as
`<os> <arch>`, e.g. `darwin arm64`), and `date` (`YYYY-MM-DD`).

```json
"meta": {
  "pairs": { "host": "mbp", "os": "darwin arm64", "date": "2026-06-04" }
}
```

### SSH

Reads `~/.ssh/config` and parses it into ordered host entries (a new entry begins
at each `Host` line; `HostName` and `IdentityFile` attach to the current entry).
Emits `ssh.hosts` with one item per host, columned `[host, hostname, identity]`.

When redaction is on (the default for `collect`), `hostname` and `identity` are
replaced with `[REDACTED]`; the host alias itself is kept. If the file is missing
or has no hosts, no section is emitted.

### Ollama

Runs `ollama list` and parses the table (dropping the `ID` column). Emits
`ai.ollama.models`, one item per model with the non-empty values among
name / size / modified.

```text
$ ollama list
NAME              ID            SIZE      MODIFIED
llama3.2:latest   a80c4f17acd5  2.0 GB    3 weeks ago
```

### Apps

Gathers the macOS application inventory:

- `apps.raycast` — `installed: true|false` from the presence of
  `/Applications/Raycast.app/Contents/Info.plist`.
- `apps.alttab` — `installed` from the AltTab plist; if installed, it runs
  `defaults read com.lwouis.alt-tab-macos` and adds `preferences: exists` when
  preferences are present.
- `apps.macos` — the sorted listing of `/Applications` (via `ls /Applications`),
  emitted only when non-empty.

### Homebrew

Runs three `brew` commands and emits the results, each only when non-empty:

- `apps.brew.formulae` — `brew list --formula`, sorted.
- `apps.brew.casks` — `brew list --cask`, sorted.
- `apps.brew.bundle` — `brew bundle dump --file=-`, cleaned into a restorable
  Brewfile. Progress / noise lines are dropped, but every directive is kept
  (`go`, `npm`, `cargo`, `uv`, `whalebrew`, `vscode`, `mas`, …) so the Brewfile
  stays restorable.

### Packages

Inventories globally-installed packages across managers, each section emitted only
when non-empty:

| Section | Source |
|---------|--------|
| `packages.npm.global` | `npm ls -g --depth=0 --json` |
| `packages.bun.global` | `bun pm ls -g` |
| `packages.pnpm.global` | `pnpm ls -g --depth=0 --json` |
| `packages.node.fnm` | `fnm ls` (node versions; the default is flagged) |
| `packages.deno.bin` | the names in `~/.deno/bin` (directory read, no command) |
| `packages.pipx` | `pipx list --short` (Python apps; name + version) |
| `packages.go.bin` | the names in `~/go/bin` (directory read, no command) — `go install`ed binaries |

Package items carry name and version columns; the default fnm node version is
marked `(default)`. `packages.go.bin` captures user tools installed via
`go install` that otherwise have no config file to reproduce them.

### VersionManagers

Inventories the versions actually installed under each runtime version manager —
complementing the declarative configs (`.tool-versions`, mise config) that live in
the [registry](../registry), so you can check installed-vs-declared parity. Each
section is emitted only when the tool is present and reports at least one version:

| Section | Source |
|---------|--------|
| `vm.asdf.versions` | `asdf list` (parsed as tool → versions; the current `*` marker is stripped) |
| `vm.pyenv.versions` | `pyenv versions --bare` (one version per line; `*`, `(set by …)`, and `system` dropped) |
| `vm.rbenv.versions` | `rbenv versions --bare` (same parsing as pyenv) |

`vm.asdf.versions` items carry `[tool, version]` columns; the pyenv / rbenv
sections are plain version lists.

### Runtimes

Inventories language and SDK toolchains. Each section is emitted only when the
corresponding tool is present and its version parses:

| Section | Command(s) |
|---------|-----------|
| `runtimes.go` | `go version` → `version`, `platform` |
| `runtimes.rust` | `rustc --version`, `cargo --version` |
| `runtimes.rust.toolchains` | `rustup toolchain list` |
| `runtimes.rust.crates` | `cargo install --list` |
| `runtimes.swift` | `swift --version` |
| `runtimes.zig` | `zig version` |
| `runtimes.xcode` | `xcodebuild -version` (+ `xcode-select -p` for `path`) |
| `runtimes.android` | the Android SDK dir + `adb version` for `platformTools` |
| `runtimes.android.buildTools` | the `build-tools` subdirectory of the SDK |
| `runtimes.android.platforms` | the `platforms` subdirectory of the SDK |

The Android SDK directory is resolved from `ANDROID_HOME`, then
`ANDROID_SDK_ROOT`, then `~/Library/Android/sdk`. The `runtimes.android.*`
sections only appear when that directory exists.

### EditorsExt

Lists installed editor extensions by running `--list-extensions` per editor,
emitting each section only when non-empty:

- `editor.vscode.extensions` — `code --list-extensions`
- `editor.cursor.extensions` — `cursor --list-extensions`

### Fonts

Lists installed font files, deduped and sorted, filtered by extension
(`.ttf`, `.ttc`, `.otf`, `.otc`, `.dfont`, `.woff`, `.woff2`, `.pfb`). Missing
directories are skipped. Two sections, each emitted only when non-empty:

- `fonts.user` — `~/Library/Fonts` (macOS), `~/.fonts`, `~/.local/share/fonts` (Linux)
- `fonts.system` — `/Library/Fonts` (macOS), `/usr/share/fonts`, `/usr/local/share/fonts` (Linux)

### DotfilesSweep

Runs `ls -A ~` and classifies the top-level `~/.X` entries against the
[registry](../registry), which is the single source of truth for what is managed.
A small built-in noise list (`.DS_Store`, `.zsh_history`, `.cache`, …) is dropped
so transient files don't clutter the output. Two sections, each emitted only when
non-empty:

- `home.dotfiles.managed` — dot entries already covered by a registry entry.
- `home.dotfiles.review` — dot entries that are neither managed nor noise, i.e.
  candidates you may want to add to the registry. Anything not clearly noise lands
  here, so nothing important is hidden.

## Running collectors

The collector pipeline is what `collect` runs:

```bash
dothaven collect
```

```text
Report saved to: /path/to/reports/mbp-20260604-101500.json
```

Useful flags (from `internal/cli/collect.go`):

- `--no-redact` — keep raw values (skip secret redaction).
- `--slim` — truncate long file contents to 10 lines.
- `-o`, `--output` — output directory. The default is `./reports` when run inside a
  git repository, otherwise `~/Downloads`; an explicit `-o` always wins.

By default `collect` redacts secrets and prints a redaction summary; `doctor` runs
the same pipeline without redaction for local inspection.

{{< cards >}}
  {{< card link="../registry" title="Registry" subtitle="The declarative collector and what it manages" >}}
  {{< card link="../commands" title="Commands" subtitle="collect, doctor, and the rest of the CLI" >}}
{{< /cards >}}
