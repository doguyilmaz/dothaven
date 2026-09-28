---
title: Collectors
weight: 14
---

A **collector** inventories one area of your machine: installed apps, Homebrew, global packages, runtimes, and so on. Collectors only read; they never change anything.

They are used in three places:

- **Every backup** runs them to record what is installed (`inventory/snapshot.json`), which is what `reinstall` and `missing` read on the new machine.
- **`dothaven collect`** runs them all, plus the registry reader, and writes a timestamped JSON [snapshot](../snapshot-format) to `~/.local/share/dothaven/snapshots`.
- **`reinstall`, `missing`** and the chezmoi install script run the relevant ones against the machine they are on, to see what is already there.

## How they run

```go
type Collector func(c Ctx) snapshot.Snapshot
```

The `Ctx` holds the context, the `sys.Env` seam, your home folder and whether to redact. Two rules, from `internal/collect/collect.go`:

- **Concurrent.** Every collector runs in its own goroutine, so a run costs about as long as the slowest one.
- **Failure-isolated.** A collector returns what it could, even nothing. A panic is recovered and logged, and the others carry on.

Collectors call external tools (`brew`, `npm`, `go`, …) through `Env.Run`, each with a time limit. A tool that is not installed simply produces no section: you get sections for the tools you have. A non-zero exit, such as `npm ls` exiting 1 on a peer warning, is tolerated and its output still parsed.

## The pipeline

In order, from `defaultCollectors()` in `internal/cli/collect.go`:

1. `MetaCollector`: host, OS and date.
2. The registry reader: the tracked config files (see [Registry](../registry)). `collect` only; a backup carries the files themselves.
3. `SSHCollector`
4. `OllamaCollector`
5. `AppsCollector`
6. `HomebrewCollector`
7. `PackagesCollector`
8. `LinuxPackagesCollector`
9. `VersionManagersCollector`
10. `RuntimesCollector`
11. `EditorsExtCollector`
12. `FontsCollector`
13. `MobileCollector`
14. `ScheduledCollector`
15. `DotfilesSweepCollector`

Results are merged after all finish. A section only appears when it has content.

## Reference

| Collector | Sections | Reads |
| --- | --- | --- |
| Meta | `meta` | Go runtime only: `host`, `os` (`<os> <arch>`), `date` |
| SSH | `ssh.hosts` | `~/.ssh/config`: host, hostname, identity file (hostname and identity redacted) |
| Ollama | `ai.ollama.models` | `ollama list`: name, size, modified. Model names only, not the weights |
| Apps | `apps.raycast`, `apps.alttab`, `apps.macos` | `/Applications` (macOS) |
| Homebrew | `apps.brew.formulae`, `apps.brew.casks`, `apps.brew.bundle` | `brew list`, `brew bundle dump` (a restorable Brewfile, taps, `mas` and `vscode` lines included) |
| Packages | `packages.npm.global`, `packages.pnpm.global`, `packages.bun.global`, `packages.node.fnm`, `packages.deno.bin`, `packages.pipx`, `packages.uv`, `packages.go.bin`, `packages.composer`, `packages.pub`, `packages.dotnet` | `npm`, `pnpm`, `bun`, `fnm`, `pipx`, `uv`, `composer`, `dart`, `dotnet`; the folders `~/.deno/bin` and `~/go/bin` |
| LinuxPackages | `packages.apt`, `packages.dnf`, `packages.pacman`, `packages.snap`, `packages.flatpak` | Explicitly installed packages: `apt-mark showmanual`, `dnf repoquery --userinstalled`, `pacman -Qqe`, `snap list`, `flatpak list --app` |
| VersionManagers | `vm.asdf.versions`, `vm.pyenv.versions`, `vm.rbenv.versions`, `vm.goenv.versions`, `vm.nodenv.versions`, `vm.sdkman.versions`, `vm.proto.versions`, `vm.jenv.versions`, `vm.fvm.versions` | The versions each manager has installed |
| Runtimes | `runtimes.go`, `runtimes.rust`, `runtimes.rust.toolchains`, `runtimes.rust.crates`, `runtimes.swift`, `runtimes.zig`, `runtimes.xcode`, `runtimes.android`, `runtimes.android.buildTools`, `runtimes.android.platforms` | `go`, `rustc`, `cargo`, `rustup`, `swift`, `zig`, `xcodebuild`, `adb`, the Android SDK folder |
| EditorsExt | `editor.vscode.extensions`, `editor.cursor.extensions` | `code --list-extensions`, `cursor --list-extensions` |
| Fonts | `fonts.user`, `fonts.system` | Font folders (`~/Library/Fonts`, `~/.fonts`, `~/.local/share/fonts`, system font folders) |
| Mobile | `mobile.ios.runtimes`, `mobile.android.sdk`, `mobile.android.avds` | `xcrun simctl list runtimes`, the Android SDK folder, `~/.android/avd`. Names only: simulator runtimes and emulator images are gigabytes and rebuilt from a name |
| Scheduled | `schedule.crontab` | `crontab -l` |
| DotfilesSweep | `home.dotfiles.managed`, `home.dotfiles.review`, `home.config.review` | Your home folder and `~/.config`, sorted into what the registry covers and what it does not |

The Android SDK folder is `ANDROID_HOME`, then `ANDROID_SDK_ROOT`, then `~/Library/Android/sdk`.

## What reinstall and missing use

`missing` compares the installable sections (`packages.*`, `runtimes.*`, `vm.*`, `apps.brew.*`, `apps.macos`, `fonts.*`, `*.extensions`) of a backup or snapshot with this machine, by name. `reinstall` installs from the Brewfile (formulae, casks, taps, App Store apps, VS Code extensions) and from the package sections it has an installer for. See [Backup & restore](../backup-restore#reinstall-and-missing).

## Running collect

```bash
dothaven collect
```

```text
Snapshot saved to: /Users/you/.local/share/dothaven/snapshots/mymac-20260927233425.json
  24 sections. Browse with `dothaven list <section>`, e.g. `dothaven list brew`.
```

| Flag | Meaning |
| --- | --- |
| `--no-redact` | Keep raw values (skip secret redaction) |
| `-o`, `--output string` | Output directory (default: `~/.local/share/dothaven/snapshots`) |
| `--slim` | Truncate long file contents to 10 lines |

Secrets are redacted by default and a summary printed. The snapshot includes the text of your tracked config files, with secret values masked, so treat it as private: it is written owner-only.

{{< cards >}}
  {{< card link="../snapshot-format" title="Snapshot format" >}}
  {{< card link="../registry" title="Registry" >}}
{{< /cards >}}
