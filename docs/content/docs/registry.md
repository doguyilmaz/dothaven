---
title: What gets backed up
linkTitle: Registry
weight: 10
---

dothaven knows over 200 config locations. This list, the registry, is compiled into the binary and is the single source of truth: `backup`, `restore`, `scan`, `check`, `collect`, `chezmoi-export` and the dashboard all read it. If a path below exists on your machine, it is in your backups. Anything else can be added with [`dothaven include`](#your-own-paths).

Only your **user-level (global)** setup is listed here. Project-level config, such as a repository's own `.claude/`, `.mcp.json` or `.vscode/`, lives in the project and travels with its git repository. Run [`dothaven ready`](../commands#ready) to make sure those repositories are pushed.

## How to read the tables

| Mark | Meaning | In a plain backup | In an encrypted backup |
| --- | --- | --- | --- |
| 🔑 | A credential: logins, keys, tokens | Left out, and listed | Included |
| 🔒 | Sensitive config that can hold a secret (an MCP server's API key in its `env`, a host name) | Included, with secret values redacted | Included |
| (none) | Ordinary config | Included (scanned and redacted like everything else) | Included |

"🔑 never pushed to GitHub" marks age keys: included in an encrypted file backup, left out of every `github push`, even an encrypted one (see [GitHub](../github#storage-modes)).

The list below is generated from the registry itself (a test fails if they disagree), so it is exactly what a backup looks for.

On restore, 🔒 and 🔑 files are written owner-only (`0600`). In a GitHub `split` push, they go into the encrypted bundle. `.npmrc` and `~/.ssh/config` are credential-adjacent files with a dedicated redactor, so they stay in plain backups with just the token, `HostName` and `IdentityFile` masked.

Paths ending in `/` are folders: everything inside is carried, minus clutter each entry names (caches, logs, plugin downloads, shell history and compiled files). Symbolic links are followed. Where macOS and Linux differ, the macOS path is shown first. Entries marked (macOS) exist only there.

## AI tooling

Your global AI setup is often the hardest thing to rebuild by hand: skills and agents you wrote, slash commands, hooks, plugins and marketplaces, and MCP servers configured with API keys. The `ai` category covers:

- **Claude Code**: `settings.json` (permissions, plugins, marketplaces, hooks, status line, model), `~/.claude.json` (user-scope MCP servers, from `claude mcp add --scope user`), skills, agents (subagents), commands, hooks, output styles, plugins with their marketplaces, keybindings, the status line script, and `CLAUDE.md`. The whole of `~/.claude.json` is carried, including Claude Code's per-project state. On Linux its login file is carried too; on macOS the login is in the Keychain, which dothaven does not copy, so you sign in again.
- **Claude Desktop**: its MCP config.
- **Codex**: `config.toml` (including MCP servers), `AGENTS.md`, prompts, skills, command rules, and its login (`auth.json`).
- **Gemini CLI**: settings, skills, commands, extensions, `GEMINI.md`, its login, and its `.env` with the API key.
- **Cursor**: `mcp.json`, skills, commands, rules and agents; its editor settings, keybindings and snippets are in `editor`.
- **Windsurf**: MCP config, skills, global rules; editor settings and keybindings in `editor`.
- **Cline** (MCP servers in VS Code and Cursor, global rules and workflows), **Roo Code** (MCP servers, global rules), **VS Code** MCP servers (`mcp.json`), **opencode**, **GitHub Copilot CLI**, **Continue**, **aider**, **Goose**, **Kiro**, **Amp**, **Qwen Code** and **LM Studio**.

Local model files (Ollama weights and the like) are not copied. `collect` records which Ollama models you have so you can pull them again.

## The full list

### ai

| Path | What | Handling |
| --- | --- | --- |
| `~/.claude/settings.json` | Claude Settings | 🔒 |
| `~/.claude.json` | Claude Code state & user MCP servers | 🔒 |
| `~/.claude/skills/` | Claude Skills |  |
| `~/.claude/agents/` | Claude subagents |  |
| `~/.claude/commands/` | Claude slash commands |  |
| `~/.claude/hooks/` | Claude hook scripts |  |
| `~/.claude/output-styles/` | Claude output styles |  |
| `~/.claude/plugins/` | Claude plugins & marketplaces |  |
| `~/.claude/keybindings.json` | Claude keybindings |  |
| `~/.claude/statusline.sh` | Claude status line script |  |
| `~/.claude/.credentials.json` | Claude Code login | 🔑 |
| `~/.claude/CLAUDE.md` | CLAUDE.md |  |
| `~/Library/Application Support/Claude/claude_desktop_config.json` (Linux: `~/.config/Claude/claude_desktop_config.json`) | Claude Desktop MCP config | 🔒 |
| `~/.codex/config.toml` | Codex config & MCP | 🔒 |
| `~/.codex/AGENTS.md` | Codex AGENTS.md |  |
| `~/.codex/prompts/` | Codex prompts |  |
| `~/.codex/skills/` | Codex skills |  |
| `~/.codex/rules/` | Codex command rules |  |
| `~/.codex/auth.json` | Codex login | 🔑 |
| `~/.cursor/mcp.json` | Cursor MCP Config | 🔒 |
| `~/.cursor/skills/` | Cursor Skills |  |
| `~/.cursor/commands/` | Cursor commands |  |
| `~/.cursor/rules/` | Cursor rules |  |
| `~/.cursor/agents/` | Cursor agents |  |
| `~/.gemini/settings.json` | Gemini Settings | 🔒 |
| `~/.gemini/skills/` | Gemini Skills |  |
| `~/.gemini/GEMINI.md` | GEMINI.md |  |
| `~/.gemini/commands/` | Gemini commands |  |
| `~/.gemini/extensions/` | Gemini extensions |  |
| `~/.gemini/oauth_creds.json` | Gemini login | 🔑 |
| `~/.gemini/.env` | Gemini .env (API key) | 🔑 |
| `~/.codeium/windsurf/mcp_config.json` | Windsurf MCP Config | 🔒 |
| `~/.codeium/windsurf/skills/` | Windsurf Skills |  |
| `~/.codeium/windsurf/memories/global_rules.md` | Windsurf global rules |  |
| `~/Library/Application Support/Code/User/mcp.json` (Linux: `~/.config/Code/User/mcp.json`) | VS Code MCP servers | 🔒 |
| `~/.config/opencode/` | opencode config, agents & commands | 🔒 |
| `~/.copilot/mcp-config.json` | Copilot CLI MCP config | 🔒 |
| `~/.copilot/config.json` | Copilot CLI config | 🔒 |
| `~/.continue/config.yaml` | Continue config | 🔒 |
| `~/.continue/config.json` | Continue config (json) | 🔒 |
| `~/.aider.conf.yml` | aider config | 🔒 |
| `~/.config/goose/config.yaml` | Goose config | 🔒 |
| `~/.kiro/settings/mcp.json` | Kiro MCP config | 🔒 |
| `~/.kiro/steering/` | Kiro steering |  |
| `~/.config/amp/settings.json` | Amp settings | 🔒 |
| `~/.qwen/settings.json` | Qwen Code settings | 🔒 |
| `~/Library/Application Support/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` (Linux: `~/.config/Code/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json`) | Cline MCP servers | 🔒 |
| `~/Library/Application Support/Cursor/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json` (Linux: `~/.config/Cursor/User/globalStorage/saoudrizwan.claude-dev/settings/cline_mcp_settings.json`) | Cline MCP servers (in Cursor) | 🔒 |
| `~/Documents/Cline/Rules/` | Cline global rules |  |
| `~/Documents/Cline/Workflows/` | Cline global workflows |  |
| `~/Library/Application Support/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json` (Linux: `~/.config/Code/User/globalStorage/rooveterinaryinc.roo-cline/settings/mcp_settings.json`) | Roo Code MCP servers | 🔒 |
| `~/.roo/rules/` | Roo Code global rules |  |
| `~/.lmstudio/mcp.json` | LM Studio MCP config | 🔒 |

### shell

| Path | What | Handling |
| --- | --- | --- |
| `~/.zshrc` | .zshrc |  |
| `~/.zprofile` | .zprofile |  |
| `~/.zshenv` | .zshenv |  |
| `~/.bash_profile` | .bash_profile |  |
| `~/.bashrc` | .bashrc |  |
| `~/.zlogin` | .zlogin |  |
| `~/.zlogout` | .zlogout |  |
| `~/.bash_aliases` | .bash_aliases |  |
| `~/.bash_logout` | .bash_logout |  |
| `~/.fzf.zsh` | .fzf.zsh |  |
| `~/.fzf.bash` | .fzf.bash |  |
| `~/.zsh/` | ~/.zsh |  |
| `~/.config/zsh/` | XDG zsh config |  |
| `~/.profile` | .profile |  |
| `~/.config/fish/` | Fish Config |  |
| `~/Library/Application Support/nushell/` (Linux: `~/.config/nushell/`) | Nushell Config |  |
| `~/.inputrc` | .inputrc |  |
| `~/.oh-my-zsh/custom/` | oh-my-zsh custom |  |
| `~/.config/sheldon/plugins.toml` | sheldon plugins |  |
| `~/.zsh_plugins.txt` | antidote plugins |  |
| `~/.config/powershell/Microsoft.PowerShell_profile.ps1` | PowerShell profile |  |

### git

| Path | What | Handling |
| --- | --- | --- |
| `~/.gitconfig` | .gitconfig |  |
| `~/.gitignore_global` | .gitignore_global |  |
| `~/.config/gh/config.yml` | GitHub CLI Config |  |
| `~/.gitattributes_global` | .gitattributes_global |  |
| `~/.config/gh/hosts.yml` | GitHub CLI hosts | 🔑 |
| `~/.config/glab-cli/config.yml` | GitLab CLI | 🔑 |
| `~/.config/git/config` | XDG git config |  |
| `~/.config/git/ignore` | XDG git ignore |  |
| `~/.config/git/attributes` | XDG git attributes |  |
| `~/.config/git/allowed_signers` | git allowed_signers |  |
| `~/.config/git/hooks/` | Global git hooks |  |
| `~/Library/Application Support/lazygit/config.yml` (Linux: `~/.config/lazygit/config.yml`) | lazygit |  |
| `~/.config/jj/config.toml` | Jujutsu config |  |
| `~/.config/gitui/` | gitui |  |
| `~/.tigrc` | .tigrc |  |

### editor

| Path | What | Handling |
| --- | --- | --- |
| `~/.config/zed/settings.json` | Zed Settings |  |
| `~/Library/Application Support/Cursor/User/settings.json` (Linux: `~/.config/Cursor/User/settings.json`) | Cursor Settings |  |
| `~/Library/Application Support/Cursor/User/keybindings.json` (Linux: `~/.config/Cursor/User/keybindings.json`) | Cursor Keybindings |  |
| `~/Library/Application Support/Cursor/User/snippets/` (Linux: `~/.config/Cursor/User/snippets/`) | Cursor Snippets |  |
| `~/.config/zed/keymap.json` | Zed Keymap |  |
| `~/.config/zed/themes/` | Zed Themes |  |
| `~/.config/zed/snippets/` | Zed Snippets |  |
| `~/Library/Application Support/Windsurf/User/settings.json` (Linux: `~/.config/Windsurf/User/settings.json`) | Windsurf Settings |  |
| `~/Library/Application Support/Windsurf/User/keybindings.json` (Linux: `~/.config/Windsurf/User/keybindings.json`) | Windsurf Keybindings |  |
| `~/.config/nvim/` | Neovim Config |  |
| `~/.vim/` | ~/.vim (without plugins) |  |
| `~/.emacs` | Emacs init |  |
| `~/.emacs.d/` | ~/.emacs.d (config only) |  |
| `~/.vimrc` | .vimrc |  |
| `~/Library/Application Support/Code/User/settings.json` (Linux: `~/.config/Code/User/settings.json`) | VS Code Settings |  |
| `~/Library/Application Support/Code/User/keybindings.json` (Linux: `~/.config/Code/User/keybindings.json`) | VS Code Keybindings |  |
| `~/Library/Application Support/Code/User/snippets/` (Linux: `~/.config/Code/User/snippets/`) | VS Code Snippets |  |
| `~/.config/helix/` | Helix Config |  |
| `~/.config/doom/` | Doom Emacs |  |
| `~/Library/Application Support/Sublime Text/Packages/User/` (Linux: `~/.config/sublime-text/Packages/User/`) | Sublime Text User |  |
| `~/.editorconfig` | .editorconfig |  |
| `~/.ideavimrc` | .ideavimrc |  |

### terminal

| Path | What | Handling |
| --- | --- | --- |
| `~/.p10k.zsh` | .p10k.zsh |  |
| `~/.tmux.conf` | .tmux.conf |  |
| `~/.config/tmux/` | XDG tmux config |  |
| `~/Library/Application Support/iTerm2/DynamicProfiles/` (macOS) | iTerm2 dynamic profiles |  |
| `~/.config/starship.toml` | Starship prompt |  |
| `~/.config/alacritty/` | Alacritty |  |
| `~/.config/kitty/` | Kitty |  |
| `~/.config/wezterm/wezterm.lua` | WezTerm |  |
| `~/.config/ghostty/config` | Ghostty |  |
| `~/.config/zellij/` | Zellij |  |
| `~/.screenrc` | .screenrc |  |

### ssh

| Path | What | Handling |
| --- | --- | --- |
| `~/.ssh/config` | SSH Config | 🔒 |
| `~/.ssh/` | SSH keys & known hosts | 🔑 |

### npm

| Path | What | Handling |
| --- | --- | --- |
| `~/.npmrc` | .npmrc | 🔒 |

### bun

| Path | What | Handling |
| --- | --- | --- |
| `~/.bunfig.toml` | .bunfig.toml |  |

### cloud

| Path | What | Handling |
| --- | --- | --- |
| `~/.aws/config` | AWS CLI config | 🔒 |
| `~/.aws/credentials` | AWS CLI credentials | 🔑 |
| `~/.config/gcloud/configurations/` | gcloud configurations | 🔒 |
| `~/.kube/config` | kubeconfig | 🔑 |
| `~/.kube/` | kube configs | 🔑 |
| `~/.docker/daemon.json` | Docker daemon.json |  |
| `~/.boto` | gsutil / boto config | 🔑 |
| `~/.config/gcloud/application_default_credentials.json` | gcloud application default credentials | 🔑 |
| `~/.config/configstore/firebase-tools.json` | Firebase CLI login | 🔑 |
| `~/.docker/config.json` | Docker config | 🔑 |
| `~/.azure/` | Azure CLI | 🔑 |
| `~/.oci/` | Oracle Cloud (OCI) | 🔑 |
| `~/Library/Application Support/doctl/config.yaml` (Linux: `~/.config/doctl/config.yaml`) | DigitalOcean (doctl) | 🔑 |
| `~/.fly/` | Fly.io | 🔑 |
| `~/.config/linode-cli` | Linode CLI | 🔑 |
| `~/.config/hcloud/cli.toml` | Hetzner (hcloud) | 🔑 |
| `~/Library/Application Support/com.vercel.cli/auth.json` (Linux: `~/.local/share/com.vercel.cli/auth.json`) | Vercel CLI | 🔑 |
| `~/.config/netlify/config.json` | Netlify CLI | 🔑 |
| `~/.supabase/` | Supabase CLI | 🔑 |
| `~/.config/stripe/config.toml` | Stripe CLI | 🔑 |
| `~/.railway/config.json` | Railway CLI | 🔑 |
| `~/.terraform.d/credentials.tfrc.json` | Terraform Cloud creds | 🔑 |
| `~/.pulumi/credentials.json` | Pulumi creds | 🔑 |
| `~/.cloudflared/` | Cloudflared | 🔑 |
| `~/.tsh/` | Teleport (tsh) | 🔑 |
| `~/Library/Application Support/ngrok/ngrok.yml` (Linux: `~/.config/ngrok/ngrok.yml`) | ngrok | 🔑 |
| `~/.config/scw/config.yaml` | Scaleway CLI | 🔑 |
| `~/.config/argocd/config` | Argo CD | 🔑 |
| `~/.config/op/config` | 1Password CLI | 🔒 |

### devops

| Path | What | Handling |
| --- | --- | --- |
| `~/Library/Preferences/helm/repositories.yaml` (Linux: `~/.config/helm/repositories.yaml`) | Helm repositories | 🔒 |
| `~/Library/Application Support/k9s/config.yaml` (Linux: `~/.config/k9s/config.yaml`) | k9s config |  |
| `~/.colima/default/colima.yaml` | Colima config |  |
| `~/.config/containers/` | Podman config | 🔒 |
| `~/.ansible.cfg` | Ansible config | 🔒 |
| `~/.terraformrc` | Terraform CLI config |  |
| `~/.packerconfig` | Packer config |  |
| `~/.skaffold/config` | Skaffold config |  |

### build

| Path | What | Handling |
| --- | --- | --- |
| `~/.m2/settings.xml` | Maven settings | 🔑 |
| `~/.gradle/gradle.properties` | Gradle properties | 🔑 |

### db

| Path | What | Handling |
| --- | --- | --- |
| `~/.pgpass` | .pgpass | 🔑 |
| `~/.my.cnf` | .my.cnf | 🔑 |
| `~/.psqlrc` | .psqlrc |  |
| `~/.sqliterc` | .sqliterc |  |
| `~/.pg_service.conf` | .pg_service.conf | 🔑 |
| `~/.config/pgcli/config` | pgcli config |  |
| `~/.myclirc` | .myclirc |  |
| `~/.config/litecli/config` | litecli config |  |
| `~/.mongoshrc.js` | .mongoshrc.js |  |

### schedule

| Path | What | Handling |
| --- | --- | --- |
| `~/Library/LaunchAgents/` (macOS) | launchd agents | 🔒 |

### net

| Path | What | Handling |
| --- | --- | --- |
| `~/.curlrc` | .curlrc | 🔒 |
| `~/.wgetrc` | .wgetrc | 🔒 |

### dev

| Path | What | Handling |
| --- | --- | --- |
| `~/.config/direnv/` | direnv |  |
| `~/.config/chezmoi/` | chezmoi config | 🔒 |

### apps

| Path | What | Handling |
| --- | --- | --- |
| `~/.config/karabiner/` (macOS) | Karabiner |  |
| `~/.hammerspoon/` (macOS) | Hammerspoon |  |
| `~/.aerospace.toml` (macOS) | AeroSpace |  |
| `~/.config/aerospace/` (macOS) | AeroSpace (XDG) |  |
| `~/.yabairc` (macOS) | yabai |  |
| `~/.skhdrc` (macOS) | skhd |  |
| `~/.config/sketchybar/` (macOS) | SketchyBar |  |
| `~/.config/bat/` | bat |  |
| `~/.ripgreprc` | .ripgreprc |  |
| `~/.config/atuin/config.toml` | atuin |  |
| `~/.config/yazi/` | yazi |  |
| `~/.config/btop/btop.conf` | btop |  |

### vm

| Path | What | Handling |
| --- | --- | --- |
| `~/.tool-versions` | .tool-versions |  |
| `~/.nvmrc` | .nvmrc |  |
| `~/.config/mise/config.toml` | mise config |  |
| `~/.asdfrc` | .asdfrc |  |

### secrets

| Path | What | Handling |
| --- | --- | --- |
| `~/.config/chezmoi/key.txt` | chezmoi age key | 🔑 never pushed to GitHub |
| `~/Library/Application Support/sops/age/keys.txt` (Linux: `~/.config/sops/age/keys.txt`) | sops age keys | 🔑 never pushed to GitHub |
| `~/.netrc` | .netrc | 🔑 |
| `~/.vault-token` | Vault token | 🔑 |
| `~/.gnupg/` | GnuPG home | 🔑 |

### lang

| Path | What | Handling |
| --- | --- | --- |
| `~/.gemrc` | .gemrc |  |
| `~/.bundle/config` | Bundler config | 🔒 |
| `~/.irbrc` | .irbrc |  |
| `~/.config/uv/uv.toml` | uv config | 🔒 |
| `~/Library/Application Support/pip/pip.conf` (Linux: `~/.config/pip/pip.conf`) | pip config | 🔒 |
| `~/.condarc` | .condarc |  |
| `~/Library/Application Support/pypoetry/auth.toml` (Linux: `~/.config/pypoetry/auth.toml`) | Poetry auth | 🔑 |
| `~/.config/go/env` | go env |  |
| `~/.cargo/config.toml` | Cargo config | 🔒 |
| `~/.cargo/credentials.toml` | Cargo credentials | 🔑 |
| `~/.composer/composer.json` (Linux: `~/.config/composer/composer.json`) | Composer config |  |
| `~/.composer/auth.json` (Linux: `~/.config/composer/auth.json`) | Composer auth | 🔑 |
| `~/.nuget/NuGet/NuGet.Config` | NuGet config | 🔑 |
| `~/.yarnrc.yml` | .yarnrc.yml | 🔑 |
| `~/.iex.exs` | .iex.exs |  |
| `~/.julia/config/startup.jl` | Julia startup |  |

### mobile

| Path | What | Handling |
| --- | --- | --- |
| `~/.android/debug.keystore` | Android debug keystore | 🔑 |
| `~/Library/Developer/Xcode/UserData/KeyBindings/` (macOS) | Xcode key bindings |  |
| `~/Library/Developer/Xcode/UserData/FontAndColorThemes/` (macOS) | Xcode themes |  |
| `~/Library/Developer/Xcode/UserData/CodeSnippets/` (macOS) | Xcode code snippets |  |

### fonts

| Path | What | Handling |
| --- | --- | --- |
| `~/Library/Fonts/` (Linux: `~/.local/share/fonts/`) | Your fonts |  |

### dothaven

| Path | What | Handling |
| --- | --- | --- |
| `~/.config/dothaven/include` | dothaven include list |  |

## Your own paths

**Files your git config points at come along by themselves.** A `.gitconfig` often names files that have to travel with it: `core.hooksPath`, `core.excludesFile`, `commit.template`, `init.templateDir`, and the files `[include]` and `[includeIf]` pull in (a work identity, a signing key's config). Whatever those point at inside your home folder is carried like an include, without you listing it, and restored to the same place.

The registry will never know every tool you use. `dothaven include` adds anything in your home folder to every backup:

```bash
dothaven include ~/.config/raycast ~/bin
dothaven include --list      # what you added, and what looks like config but nothing covers
dothaven include --review    # pick from those interactively
```

- Your paths are kept in `~/.config/dothaven/include` (one per line; a line starting with `!` is a path you declined, so dothaven stops asking). That file is itself in the registry, so it travels with your backups.
- In a backup they live under `extra/`, and restore puts them back in the same place under your home folder, even on a new machine that has no include list yet.
- They are treated as 🔒: scanned and redacted in plain backups, written owner-only on restore.
- A credential folder stays protected however it is reached. Including `~/.aws` does not put `~/.aws/credentials` into a plain backup.

To find candidates, dothaven looks at the dot-files in your home folder, the entries in `~/.config`, `~/.claude`, `~/.codex` and `~/.gemini` (skipping their sessions, history and caches), and `~/bin`, and lists what no entry or include covers. On a terminal, `backup` offers these once.

## Missing a tool?

If a config or CLI you use should be in the registry, [request it](https://github.com/doguyilmaz/dothaven/issues/new?template=config-request.yml) with the tool's name and config path. In the meantime, `dothaven include <path>` carries it today.

## For contributors: the entry model

Each entry is one Go struct in `internal/registry/registry.go`:

```go
type Entry struct {
	ID          string            // stable section key, e.g. "shell.zshrc"
	Name        string            // human-readable label
	Paths       map[string]string // keyed by GOOS: "darwin", "linux", "windows"
	Category    string            // what --only / --skip select
	Kind        Kind              // File, FileMetadata, Dir, JSONExtract
	Fields      []string          // JSONExtract only: keys to pull into a snapshot
	BackupDest  string            // path inside a backup
	Sensitivity Sensitivity       // low, medium, high
	Redact      func(string) string // optional structure-preserving scrubber
	Exclude     []string          // Dir only: clutter to skip (caches, logs…)
}
```

- `Paths` use `~` for your home folder. An entry with no path for the current OS is skipped.
- `Kind` decides what a `collect` snapshot records: the file's text (`File`), just whether it exists and its line count (`FileMetadata`, used for the large generated `.p10k.zsh`), a folder listing (`Dir`), or selected JSON keys (`JSONExtract`). Backups always carry the whole file or folder.
- `Sensitivity: high` with no `Redact` is what keeps an entry out of plain backups. Two entries have a redactor: `ssh.config` (`HostName`, `IdentityFile`) and `npm.config` (`_authToken` and the legacy `_auth`/`_password`).
- `Exclude` patterns without a `/` match any path segment (`logs`, `*.log`); with a `/` they match from the entry's root.

`registry.BackupTargets` projects the entries onto source and destination paths. Backup uses it to copy and restore uses it to map each file back, so a file always returns to the path it came from.

{{< cards >}}
  {{< card link="../backup-restore" title="Backup & restore" >}}
  {{< card link="../security" title="Security" >}}
{{< /cards >}}
