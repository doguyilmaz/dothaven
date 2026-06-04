# Execution Flows

Runtime behavior of each major command, visualized with Mermaid diagrams.

## `collect` Flow

```mermaid
flowchart TD
  A[Parse args: --no-redact, --slim, -o] --> B[Resolve output directory]
  B --> C[Create output dir via mkdir -p]
  C --> D[Build CollectorContext]
  D --> E["Run all 6 collectors via Promise.allSettled"]

  E --> F[collectMeta]
  E --> G[registryCollector — 23 entries]
  E --> H[collectSsh]
  E --> I[collectOllama]
  E --> J[collectApps]
  E --> K[collectHomebrew]

  F & G & H & I & J & K --> L[Merge fulfilled results into sections map]

  L --> M{Redaction enabled?}
  M -->|yes| N[For each content section: scanContent]
  N --> O{Action?}
  O -->|skip| P[Delete section from map]
  O -->|redact| Q[applyRedactions — replace with REDACTED]
  O -->|include| R[Keep as-is]
  M -->|no| R

  P & Q & R --> S{--slim enabled?}
  S -->|yes| T[Truncate content sections to 10 lines]
  S -->|no| U[Keep full content]
  T & U --> V["serializeSnapshot(snapshot) via JSON.stringify"]
  V --> W["Write hostname-YYYYMMDDHHMMSS.json"]
  W --> X[Print file path]
  X --> Y{Has findings?}
  Y -->|yes| Z[Print sensitivity report]
  Y -->|no| AA[Done]
  Z --> AA
```

## `backup` Flow

```mermaid
flowchart TD
  A[Parse args: --no-redact, --archive, --only, --skip, -o] --> B[Resolve output directory]
  B --> C["Create backup-hostname-timestamp dir"]
  C --> D[Filter backup sources by --only/--skip]
  D --> E[Iterate filtered sources]

  E --> F{Entry type?}
  F -->|file| G["Bun.file(src) — check exists"]
  G -->|not found| H[Skip]
  G -->|exists| I[Read content as text]
  I --> J[scanContent — determine action]
  J --> K{Redact enabled & action=skip?}
  K -->|yes| H
  K -->|no| L{Has custom redact function?}
  L -->|yes| M[Apply entry.redact]
  L -->|no| N[Apply pattern applyRedactions]
  M --> N
  N --> O[Write to backup dest]

  F -->|dir| P["Bun.Glob('**/*').scan with dot:true"]
  P --> Q[Copy each file to backup dest]

  O & Q --> R[Count category files]
  H --> R
  R --> S{More entries?}
  S -->|yes| E
  S -->|no| T{Total files > 0?}
  T -->|no| U["Print 'No files found to backup.'"]
  T -->|yes| V{--archive?}
  V -->|yes| W["tar czf → .tar.gz"]
  W --> X[rm -rf backup dir]
  V -->|no| Y[Keep backup directory]
  X & Y --> Z[Print summary — file count per category]
  Z --> AA{Redaction enabled?}
  AA -->|yes| AB[Print sensitivity report]
  AA -->|no| AC[Done]
  AB --> AC
```

## `restore` Flow

```mermaid
flowchart TD
  A[Parse args: backup-path, --pick, --dry-run] --> B{Backup path provided?}
  B -->|no| C[Print usage]
  B -->|yes| D["buildRestorePlan(backupDir, home)"]

  D --> D1[buildRestoreMap from backupSources]
  D1 --> D2["Glob scan backup dir (dot:true)"]
  D2 --> D3[For each file: match to map]
  D3 --> D4{Match type?}
  D4 -->|direct file match| D5[Use mapping path + category]
  D4 -->|dir prefix match| D6[Build path from dir base + relative suffix]
  D4 -->|.local suffix| D7[Map to base entry path + .local]
  D4 -->|no match| D8[Skip file]
  D5 & D6 & D7 --> D9["resolveFileStatus via Bun.hash()"]
  D9 --> D10[Add RestoreEntry to plan]

  D10 --> E{Plan empty?}
  E -->|yes| F["Print 'No restorable files found'"]
  E -->|no| G{--pick?}
  G -->|yes| H[Show category picker with counts]
  H --> I{Selection empty?}
  I -->|yes| J["Print 'No categories selected'"]
  I -->|no| K[Filter plan to selected categories]
  G -->|no| K

  K --> L{--dry-run?}
  L -->|yes| M[Print plan with status labels]
  L -->|no| N[createSnapshot for conflicts]
  N --> O{Snapshot created?}
  O -->|yes| P[Print snapshot path]
  O -->|no| Q[Continue]

  P & Q --> R[Iterate plan entries]
  R --> S{Status?}
  S -->|same| T[Skip silently]
  S -->|redacted| U[Skip with message]
  S -->|new| V[Write file]
  S -->|conflict| W{skipAll active?}
  W -->|yes| T
  W -->|no| X{overwriteAll active?}
  X -->|yes| V
  X -->|no| Y[Prompt: o/s/d/a/l]
  Y --> Z{Response?}
  Z -->|overwrite| V
  Z -->|skip| T
  Z -->|diff| Y
  Z -->|overwrite-all| V
  Z -->|skip-all| T

  V & T & U --> AA{More entries?}
  AA -->|yes| R
  AA -->|no| AB[Print restore summary]
```

## `diff` Flow

```mermaid
flowchart TD
  A[Parse args: path, --section] --> B{Backup path provided?}
  B -->|yes| C[Use provided path]
  B -->|no| D[findLatestBackup]
  D --> E{Found?}
  E -->|no| F["Print 'No backup found'"]
  E -->|yes| C

  C --> G["buildRestorePlan(backupDir, home)"]
  G --> H{--section filter?}
  H -->|yes| I[Filter entries by category]
  I --> J{Any entries?}
  J -->|no| K["Print 'No entries found for section'"]
  J -->|yes| L[Group by category]
  H -->|no| L

  L --> M[For each category group]
  M --> N[Print category header]
  N --> O[For each entry: print with color + status label]
  O --> P[Print summary line — counts by status]
```

## `status` Flow

```mermaid
flowchart TD
  A[findLatestBackup] --> B{Found?}
  B -->|no| C["Print 'No backup found'"]
  B -->|yes| D[getBackupAge — mtime to human string]
  D --> E["buildRestorePlan(backupDir, home)"]
  E --> F[Count statuses: modified, unchanged, new, redacted]
  F --> G[Print backup age + directory name]
  G --> H[Print file counts]
  H --> I{New files?}
  I -->|yes| J[Print new count]
  I -->|no| K{Redacted files?}
  J --> K
  K -->|yes| L[Print redacted count]
  K -->|no| M{Modified > 0?}
  L --> M
  M -->|yes| N[List modified file paths]
  M -->|no| O["Print 'Everything up to date.'"]
```

## `scan` Flow

```mermaid
flowchart TD
  A["Resolve target path (default: '.')"] --> B{Is file?}
  B -->|yes| C[scanFile — single file]
  B -->|no| D["Glob scan directory recursively"]
  D --> E[Skip node_modules, .git]
  E --> F[Skip files > 1 MiB]
  F --> G[scanFile each remaining file]

  C & G --> H{All clean?}
  H -->|yes| I["Print 'No sensitive data found.'"]
  H -->|no| J[Print detailed findings per file]
  J --> K[Sort findings by severity]
  K --> L["Print: L{n} [SEVERITY] label: match"]
  L --> M[Print summary report]
```

## `compare` Flow

```mermaid
flowchart TD
  A{Two file args provided?} -->|yes| B[Use provided paths]
  A -->|no| C["Glob scan <cwd>/reports/*.json"]
  C --> D[Sort by mtime descending]
  D --> E{At least 2 files?}
  E -->|no| F[Print error + usage]
  E -->|yes| G[Use two newest files]

  B & G --> H["Read both files in parallel"]
  H --> I["parseSnapshot() both via JSON.parse"]
  I --> J["compareSnapshots(left, right) → diff"]
  J --> K["formatDiff(diff, { labels, color }) — in-tree"]
  K --> L{Output empty?}
  L -->|yes| M["Print 'No differences found.'"]
  L -->|no| N[Print formatted diff]
```

## `list` Flow

```mermaid
flowchart TD
  A{Section arg provided?} -->|no| B[Print usage]
  A -->|yes| C["Glob scan <cwd>/reports/*.json"]
  C --> D[Sort by mtime descending]
  D --> E{Any reports?}
  E -->|no| F["Print 'No reports found'"]
  E -->|yes| G[Read newest report]
  G --> H["parseSnapshot() → Snapshot"]
  H --> I[Filter sections by fuzzy match]
  I --> J{Any matches?}
  J -->|no| K[Print available sections]
  J -->|yes| L["Print each matching section: pairs, items, content"]
  L --> M[Print output]
```
