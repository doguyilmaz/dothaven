# Clean macOS Install — Migration Runbook

Move to a fresh machine without losing dev configs, secrets, or installed tooling —
while leaving the old machine's accumulated cruft behind. Four layers:

1. **Config** — chezmoi (storage + apply), fed by this tool's discovery.
2. **Secrets** — carried encrypted (age), never plaintext.
3. **Reinstall manifests** — Brewfile + global packages + toolchains, captured by `collect`.
4. **Manual checklist** — the handful of things no tool should automate.

> Do this **before** wiping the old machine. Keep the old machine until the new one is verified.

---

## Phase 0 — Prerequisites (old + new machine)

```bash
brew install chezmoi age      # backbone + encryption
# age key — generate ONCE, store the private key OUTSIDE any repo (password manager / offline backup)
age-keygen -o ~/key.txt       # prints the public recipient (age1...)
```

Configure chezmoi to use it (`~/.config/chezmoi/chezmoi.toml`):

```toml
encryption = "age"
[age]
  identity = "~/key.txt"
  recipient = "age1...your-public-key..."
```

> ⚠️ Lose `key.txt` → you cannot decrypt your secrets. Back it up to a password manager. Never commit it. See [encryption](./encryption.md).

---

## Phase 1 — Capture & audit (old machine, and the current work machine)

```bash
# Full inventory snapshot (packages, toolchains, fonts, brew, every ~/.* dotfile)
bunx dothaven collect

# Security report — what's risky before you sync anything
bunx dothaven security ~ -o ~/SECURITY.md
```

Review `SECURITY.md` and the snapshot's `home.dotfiles.review` section — it surfaces obscure
configs you'd otherwise forget (e.g. `.app-store`, `.netrc`, `.aws`). Decide what to carry.

Run this on **both** the old personal machine and the current work machine — keep both `.json`
files; `doctor` and `compare` use them later.

---

## Phase 2 — Export to chezmoi (config + encrypted secrets)

```bash
chezmoi init                         # creates the source repo at ~/.local/share/chezmoi
bunx dothaven chezmoi-export   # DRY RUN — review the plan (what's plain vs 🔒 encrypted)
bunx dothaven chezmoi-export --apply
```

`chezmoi-export` adds every managed config, choosing `--encrypt` for high-sensitivity entries
(`~/.ssh` keys, `~/.gnupg`, `~/.aws/credentials`, `~/.npmrc`, kube/docker config) **and** for any
file the scanner finds secrets in (the [secret gate](./encryption.md#secret-gate)). Then:

```bash
cd ~/.local/share/chezmoi
git init && git add . && git commit -m "initial"
gh repo create dotfiles-private --private --source=. --push   # PRIVATE — even encrypted secrets live here
```

> The data repo is **separate and private**. This tool's public repo holds only code.

---

## Phase 3 — Manifests (reinstall, not file-copy)

These are already in your `collect` snapshot — no extra step to capture. On the new machine they
drive reinstalls (see Phase 5):

- `apps.brew.bundle` → a restorable `Brewfile` (`brew bundle`)
- `packages.*` → npm / bun / pnpm globals; `packages.node.fnm` → node versions to `fnm install`
- `runtimes.*` → go / rust / swift / xcode / android (re-install toolchains; re-pull, don't copy)

Optionally wire the Brewfile into chezmoi as a `run_onchange_` script so `chezmoi apply` installs it — see [chezmoi](./chezmoi.md#packages-on-apply).

---

## Phase 4 — Manual checklist (don't automate these)

- [ ] **Apple Developer certs** → Keychain Access → export `.p12` (strong password) → into the encrypted bundle / chezmoi `--encrypt`.
- [ ] **Provisioning profiles** → `~/Library/MobileDevice/Provisioning Profiles/`.
- [ ] **`/etc/hosts`** custom entries (needs sudo to restore).
- [ ] **VPN profiles** (Tailscale / OpenVPN / Cisco).
- [ ] **Local DB dumps** (only if you run local DBs): `pg_dumpall`, `mysqldump` — treat as secrets, encrypt.
- [ ] **Ollama models** — re-pull from the captured `ai.ollama.models` list (don't copy the GBs).
- [ ] **Browser profiles** — use the browser's own sync / a password manager.
- [ ] **Uncommitted work** — across `~`, push/stash before wiping: `find ~ -name .git -maxdepth 4 -exec dirname {} \;` then check `git status` in each.

---

## Phase 5 — New machine bootstrap

```bash
# 1. chezmoi + your private data, applied in one line (place key.txt first)
sh -c "$(curl -fsLS get.chezmoi.io)" -- init --apply git@github.com:<you>/dotfiles-private.git

# 2. packages & toolchains
brew bundle --file=~/Brewfile        # from apps.brew.bundle
fnm install <each version>           # from packages.node.fnm
# re-install global npm/bun/pnpm packages + go/rust/etc. toolchains from the snapshot lists
```

---

## Phase 6 — Verify parity

```bash
bunx dothaven doctor old-machine.json
```

Lists anything the old machine had that's still missing here (packages, toolchains, fonts,
extensions). Exit code is non-zero until parity is reached. Only wipe the old machine once this is clean.
