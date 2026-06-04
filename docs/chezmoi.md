# Hybrid Model: this tool + chezmoi

The two tools have different centers of gravity and compose cleanly:

| | this tool | [chezmoi](https://chezmoi.io) |
|---|---|---|
| Role | **discover + audit** | **store + encrypt + apply** |
| Strength | knows *where* dev/AI configs live; classifies secrets; snapshots/diffs a machine | source-state repo, age encryption, per-machine templating, one-line bootstrap |
| Does not | store, encrypt, or apply | discover what to manage (you `add` it) |

`chezmoi-export` is the bridge: it discovers every managed config, decides plain vs `--encrypt`
(the [secret gate](./encryption.md#secret-gate)), and runs `chezmoi add` for you. chezmoi then owns
the private repo, encryption, templating, and `apply`. Nothing is reimplemented.

---

## Bootstrap a new machine

```bash
sh -c "$(curl -fsLS get.chezmoi.io)" -- init --apply git@github.com:<you>/dotfiles-private.git
```

Clones your **private** data repo and applies it (place your age `key.txt` first — see
[encryption](./encryption.md)). The data repo is separate from this tool's public code repo.

---

## Per-machine differences (work vs personal)

This is chezmoi's templating, not something this tool needs to model. One source file branches by
machine:

```
# dot_gitconfig.tmpl
[user]
  email = {{ if eq .chezmoi.hostname "work-mac" }}you@work.com{{ else }}you@personal.com{{ end }}
```

`collect` tags every snapshot with the hostname (`meta` section), so you can `compare` a work vs
personal `.json` to see exactly what differs and decide what to templatize.

---

## Packages on apply

Wire the captured `Brewfile` into chezmoi so `chezmoi apply` installs packages on a new machine —
a `run_onchange_` script re-runs whenever the Brewfile changes:

```bash
# run_onchange_darwin-install-packages.sh.tmpl  (in your chezmoi source)
{{- if eq .chezmoi.os "darwin" -}}
#!/bin/bash
brew bundle --file=/dev/stdin <<'EOF'
{{ include "Brewfile" }}
EOF
{{ end -}}
```

Generate the `Brewfile` from a snapshot's `apps.brew.bundle` section (`dotfiles list brew`), and the
same pattern works for `fnm install` of the captured node versions and global package lists.
