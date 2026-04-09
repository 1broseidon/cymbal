# Changelog

All notable changes to cymbal are documented here.

## [0.9.0] - 2026-04-09

### Changed

- **Library-ready package layout** — moved all four `internal/` packages to top-level importable packages: `symbols/`, `parser/`, `index/`, `walker/`. External Go projects can now import cymbal as a library (e.g., `import "github.com/1broseidon/cymbal/index"`). The CLI (`cmd/`) continues to work unchanged. This is a **breaking change** for any code that imported `internal/` paths directly (which was not possible for external consumers, but affects forks).

### Added

- **MIT license file**.
- **README badges** — GitHub stars, Go Reference, Go Report Card, latest release.

## [0.8.8] - 2026-04-08

### Added

- **Multi-language benchmark corpus and regression detection** for the bench harness.

### Removed

- **Deprecated unused LLM summarization feature** — removed `--summarize`, `--backend`, and `--model` flags from `cymbal index`. The feature was underdeveloped (summaries only surfaced in `outline`, not in `search`, `investigate`, or other commands) and added significant indexing latency for minimal value. Removed upstream dependency on `oneagent`.

## [0.8.7] - 2026-04-07

### Added

- **Salesforce Apex language support** — classes, methods, fields, constructors, interfaces, and enums via `classifyJavaLike` reuse (PR #6, @lynxbat).
- **Dart language support** — classes, enums, mixins, extensions, type aliases, functions, methods, getters, setters, constructors, imports, and refs (PR #11, @Phototonic).

### Fixed

- **investigate member bleed across files** — `investigate` no longer mixes members from different symbols that share the same parent name across files or languages. Member lookup is now scoped to the resolved symbol's file. Fixes #9.

## [0.8.6] - 2026-04-06

### Added

- **Elixir language support** — modules (`defmodule`), functions (`def`/`defp`), macros (`defmacro`), protocols (`defprotocol`), imports (`alias`/`import`/`use`/`require`), and cross-module refs.
- **HCL/Terraform language support** — resources, variables, outputs, data sources, modules, providers, and locals blocks with synthesized names (e.g., `aws_instance.web`).
- **Protobuf language support** — messages, enums, services, RPCs, and proto imports.
- **Kotlin language support** — proper symbol extraction for classes, interfaces, objects, enums, methods, and companion objects (merged PR #7).
- **CI workflows** — build, lint, test, security (`govulncheck`), and dependency review checks on PRs and main branch.
- **PR template** — required structure with summary, testing checklist, security notes, and rollout risk.
- **PR body validation** — CI check that enforces template usage and completed checklist items on non-draft PRs.

### Changed

- **Go version** — bumped from 1.25.7 to 1.25.8 to resolve stdlib vulnerability flagged by new security check.
- **Makefile** — added `build-check`, `vulncheck`, and `ci` targets.

## [0.8.5] - 2026-04-05

### Changed

- **Smart truncation for type symbols** — `show` and `investigate` now cap class/struct/interface/enum output at 60 lines instead of dumping the entire body (e.g., FastAPI class went from 170KB to 1.8KB). Full source remains available via `cymbal show file:L1-L2`. Members are listed separately in `investigate`.
- **Truncated member signatures** — multi-line signatures in `investigate` member listings are collapsed to the first line, preventing huge docstring-heavy parameter lists from bloating output.

### Fixed

- **README accuracy** — removed unsupported languages (HCL, Dockerfile, TOML, HTML, CSS) from supported list, corrected benchmark numbers to match actual RESULTS.md, fixed "Go, Python, and TypeScript" to "Go and Python" (no TS corpus repo).
- **Benchmark token efficiency** — `show` and `investigate` for large types now dramatically outperform ripgrep instead of losing badly (FastAPI show: -1413% → 84% savings; APIRouter agent workflow: -248% → 95% savings).

## [0.8.4] - 2026-04-04

### Added

- **Auto-index on first query** — no more manual `cymbal index .` step. The first command in a repo automatically builds the index, with a progress indicator for large repos. Subsequent queries continue to refresh incrementally. Closes #3. (@Ismael)
- **Git worktree support** — `FindGitRoot` now detects `.git` files (used by worktrees) in addition to `.git` directories, so all commands work correctly inside `git worktree` checkouts.
- **Intel Mac builds** — release pipeline now produces `darwin_amd64` binaries and the Homebrew formula includes Intel Mac support. Closes #4. (@alec-pinson)

### Fixed

- **Correct index path documentation** — README now documents the actual OS cache directory paths (`~/.cache/cymbal/` on Linux, `~/Library/Caches/cymbal/` on macOS, `%LOCALAPPDATA%\cymbal\` on Windows) instead of the stale `~/.cymbal/` reference. Closes #5. (@candiesdoodle)
- **Proper error propagation** — commands no longer call `os.Exit(1)` on "not found" or "ambiguous" results. Errors now flow through cobra's error handling for consistent `Error:` prefixed output and proper exit codes.
- **Non-git-repo warning** — running cymbal outside a git repository now prints a clear warning (`not inside a git repository — results may be empty`) instead of silently returning empty results.

## [0.8.3] - 2026-04-02

### Added

- **GHCR container** — pre-built multi-arch Docker image (linux/amd64, linux/arm64) published to `ghcr.io/1broseidon/cymbal` on every release, tagged with version and `latest`.

## [0.8.2] - 2026-04-02

### Added

- **Docker support** — Dockerfile, docker-compose.yml, and `CYMBAL_DB` environment variable for running cymbal from a container with no local Go/CGO setup. Index stored at `.cymbal/index.db` in the repo root. (@VertigoOne1)
- PowerShell uninstall script (`uninstall.ps1`) with optional `-Purge` flag to remove index data. (@VertigoOne1)

### Fixed

- Windows binary no longer requires MinGW DLLs (`libgcc_s_seh-1.dll`, `libstdc++-6.dll`). Release workflow now statically links the C runtime on Windows. Fixes #1.
- Quoted `$(pwd)` in all Docker documentation examples to handle paths with spaces.

## [0.8.1] - 2026-03-27

### Fixed

- `cymbal structure` "Try" suggestions now deduplicated by symbol name — no more repeated suggestions when the same symbol appears in multiple files.

## [0.8.0] - 2026-03-27

### Added

- **`cymbal structure`** — structural overview of an indexed codebase. Shows entry points, most-referenced symbols (by call-site count), largest packages, and most-imported files. All derived from existing index data — no AI, no guessing. Answers "I've never seen this repo, where do I start?" Supports `--json`.
- **Batch mode** for symbol commands — `investigate`, `show`, `refs`, `context`, and `impact` now accept multiple symbols: `cymbal investigate Foo Bar Baz`. One invocation, one JIT freshness check, multiple results. Reduces agent round-trips.
- **Benchmark harness v2** — `go run ./bench run` now measures speed, accuracy (37/37 ground-truth checks), token efficiency vs ripgrep, JIT freshness overhead, and agent workflow savings across gin, kubectl, and fastapi.

## [0.7.3] - 2026-03-27

### Added

- **JIT freshness** — every query command (search, show, investigate, refs, importers, impact, trace, context, outline, diff, ls --stats) now automatically checks for changed files and reindexes them before returning results. No manual `cymbal index` needed between edits. The index is always correct.
  - Hot path (nothing changed): ~2ms overhead on small repos, ~14ms on 3000-file repos
  - Dirty path (files edited): only changed files are reparsed — 5 touched files on a 770-file repo adds ~40ms
  - Deleted files are automatically pruned from the index
  - No watch daemons, no hooks, no flags — it just works
- `index.EnsureFresh(dbPath)` public API for programmatic use.

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
