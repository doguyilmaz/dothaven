---
title: dothaven
layout: hextra-home
---

{{< hextra/hero-badge >}}
  <div class="hx:w-2 hx:h-2 hx:rounded-full hx:bg-primary-400"></div>
  Single static binary · macOS &amp; Linux
{{< /hextra/hero-badge >}}

<div class="hx:mt-6 hx:mb-6">
{{< hextra/hero-headline >}}
  Move your dev machine,&nbsp;<br class="hx:sm:block hx:hidden" />without losing a thing
{{< /hextra/hero-headline >}}
</div>

<div class="hx:mb-12">
{{< hextra/hero-subtitle >}}
  dothaven inventories your machine's config, scans it for secrets,&nbsp;<br class="hx:sm:block hx:hidden" />and feeds chezmoi age-encrypted backups for clean-install migration.
{{< /hextra/hero-subtitle >}}
</div>

<div class="hx:mb-6">
{{< hextra/hero-button text="Get started" link="docs/" >}}
</div>

<div class="hx:mt-6"></div>

{{< hextra/feature-grid >}}
  {{< hextra/feature-card
    title="Inventory everything"
    subtitle="Shell, git, editors, SSH, cloud CLIs, Homebrew, global packages, runtimes, fonts, AI tooling — captured into one JSON snapshot."
  >}}
  {{< hextra/feature-card
    title="Secrets never leak"
    subtitle="A pattern scanner redacts tokens and keys, and refuses to write a private key into a plaintext backup."
  >}}
  {{< hextra/feature-card
    title="Encrypted migration"
    subtitle="Hands off to chezmoi with age — high-sensitivity files are added with --encrypt, plain configs stay plain."
  >}}
  {{< hextra/feature-card
    title="Reinstall on apply"
    subtitle="Generates a run_onchange script so a fresh machine reinstalls Homebrew, node, and global packages automatically."
  >}}
  {{< hextra/feature-card
    title="Verify parity"
    subtitle="doctor diffs a snapshot against a new machine and lists exactly what's still missing — with a CI-friendly exit code."
  >}}
  {{< hextra/feature-card
    title="No runtime to install"
    subtitle="One signed, notarized Go binary via Homebrew. No Node, no Bun, no interpreter — it just runs."
  >}}
{{< /hextra/feature-grid >}}
