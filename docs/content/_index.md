---
title: dothaven
layout: hextra-home
---

{{< hextra/hero-badge >}}
  <div class="hx:w-2 hx:h-2 hx:rounded-full hx:bg-primary-400"></div>
  One static binary · macOS &amp; Linux · encryption built in
{{< /hextra/hero-badge >}}

<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Move your dev setup&nbsp;<br class="hx:sm:block hx:hidden" />to a new machine
{{< /hextra/hero-headline >}}
</div>

<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  Dotfiles, AI tool setup, SSH keys, cloud logins, installed apps and macOS settings,&nbsp;<br class="hx:sm:block hx:hidden" />packed into one encrypted file or a private GitHub repo and restored selectively on the next machine.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx:mb-6">
{{< hextra/hero-button text="Get started" link="docs/" >}}
</div>

<div class="hx:mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="One complete, encrypted backup"
    subtitle="`dothaven backup --encrypt` writes a single age file with your config, keys and tokens, the list of apps you had, and your macOS settings. It is never written unencrypted, not even as a temporary file."
  >}}
  {{< hextra/feature-card
    title="AI tool config included"
    subtitle="Claude Code, Codex, Gemini CLI, Cursor, Windsurf, VS Code, opencode, Copilot CLI and more: skills, agents, commands, hooks, plugins and MCP servers."
  >}}
  {{< hextra/feature-card
    title="Restore what you choose"
    subtitle="Everything, some categories or single files. You see a diff before anything is replaced, the old copy is kept, and a rerun shows what is already applied."
  >}}
  {{< hextra/feature-card
    title="Or keep it on GitHub"
    subtitle="`dothaven github push` keeps each machine in a private repo, encrypted by default. `dothaven restore github` brings it back anywhere. No git needed on either side."
  >}}
  {{< hextra/feature-card
    title="A local dashboard"
    subtitle="`dothaven ui` opens a local, read-only page: what your backups cover, what they miss, secrets in plain files, and repos with unpushed work."
  >}}
  {{< hextra/feature-card
    title="Checks before you wipe"
    subtitle="`ready` finds unpushed work and gitignored `.env` files. `missing` lists what the new machine still lacks. `doctor` checks that dothaven itself can do its job."
  >}}
{{< /hextra/feature-grid >}}
