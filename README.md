# cymbal

Fast, language-agnostic code indexer and symbol navigator built on [tree-sitter](https://tree-sitter.github.io/).

cymbal parses your codebase into a local SQLite index, then gives you instant symbol search, cross-references, impact analysis, and scoped diffs — all from the command line. Designed to be called by AI agents, editor plugins, or directly from your terminal.

## Install

Homebrew:

```sh
brew install 1broseidon/tap/cymbal
```

Or with Go (requires CGO for tree-sitter + SQLite):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Or grab a binary from [releases](https://github.com/1broseidon/cymbal/releases).

## Quick start

```sh
# Index the current project (~100ms for most repos)
cymbal index .

# Browse the file tree
cymbal ls

# Find a symbol
cymbal search handleAuth

# Full-text grep
cymbal search "TODO" --text

# Read a symbol's source
cymbal show handleAuth

# File outline — all symbols in a file
cymbal outline internal/auth/handler.go

# Who calls this?
cymbal refs handleAuth

# Who imports this package?
cymbal importers internal/auth

# What breaks if I change this?
cymbal impact handleAuth

# Git diff scoped to a single function
cymbal diff handleAuth main

# Everything you need to understand a symbol in one call
cymbal context handleAuth
```

## Commands

| Command | What it does |
|---------|-------------|
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

All commands support `--json` for structured output.

## Agent integration

cymbal is built to be an agent's code navigation layer. Add this to your agent's instructions (e.g. `CLAUDE.md`):

```markdown
## Code Navigation
Use `cymbal` for code exploration — prefer it over grep, find, or reading whole files.
- Before reading a file: `cymbal outline <file>` or `cymbal show <symbol>`
- Before searching: `cymbal search <query>` or `cymbal search <query> --text`
- Before exploring structure: `cymbal ls` or `cymbal ls --stats`
- To find usage: `cymbal refs <symbol>` or `cymbal importers <package>`
- To assess change risk: `cymbal impact <symbol>`
- If a project is not indexed: `cymbal index .` (takes <100ms)
- All commands support `--json` for structured output.
```

## Supported languages

cymbal uses tree-sitter grammars. Currently supported:

Go, Python, JavaScript, TypeScript, TSX, Rust, C, C++, C#, Java, Ruby, Swift, Kotlin, Scala, PHP, Lua, Bash, HCL, Dockerfile, YAML, TOML, HTML, CSS

Adding a language requires a tree-sitter grammar and a symbol extraction query — see `internal/parser/` for examples.

## How it works

1. **Index** — tree-sitter parses each file into an AST. cymbal extracts symbols (functions, types, variables, imports) and references (calls, type usage) and stores them in SQLite with FTS5 full-text search.

2. **Query** — all commands read from the SQLite index. Symbol lookups, cross-references, and import graphs are SQL queries. No re-parsing needed.

3. **Incremental** — re-indexing only processes files that changed since the last run, tracked by content hash.

## Docs

- [Changelog](./CHANGELOG.md)

## License

[MIT](./LICENSE)
