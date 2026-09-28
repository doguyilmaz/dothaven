---
title: Introduction
weight: 1
---

dothaven moves your development setup from one machine to another. It finds your config (shell, git, editors, terminals, AI tools, SSH, cloud CLIs), records which apps and packages you have installed, captures your macOS settings, and packs all of it so a fresh machine can be set up the way the old one was.

## The problem

Migration Assistant copies a whole disk. A clean install gives you a fresh machine, and then a week of finding out what you forgot: the zsh plugin config, the Claude Code skills and MCP servers, the kubeconfig, the git hooks, the scroll direction, the global npm packages.

Most of this lives in your home folder, spread across hundreds of places, and some of it is secret. dothaven knows where those places are. It moves the files, keeps the secrets safe on the way, and tells you what it could not carry.

## What it carries

| What | Examples |
| --- | --- |
| Config files | `.zshrc`, `.gitconfig` and global hooks, Neovim, VS Code, Zed, Ghostty, tmux, Starship |
| AI tooling | Claude Code skills, agents, commands, hooks, plugins, MCP servers, `CLAUDE.md`; Codex, Gemini CLI, Cursor, Windsurf, opencode, Copilot CLI and more |
| Credentials | `~/.ssh` keys and `known_hosts`, AWS/GCP/Azure logins, kubeconfig, `.npmrc` tokens, GnuPG keys (encrypted backups only) |
| Installed software | Homebrew formulae, casks and App Store apps, global npm/pnpm/bun/pipx/uv/cargo packages, editor extensions, Linux packages |
| macOS settings | Trackpad, keyboard, Dock (including its apps), Finder, hot corners, keyboard shortcuts and layouts, language order |
| Your own paths | Anything else in your home folder you add with `dothaven include` |

The full list is on the [Registry](registry) page.

## Three ways to move

| Way | Command on the old machine | Command on the new machine | Good for |
| --- | --- | --- | --- |
| **One encrypted file** | `dothaven backup --encrypt` | `dothaven restore <file>` | A one-time move. Carry the file on a USB drive or cloud storage. |
| **Private GitHub repo** | `dothaven github push` | `dothaven restore github` | Keeping every machine backed up somewhere you can reach from anywhere. |
| **chezmoi repo** | `dothaven chezmoi-export --apply` | `dothaven migrate` | Keeping several machines in sync with [chezmoi](https://www.chezmoi.io/). |

All three are covered step by step in [Moving to a new machine](migration).

## How it keeps secrets safe

- **Plain backups never hold a credential.** Unless you ask for raw values with `--no-redact`, a plain `dothaven backup` masks secret values (the setting's name stays, the value becomes `[REDACTED]`) and leaves out SSH keys, cloud logins and other credential files. It lists everything it left out, on screen and in the backup's `MANIFEST.txt`.
- **The encrypted backup carries everything.** `dothaven backup --encrypt` streams all of it straight into [age](https://age-encryption.org) encryption. Nothing is written in plaintext, not even as a temporary file. Encryption is built into dothaven; you do not need to install age.
- **Nothing you already have is replaced without asking.** Restore shows a diff for each file that differs, saves the old copy first, and remembers what you applied or skipped.
- **GitHub repos must be private.** dothaven refuses to write to a public repository.

Details are on the [Security](security) page.

## What it does not carry

Some things are out of scope on purpose, because copying them would be wrong or unsafe:

- **Data**: databases, Docker volumes, anything a service stores.
- **System config** outside your home folder, such as `/etc/hosts`.
- **Apps, caches and toolchains.** They are reinstalled from the list, not copied.
- **Keychain items**, such as code-signing certificates and saved passwords.
- **Project-level config** (a repo's own `.claude/` or `.mcp.json`). It travels with the project's repository.

[Moving to a new machine](migration#what-does-not-travel) has a checklist for these.

## Where to next

{{< cards >}}
  {{< card link="installation" title="Installation" subtitle="Install on a fresh machine, with or without Homebrew." >}}
  {{< card link="quick-start" title="Quick start" subtitle="Your first backup, scan and dashboard in five minutes." >}}
  {{< card link="migration" title="Moving to a new machine" subtitle="The end-to-end guide." >}}
  {{< card link="commands" title="Commands" subtitle="Every command and flag." >}}
{{< /cards >}}
