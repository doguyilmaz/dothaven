---
layout: home

hero:
  name: 'dothaven'
  text: Machine Identity CLI
  tagline: Collect, backup, restore, and diff your configs across machines.
  actions:
    - theme: brand
      text: Get Started
      link: /getting-started
    - theme: alt
      text: Commands
      link: /commands

features:
  - title: Snapshot
    details: Generate a structured .json report of your entire machine config. AI tools, shell, git, editors, brew, apps, SSH, and more.
  - title: Backup & Restore
    details: Copy real config files into a categorized directory. Restore on a new machine with conflict resolution, rollback, and interactive picker.
  - title: Sensitivity Scan
    details: 27+ patterns detect tokens, keys, and secrets. Auto-redact on collect and backup. Never silent about sensitive data.
  - title: Diff & Compare
    details: Color-coded diff of backup vs live system. Compare two .json reports side by side. Track what changed since last backup.
  - title: Registry-Driven
    details: Single source of truth for all config paths. Add a new tool by adding one entry and collection, backup, and restore all stay in sync.
  - title: Multi-Platform
    details: macOS, Linux, and Windows path resolution. Per-platform paths in registry entries. Platform guards for OS-specific collectors.
---

## Quick Start

```bash
# Snapshot your machine
bunx dothaven collect

# Back up real files
bunx dothaven backup

# Restore on a new machine
bunx dothaven restore ./backup --pick --dry-run
```

## What's in a Snapshot?

A `.json` report captures:

| Category | What's Collected |
|----------|-----------------|
| **AI Tools** | Claude, Cursor, Gemini, Windsurf (settings, skills, MCP configs) |
| **Shell** | `.zshrc` |
| **Git** | `.gitconfig`, `.gitignore_global`, GitHub CLI |
| **Editors** | Zed, Cursor, Neovim, Vim |
| **Terminal** | Powerlevel10k, tmux |
| **SSH** | Host config (structured + redacted) |
| **Package Managers** | npm, Bun, Homebrew (formulae + casks) |
| **Apps** | macOS `/Applications`, Raycast, AltTab, Ollama models |
| **Meta** | Hostname, OS, architecture, timestamp |
