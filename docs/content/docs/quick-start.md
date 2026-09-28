---
title: Quick start
weight: 3
---

This takes about five minutes. You will make a backup, see what it covers, look for secrets in your config, and open the dashboard. Nothing here changes your files.

If you are about to move to a new machine, go straight to [Moving to a new machine](../migration).

## 1. Install and check

```bash
curl -fsSL https://raw.githubusercontent.com/doguyilmaz/dothaven/main/scripts/install.sh | sh
dothaven doctor
```

Other ways to install are on the [Installation](../installation) page. `doctor` ends with one line telling you whether everything dothaven needs is in place:

```text
✓ Everything dothaven needs is in place.
```

## 2. Or open the menu

```bash
dothaven
```

With no arguments on a terminal, dothaven opens a menu grouped by what you want to do: moving to a new machine, your private GitHub repo, everyday tasks, and the optional chezmoi sync. Every entry says whether it only looks or also writes. See [Interactive mode](../interactive).

The rest of this page uses commands, so you can see what each step does.

## 3. Make a quick backup

```bash
dothaven backup
```

On a terminal it first asks which categories to include (everything is selected), then offers any config-looking paths it does not know about yet. Pass `--only` or `--skip` to skip the questions.

The result is a folder in `~/.local/share/dothaven`:

```text
✓ Backup saved: 9 files, 400 B
  /Users/you/.local/share/dothaven/backup-mymac-20260927232931
  ai (4), git (2), npm (1), shell (1), ssh (1)
  + installed apps & packages list

⚠ 2 paths with credentials left out of this plaintext backup:
    cloud/aws/credentials
    ssh
  For a complete copy, keys included: dothaven backup --encrypt

⚠ Sensitivity report:
  HIGH   ai/claude/claude.json          GitHub token (redacted)
  HIGH   npm/.npmrc                     npm auth token (redacted)
  MEDIUM ssh/config                     IP address (redacted)

  3 items redacted. Use --no-redact to include all.
  Redacted files are kept for reference but not restored over your real ones.
```

This plain backup is a safe local copy: secret values are masked, and SSH keys and cloud logins are left out and listed. Use it to see what changed since last week. To move to a new machine, use the encrypted one: `dothaven backup --encrypt`. The [Backup & restore](../backup-restore) page explains the difference.

## 4. See what changed since

```bash
dothaven status   # one-screen summary
dothaven diff     # file by file
```

```text
Last backup: 2h ago (backup-mymac-20260927232931)
  9 files tracked: 1 modified, 5 unchanged
  3 redacted

Modified since backup:
  ~ shell/.zshrc
```

## 5. Find config nothing covers yet

dothaven knows a few hundred config locations. Everything else that looks like config is listed here:

```bash
dothaven include --list
```

```text
Not covered by anything (3), so not in your backups:
  ? ~/.mytoolrc
  ? ~/.config/raycast
  ? ~/bin
```

Add what you want carried in every backup:

```bash
dothaven include ~/.config/raycast ~/bin
```

## 6. Look for secrets in your config

```bash
dothaven scan
```

With no path, `scan` checks every file a backup would carry. It shows where each secret is and what kind it is, never the value itself:

```text
~/.aws/credentials
  L2 [HIGH] AWS access key: AKIA••••LE

~/.ssh/id_ed25519
  L1 [HIGH] private key: -----BEGIN OPENSSH PRIVATE KEY-----
```

It exits with code 2 when it finds anything HIGH, so you can use it in a commit hook. Pass a path to scan any file or folder: `dothaven scan ~/projects/api`.

## 7. Open the dashboard

```bash
dothaven ui
```

This opens a page in your browser, served from your own machine only: coverage by category, what nothing covers, the backups it can find, secrets in plain files, repositories with unpushed work, installed software, and your GitHub backup. It never writes anything. Press Enter in the terminal to stop it. See [Dashboard](../dashboard).

## Where to go next

{{< cards >}}
  {{< card link="../migration" title="Moving to a new machine" subtitle="Ready, pack, restore, reinstall, check." >}}
  {{< card link="../github" title="GitHub sync" subtitle="Keep every machine in a private repo." >}}
  {{< card link="../commands" title="Commands" subtitle="Every command and flag." >}}
{{< /cards >}}
