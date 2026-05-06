# Changelog

All notable changes to cymbal are documented here.

## [0.12.3] - 2026-05-06

### Fixed

- **Release tests now pass on macOS arm64 runners** — repository and file paths are canonicalized at the index, outline, and diff boundaries, so macOS `/var/...` and `/private/var/...` temp-directory spellings use the same per-repo database and git-relative diff paths.
- **OpenCode project-scope tests no longer depend on the developer's user config** — tests now isolate config roots, and the managed plugin symlink guard still rejects symlinks inside the OpenCode plugin path without rejecting normal platform-level aliases like macOS `/var -> /private/var`.
- **Makefile builds now embed the requested version** — `make build VERSION=vX.Y.Z` now passes `$(VERSION)` through ldflags instead of the stale hardcoded `v0.12.1` value.

## [0.12.2] - 2026-05-02

### Added

- **Regression coverage now reaches the 80% product target** — coverage excludes the internal `bench/` evaluation harness from the product denominator and now covers parser/walker behavior, command-facing workflows, public index facade APIs, diff output, and update-notifier state transitions. `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage` reports 80%+ total product coverage.
- **Codecov now uses the same product coverage denominator as CI** — CI converts Go's block coverprofile into LCOV line hits before upload, `codecov.yml` ignores the internal bench harness plus non-executable test, entrypoint, type, and registry files, and command entrypoint regression tests keep Codecov's line-oriented calculation above the threshold.
- **OpenCode now has a first-class installer path** — `cymbal hook install opencode` installs a cymbal-managed OpenCode plugin in the documented plugin directory for user or project scope, making OpenCode the main supported cymbal integration path instead of relying on `AGENTS.md` bootstrap text. The managed plugin refreshes guidance through `cymbal hook remind --update=if-stale`, so update guidance stays fresh automatically while cymbal still never self-updates by default.

### Fixed

- **Elixir and Scala parser coverage exposed real extraction gaps** — Elixir function/macro symbols now use the callable name instead of including arguments, and Scala now has dedicated symbol/ref extraction for classes, traits, objects, methods, fields, and call expressions.
- **Agent reminder text leads with the Grep/Glob/Read tools Claude Code actually routes through** — the `cymbal hook remind` trailing line previously compared cymbal to `rg`/`grep`/`find`/`fd`, but Claude Code's system prompt directs the model away from shelling out to those tools. The reminder now leads with Grep/Glob/Read and keeps shell tools in parens for shell-style agents (Cursor, Aider, Cline, etc.). Fixes [#46](https://github.com/1broseidon/cymbal/issues/46).
- **`cymbal` no longer dumps full Usage text on every RunE error** — Cobra's default behavior printed the entire help block whenever a command returned an error (e.g. `cymbal search` with no matches), drowning the real error and confusing AI agents that re-read it as a hint to retry. The root command now sets `SilenceUsage`, so the error message itself still prints but the Usage/Flags block stays out of the way. `--help` and unknown-flag paths continue to show Usage as expected.

## [0.12.1] - 2026-04-23

### Added

- **`cymbal search` accepts batched symbols and rg-style path operands** — `cymbal search Foo Bar` now searches each symbol independently instead of joining the words into one query, and `--stdin` works for newline-separated symbol batches. `cymbal search <query> [path ...]` also treats trailing files, directories, and globs as `--path` filters, so scoped text searches like `cymbal search --text 'os\.WriteFile\(' tools/file.go tools/patch.go` work without translating the command shape. `--file` is accepted as an alias for `--path`, and agent hook guidance now distinguishes symbol navigation from literal/regex text search.

### Fixed

- **Agent reminders can refresh stale update status** — `cymbal hook remind --update=if-stale` keeps the default cache-only behavior unless explicitly requested, then performs a bounded live update check only when the cache is stale or missing. Claude Code installs now use the stale-aware reminder command, and Opencode docs now describe the bootstrap as best effort with Windows path guidance. Fixes [#40](https://github.com/1broseidon/cymbal/issues/40).
- **SQLite WAL files are checkpointed on close** — `Store.Close` now runs `PRAGMA wal_checkpoint(TRUNCATE)` before releasing the database handle, so committed index rows are folded back into the main `index.db` artifact at process exit. This makes DBs created on Windows less likely to appear empty when later read from WSL through `/mnt/c/...`. Fixes [#16](https://github.com/1broseidon/cymbal/issues/16).
- **Python private functions are indexed again** — underscore-prefixed Python functions such as `_parse_token` and `__helper` are no longer silently dropped from the symbol index. This restores `search`, `outline`, `show`, `refs`, `trace`, `impact`, and `investigate` coverage for Python codebases that keep most implementation logic behind module-private helper functions. Fixes [#41](https://github.com/1broseidon/cymbal/issues/41).

## [0.12.0] - 2026-04-22

### Added

- **Visual graph output for relationship commands.** `trace`, `impact`, `importers`, and `impls` can now render Mermaid (TTY default), DOT, or JSON graphs directly from the existing verbs.
  ```bash
  cymbal trace <symbol> --graph
  cymbal impact <symbol> --graph
  cymbal importers <file> --graph
  cymbal impls <symbol> --graph
  ```
  - **Shared Graph Flags:**
    - `--graph` — triggers graph mode instead of standard text output.
    - `--graph-format mermaid|dot|json` — overrides the format.
    - `--graph-limit N` — caps output at the top-N most-connected nodes (preserving roots and adding a `_truncated` sentinel).
    - `--include-unresolved` — visualizes external/unindexed relationships (like stdlib imports or conformance to unindexed protocols) as dashed nodes, prefixed with `ext:`.

- **Tightened rendering UX:**
  - Mermaid output automatically caps at 500 nodes to prevent UI rendering lockups, truncating by degree severity and alerting via stderr. You can use `--graph-limit` to dial this in manually.
  - `impact --graph` defaults to a depth of `1` (rather than `impact`'s normal text default of `2`), significantly improving visual readability on hot functions. You can still explicitly pass `--depth 2` to override.

### Fixed

- **Repo boundaries are now enforced for direct file reads and indexing.** `cymbal show <file>` now resolves symlinks, refuses paths outside the indexed repository, and rejects `.git` reads instead of opening arbitrary local files. The walker also skips symlinked files entirely, so a repo can no longer smuggle external source into the index via `foo.go -> /private/path`.
- **JIT freshness now catches normal in-place edits again.** The old directory-mtime shortcut could miss ordinary file saves and return stale `search` / `show` / `refs` / `trace` results until some later directory-level change happened. `EnsureFresh` now falls back to the existing file-level incremental path, so ordinary edits, touches, and deletions refresh correctly.
- **Text search now respects global limits without racing.** The pure-Go fallback had a shared-slice race and only weakly enforced `--limit`; the ripgrep path buffered all stdout in memory. `TextSearch` now cancels promptly once the global limit is reached, and the `rg` fast path streams matches instead of slurping the whole result set first.
- **Frontmatter, cache files, and settings writes are safer for agent workflows.** Frontmatter metadata now quotes unsafe scalar values instead of interpolating raw newlines/control characters, update-check state no longer persists arbitrary cached update commands, index/update-cache files are created with private permissions, and JSON settings/cache writes now use temp-file + rename semantics.
- **Long-line and cache-path edge cases are handled correctly.** File-reading paths now raise the scanner buffer so minified/generated single-line files do not fail with `token too long`, and `cymbal ls --repos` now scans the actual cache root (including `CYMBAL_CACHE_DIR`) rather than the stale legacy `~/.cymbal/repos/...` location.

## [0.11.8] - 2026-04-22

### Added

- **Cached update notifications with install-specific guidance** — cymbal now checks the latest GitHub release on a throttled cache, then shows a small passive notice on interactive non-JSON commands when a newer version is available. The suggested upgrade command is tailored to how cymbal was installed (`brew upgrade 1broseidon/tap/cymbal`, rerun the PowerShell installer, `docker pull`, `go install`, or the releases page), with environment overrides for custom setups via `CYMBAL_INSTALL_METHOD` and `CYMBAL_UPDATE_COMMAND`. Passive notices can be disabled entirely with `CYMBAL_NO_UPDATE_NOTIFIER=1`.
- **Update status in `cymbal version` and agent reminders** — `cymbal version` now includes cached release status in human and JSON output, and `cymbal hook remind` appends the exact update command when the cache already knows a newer release exists. Reminder output stays cache-only, so agent session startup does not trigger extra network checks; agents that can run shell commands are instructed to run the suggested update command immediately, otherwise to tell the user what to run.

### Fixed

- **GitHub releases now publish changelog notes automatically.** The tag workflow now checks out the tagged source, extracts the matching `## [X.Y.Z]` section from `CHANGELOG.md`, and uses it as the GitHub Release body. Releases now fail fast if the changelog entry for the tag is missing, which makes release notes part of the tag workflow instead of a manual post-step.
- **Update-notifier cache now has a cross-platform override.** `updatecheck.cymbalDir()` honors a new `CYMBAL_CACHE_DIR` environment variable before falling back to `os.UserCacheDir()` / the home directory. This unblocks reminder tests on macOS (where `os.UserCacheDir()` returns `~/Library/Caches` regardless of `XDG_CACHE_HOME`) and gives users a way to relocate the update-check cache on custom installs.

## [0.11.6] - 2026-04-20

### Fixed

- **Claude Code `PreToolUse` nudge hook no longer fails JSON validation.** The `claude-code` output shape emitted the deprecated top-level `decision` + `systemMessage` fields, which Claude Code's current schema rejects with `Hook JSON output validation failed — (root): Invalid input`. Nudge now emits `hookSpecificOutput.{hookEventName,permissionDecision,permissionDecisionReason,additionalContext}` per the current hooks schema, and the suggestion is injected as `additionalContext` (visible to the model) rather than `systemMessage` (user-facing warning only). The `remind` hook's `claude-code` format got the same treatment for `SessionStart`. Generic `json` format (`{"systemMessage":"..."}`) is unchanged for non–Claude Code agents.

## [0.11.5] - 2026-04-20

### Fixed

- **Lua function/method `start_line` now points at the `function` keyword**, not the preceding whitespace. tree-sitter-lua (smacker fork) folds leading whitespace from the previous statement into the next `function_statement` node, so `node.StartPoint().Row` was landing 1–3 lines early. The parser now anchors Lua function/method start lines to the name node, which matches what users `grep` for. Caught by running `./bench check` on the newly-added lazy.nvim corpus entry — `cymbal show headless` pointed at the correct line, but `search`'s reported line diverged from the file.

### Added

- **Four new bench corpus entries** for the languages that gained dedicated coverage in 0.11.4 — `dapper` (C#), `monolog` (PHP), `lazy.nvim` (Lua), and `nvm` (Bash). Pinned to tagged refs for reproducibility. Each repo supplies 1–3 canonical symbols plus a footgun scenario (common method name, stdlib collision, or cross-file name reuse) to exercise ranking and grep-style noise avoidance. Full suite still reports zero accuracy failures and no regressions vs baseline.

## [0.11.4] - 2026-04-20

### Fixed

- **C# `using` extraction now uses AST fields instead of text trimming** (follow-up to [#35](https://github.com/1broseidon/cymbal/pull/35); Codex review P1). The previous implementation stripped `"using"` / `"static"` prefixes from the directive's raw text, which produced malformed paths on `global using System.Text;` (→ `"global System.Text"`) and `using Alias = System.IO.Path;` (→ `"Alias = System.IO.Path"`). The extractor now walks `using_directive` / `global_using_directive` children, detects the alias form via a trailing `=`, and returns the real `qualified_name` / `identifier` target. Tests added for both shapes, plus a negative assertion that the old malformed outputs never appear.
- **PHP `use` extraction now handles grouped and comma-separated imports.** Previously only the first clause of `use Foo\Bar, Baz\Qux;` was emitted, and group form `use My\{A, B as C, D};` / `use function Foo\helper;` / `use const Foo\MAX;` produced nothing at all. The extractor now appends one `Import` per resolved path (prefixing group leaves with their common namespace) and relies on `phpUseClausePath` to strip `as Alias` suffixes. Tests cover all three shapes.
- **PHP `new \Fully\Qualified\Name()` refs now resolve to the leaf name.** `extractCallName` strips `.` separators but PHP uses `\` for namespaces, so the previous ref was emitted as `"\\Fully\\Qualified\\Name"`. It now collapses to `"Name"` so downstream commands (`refs`, `impact`) match the same symbol.

### Added

- **Dedicated parser coverage for C#, PHP, Lua, and Bash** (part of [#22](https://github.com/1broseidon/cymbal/issues/22)) — these four languages were parseable by tree-sitter but still falling through to generic symbol extraction, which left them with missing kinds and empty import/reference graphs. Each now has proper `classify` / `extractImport` / `extractRef` paths:
  - **C#** — `namespace`, `class`, `struct`, `interface`, `enum`, `record`, `delegate`, `method`, `constructor`, `destructor`, `property`, `field`; `using` / `using static` imports; `invocation_expression` and `object_creation_expression` refs.
  - **PHP** — `namespace`, `class`, `interface`, `trait`, `enum`, `function`, `method`, `const_element`, `enum_case`; `namespace_use_declaration` imports; `function_call_expression`, `member_call_expression`, `scoped_call_expression`, `nullsafe_member_call_expression`, and `object_creation_expression` refs.
  - **Lua** — `function` / `method` symbols from `function_statement` (including `M.foo` and `M:new` forms); `require("x")` / `require "x"` / `require 'x'` imports; call refs for flat `util.debug(...)` / `M:new(...)` shapes.
  - **Bash** — `function` symbols from `function_definition`; `source x.sh` / `. x.sh` imports; command-invocation refs with a small ignore list for control-flow builtins (`if`/`for`/`set`/`local`/etc.) so real call signal isn't drowned by keywords.

  This directly improves `cymbal refs`, `cymbal depends` (PR #18), and `cymbal dead` (PR #17) accuracy on these languages. YAML is intentionally left in the generic path — its semantic model (no functions, no imports, no calls) doesn't fit cymbal's indexed shape. Swift, which was listed in #22, already had dedicated coverage before this change.

- **`cymbal refs --stdin`** — `refs` now accepts newline-separated symbol names on stdin, matching `show` / `impact` / `trace` / `impls`. This finishes the documented pipe idiom (`cymbal outline big.go -s --names | cymbal refs --stdin`) that was previously rejected as `unknown flag: --stdin`.

## [0.11.3] - 2026-04-20

### Added

- **`cymbal version` and `--version`** (fixes [#26](https://github.com/1broseidon/cymbal/issues/26)) — print the binary version, commit, build date, and Go toolchain. Release builds embed the tag, commit, and timestamp via `-ldflags`; `go install`-style builds fall back to `debug.ReadBuildInfo()` so module version and VCS stamp still surface. JSON mode is available via `--json`.
- **`cymbal outline --names`** (fixes [#28](https://github.com/1broseidon/cymbal/issues/28)) — emit one deduplicated symbol name per line. This flag was already documented in the `impact`/`trace`/`show` help text and earlier CHANGELOG entries for pipe-driven multi-symbol workflows (`cymbal outline big.go -s --names | cymbal show --stdin`), but the flag itself had never been wired up.
- **Example Agent Skill for cymbal** (fixes [#23](https://github.com/1broseidon/cymbal/issues/23)) — added `examples/skills/cymbal/{SKILL.md,README.md}` as a ready-to-install skill for Claude Code and other runtimes that read the same format. The skill encodes the “use cymbal first” rule, a command decision tree, path-filter / stdin / JSON usage, and field-manual guidance (pivot rule, stop rules, anti-patterns, real constraints) so the behavior survives long-context drift better than a one-off prompt.

### Fixed

- **`cymbal trace --help` example shorthand** — corrected the help text to stop implying a nonexistent `--depth` shorthand. `-d` is the global DB flag; depth remains long-form only.

### Changed

- **PR body validation is now proportional to PR size** — trivial PRs (small diffs or docs/trivial/dependencies labels) skip the checklist gate, larger PRs require only a real `## Summary`, and unchecked checklist boxes are surfaced as warnings instead of hard failures. This removes boilerplate friction for one-line fixes while still preserving signal on larger changes.

## [0.11.2] - 2026-04-18

### Changed

- **`cymbal hook install claude-code` wires the reminder to `SessionStart` instead of `UserPromptSubmit`** — the reminder now injects once per session (its intended purpose, per `cymbal hook remind --help`) rather than on every user prompt. Users saw the ~730-byte primer re-injected every turn, paying thousands of tokens over a long session for content that only changes meaning at session boundaries. `SessionStart` primes the agent once and stays out of the way after.
- **Re-running the installer migrates pre-0.11.2 installs automatically** — `mergeClaudeHooks` now strips any prior cymbal-marked entries (including old `UserPromptSubmit` ones) before adding the new `SessionStart` entry. Unrelated hooks in either location are preserved via the marker-based filter.

### Fixed

- **`cymbal impls` on Rust** — `impl Trait for Type { }` edges now resolve the implementer name instead of returning `(anonymous)`, and `cymbal impls --of Type` now walks through separate `impl` blocks to find conformances. Previously the SQL that resolves the owning symbol of an implements-ref excluded Rust's `impl` kind, so `cymbal impls Error` in mini-redis returned `(anonymous) (anonymous)` and `cymbal impls --of Frame` returned no edges at all.
- **`cymbal impls --of <outer>` no longer over-reports conformances from nested types** — in Swift (and any language where types nest), a big outer class like Alamofire's `Session` was attributing conformances of nested private types (e.g. `struct RequestConvertible: URLRequestConvertible`) to the outer. `--of Session` now correctly returns only the outer's direct conformances (just `Sendable`), matching `FindImplementors`' existing smallest-enclosing-symbol logic.
- **Rust `impl Foo<T>` blocks now index under the bare name `Foo`** — the parser previously stored the generic form verbatim (`impl Foo<T, U>`), so `cymbal impls --of Foo` missed blocks with generic parameters. Stripping generics at classification time means ripgrep's `impl Sink for JSONSink<'p, 's, M, W>` now correctly resolves under `JSONSink`.

### Changed

- **Stricter bench ground truth** — added trait-impls cases for ripgrep (`Sink` with 9 implementors across 3 crates including blanket impls on `&mut S` and `Box<S>`; `Matcher` with 3 same-named `RegexMatcher` types across pcre2/regex/testutils; `JSONSink` inverse `--of` direction). Pushed total ground-truth checks from 59 to 61; cymbal passes all 61. Also tightened existing `JSONSink` and `SearchWorker` search expectations to include their bare-named `impl` blocks.
- **Bench internals refactored** — `executeBench`, `generateReport`, and `tunedGrepScore` split into per-phase / per-section / per-concern helpers. All three dropped from 32–39 cyclomatic complexity to single-digit (under the repo's pre-commit threshold of 30).

## [0.11.0] - 2026-04-18

### Added

- **`cymbal impls <symbol>`** — find types that implement / conform to / extend a protocol, interface, trait, or base class. Language-agnostic: Swift protocol conformance, Go interface embedding, Java/C#/Kotlin/TypeScript implements clauses, Scala extends/with, Rust `impl Trait for Type`, Dart interfaces/mixins, Python base classes, Ruby `include`/`extend`/`<`, PHP implements, and C++ base classes all register as implements-kind refs. External framework targets (e.g. `LiveActivityIntent` from ActivityKit) are stored by name and returned with `resolved=false`. Supports `--of <type>` for the inverse direction ("what does this type implement?"), `--resolved` / `--unresolved` filters, plus the standard `--lang`, `--path`, `--exclude`, `--json` flags.
- **Implements / Implementors sections in `investigate` and `context`** — when a symbol is a type-like kind (class, struct, interface, protocol, trait, enum, record, object, mixin, actor, extension), the output now includes who implements it and what it implements. External vs. local targets are marked inline.
- **Typed refs** — refs now carry a `Kind` field (`call`, `implements`, `use`). This is the foundation for the implements graph and for trace noise reduction.
- **Multi-symbol invocation for `show`, `impls`, `impact`, `trace`** — every symbol-taking command now accepts N names in a single turn. Human output groups results under `═══ <name> ═══` headers; JSON mode returns a map keyed by the requested name. For `impact` and `trace`, identical call sites are deduplicated across inputs and each surviving row carries a `hit_symbols` attribution list (rendered inline as `[sym1,sym2]`, returned as structured data in JSON). Per-symbol "not found" is a warning, not a hard failure — agents get partial results back.
- **`--stdin` flag on `show`, `impls`, `impact`, `trace`** — read newline-separated symbol names from stdin, so `cymbal outline big.go -s --names | cymbal show --stdin` works cleanly. Comment lines (`#`) and blanks are skipped; positional args and stdin input are merged and deduplicated in first-seen order.
- **`cymbal hook` — agent integration hooks** (fixes [#23](https://github.com/1broseidon/cymbal/issues/23)). Two small, agent-agnostic subcommands that keep coding agents using cymbal instead of sliding back to raw `grep`/`find` as context grows:
  - `cymbal hook nudge` reads a would-be shell command (from argv or a Claude Code-style JSON payload on stdin) and, if it looks like a code search on source-like input, emits a short suggestion for the cymbal equivalent. Never blocks. Output formats: `claude-code` (default JSON for `PreToolUse`), `text` (stderr), `json` (generic). Detection is deliberately narrow: `rg`/`grep`/`egrep`/`fgrep`/`ack`/`ag`/`find -name`/`fd`, skipping short queries, heavy regexes, and non-source globs to avoid false-positive nagging.
  - `cymbal hook remind` prints a tone-calibrated reminder block an agent can inject at session start or on demand. Formats: `text`, `json`, `claude-code`.
  - `cymbal hook install claude-code` / `uninstall claude-code` — one-liner installer for Claude Code. Merges `PreToolUse` (matcher=Bash) + `UserPromptSubmit` entries into `~/.claude/settings.json` with `--scope user|project` and `--dry-run`. Idempotent, marker-tagged, preserves unrelated user config.
  - For other agents (Cursor, Windsurf, aider, Cline, Continue, Zed, Codex/OpenAI Agents SDK, generic shell), see [`docs/AGENT_HOOKS.md`](docs/AGENT_HOOKS.md) — the two subcommands are the whole surface and each integration is one or two lines of rules-file or config-file glue.

### Changed

- **`cymbal trace` defaults to call-only edges** — previously `trace` surfaced every identifier seen inside a symbol's line range, which made Swift output noisy with type annotations (`UUID`, `Date`, `Sendable`, `@escaping`, etc.) that weren't actually callees. Trace now filters to `kind='call'` by default. Use `--kinds call,use` (or `--kinds call,use,implements`) to opt back into the wider behavior.
- **`symbols.Ref.Kind`** — new field on the public `symbols.Ref` type. Empty is treated as `use` by the store, so older callers keep working without changes.
- **`--limit` on `impls`, `impact`, `trace`** — now documented as per-symbol, not a total cap across a multi-symbol call. Single-symbol behavior is unchanged.

### Migration

- **`refs.kind` column added via `ALTER TABLE`** — existing databases are migrated automatically on first open; no action needed. Re-index (`cymbal index`) once to populate `kind` values for existing rows. Until you do, new commands (`impls`, investigate/context implements sections) will be empty while `trace` will correctly filter to the new default.
- **`index.FindTrace` signature is now variadic** — `FindTrace(db, name, depth, limit, kinds...)`. Existing calls without `kinds` keep working and get the new call-only default.
- **Single-symbol output is unchanged** — all multi-symbol rendering (banners, `symbols:` frontmatter, `hit_symbols` attribution, JSON map shape) only activates when more than one name is passed or `--stdin` is set.

## [0.10.1] - 2026-04-17

### Fixed

- **Swift references, impact, and trace now work** — Swift files were parsed for symbols but had no reference-extraction dispatch, so `refs`, `impact`, and `trace` always returned empty on Swift code. The new `extractRefSwift` emits refs for call expressions (including `x.y.z()` navigation-expression callees) and named type usages (`FeedingStore`, `BabyTrackingService`, `Formatter`, etc.) across annotations, inheritance clauses, generics, parameters, and return types.
- **Swift declaration classification is now accurate** — tree-sitter-swift collapses `struct`, `class`, `enum`, `extension`, and `actor` into shared declaration node families. cymbal now disambiguates these by inspecting the leading keyword, so outlines and search results correctly label Swift declarations instead of misclassifying them as generic `class` nodes.
- **Swift `actor` declarations are recognized explicitly** — actor types now surface as `actor` symbols instead of falling back to `class`, and members nested inside actor bodies keep the correct parent symbol.
- **`search -i` / `--ignore-case` now implies `--exact`** — case-insensitive symbol search now matches the CLI UX and changelog docs: `-i` upgrades symbol lookup to an exact case-insensitive match, while `--text -i` remains unsupported.

### Added

- **`search -i` / `--ignore-case`** — case-insensitive exact match for symbol search. `-i` now implies `--exact`, and remains unsupported with `--text`. Backed by a `COLLATE NOCASE` predicate; leaves FTS5 prefix/fuzzy search (already case-insensitive) untouched. Exposed on `index.SearchQuery` as `IgnoreCase`.
- **Swift feature coverage tests** — parser and store tests now cover Swift declaration classification, actor members, Swift reference extraction, and case-insensitive exact search behavior.

## [0.10.0] - 2026-04-15

### Highlights

**Canonical ranking** — `search` and `show` now return the most relevant definition first, not whatever SQLite happened to store first. A `SymbolScore` ranker penalises test, playground, docs, vendor, generated-code, and mirror-tree paths while boosting well-known source roots. Before: `show createServer` in Vite opened a playground copy. After: the real implementation ranks #1 across all benchmark repos (100% canonical @1, 1.00 MRR).

**Faster freshness checks** — `EnsureFresh` now checks directory mtimes before doing a full file walk. If nothing changed since the last index, the check completes in microseconds instead of hashing every file (~500 dir stats on a 10k-file repo instead of ~10k file hashes).

**Process-scoped DB** — all queries in a single command invocation share one SQLite connection instead of opening and closing it per function call. `investigate` went from 5+ DB opens to 1.

### Added

- **`--path` and `--exclude` glob filters** on `search`, `refs`, and `show` — scope results to specific directories or exclude test/vendor/generated paths. Composable with `--kind`, `--lang`, `--exact`.
- **`show --all`** — emit every matched definition, not just the top-ranked one. Useful when the agent needs to see all overloads or cross-module variants.
- **`refs --file <fragment>`** — restrict reference results to files whose path contains the given fragment. Useful for scoping `refs Context` to files that actually import `context.go`.
- **Structured alternatives in JSON** — `show` JSON responses include `match_count` and `also: []SymbolResult`; `context` JSON responses include `match_count` and `matches: []SymbolResult`. Agents can follow up on alternatives without string-parsing frontmatter.
- **`search --text` delegates to `rg`** — when ripgrep is on `PATH`, text search shells out to `rg` for full SIMD/mmap speed. Falls back to the pure-Go implementation when `rg` is absent.
- **Generated code ranking penalties** — symbols in `.pb.go`, `_generated.go`, `_gen.go`, `.gen.ts`, `_pb2.py`, `__generated__`, `.g.dart`, `/generated/`, `/gen/` paths are ranked below hand-written code.
- **Ground-truth precision/recall benchmark** — the bench harness validates cymbal output against curated expected-definition and expected-reference sets per symbol across 7 corpus repos (43 checks, 100% pass rate).
- **Canonical ranking hard-mode benchmark** — measures search@1 accuracy, MRR, and show-exactness against 9 hand-picked disambiguation cases with a tuned-grep baseline for fair comparison.
- **Grep footgun benchmark** — explicit test cases proving cymbal's advantage on common names (e.g. `Context`: 915 grep hits → 5 cymbal results; `FastAPI`: 11k → 8).

### Fixed

- **`context` no longer errors on ambiguous symbols** — ranks all candidates and picks the top result instead of returning `AmbiguousError`. Output includes `matches: N` metadata so agents know alternatives exist.
- **Ranking before SQL LIMIT** — search over-fetches a wider candidate window, applies canonical ranking, then truncates. Canonical definitions are never silently dropped by DB row order. Exact-match queries fetch all rows (candidate set is inherently bounded); FTS queries over-fetch `min(limit×5, 500)` rows and rank within tier boundaries (exact > prefix > fuzzy).
- **Improved signature extraction** — `extractSignature` now captures `return_type` nodes for Go, Python, Rust, and TypeScript/JavaScript. Python signatures show `-> ReturnType`.

### Breaking (library API)

- **`index.SymbolContext` no longer returns `*AmbiguousError`** — it ranks all candidates and returns the top match. Callers that checked for `AmbiguousError` should instead inspect `ContextResult.MatchCount` and `ContextResult.Matches`.

### Changed

- **Process-scoped store pool** — all public index functions share a cached `*Store` per `dbPath` via `openCached`. `CloseAll()` is deferred in `main.go` so handles flush on both success and error paths.
- **EnsureFresh directory-mtime fast path** — records `last_index_ns` after each index run. On subsequent commands, walks only directories (skipping `.git`, `node_modules`, `vendor`, etc.) and checks mtimes. If no directory is newer, skips the full file-walk entirely.
- **DRY output rendering** — extracted shared `renderJSONOrFrontmatter` into `cmd/render.go`, deduplicating the json-or-frontmatter pattern across 6 commands.
- **Benchmark corpus enriched** — all 7 corpus repos carry tier/complexity/tags metadata, full ground-truth specs, and 9 canonical disambiguation cases with prefer/avoid path annotations.

## [0.9.3] - 2025-04-14

### Added

- **Unified language registry** — added a new `lang` package as the single source of truth for language names, file extensions, special filenames, and tree-sitter grammar availability.
- **Broader file recognition** — cymbal now recognizes additional source/config variants during file classification, including `.mjs`, `.cjs`, `.mts`, `.cts`, `.pyw`, `.cxx`, `.hxx`, `.hh`, `.kts`, `.rake`, `.gemspec`, `.sc`, and `.tfvars`.
- **Recognition for non-parseable file types** — cymbal can now classify additional file types for CLI/path heuristics even when they are not indexed, including `Dockerfile`, `Makefile`, `Jenkinsfile`, `CMakeLists.txt`, JSON, TOML, Markdown, SQL, Vue, Svelte, Zig, Erlang, Haskell, OCaml, R, and Perl.

### Changed

- **Shared language resolution across indexing and parsing** — `walker`, `parser`, and `index` now all use the same registry-backed language lookup and parseable-language filtering, reducing drift between file discovery and parser support.
- **Recognized vs parseable languages are now explicit** — indexing walks the parseable subset, while file classification can still identify recognized-but-non-indexable file types.

### Docs

- Updated README agent-integration guidance to reference `AGENTS.md` instead of `agent.md`.

## [0.9.2] - 2026-04-13

### Fixed

- **Go composite literal & JS/TS `new` expression ref extraction** — `refs`, `trace`, `impact`, and `investigate` now detect references from Go composite literals (`Config{}`, `http.Client{}`, `map[K]V{}`, `[]T{}`, `[N]T{}`), including qualified types (`pkg.Type`), and from JavaScript/TypeScript `new` expressions (`new Foo()`, `new pkg.Bar()`) (PR #14).

## [0.9.1] - 2026-04-11

### Added

- **C/C++ call-site reference extraction** — `refs`, `trace`, `impact`, and `investigate` now return call-graph data for C and C++ files. Includes normalization for dot, arrow (`->`), and scope-resolution (`::`) separators, plus C++ template call stripping (`std::max<int>` → `max`) (PR #12, @Phototonic).

### Changed

- **Library usage guide** — added `docs/library.md` and a README section covering how to import cymbal as a Go library.
- **Test helper dedup** — extracted shared `debugParseResult` helper from duplicated closures in C/C++ feature tests.

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
