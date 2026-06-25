---
title: Architecture
weight: 13
---

dothaven is a single static Go binary built on [Cobra](https://github.com/spf13/cobra). There is no runtime dependency — no interpreter, no Node, no package manager to ship alongside it. The module path is `github.com/doguyilmaz/dothaven` and the binary is built from `./cmd/dothaven`.

This page is the contributor's mental map: how the packages divide responsibility, the `sys.Env` seam that makes side effects testable, the concurrency model in the collector, the testing approach, and how the CLI composition root wires everything together.

## The hybrid model

dothaven owns one half of a two-tool workflow and deliberately stops at the boundary:

- **dothaven** does **discovery + audit + export** — it inventories the machine, scans for secrets, and plans what to hand off.
- **[chezmoi](https://www.chezmoi.io/)** does **storage + age-encryption + apply** — it persists the config and applies it on the target machine.

[age](https://age-encryption.org/) is the encryption backend, driven by chezmoi.

{{< callout type="warning" >}}
Encryption lives entirely in chezmoi/age — dothaven never holds the key. Losing the age key means the encrypted files are unrecoverable.
{{< /callout >}}

This split shapes the architecture: most commands are pure planners. They classify, diff, or render, and leave filesystem mutation to a small set of `Execute`-style functions or to chezmoi itself.

## Package layout

```text
cmd/dothaven/        main + testscript e2e harness
internal/
  sys/               injectable seam for all side effects
  snapshot/          the inventory model: JSON, compare, diff render
  registry/          declarative table of known config sources
  collect/           concurrent machine collectors
  scan/              secret detection + redaction
  backup/            plaintext backup tree (redaction-gated)
  restore/           plan a restore against the live machine, then apply
  chezmoi/           plan the hybrid export + install script
  cli/               Cobra composition root + per-command wiring
  tui/               interactive terminal prompts (charmbracelet/huh)
```

Everything under `internal/` is unexported API — the only public surface is the CLI itself. `main` is intentionally tiny: it builds the root command, runs it, and maps a typed error to an exit code.

### Each package's responsibility

| Package | Responsibility |
| --- | --- |
| `sys` | The single seam for side effects: run commands, read files/dirs, env vars, home dir, path resolution. Real and fake implementations live here. |
| `snapshot` | The inventory data model (`Snapshot` → `Section`), its JSON serialize/parse, and the structural `Compare` + diff renderer. |
| `registry` | A declarative table (`Entries`) of every config source dothaven knows — path per OS, kind, sensitivity, redact rule, backup destination — plus the reader that turns them into snapshot sections. |
| `collect` | Active inventory: collectors that shell out (`go version`, `brew bundle`, etc.) and parse output into sections, run concurrently and failure-isolated. |
| `scan` | Pattern-based secret detection over text, with a severity/action model and content redaction. |
| `backup` | Copies tracked files into a timestamped tree, applying the same redaction/skip gate as collect so a plaintext backup never carries a raw secret. |
| `restore` | Builds a plan from a backup directory by classifying each file against the live machine (`new`/`conflict`/`same`/`redacted`), then applies it. |
| `chezmoi` | Plans the hybrid export — which sources go plain vs. `--encrypt` vs. `--template` (rewriting absolute home paths) — and generates a `run_onchange_` install script. |
| `cli` | The composition root: builds the Cobra tree, wires each subcommand with its `sys.OS`, and holds shared rendering helpers. |
| `tui` | Interactive terminal flows (charmbracelet/huh): the menu launcher, category pickers, and conflict prompts — only invoked when stdin/stdout are a TTY. |

## The `sys.Env` seam

Every side effect funnels through one interface in `internal/sys/env.go`. Collectors and commands accept an `Env` rather than calling `os`/`exec` directly, which is what makes the whole inventory pipeline testable without touching the real machine.

```go
type Env interface {
	Run(ctx context.Context, args ...string) (string, error)
	ReadFile(path string) ([]byte, error)
	ListDir(path string) ([]string, error)
	Exists(path string) bool
	Getenv(key string) string
	Home() string
}
```

`Run` has one deliberate quirk: a non-zero exit is **tolerated** (it returns whatever made it to stdout with a `nil` error), because tools like `npm ls` exit non-zero on benign warnings. Only a spawn failure — command not found, context cancelled — is surfaced as an error. Collectors lean on this so a missing tool simply yields nothing instead of aborting the run.

There are two implementations:

- **`sys.OS`** (`sys.Real()`) — the real, filesystem-backed env. It also carries the cross-cutting path helpers: `ResolveOutputDir`, the `Timestamp` formatter, and the shared `WriteFile` mkdir-p primitive used by backup and restore.
- **`sys.Fake`** (`internal/sys/fake.go`) — an in-memory env for tests. Files, directories, command outputs, and env vars are served from plain maps; `Run` looks up its stdout by the space-joined command string.

```go
f := &sys.Fake{
	Cmds:  map[string]string{"go version": "go version go1.26.3 darwin/arm64"},
	Files: map[string]string{"/home/u/.zshrc": "export EDITOR=vim"},
	Dirs:  map[string][]string{"/home/u/.ssh": {"config", "id_ed25519"}},
	Vars:  map[string]string{"ANDROID_HOME": "/opt/android"},
	HomeDir: "/home/u",
}
```

With a `Fake`, a collector's exact behavior — including parsing — is asserted against fixed inputs with no machine state involved.

### Output directory resolution

`ResolveOutputDir` is the single place that decides where reports and backups land:

1. An explicit `-o`/`--output` path always wins.
2. Otherwise, if the current directory is a git repo (it has a `.git/HEAD`), use `<cwd>/reports`.
3. Otherwise fall back to `~/Downloads`.

## The collector concurrency model

`internal/collect/collect.go` defines the contract and the runner. A collector is just a function:

```go
type Collector func(c Ctx) snapshot.Snapshot
```

It receives a shared `Ctx` (the `context.Context`, the `sys.Env`, the home dir, and a `Redact` flag) and returns whatever sections it could gather. The rule baked into the contract: a collector **must not abort the run on failure** — it returns what it has (an empty map is fine), and panics are recovered.

`RunCollectors` is a goroutine fan-out with two safety properties:

```go
func RunCollectors(c Ctx, collectors []Collector) snapshot.Snapshot {
	results := make([]snapshot.Snapshot, len(collectors))
	var wg sync.WaitGroup
	for i, col := range collectors {
		wg.Add(1)
		go func(i int, col Collector) {
			defer wg.Done()
			defer func() { _ = recover() }()
			results[i] = col(c)
		}(i, col)
	}
	wg.Wait()

	merged := snapshot.Snapshot{}
	for _, r := range results {
		for k, v := range r {
			merged[k] = v
		}
	}
	return merged
}
```

- **Fan-out:** every collector runs in its own goroutine; a `sync.WaitGroup` joins them. Each writes to its own pre-allocated index in `results`, so the goroutines never share a mutable destination and there is no lock on the hot path.
- **Failure isolation:** a `recover()` in each goroutine means a panic (or empty return) from one collector can never affect the others — at worst, that collector contributes nothing.

Merging happens **after** the join, single-threaded, in collector order — so a later collector wins a key conflict. That ordering is meaningful: in the canonical pipeline `MetaCollector` runs first (it labels the snapshot with host/OS), the declarative `registry` adapter runs next, then the command-backed collectors (ssh, ollama, apps, homebrew, packages, runtimes, editor extensions, fonts, dotfiles sweep).

A representative collector (`RuntimesCollector`) shows the shape: call `Env.Run`, hand the output to a pure parser, and only emit a section when the parse yields something:

```go
if outStr, _ := c.Env.Run(c.Context, "go", "version"); true {
	if g := ParseGoVersion(outStr); g != nil {
		out["runtimes.go"] = snapshot.Section{Pairs: map[string]string{
			"version": g.Version, "platform": g.Platform,
		}}
	}
}
```

## Testing approach

The codebase separates **pure logic** from **side effects**, and tests each accordingly.

### Pure parsers + table tests

Output parsing is extracted into standalone, side-effect-free functions — `ParseGoVersion`, `ParseRustupToolchains`, `ParseCargoCrates`, and so on. They take a string and return a struct, which makes them ideal for table-driven tests: a list of `{input, want}` cases covering real-tool output and its edge cases (empty output, source-installed crates, missing build numbers). The same pattern holds across packages — `scan.ScanContent`, `restore.classify`, `snapshot.Compare`, and the `chezmoi` planners are all pure and unit-tested in isolation.

The planning/applying split is explicit in `restore`: `BuildPlan`, `matchTarget`, `classify`, `Filter`, and `Tally` are pure; only `Execute` mutates the filesystem. You can assert the entire plan against a `Fake`-style fixture before any write happens.

### Golden / deterministic rendering

The serializers and renderers are written to be byte-stable so their output can be compared against golden expectations: `Snapshot.Serialize` disables HTML escaping and relies on `encoding/json`'s alphabetical key order, and `SnapshotDiff.Format` emits sections and pair keys in sorted order. Stable output keeps both golden tests and real git diffs clean.

### testscript end-to-end

`cmd/dothaven/main_test.go` wires the [testscript](https://pkg.go.dev/github.com/rogpeppe/go-internal/testscript) harness. `TestMain` lets the test binary re-exec itself as the real `dothaven` command, and `TestScripts` runs every `.txtar` under `testdata/script/` in a hermetic temp dir with `HOME` isolated:

```go
func TestMain(m *testing.M) {
	testscript.Main(m, map[string]func(){
		"dothaven": main,
		"chezmoi":  fakeChezmoi, // plus fake `brew` and `defaults`
		// …
	})
}
```

Each `.txtar` is a self-contained scenario — commands to run, expected stdout/stderr, and inline files — driving the real binary. For example, `scan.txtar` asserts that a secret is flagged, a clean file reports clean, and a missing path is an error:

```text
exec dothaven scan secret.txt
stdout '\[HIGH\] GitHub token'
stdout 'Sensitivity report'

exec dothaven scan clean.txt
stdout 'No sensitive data found'

! exec dothaven scan nope.txt
stderr 'path not found'

-- secret.txt --
token=ghp_aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa
-- clean.txt --
just some plain text with nothing sensitive
```

The e2e scripts cover the filesystem/render commands (scan, security, compare, list, status, backup, restore, diff, chezmoi-export, help/version) directly. For commands that shell out — `chezmoi-export --apply`, `migrate`, `defaults`, `services` — the harness registers **fake `chezmoi`, `brew`, and `defaults` binaries** (the same self-re-exec trick used for `dothaven`), so those paths are driven end-to-end on any OS without depending on a real toolchain. The suite stays hermetic.

## The CLI composition root

`internal/cli/root.go` is where the binary is assembled. `NewRoot` takes the real `*sys.OS` and the version string, builds the Cobra root, and adds every subcommand:

```go
func NewRoot(env *sys.OS, version string) *cobra.Command {
	root := &cobra.Command{ Use: "dothaven", /* ... */ Version: version }
	root.AddCommand(
		newCollectCmd(env), newDoctorCmd(env), newBackupCmd(env),
		newRestoreCmd(env), newStatusCmd(env), newDiffCmd(env),
		newChezmoiExportCmd(env), newInitCmd(env), newScanCmd(env),
		newSecurityCmd(env), newCompareCmd(env), newListCmd(env),
	)
	return root
}
```

That is the dependency-injection point: the `*sys.OS` is created once in `main` (`sys.Real()`) and threaded into each `newXxxCmd` constructor, which closes over it inside the command's `RunE`. Tests build their own commands with a different `Env`, so nothing reaches for global state.

`main` stays minimal — it builds the root, executes, and translates a typed `cli.ExitError` into a process exit code:

```go
func main() {
	root := cli.NewRoot(sys.Real(), version)
	if err := root.Execute(); err != nil {
		var ee cli.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.Code)
		}
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
```

`ExitError` carries a desired exit code with no printed message. This is how a CI-style outcome is expressed: when `doctor` finds drift (installable items in the snapshot that are missing on the machine), it prints the report to stdout and returns `ExitError{Code: 1}`. A drift or parity failure is a normal CI signal, not an error to dump — so `main` maps it straight to `os.Exit` without the `error:` prefix. The `version` variable is `"dev"` by default and overridden at release time via `-ldflags`.

### Where commands compose the packages

`internal/cli/collect.go` shows the wiring pattern end to end. `defaultCollectors()` assembles the canonical pipeline (and adapts the declarative registry into a `Collector` that reads `Redact` from the `Ctx`, so the same list serves redacting `collect` and raw `doctor`). The command then runs the pipeline through `collect.RunCollectors`, optionally redacts each section via `scan`, serializes through `snapshot`, and writes to the resolved output directory:

```go
snap := gatherSnapshot(env, redact)   // collect → snapshot
// ... scan.RedactSection per section ...
data, _ := snap.Serialize()           // snapshot → JSON
dir := env.ResolveOutputDir(output)   // sys path policy
```

Each command is a thin orchestrator over the focused packages; the packages themselves stay free of CLI concerns.

## Related pages

{{< cards >}}
  {{< card link="../commands" title="Commands" subtitle="Every command, flag, and example" >}}
  {{< card link="../collectors" title="Collectors" subtitle="What each collector gathers" >}}
  {{< card link="../registry" title="Registry" subtitle="The declarative config-source table" >}}
  {{< card link="../snapshot-format" title="Snapshot format" subtitle="The JSON inventory model" >}}
{{< /cards >}}
