---
title: Installation
weight: 2
---

dothaven ships as a single static Go binary built with [Cobra](https://github.com/spf13/cobra). There is no interpreter, runtime, or package manager to install alongside it — pick one of the methods below and you have a working `dothaven` command.

## Install

{{< tabs >}}
  {{< tab name="Homebrew" >}}
Install from the tap. Homebrew resolves the right binary for your platform:

```bash
brew install doguyilmaz/tap/dothaven
```
  {{< /tab >}}
  {{< tab name="Go" >}}
Build and install the latest release straight from the module:

```bash
go install github.com/doguyilmaz/dothaven/cmd/dothaven@latest
```

This drops the `dothaven` binary into `$(go env GOPATH)/bin` — make sure that directory is on your `PATH`.
  {{< /tab >}}
  {{< tab name="Source" >}}
Clone the repo and build the `./cmd/dothaven` package:

```bash
git clone https://github.com/doguyilmaz/dothaven.git
cd dothaven
go build ./cmd/dothaven
```

The resulting `dothaven` binary lands in the current directory. Move it somewhere on your `PATH` (for example `/usr/local/bin`) if you want it available everywhere.
  {{< /tab >}}
{{< /tabs >}}

## Supported platforms

Release builds are static (`CGO_ENABLED=0`) and cross-compiled for:

| OS              | Architectures   |
| --------------- | --------------- |
| macOS (darwin)  | `amd64`, `arm64` |
| Linux           | `amd64`, `arm64` |

Building from source with the Go or Source method works on any platform the Go toolchain targets, but the packaged Homebrew/release artifacts cover the matrix above.

## Verify the install

Confirm the binary is on your `PATH` and prints a version:

```bash
dothaven --version
```

```text
dothaven version 1.0.0
```

A source or `go install` build that wasn't stamped at release time reports `dev`:

```text
dothaven version dev
```

The release version is embedded into the binary at build time via `-ldflags -X main.version`; source builds fall back to the default `dev` value.

## Runtime dependencies

Running `dothaven` itself needs nothing else — the binary is self-contained. Discovery, audit, and reporting commands (`collect`, `doctor`, `scan`, `security`, `status`, `diff`, `compare`, `list`, `backup`, `restore`) work out of the box.

Two commands reach into the storage/encryption layer and need additional tooling:

{{< callout type="info" >}}
`chezmoi-export --apply` and `init` use [chezmoi](https://www.chezmoi.io) for storage and [age](https://github.com/FiloSottile/age) for encryption. Install both and configure an age key before applying changes — dothaven handles discovery, audit, and export planning, while chezmoi performs storage, encryption, and apply.
{{< /callout >}}

{{< callout type="warning" >}}
age is the encryption backend. If you lose the age key, encrypted files become unrecoverable. Back up your key before exporting secrets.
{{< /callout >}}

See [Encryption](../encryption) for setting up an age key and the chezmoi integration.

## Where reports are written

Commands that emit JSON snapshots resolve the output directory in this order:

1. An explicit `-o` / `--output` flag wins.
2. Otherwise, `<cwd>/reports` when the current directory is inside a git repository.
3. Otherwise, `~/Downloads`.

```bash
dothaven collect -o ./snapshots
```

## Shell completion

Cobra provides a built-in `completion` command that generates completion scripts for bash, zsh, fish, and PowerShell. Print the script for your shell:

```bash
dothaven completion zsh
```

To load completions in the current session:

```bash
source <(dothaven completion zsh)
```

Run `dothaven completion --help` for per-shell instructions on installing the script permanently.

## Next steps

{{< cards >}}
  {{< card link="../quick-start" title="Quick start" >}}
  {{< card link="../commands" title="Commands" >}}
  {{< card link="../encryption" title="Encryption" >}}
{{< /cards >}}
