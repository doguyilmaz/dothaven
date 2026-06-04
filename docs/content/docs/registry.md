---
title: Registry
weight: 6
---

The registry is dothaven's single declarative source of truth for the config files
and directories it knows about. Every entry is one Go struct in
`internal/registry/registry.go`. There is no config file to edit and no plugin
system — the registry is compiled into the binary, and both `collect` and
`backup` are driven by the same list. Add or change an entry once, and discovery,
auditing, and copying all follow.

## The entry model

Each registered source is an `Entry`:

```go
type Entry struct {
	ID          string
	Name        string
	Paths       map[string]string // keyed by GOOS: "darwin", "linux", "windows"
	Category    string
	Kind        Kind
	Fields      []string          // JSONExtract only (empty = all keys)
	BackupDest  string
	Sensitivity Sensitivity
	Redact      func(string) string
}
```

- **`ID`** — stable section key in snapshots (e.g. `shell.zshrc`).
- **`Name`** — human-readable label.
- **`Paths`** — per-OS path templates (see [Paths and `~` expansion](#paths-and--expansion)).
- **`Category`** — grouping used by `--only` / `--skip` and in summaries.
- **`Kind`** — how the source is read (see [Entry kinds](#entry-kinds)).
- **`Fields`** — for `JSONExtract`, which top-level keys to pull (empty = all).
- **`BackupDest`** — relative destination path inside a backup tree.
- **`Sensitivity`** — `low`, `medium`, or `high` (see [Sensitivity levels](#sensitivity-levels)).
- **`Redact`** — optional content scrubber applied when redaction is on.

## Entry kinds

`Kind` decides how `Collect` turns a path into a snapshot section. There are four:

| Kind | Reads | Snapshot section produced |
| --- | --- | --- |
| `File` | Whole file content | `Content` — the trimmed file text (redacted if a `Redact` rule applies) |
| `FileMetadata` | File, but not its content | `Pairs` — `exists: true` and `lines: <count>` only |
| `Dir` | Directory listing | `Items` — one row per entry name, sorted |
| `JSONExtract` | JSON file, selected fields | `Pairs` — key/value pairs from the chosen top-level keys |

The mapping to snapshot shapes (`Content`, `Pairs`, `Items`) comes straight from
`Collect` in `registry.go`:

- **`File`** reads the file, optionally runs the `Redact` function when redaction
  is enabled, trims surrounding whitespace, and stores the result as the
  section's `Content`.
- **`FileMetadata`** reads the file only to count lines. It never stores the
  content — the section is just `{exists: "true", lines: "<n>"}`. This is how a
  large generated file like `.p10k.zsh` is recorded without dragging its body
  into the snapshot.
- **`Dir`** lists the directory, sorts the names, and emits one `Item` per name.
  An empty or unreadable directory is skipped entirely.
- **`JSONExtract`** parses the file as JSON and pulls the keys named in `Fields`.
  If a selected field is itself an object, its inner keys are flattened into the
  pair set; otherwise the field's scalar value is stored. An empty `Fields`
  means "extract every top-level key."

Any source that does not exist on disk (or fails to read/parse) is silently
skipped, so the registry can list more than any one machine has.

## Sensitivity levels

Every entry carries a `Sensitivity` of `low`, `medium`, or `high`. This is a
classification of how dangerous the file's contents are if they leak — it
documents intent and drives how you should treat each entry, especially when
exporting to chezmoi for age-encryption.

| Level | Meaning | Examples in the registry |
| --- | --- | --- |
| `low` | Safe to read and share; no secrets expected | shell rc files, `.gitconfig`, editor settings, `.tmux.conf` |
| `medium` | May contain identifying or environment detail | `~/.ssh/config`, AWS CLI `config`, gcloud configurations |
| `high` | Holds credentials or private key material | `~/.npmrc`, AWS `credentials`, `kubeconfig`, Docker config, GnuPG home |

{{< callout type="warning" >}}
Sensitivity drives real behavior, not just labeling. Entries with a `Redact` rule
have their content scrubbed during `collect`/`backup`. A `high`-sensitivity entry
with **no** redactor (e.g. AWS `credentials`, the GnuPG home) is **excluded from a
plaintext backup** — content scanning can't be trusted to catch every secret
(binary key material has no signature). Carry those with `chezmoi-export`, which
age-encrypts them at rest.
{{< /callout >}}

## Paths and `~` expansion

`Paths` is a map keyed by Go's `runtime.GOOS` — `"darwin"`, `"linux"`, or
`"windows"`. `ResolvePath` picks the template for the current OS and expands it:

```go
func ResolvePath(e Entry, home string) string {
	tmpl, ok := e.Paths[runtime.GOOS]
	if !ok {
		return "" // entry not applicable on this platform
	}
	return strings.Replace(tmpl, "~", home, 1)
}
```

Two rules follow from this:

- **Leading `~` is replaced by the home directory** (first occurrence only).
- **No entry for the current OS means an empty path**, and the entry is skipped
  by both `Collect` and `BackupTargets`. That is why some entries (shell rc
  files, `.p10k.zsh`, GnuPG, gcloud) define only `darwin` and `linux` — they are
  simply absent on Windows.

Windows templates use `%USERPROFILE%` and `%APPDATA%` literally; these are not
shell-expanded by `ResolvePath` (it only substitutes `~`).

## The Redact rule

`Redact` is an optional `func(string) string` that scrubs a `File` entry's
content before it is stored. It runs only for `Kind: File`, and only when
redaction is enabled (the default; disabled with `--no-redact`). Two registry
entries set one today, both from `internal/scan`:

- **`ssh.config`** uses `RedactSSHConfig`, which replaces `HostName` and
  `IdentityFile` values with `[REDACTED]` while keeping the file's structure.
- **`npm.config`** uses `RedactNpmTokens`, which replaces the value after
  `_authToken=` with `[REDACTED]`.

These are structure-preserving: the keys and layout survive so the redacted file
still reads as a valid config, only the secret value is masked.

## Registered entries

The registry currently declares around 84 entries across 17 categories. The lists
below are grouped by category; large categories show the notable entries and end
with "and more." The path shown is the macOS/Linux (`~`-relative) template;
Windows templates differ where defined and some entries are macOS/Linux-only.

{{< callout type="warning" >}}
Every credential-bearing entry is classified `high`. On a chezmoi export those are
age-encrypted at rest, and because they carry no `Redact` rule they are excluded
from a plaintext backup entirely.
{{< /callout >}}

### ai

AI assistant configs, skills, and project-memory files for Claude, Cursor, Gemini,
and Windsurf.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `ai.claude.settings` | Claude Settings | `~/.claude/settings.json` | JSONExtract | low |
| `ai.claude.skills` | Claude Skills | `~/.claude/skills` | Dir | low |
| `ai.claude.md` | CLAUDE.md | `~/.claude/CLAUDE.md` | File | low |
| `ai.cursor.mcp` | Cursor MCP Config | `~/.cursor/mcp.json` | File | low |
| `ai.gemini.settings` | Gemini Settings | `~/.gemini/settings.json` | JSONExtract | low |
| `ai.gemini.md` | GEMINI.md | `~/.gemini/GEMINI.md` | File | low |
| `ai.windsurf.mcp` | Windsurf MCP Config | `~/.codeium/windsurf/mcp_config.json` | File | low |

…and the matching skills directories for Cursor, Gemini, and Windsurf.

The Claude settings entry extracts only the `permissions` and `enabledPlugins`
fields; the Gemini settings entry extracts all top-level keys.

### shell

macOS/Linux only.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `shell.zshrc` | .zshrc | `~/.zshrc` | File | low |
| `shell.zprofile` | .zprofile | `~/.zprofile` | File | low |
| `shell.bashrc` | .bashrc | `~/.bashrc` | File | low |
| `shell.profile` | .profile | `~/.profile` | File | low |
| `shell.fish` | Fish Config | `~/.config/fish` | Dir | low |
| `shell.nushell` | Nushell Config | `~/.config/nushell` | Dir | low |

…and `shell.zshenv`, `shell.bash_profile`, and `shell.inputrc`.

### git

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `git.config` | .gitconfig | `~/.gitconfig` | File | low |
| `git.ignore` | .gitignore_global | `~/.gitignore_global` | File | low |
| `git.attributes` | .gitattributes_global | `~/.gitattributes_global` | File | low |
| `gh.config` | GitHub CLI Config | `~/.config/gh/config.yml` | File | low |

### editor

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `editor.zed` | Zed Settings | `~/.config/zed/settings.json` | File | low |
| `editor.cursor` | Cursor Settings | `~/Library/Application Support/Cursor/User/settings.json` | File | low |
| `editor.nvim` | Neovim Config | `~/.config/nvim` | Dir | low |
| `editor.vscode.settings` | VS Code Settings | `~/Library/Application Support/Code/User/settings.json` | File | low |
| `editor.helix` | Helix Config | `~/.config/helix` | Dir | low |
| `editor.editorconfig` | .editorconfig | `~/.editorconfig` | File | low |

…and `editor.vimrc`, VS Code keybindings/snippets, Doom Emacs, and Sublime Text.

The Cursor settings path differs by OS: `~/.config/Cursor/User/settings.json` on
Linux and `%APPDATA%/Cursor/User/settings.json` on Windows.

### terminal

macOS/Linux only.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `terminal.p10k` | .p10k.zsh | `~/.p10k.zsh` | FileMetadata | low |
| `terminal.tmux` | .tmux.conf | `~/.tmux.conf` | File | low |
| `terminal.starship` | Starship prompt | `~/.config/starship.toml` | File | low |
| `terminal.ghostty` | Ghostty | `~/.config/ghostty/config` | File | low |

…and Alacritty, Kitty, and WezTerm.

`.p10k.zsh` is recorded as metadata only (`exists` + `lines`), not content.

### ssh

| ID | Name | Path | Kind | Sensitivity | Redact |
| --- | --- | --- | --- | --- | --- |
| `ssh.config` | SSH Config | `~/.ssh/config` | File | medium | `RedactSSHConfig` |

### npm

| ID | Name | Path | Kind | Sensitivity | Redact |
| --- | --- | --- | --- | --- | --- |
| `npm.config` | .npmrc | `~/.npmrc` | File | high | `RedactNpmTokens` |

`.npmrc` is the one `high` entry with a redactor: its `_authToken` value is masked
in plaintext output, so it is the exception that can be backed up scrubbed.

### bun

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `bun.config` | .bunfig.toml | `~/.bunfig.toml` | File | low |

### cloud

Cloud and PaaS CLI configs. The `config`-style files are `medium`; anything that
stores auth tokens or keys is `high` (age-encrypted on export, never in a
plaintext backup).

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `cloud.aws.config` | AWS CLI config | `~/.aws/config` | File | medium |
| `cloud.aws.credentials` | AWS CLI credentials | `~/.aws/credentials` | File | high |
| `cloud.gcloud.configurations` | gcloud configurations | `~/.config/gcloud/configurations` | Dir | medium |
| `cloud.kube.config` | kubeconfig | `~/.kube/config` | File | high |
| `cloud.docker.config` | Docker config | `~/.docker/config.json` | File | high |
| `cloud.vercel` | Vercel CLI | `~/Library/Application Support/com.vercel.cli/auth.json` | File | high |
| `cloud.stripe` | Stripe CLI | `~/.config/stripe/config.toml` | File | high |

…and Azure, OCI, DigitalOcean, Fly.io, Linode, Hetzner, Netlify, Supabase,
Railway, Terraform Cloud, Pulumi, and Cloudflared — all `high`.

gcloud configurations and most token-bearing entries are macOS/Linux only.

### devops

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `devops.helm` | Helm repositories | `~/.config/helm/repositories.yaml` | File | medium |
| `devops.k9s` | k9s config | `~/.config/k9s/config.yaml` | File | low |
| `devops.colima` | Colima config | `~/.colima/default/colima.yaml` | File | low |
| `devops.podman` | Podman config | `~/.config/containers` | Dir | medium |

macOS/Linux only.

### build

Build tools that may hold repository credentials, so both are `high`.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `build.maven` | Maven settings | `~/.m2/settings.xml` | File | high |
| `build.gradle` | Gradle properties | `~/.gradle/gradle.properties` | File | high |

### db

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `db.pgpass` | .pgpass | `~/.pgpass` | File | high |
| `db.mycnf` | .my.cnf | `~/.my.cnf` | File | high |
| `db.psqlrc` | .psqlrc | `~/.psqlrc` | File | low |
| `db.sqliterc` | .sqliterc | `~/.sqliterc` | File | low |

`.pgpass` and `.my.cnf` carry DB passwords, so both are `high`.

### net

macOS/Linux only.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `net.curlrc` | .curlrc | `~/.curlrc` | File | medium |
| `net.wgetrc` | .wgetrc | `~/.wgetrc` | File | medium |

### dev

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `dev.direnv` | direnv | `~/.config/direnv` | Dir | low |

macOS/Linux only.

### apps

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `apps.karabiner` | Karabiner | `~/.config/karabiner/karabiner.json` | File | low |

macOS only.

### vm

Version-manager declarative config (live installed versions come from collectors).
macOS/Linux only.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `vm.tool-versions` | .tool-versions | `~/.tool-versions` | File | low |
| `vm.nvmrc` | .nvmrc | `~/.nvmrc` | File | low |
| `vm.mise` | mise config | `~/.config/mise/config.toml` | File | low |
| `vm.asdfrc` | .asdfrc | `~/.asdfrc` | File | low |

### secrets

Bare credential stores. All `high`: age-encrypted on chezmoi export and never
written to a plaintext backup. macOS/Linux only.

| ID | Name | Path | Kind | Sensitivity |
| --- | --- | --- | --- | --- |
| `secrets.netrc` | .netrc | `~/.netrc` | File | high |
| `secrets.vault` | Vault token | `~/.vault-token` | File | high |
| `secrets.gnupg` | GnuPG home | `~/.gnupg` | Dir | high |

The GnuPG entry is declarative: it is a no-op until `~/.gnupg` holds real keys.

## How the registry feeds collect and backup

The same `Entries` slice drives two different projections.

### Collect

`collect` calls `registry.Collect(env, home, redact, registry.Entries)`. For each
entry it resolves the path, skips it if empty or missing, and reads it according
to its `Kind` into a snapshot section. Redaction (on by default) applies a `File`
entry's `Redact` rule before the content is stored:

```bash
dothaven collect
```

Pass `--no-redact` to keep raw values:

```bash
dothaven collect --no-redact
```

### Backup

`backup` and `restore` both consume `registry.BackupTargets(home, entries)`,
which is the single projection of the registry into copy operations. For every
entry that has a path on the current platform it produces a `BackupTarget`:

```go
type BackupTarget struct {
	Src      string             // resolved live path
	Dest     string             // entry's BackupDest, relative to the backup tree
	Category string
	IsDir    bool               // true when Kind == Dir
	Redact   func(string) string
}
```

`Src` is the resolved live path, `Dest` is the entry's `BackupDest`, `IsDir`
reflects whether the kind is `Dir`, and `Redact` carries the same optional
scrubber. Because backup and restore read from one projection, a file always
maps back to the live path it came from.

```bash
dothaven backup
```

Backups honor the same redaction default and accept category filters that match
the entry `Category` field:

```bash
dothaven backup --only shell,git
dothaven backup --skip cloud,secrets
dothaven backup --archive          # write a .tar.gz instead of a directory
dothaven backup --no-redact        # keep raw values
```

The output directory follows dothaven's standard resolution: an explicit `-o`
wins; otherwise `<cwd>/reports` when run inside a git repo, else `~/Downloads`.

{{< callout type="info" >}}
Sensitive entries are best carried through the hybrid model: dothaven discovers,
audits, and exports them; chezmoi stores them and encrypts with age. Losing the
age key means those encrypted files are unrecoverable, so back the key up
separately.
{{< /callout >}}

## Missing a tool?

dothaven aims to be a **superset** of what chezmoi covers. If a config or CLI you use isn't in the registry yet, adding it is usually a one-line entry — open a request with the tool name and its config path:

{{< callout type="info" >}}
**[Request a config / tool →](https://github.com/doguyilmaz/dothaven/issues/new?template=config-request.yml)**
{{< /callout >}}

## Related

{{< cards >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../security" title="Security & redaction" >}}
{{< /cards >}}
