# cymbal

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

No local Go toolchain or CGO setup needed — run cymbal from a container against any repo on your machine.

```sh
# Build the image
docker build -t cymbal .
```

Mount any repo and the SQLite index lands at `.cymbal/index.db` inside it by default:

```sh
# Index a repo
docker run --rm -v /path/to/your/repo:/workspace cymbal index .

# Query it (index persists at /path/to/your/repo/.cymbal/index.db)
docker run --rm -v /path/to/your/repo:/workspace cymbal investigate handleAuth

# Override the DB location if needed
docker run --rm -v /path/to/your/repo:/workspace -e CYMBAL_DB=/some/other/path.db cymbal index .
```

Or use docker compose (mounts the current directory by default):

```sh
docker compose run --rm cymbal index .
docker compose run --rm cymbal investigate handleAuth
```

Add `.cymbal/` to your `.gitignore` to keep the index out of version control.

## Quick start

Define a shell alias once so every command looks like the native binary:

```sh
alias cymbal='docker run --rm -v "$(pwd)":/workspace cymbal'
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

The SQLite index is written to `.cymbal/index.db` in the current directory and persists between runs.

## Commands

| Command | What it does |
|---------|-------------|
| `investigate` | **Start here.** Kind-adaptive exploration — one call, right shape |
| `structure` | Structural overview — entry points, hotspots, central packages |
| `trace` | Downward call graph — what does this symbol call? |
| `index` | Parse and index a directory |
| `ls` | File tree, repo list, or `--stats` overview |
| `search` | Symbol search (or `--text` for grep) |
| `show` | Display a symbol's source code |
| `outline` | List all symbols in a file |
| `refs` | Find references / call sites |
| `importers` | Reverse import lookup — who imports this? |
| `impact` | Transitive callers — what's affected by a change? |
| `diff` | Git diff scoped to a symbol's line range |
| `context` | Bundled view: source + types + callers + imports |

Commands that accept symbols support **batch**: `cymbal investigate Foo Bar Baz` runs all three in one invocation.

All commands support `--json` for structured output.

## Agent integration

cymbal is designed as the code navigation layer for AI agents. One command handles most investigations — specific commands exist as escape hatches when you need more control.

Add this to your agent's system prompt (e.g., `CLAUDE.md`, `agent.md`, or MCP tool descriptions).

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
- Before searching: `cymbal search <query>` (symbols) or `cymbal search <query> --text` (grep)
- Before exploring structure: `cymbal ls` (tree) or `cymbal ls --stats` (overview)
- To disambiguate: `cymbal show path/to/file.go:SymbolName` or `cymbal investigate file.go:Symbol`
- First run: `cymbal index .` to build the initial index (<1s). After that, queries auto-refresh — no manual reindexing needed.
- All commands support `--json` for structured output.
```

**Docker (no local install required):**

```markdown
## Code Exploration Policy
Use `cymbal` via Docker for code navigation — prefer it over Read, Grep, Glob, or Bash for code exploration.
Run all cymbal commands as: `docker run --rm -v "$(pwd)":/workspace cymbal <command>`
- **New to a repo?**: `docker run --rm -v "$(pwd)":/workspace cymbal structure` — entry points, hotspots, central packages. Start here.
- **To understand a symbol**: `docker run --rm -v "$(pwd)":/workspace cymbal investigate <symbol>` — returns source, callers, impact, or members based on what the symbol is.
- **To understand multiple symbols**: `docker run --rm -v "$(pwd)":/workspace cymbal investigate Foo Bar Baz` — batch mode, one invocation.
- **To trace an execution path**: `docker run --rm -v "$(pwd)":/workspace cymbal trace <symbol>` — follows the call graph downward (what does X call, what do those call).
- **To assess change risk**: `docker run --rm -v "$(pwd)":/workspace cymbal impact <symbol>` — follows the call graph upward (what breaks if X changes).
- Before reading a file: `docker run --rm -v "$(pwd)":/workspace cymbal outline <file>` or `docker run --rm -v "$(pwd)":/workspace cymbal show <file:L1-L2>`
- Before searching: `docker run --rm -v "$(pwd)":/workspace cymbal search <query>` (symbols) or add `--text` for grep
- Before exploring structure: `docker run --rm -v "$(pwd)":/workspace cymbal ls` or `docker run --rm -v "$(pwd)":/workspace cymbal ls --stats`
- To disambiguate: `docker run --rm -v "$(pwd)":/workspace cymbal investigate path/to/file.go:Symbol`
- First run: `docker run --rm -v "$(pwd)":/workspace cymbal index .` to build the initial index. After that, queries auto-refresh — no manual reindexing needed.
- The SQLite index is stored at `.cymbal/index.db` in the repo root and persists between runs.
- All commands support `--json` for structured output.
```

### Why this works

An agent tracing an auth flow typically makes 15-20 sequential tool calls: show function → read the code → guess the next function → show that → repeat. Each call costs a reasoning step (~500 tokens). Three commands eliminate this:

| Command | Question it answers | Direction |
|---|---|---|
| `investigate X` | "Tell me about X" | Adaptive (source + callers + impact or members) |
| `trace X` | "What does X depend on?" | Downward (callees, depth 3) |
| `impact X` | "What depends on X?" | Upward (callers, depth 2) |

`investigate` replaces search → show → refs. `trace` replaces 10+ sequential show calls to follow a call chain. Together they reduce a 22-call investigation to 4 calls.

## Supported languages

cymbal uses tree-sitter grammars. Currently supported:

Go, Python, JavaScript, TypeScript, TSX, Rust, C, C++, C#, Java, Ruby, Swift, Kotlin, Scala, PHP, Lua, Bash, HCL, Dockerfile, YAML, TOML, HTML, CSS

Adding a language requires a tree-sitter grammar and a symbol extraction query — see `internal/parser/` for examples.

## How it works

1. **Index** — tree-sitter parses each file into an AST. cymbal extracts symbols (functions, types, variables, imports) and references (calls, type usage) and stores them in SQLite with FTS5 full-text search. Each repo gets its own database at `~/.cymbal/repos/<hash>/index.db` by default. Override with `--db <path>` or the `CYMBAL_DB` environment variable.

2. **Query** — all commands read from the current repo's SQLite index. Symbol lookups, cross-references, and import graphs are SQL queries. No re-parsing needed. No cross-repo bleed.

3. **Always fresh** — every query automatically checks for changed files and reindexes them before returning results. No manual reindexing, no watch daemons, no hooks. Edit a file, run a query, get the right answer. The mtime+size fast path adds ~2ms when nothing changed; only dirty files are re-parsed.

## Benchmarks

Measured against ripgrep on three real-world repos (gin, kubectl, fastapi) across Go, Python, and TypeScript. Full harness in `bench/`.

```sh
go run ./bench setup   # clone pinned corpus repos
go run ./bench run     # run all benchmarks → bench/RESULTS.md
```

**Speed** — cymbal queries complete in 9-33ms. Reindex with nothing changed: 7-24ms.

**Accuracy** — 100% automated ground-truth verification across 37 checks (search returns correct file+kind, show returns correct source, refs finds known callers, investigate includes expected signature).

**Token efficiency** — for targeted lookups, cymbal uses 62-100% fewer tokens than ripgrep. Refs queries show the biggest wins (93-100% savings) because cymbal returns semantic call sites, not every line mentioning the string.

**JIT freshness** — queries auto-detect and reparse changed files. Overhead: ~2ms when nothing changed, ~25-47ms after touching 1 file, ~50-75ms after touching 5 files. Deleted files are automatically pruned.

**Agent workflow** — `cymbal investigate` replaces 3 separate ripgrep calls (search + show + refs) with 1 call. Typical savings: 81-99% fewer tokens for focused symbols.

## Docs

- [Changelog](./CHANGELOG.md)

## License

[MIT](./LICENSE)
