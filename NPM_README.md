# dothaven

> **Renamed from `@dotformat/cli`.** If you have the old package, switch with `npm i -g dothaven` and remove `@dotformat/cli`. The `.dotf` format was retired in favour of plain JSON; the CLI now has **zero runtime dependencies**.

Discover, back up, and migrate your machine's dev config across machines — with built-in secret scanning and a [chezmoi](https://chezmoi.io) export (age-encrypted).

## Usage

```bash
bunx dothaven collect        # snapshot your machine → JSON
bunx dothaven backup         # copy real config files (redacted)
bunx dothaven security ~     # write a Markdown secret-scan report
bunx dothaven chezmoi-export # feed chezmoi (encrypt secrets), with an install script
bunx dothaven doctor snap.json  # parity-check a new machine against a snapshot
```

Requires [Bun](https://bun.sh) ≥ 1.0. See the [full docs](https://doguyilmaz.github.io/dothaven) for every command.
