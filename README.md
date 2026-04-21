# cymbal

[![GitHub Stars](https://img.shields.io/github/stars/1broseidon/cymbal?style=social)](https://github.com/1broseidon/cymbal/stargazers)
[![Go Reference](https://pkg.go.dev/badge/github.com/1broseidon/cymbal.svg)](https://pkg.go.dev/github.com/1broseidon/cymbal)
[![Go Report Card](https://goreportcard.com/badge/github.com/1broseidon/cymbal)](https://goreportcard.com/report/github.com/1broseidon/cymbal)
[![Latest Release](https://img.shields.io/github/v/release/1broseidon/cymbal)](https://github.com/1broseidon/cymbal/releases/latest)

Fast, language-agnostic code indexer and symbol navigator built on [tree-sitter](https://tree-sitter.github.io/).

cymbal parses your codebase into a local SQLite index, then gives you instant symbol search, cross-references, impact analysis, and scoped diffs — all from the command line. Designed to be called by AI agents, editor plugins, or directly from your terminal.

## Install

Homebrew (macOS / Linux):

```sh
brew install 1broseidon/tap/cymbal
```

Windows (PowerShell):

```powershell
irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex
```

To uninstall (keeps index data by default):

```powershell
# Remove binary and PATH entry, keep SQLite indexes
irm https://raw.githubusercontent.com/1broseidon/cymbal/main/uninstall.ps1 | iex

# Also remove all SQLite indexes
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/1broseidon/cymbal/main/uninstall.ps1))) -Purge
```

> **Note:** `-Purge` removes all per-repo SQLite indexes stored under `%LOCALAPPDATA%\cymbal\repos\`. Omit it to keep your indexes intact in case you reinstall.

Go (requires CGO for tree-sitter + SQLite):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Or grab a binary from [releases](https://github.com/1broseidon/cymbal/releases).

### Docker

No local Go toolchain or CGO setup needed — run cymbal from a pre-built container (linux/amd64 and arm64):

```sh
docker pull ghcr.io/1broseidon/cymbal:latest
```

Mount any repo and the SQLite index lands at `/workspace/.cymbal/index.db` inside the container by default (via `CYMBAL_DB`):

```sh
# Index a repo
docker run --rm -v /path/to/your/repo:/workspace ghcr.io/1broseidon/cymbal index .

# Query it (index persists at /path/to/your/repo/.cymbal/index.db)
docker run --rm -v /path/to/your/repo:/workspace ghcr.io/1broseidon/cymbal investigate handleAuth

# Override the DB location if needed
docker run --rm -v /path/to/your/repo:/workspace -e CYMBAL_DB=/some/other/path.db ghcr.io/1broseidon/cymbal index .
```

Pin a specific version if needed:

```sh
docker pull ghcr.io/1broseidon/cymbal:v0.8.4
```

Or build the image yourself:

```sh
docker build -t cymbal .
# or with docker compose (mounts the current directory by default):
docker compose run --rm cymbal index .
```

Add `.cymbal/` to your `.gitignore` to keep the index out of version control.

### Update notifications

`cymbal` caches release checks and can show a small update notice on interactive, non-JSON commands when a newer release is available. It never self-updates by default.

Update commands by install type:

- Homebrew: `brew upgrade 1broseidon/tap/cymbal`
- Windows PowerShell: `irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex`
- Docker: `docker pull ghcr.io/1broseidon/cymbal:latest` (or the tagged image cymbal suggests)
- Go: `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest`
- Manual binary: download the latest release from GitHub

Environment overrides:

- `CYMBAL_NO_UPDATE_NOTIFIER=1` — disable passive update notices
- `CYMBAL_INSTALL_METHOD=homebrew|powershell|docker|go|manual` — override install detection
- `CYMBAL_UPDATE_COMMAND="..."` — override the suggested update command completely

## Quick start

Define a shell alias once so every command looks like the native binary:

```sh
alias cymbal='docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal'
```

Then:

```sh
# Index the current project
cymbal index .

# Investigate any symbol — one call, right answer
cymbal investigate handleAuth    # function → source + callers + impact
cymbal investigate UserModel     # type → definition + members + references
cymbal trace handleAuth          # downward call chain — what does it call?

# Or use specific commands when you need control
cymbal search handleAuth         # find a symbol
cymbal search "TODO" --text      # full-text grep
cymbal show handleAuth           # read source
cymbal outline internal/auth/handler.go  # file structure
cymbal refs handleAuth           # who calls this?
cymbal importers internal/auth   # who imports this package?
cymbal impact handleAuth         # what breaks if I change this?
cymbal diff handleAuth main      # git diff scoped to a function
cymbal context handleAuth        # bundled: source + types + callers + imports
cymbal ls                        # file tree
```

The index auto-builds on first use — no manual `cymbal index .` required. Subsequent queries auto-refresh incrementally (~2ms when nothing changed).

## Commands

| Command | What it does |
|---------|-------------|
| `investigate` | **Start here.** Kind-adaptive exploration — one call, right shape |
| `structure` | Structural overview — entry points, hotspots, central packages |
| `trace` | Downward call graph — what does this symbol call? |
| `index` | Parse and index a directory |
| `ls` | File tree, repo list, or `--stats` overview |
| `search` | Symbol search (or `--text` for grep). Supports `--path`, `--exclude` |
| `show` | Display a symbol's source code. `--all` for every match |
| `outline` | List all symbols in a file |
| `refs` | Find references / call sites. `--file` to scope by path |
| `importers` | Reverse import lookup — who imports this? |
| `impls` | Types that implement / conform to / extend this symbol. `--of <type>` for inverse |
| `impact` | Transitive callers — what's affected by a change? |
| `diff` | Git diff scoped to a symbol's line range |
| `context` | Bundled view: source + types + callers + imports |
| `hook` | Agent-integration hooks — `nudge`, `remind`, `install <agent>` |
| `version` | Print version/build info and cached release status |

Commands that accept symbols support **batch**: `cymbal investigate Foo Bar Baz` runs all three in one invocation.

All commands support `--json` for structured output.

## Agent integration

cymbal is designed as the code navigation layer for AI agents. One command handles most investigations — specific commands exist as escape hatches when you need more control.

Add this to your agent's system prompt (e.g., `CLAUDE.md`, `AGENTS.md`, or MCP tool descriptions).

**Native install:**

```markdown
## Code Exploration Policy
Use `cymbal` CLI for code navigation — prefer it over Read, Grep, Glob, or Bash for code exploration.
- **New to a repo?**: `cymbal structure` — entry points, hotspots, central packages. Start here.
- **To understand a symbol**: `cymbal investigate <symbol>` — returns source, callers, impact, or members based on what the symbol is.
- **To understand multiple symbols**: `cymbal investigate Foo Bar Baz` — batch mode, one invocation.
- **To trace an execution path**: `cymbal trace <symbol>` — follows the call graph downward (what does X call, what do those call).
- **To assess change risk**: `cymbal impact <symbol>` — follows the call graph upward (what breaks if X changes).
- Before reading a file: `cymbal outline <file>` or `cymbal show <file:L1-L2>`
- Before searching: `cymbal search <query>` (symbols) or `cymbal search <query> --text` (grep, delegates to rg when available)
- To filter results: `cymbal search --path 'src/*' --exclude '*_test.go' <query>`
- To see all definitions: `cymbal show --all <symbol>` or `cymbal refs --file context.go <symbol>`
- Before exploring structure: `cymbal ls` (tree) or `cymbal ls --stats` (overview)
- To disambiguate: `cymbal show path/to/file.go:SymbolName` or `cymbal investigate file.go:Symbol`
- The index auto-builds on first use — no manual indexing step needed. Queries auto-refresh incrementally.
- All commands support `--json` for structured output.
```

**Docker (no local install required):**

```markdown
## Code Exploration Policy
Use `cymbal` via Docker for code navigation — prefer it over Read, Grep, Glob, or Bash for code exploration.
Run all cymbal commands as: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal <command>`
- **New to a repo?**: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal structure` — entry points, hotspots, central packages. Start here.
- **To understand a symbol**: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal investigate <symbol>` — returns source, callers, impact, or members based on what the symbol is.
- **To understand multiple symbols**: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal investigate Foo Bar Baz` — batch mode, one invocation.
- **To trace an execution path**: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal trace <symbol>` — follows the call graph downward (what does X call, what do those call).
- **To assess change risk**: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal impact <symbol>` — follows the call graph upward (what breaks if X changes).
- Before reading a file: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal outline <file>` or `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal show <file:L1-L2>`
- Before searching: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal search <query>` (symbols) or add `--text` for grep
- Before exploring structure: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal ls` or `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal ls --stats`
- To disambiguate: `docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal investigate path/to/file.go:Symbol`
- The index auto-builds on first use — no manual indexing step needed. Queries auto-refresh incrementally.
- The SQLite index is stored at `.cymbal/index.db` inside the mounted repo (via `CYMBAL_DB`).
- All commands support `--json` for structured output.
```

### Agent hooks

Prompting works, but agents drift back to `grep`/`find` as context grows (see [issue #23](https://github.com/1broseidon/cymbal/issues/23)). Cymbal ships two small, agent-agnostic hook commands:

| Command | What it does |
|---|---|
| `cymbal hook nudge` | Inspect a would-be shell command and, if it looks like a code search, emit a short system-message suggesting the cymbal equivalent. Never blocks. |
| `cymbal hook remind` | Print a tone-calibrated reminder block agents can inject at session start or on demand. |

**Claude Code — one-liner install:**

```bash
cymbal hook install claude-code                   # ~/.claude/settings.json
cymbal hook install claude-code --scope project   # .claude/settings.json
cymbal hook uninstall claude-code                 # clean removal
```

The installer is idempotent, preserves unrelated settings, and marks its own entries so `uninstall` is surgical.

When `hook remind` already knows about a newer cymbal release from the cached update check, it also includes the exact update command. Agents that can run shell commands should run that command immediately; otherwise they should tell the user exactly what to run.

**Other agents** (Cursor, Windsurf, aider, Cline, Continue, Zed, Codex/OpenAI Agents SDK, or anything that can shell out on a pre-tool event) — see [`docs/AGENT_HOOKS.md`](docs/AGENT_HOOKS.md) for copy-paste snippets. The same two subcommands work everywhere; `nudge` offers `--format=claude-code|json|text` and `remind` offers the same three formats, so every integration is one or two lines.

### Why this works

An agent tracing an auth flow typically makes 15-20 sequential tool calls: show function → read the code → guess the next function → show that → repeat. Each call costs a reasoning step (~500 tokens). Three commands eliminate this:

| Command | Question it answers | Direction |
|---|---|---|
| `investigate X` | "Tell me about X" | Adaptive (source + callers + impact or members) |
| `trace X` | "What does X depend on?" | Downward (callees, depth 3) |
| `impact X` | "What depends on X?" | Upward (callers, depth 2) |

`investigate` replaces search → show → refs. `trace` replaces 10+ sequential show calls to follow a call chain. Together they reduce a 22-call investigation to 4 calls.

## Supported languages

cymbal currently parses and indexes these languages with tree-sitter:

- Go
- Python (`.py`, `.pyw`)
- JavaScript (`.js`, `.jsx`, `.mjs`, `.cjs`)
- TypeScript (`.ts`, `.tsx`, `.mts`, `.cts`)
- Rust
- C / C++ (`.c`, `.h`, `.cpp`, `.cc`, `.hpp`, `.cxx`, `.hxx`, `.hh`)
- C#
- Java
- Apex
- Ruby (`.rb`, `.rake`, `.gemspec`)
- Swift
- Kotlin (`.kt`, `.kts`)
- Scala (`.scala`, `.sc`)
- PHP
- Lua
- Bash / shell
- YAML
- Elixir
- HCL / Terraform (`.tf`, `.hcl`, `.tfvars`)
- Protobuf
- Dart

cymbal also recognizes additional file types for classification and CLI path heuristics, even when they are not parseable/indexable: `Dockerfile`, `Makefile`, `Jenkinsfile`, `CMakeLists.txt`, JSON, TOML, Markdown, SQL, Vue, Svelte, Zig, Erlang, Haskell, OCaml, R, and Perl.

Adding a language requires a tree-sitter grammar and a symbol extraction query.

## How it works

1. **Index** — tree-sitter parses each file into an AST. cymbal extracts symbols (functions, types, variables, imports) and references (calls, type usage) and stores them in SQLite with FTS5 full-text search. Each repo gets its own database under the OS cache directory (`~/.cache/cymbal/repos/<hash>/index.db` on Linux, `~/Library/Caches/cymbal/repos/` on macOS, `%LOCALAPPDATA%\cymbal\repos\` on Windows). Override with `--db <path>` or the `CYMBAL_DB` environment variable. The index auto-builds on first query — no manual `cymbal index .` required.

2. **Query** — all commands read from the current repo's SQLite index. Symbol lookups, cross-references, and import graphs are SQL queries. No re-parsing needed. No cross-repo bleed.

3. **Always fresh** — every query automatically checks for changed files and reindexes them before returning results. No manual reindexing, no watch daemons, no hooks. Edit a file, run a query, get the right answer. The mtime+size fast path adds ~10-24ms when nothing changed; only dirty files are re-parsed.

## Benchmarks

Measured against ripgrep on three real-world repos (gin, kubectl, fastapi) across Go and Python. Full harness in `bench/`.

```sh
go run ./bench setup   # clone pinned corpus repos
go run ./bench run     # run all benchmarks → bench/RESULTS.md
```

**Speed** — cymbal queries complete in 9-27ms. Reindex with nothing changed: 8-20ms.

**Accuracy** — 100% ground-truth precision/recall across 43 checks. 100% canonical @1 ranking across 9 hard disambiguation cases. 7/7 grep-footgun avoidance tests pass.

**Token efficiency** — for targeted lookups, cymbal uses 17-100% fewer tokens than ripgrep (`FastAPI`: 11k grep hits → 8 cymbal results; `Context`: 915 → 5). Refs queries show the biggest wins because cymbal returns semantic call sites, not every line mentioning the string.

**JIT freshness** — queries auto-detect and reparse changed files. Overhead: ~10-23ms when nothing changed, ~22-27ms after touching 1 file, ~33-43ms after touching 5 files. Deleted files are automatically pruned.

**Agent workflow** — `cymbal investigate` replaces 3 separate ripgrep calls (search + show + refs) with 1 call. Typical savings: 41-100% fewer tokens for focused symbols.

## Use as a library

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go get github.com/1broseidon/cymbal@latest
```

Five packages are exported:

| Package | What it does |
|---------|-------------|
| `index` | Indexing engine, SQLite store, and all query APIs |
| `lang` | Unified language registry for names, extensions, special filenames, and parser availability |
| `parser` | Tree-sitter parsing for 22 languages |
| `symbols` | Core data types (Symbol, Import, Ref) |
| `walker` | Concurrent file discovery with language detection |

```go
import (
    "fmt"
    "github.com/1broseidon/cymbal/index"
)

// Index a repo
stats, _ := index.Index("/path/to/repo", "", index.Options{})
fmt.Printf("%d files, %d symbols\n", stats.FilesIndexed, stats.SymbolsFound)

// Query — all functions take a dbPath and return typed results
dbPath, _ := index.RepoDBPath("/path/to/repo")

results, _ := index.SearchSymbols(dbPath, index.SearchQuery{Text: "handleAuth"})
inv, _ := index.Investigate(dbPath, "handleAuth")
trace, _ := index.FindTrace(dbPath, "handleAuth", 3, 50)
impact, _ := index.FindImpact(dbPath, "handleAuth", 2, 100)
refs, _ := index.FindReferences(dbPath, "handleAuth", 50)
```

```go
import "github.com/1broseidon/cymbal/lang"

fmt.Println(lang.Default.Supported("typescript"))     // true
fmt.Println(lang.Default.Known("dockerfile"))        // true
fmt.Println(lang.Default.LangForFile("Dockerfile"))  // "dockerfile"
fmt.Println(lang.Default.LangForFile("notes.toml"))  // "toml"
```

For the full API reference, streaming patterns, and lower-level store access, see the [library guide](./docs/guide/library.md).

## Docs

- [Library guide](./docs/guide/library.md)
- [Changelog](./CHANGELOG.md)

## License

[MIT](./LICENSE)
