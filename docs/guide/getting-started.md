# Getting Started

## Install

**Homebrew** (macOS / Linux):

```sh
brew install 1broseidon/tap/cymbal
```

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex
```

**Go** (requires CGO for tree-sitter + SQLite):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Or grab a binary from [releases](https://github.com/1broseidon/cymbal/releases).

## Updating cymbal

`cymbal` can show a cached update notice during normal interactive use, but it does not self-update by default.

- **Homebrew**: `brew upgrade 1broseidon/tap/cymbal`
- **Windows (PowerShell)**: `irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex`
- **Docker**: `docker pull ghcr.io/1broseidon/cymbal:latest` (or the tagged image suggested by cymbal)
- **Go**: `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest`
- **Manual binary**: download the latest release from GitHub

If you want to disable passive update notices entirely, set `CYMBAL_NO_UPDATE_NOTIFIER=1`.

## Quick Start

```sh
# Investigate any symbol — one call, right answer
cymbal investigate handleAuth
cymbal investigate UserService

# Trace execution downward
cymbal trace handleAuth

# Assess upstream impact
cymbal impact handleAuth

# Use specific commands when you need more control
cymbal search handleAuth
cymbal show handleAuth
cymbal outline internal/auth/handler.go
cymbal refs handleAuth
cymbal context handleAuth
cymbal ls --stats
```

The index auto-builds on first use, so a manual `cymbal index .` step is optional rather than required.

## Agent Integration

If you use OpenCode, the main supported setup is a one-line installer:

```sh
cymbal hook install opencode
```

This installs a cymbal-managed plugin into OpenCode's plugin directory for the
chosen scope. The plugin refreshes startup guidance with
`cymbal hook remind --update=if-stale` and soft-nudges bash grep/find/fd-style
commands back toward cymbal-first navigation on non-Windows shells. Update guidance stays fresh
without editing `AGENTS.md`. Cymbal still does not self-update by default; it
only surfaces the explicit update command to run.

cymbal can also be used through plain agent instructions when a runtime does
not support native plugins or hooks.

cymbal is designed to be an AI agent's code comprehension layer. Add this to your `CLAUDE.md` (or equivalent agent instructions):

```markdown
## Code Exploration Policy
Use `cymbal` CLI for code navigation — prefer it over Read, Grep, Glob, or Bash for code exploration.
- **New to a repo?**: `cymbal structure` — entry points, hotspots, central packages. Start here.
- **To understand a symbol**: `cymbal investigate <symbol>` — returns source, callers, impact, or members based on what the symbol is.
- **To understand multiple symbols**: `cymbal investigate Foo Bar Baz` — batch mode, one invocation.
- **To trace an execution path**: `cymbal trace <symbol>` — follows the call graph downward.
- **To assess change risk**: `cymbal impact <symbol>` — follows the call graph upward.
- Before reading a file: `cymbal outline <file>` or `cymbal show <file:L1-L2>`
- Before searching: `cymbal search <query>` (symbols) or `cymbal search <query> --text` (grep)
- Before exploring structure: `cymbal ls` (tree) or `cymbal ls --stats` (overview)
- To find usage: `cymbal refs <symbol>` or `cymbal refs <symbol> --importers`
- The index auto-builds on first use — no manual indexing step needed. Queries auto-refresh incrementally.
- Use `cymbal show <symbol>` to read a specific function/type instead of reading the whole file.
- All commands support `--json` for structured output.
```

This tells the agent to prefer cymbal over grep/find/cat, reducing tool calls and token usage while giving the agent structured, relevant context.

## Supported Languages

cymbal currently parses and indexes:

Go, Python, JavaScript, TypeScript, Rust, C, C++, C#, Java, Apex, Ruby, Swift, Kotlin, Scala, PHP, Lua, Bash, YAML, Elixir, HCL/Terraform, Protobuf, and Dart.

Notable extension coverage includes `.pyw`, `.mjs`, `.cjs`, `.mts`, `.cts`, `.kts`, `.rake`, `.gemspec`, `.sc`, `.tfvars`, `.cxx`, `.hxx`, and `.hh`.

cymbal also recognizes additional file types for classification and CLI path heuristics, even when they are not parseable/indexable: `Dockerfile`, `Makefile`, `Jenkinsfile`, `CMakeLists.txt`, JSON, TOML, Markdown, SQL, Vue, Svelte, Zig, Erlang, Haskell, OCaml, R, and Perl.

Adding a language requires a tree-sitter grammar and a symbol extraction query.
