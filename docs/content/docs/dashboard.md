---
title: Dashboard
weight: 7
---

`dothaven ui` opens a dashboard in your browser that shows, on one page, how ready this machine is to move: what your backups cover, what they miss, and what is at risk. It is served from your own machine, it never changes anything, and it loads nothing from the internet.

```bash
dothaven ui              # also: dothaven dashboard
dothaven ui --no-open    # print the link, don't open a browser
```

```text
✓ Dashboard running at
  http://127.0.0.1:53817/?k=5f0c…
  Press Enter (or Ctrl-C) to stop.
```

The page refreshes its panels on demand (there is a refresh button) and has a light and dark theme. Stop the server with Enter or Ctrl-C in the terminal; closing the tab does not stop it.

## What it shows

| Panel | What it tells you |
| --- | --- |
| **Coverage by category** | How many files and bytes each category would put in a backup, and which categories hold credentials |
| **Not covered yet** | Paths in your home folder that look like config but are in no backup. Add them with `dothaven include` |
| **Backups found** | Every backup it can find (dothaven's folder, Downloads, Desktop, Documents, mounted drives), with kind, date and size |
| **Secrets in plain files** | Which tracked files hold secrets, of what kind and how many. Never the values |
| **Work that exists only here** | Git repositories under your home folder with uncommitted files, unpushed commits, stashes, no remote, or ignored secret files (the same check as `dothaven ready`) |
| **Installed software** | Counts per package manager, from your newest `collect` snapshot or backup |
| **Applied on this machine** | What `restore` has written and skipped here, and when |
| **GitHub** | Who you are signed in as, your backup repository, whether it is private, and which machines are in it |

Slow panels (the repository scan, the secret scan) are worked out in the background and reused for 20 seconds, so reloading the page does not repeat the work.

## Security model

The dashboard shows sensitive facts about your machine, so it is built to be safe to leave open on a laptop:

- **Loopback only.** It listens on `127.0.0.1`, on a random port. Other machines on your network cannot reach it.
- **A one-time key in the link.** Every run makes a new random key. The link dothaven prints carries it; your first visit swaps it for an `HttpOnly`, `SameSite=Strict` session cookie (a different secret) and redirects to a clean URL. After that the link opens nothing anywhere else, so a copy of it in your scrollback or browser history is useless. A request without the session gets `403`.
- **Host check.** Requests must be addressed to `127.0.0.1:<port>` or `localhost:<port>`. This blocks DNS rebinding, where a web page elsewhere points its own name at `127.0.0.1` to reach local servers.
- **Read-only.** It answers `GET` and `HEAD` only, and nothing it runs writes to your files.
- **No outside assets.** The page's scripts and styles are built into dothaven. A strict Content Security Policy allows nothing from other origins, framing is denied, and responses are not cached.
- **Secrets stay out.** The secrets panel shows file names, the kind of secret and a count, never the value.

The page itself loads nothing from the internet. The GitHub panel is filled in by dothaven on your machine calling the GitHub API, and only when you are signed in.

{{< cards >}}
  {{< card link="../quick-start" title="Quick start" >}}
  {{< card link="../security" title="Security" >}}
  {{< card link="../commands" title="Commands" >}}
{{< /cards >}}
