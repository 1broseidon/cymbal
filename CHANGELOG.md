# Changelog

All notable changes to cymbal are documented here.

## [0.7.2] - 2026-03-26

### Added

- PowerShell install script for Windows (`install.ps1`) — `irm .../install.ps1 | iex` fetches the latest release, extracts to `%LOCALAPPDATA%\cymbal`, and adds to PATH.

### Fixed

- Database file created inside the project directory on Windows when `%USERPROFILE%` is unset. Now uses `os.UserCacheDir()` (`%LOCALAPPDATA%` on Windows) as the primary data directory, with safe fallbacks that never produce a relative path.

## [0.7.1] - 2026-03-26

### Added

- Windows (amd64) binary in release pipeline — builds with Cgo on `windows-latest`, packaged as `.zip`.

## [0.7.0] - 2026-03-25

### Added

- Flexible symbol resolution pipeline (`flexResolve`) — shared by `show`, `investigate`, and `context`:
  - **Ambiguity auto-resolve**: picks best match by path proximity and kind priority, notes alternatives in frontmatter (`matches: 2 (also: path:line)`)
  - **Dot-qualified names**: `config.Load` resolves by filtering parent/path. Works for `pkg.Function` and `Class.method` patterns.
  - **Fuzzy fallback**: exact → case-insensitive (`COLLATE NOCASE`) → FTS prefix match. Marks results with `fuzzy: true`.
- `SearchSymbolsCI` store method for case-insensitive exact name match.
- `InvestigateResolved` for investigating pre-resolved symbols.

### Changed

- `show` and `investigate` no longer error on ambiguous symbols — they auto-resolve and return the best match with disambiguation hints in frontmatter.

## [0.6.0] - 2026-03-25

### Added

- `cymbal trace <symbol>` — downward call graph traversal. Follows what a symbol calls, what those call, etc. (BFS, depth 3 default, max 5). Filters stdlib noise to surface project-defined symbols. Complementary to `impact` (upward) and `investigate` (adaptive).

### Changed

- Agent integration guide (README, CLAUDE.md, AGENTS.md) restructured around three core commands: `investigate` (understand), `trace` (downward), `impact` (upward). Based on real-world observation of an agent making 22 sequential calls that trace + investigate handled in 4.

## [0.5.1] - 2026-03-25

### Fixed

- `show` and `investigate` now accept `file:Symbol` syntax to disambiguate when multiple symbols share a name (e.g., `cymbal show config.go:Config`, `cymbal investigate internal/config/config.go:Config`).
- `show` line range parser accepts `L`-prefixed ranges (`file.go:L119-L132`) — was advertised in README but broken.

## [0.5.0] - 2026-03-25

### Added

- `cymbal investigate <symbol>` — kind-adaptive symbol exploration. Returns the right shape of information based on what a symbol is: functions get source + callers + shallow impact; types get source + members + references; ambiguous names get ranked candidates. Eliminates the agent's decision loop of choosing between search/show/refs/impact.
- `ChildSymbols` store method for querying methods/fields by parent type name.
- Benchmark suite now tracks output size (bytes + approximate tokens) and includes ripgrep refs/show equivalents for fair token efficiency comparison.

### Fixed

- TypeScript/JavaScript `export` statement dedup — exported functions, classes, interfaces, types, and enums no longer appear twice in the index (same pattern as the Python decorator fix in v0.4.1).

### Changed

- README rewritten with workflow-centric agent integration guide. `investigate` is the recommended default, specific commands are escape hatches.

## [0.4.1] - 2026-03-24

### Added

- Benchmark suite now tracks output size (bytes + approximate tokens) per query, comparing token efficiency across tools. Ripgrep refs and show equivalents added for fair comparison.

### Fixed

- Python decorated functions and classes no longer appear twice in outline/search/show. Tree-sitter's `decorated_definition` wrapper was causing double emission — inner `function_definition`/`class_definition` nodes are now skipped when their parent already emitted them.

## [0.4.0] - 2026-03-24

### Changed

- **Indexing 2x faster** — separated parse workers (parallel, CPU-bound) from serial writer with batched transactions, eliminating goroutine contention on SQLite's writer lock. Cold index dropped from 2.4s to 1.05s on cli/cli (729 files).
- **Reindex 4x faster** — mtime_ns + file size skip check with pre-loaded map avoids reading files or querying DB per-file. Reindex dropped from 57ms to 14ms on cli/cli.
- **Prepared statement reuse** — statements prepared once per batch (5 per batch vs 5 per file), reducing cgo overhead on large repos.
- **Read-once parse+hash** — workers read each file once and pass bytes to both parser and hasher, eliminating duplicate I/O.
- **Row-based batch flushing** — flush at 100 files OR 50k rows (symbols+imports+refs), preventing pathological batches from symbol-dense repos.
- **Robust change detection** — mtime stored as nanosecond integer + file size; skip only when both match exactly. Catches coarse FS timestamps and tools that preserve mtime.
- **Walker language filtering** — unsupported languages (.json, .md, .toml) filtered before stat, reducing channel traffic and allocations.

### Added

- Benchmark suite (`bench/`) comparing cymbal vs ripgrep vs ctags across Go, Python, and TypeScript repos with reproducible pinned corpus.
- Progress indicator on stderr after 10s for large repos (e.g., kubernetes at 16k files).
- `ParseBytes` function for parsing from pre-read byte slices.

### Fixed

- **Stale file pruning** — deleted/renamed files are removed from the index on reindex by diffing walker paths against stored paths.
- **Savepoint-per-file in batch writer** — a single file write failure no longer corrupts the entire batch; partial data is rolled back cleanly.
- **Accurate stats after commit** — indexed/found counts published only after successful tx.Commit(), preventing inflation on commit failure.
- **Split error types** — skip reasons separated into unchanged, unsupported, parse_error, write_error; CLI shows non-zero counts conditionally.

## [0.3.0] - 2026-03-24

### Changed

- **Per-repo databases** — each repo gets its own SQLite DB at `~/.cymbal/repos/<hash>/index.db`, eliminating cross-repo symbol bleed. Searching in repo A no longer returns results from repo B.
- Removed `repos` table and `repo_id` column — no longer needed since each DB is one repo
- Added `meta` table storing `repo_root` path per database
- `cymbal ls --repos` lists all indexed repos with file/symbol counts
- `--repo` flag removed (repo identity comes from DB path now)
- `--db` flag still works as override for all commands

### Added

- `refs` and `impact` now show surrounding call-site context (1 line above/below by default, adjustable with `-C`)
- VitePress docs site at chain.sh/cymbal with chain.sh design language

### Fixed

- Stale symbol entries from moved/deleted repos no longer pollute search results

## [0.2.0] - 2026-03-23

### Changed

- All commands now output agent-native frontmatter+content format by default (YAML metadata + content body, optimized for LLM token efficiency)
- `refs` and `impact` deduplicate identical call sites — grouped by file with site count
- `context` callers section uses the same dedup
- `search` results ranked by relevance: exact name match first, then prefix, then contains
- Default limits lowered: refs 50→20, impact 100→50, search 50→20
- `refs`, `impact`, and `context` now show actual source lines at call sites, not just line numbers

## [0.1.0] - 2026-03-23

### Added

- Core indexing engine with tree-sitter parsing, SQLite FTS5 storage, and AI summaries via oneagent
- Batched summarization with diff tracking and model selection
- `cymbal index` — index a codebase
- `cymbal ls` — list files and repo stats
- `cymbal outline` — show file structure
- `cymbal search` — symbol and text search
- `cymbal show` — display symbol source
- `cymbal refs` — find references to a symbol
- `cymbal importers` — reverse import lookup
- `cymbal impact` — transitive caller analysis
- `cymbal diff` — git diff scoped to a symbol
- `cymbal context` — bundled source, callers, and imports in one call

### Fixed

- Overlapping sub-repo detection prevents duplicate symbol indexing
