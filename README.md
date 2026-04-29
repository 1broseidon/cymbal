# cymbal

[![GitHub Stars](https://img.shields.io/github/stars/1broseidon/cymbal?style=social)](https://github.com/1broseidon/cymbal/stargazers)
[![Go Reference](https://pkg.go.dev/badge/github.com/1broseidon/cymbal.svg)](https://pkg.go.dev/github.com/1broseidon/cymbal)
[![Go Report Card](https://goreportcard.com/badge/github.com/1broseidon/cymbal)](https://goreportcard.com/report/github.com/1broseidon/cymbal)
[![codecov](https://codecov.io/gh/1broseidon/cymbal/branch/main/graph/badge.svg)](https://codecov.io/gh/1broseidon/cymbal)
[![Latest Release](https://img.shields.io/github/v/release/1broseidon/cymbal)](https://github.com/1broseidon/cymbal/releases/latest)

cymbal is a fast, language-agnostic code navigator. It parses your codebase
into a local SQLite index, then answers symbol lookups, cross-references,
impact analysis, and relationship queries in milliseconds from your terminal or
from an AI agent.

Use it when you need:

- A CLI for understanding an unfamiliar repo without bouncing between `grep`,
  `find`, and ad hoc file reads
- An agent-facing code navigation layer that replaces long chains of
  search/show/refs calls with one focused command
- A Go library for embedding indexed code navigation in editor tooling, bots,
  or internal automation

## Contents

- [Documentation](#documentation)
- [Install](#install)
- [Quick Start](#quick-start)
- [Why Cymbal](#why-cymbal)
- [Commands at a Glance](#commands-at-a-glance)
- [Graph Mode](#graph-mode)
- [How It Works](#how-it-works)
- [Benchmarks](#benchmarks)
- [AI Agents](#ai-agents)
- [Use as a Library](#use-as-a-library)
- [Supported Languages](#supported-languages)
- [License](#license)

## Documentation

| For | Start here |
|---|---|
| Operators / CLI users | [Quick Start](#quick-start) · [Commands at a Glance](#commands-at-a-glance) · [docs/reference/commands.md](docs/reference/commands.md) |
| AI agents / integrations | [AI Agents](#ai-agents) · [docs/AGENT_HOOKS.md](docs/AGENT_HOOKS.md) · [docs/guide/agent-native.md](docs/guide/agent-native.md) |
| Go library consumers | [Use as a Library](#use-as-a-library) · [docs/guide/library.md](docs/guide/library.md) |
| Contributors / evaluators | [How It Works](#how-it-works) · [Benchmarks](#benchmarks) · [CHANGELOG.md](CHANGELOG.md) |
| Full docs site | [docs/index.md](docs/index.md) · [docs/guide/getting-started.md](docs/guide/getting-started.md) |

## Install

**Homebrew** (macOS / Linux):

```sh
brew install 1broseidon/tap/cymbal
```

**Arch Linux** (AUR, community-maintained):

```sh
yay -S cymbal
```

Without an AUR helper, build the package from
[aur.archlinux.org/packages/cymbal](https://aur.archlinux.org/packages/cymbal).

**Windows** (PowerShell):

```powershell
irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex
```

To uninstall on Windows (keeping index data by default):

```powershell
# Remove binary and PATH entry, keep SQLite indexes
irm https://raw.githubusercontent.com/1broseidon/cymbal/main/uninstall.ps1 | iex

# Also remove all SQLite indexes
& ([scriptblock]::Create((irm https://raw.githubusercontent.com/1broseidon/cymbal/main/uninstall.ps1))) -Purge
```

> **Note:** `-Purge` removes all per-repo SQLite indexes stored under
> `%LOCALAPPDATA%\cymbal\repos\`. Omit it to keep indexes intact for a later
> reinstall.

**Go** (requires CGO for tree-sitter + SQLite):

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Or download a binary from [releases](https://github.com/1broseidon/cymbal/releases).

### Docker

No local Go toolchain or CGO setup required:

```sh
docker pull ghcr.io/1broseidon/cymbal:latest
```

Mount a repo and run cymbal inside the container:

```sh
# Index a repo
docker run --rm -v /path/to/repo:/workspace ghcr.io/1broseidon/cymbal index .

# Query it
docker run --rm -v /path/to/repo:/workspace ghcr.io/1broseidon/cymbal investigate handleAuth

# Optional shell alias for repeated use
alias cymbal='docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal'
```

By default the SQLite index lands at `/workspace/.cymbal/index.db` inside the
mounted repo via `CYMBAL_DB`. Add `.cymbal/` to `.gitignore` if you use the
container flow regularly.

### Updating Cymbal

`cymbal` can show a cached update notice during normal interactive use, but it
never self-updates by default.

- Homebrew: `brew upgrade 1broseidon/tap/cymbal`
- Arch Linux (AUR): update with your AUR helper, for example `yay -Syu cymbal`
- Windows PowerShell: `irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex`
- Docker: `docker pull ghcr.io/1broseidon/cymbal:latest` (or the tagged image cymbal suggests)
- Go: `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest`
- Manual binary: download the latest release from GitHub

Environment overrides:

- `CYMBAL_NO_UPDATE_NOTIFIER=1` disables passive update notices
- `CYMBAL_INSTALL_METHOD=homebrew|powershell|docker|go|manual` overrides install detection
- `CYMBAL_UPDATE_COMMAND="..."` overrides the suggested update command

## Quick Start

Command at a glance:

```sh
cymbal investigate Foo      # one call -> source + callers + impact or members
cymbal trace Foo --graph    # visual dependency map
cymbal impact Foo           # upstream blast radius
```

Common first session:

```sh
# Optional warm-up; queries auto-build the index on first use
cymbal index .

# Start with the adaptive command
cymbal investigate handleAuth
cymbal investigate UserModel

# Follow dependencies and impact
cymbal trace handleAuth
cymbal impact handleAuth

# Drop to specific commands when you need more control
cymbal search handleAuth
cymbal search PatchMulti MultiEdit EditTool PatchTool
cymbal search "TODO" --text
cymbal search --text 'os\.WriteFile\(' tools/file.go tools/patch.go
cymbal show handleAuth
cymbal outline internal/auth/handler.go
cymbal refs handleAuth
cymbal importers internal/auth
cymbal context handleAuth
cymbal ls --stats
```

The index auto-builds on first use. After that, queries auto-refresh
incrementally and only reparse changed files.

## Why Cymbal

For both humans and agents, three commands cover most investigations:

| Command | Question it answers | Direction |
|---|---|---|
| `investigate X` | "Tell me about X" | Adaptive: source + callers + impact or members |
| `trace X` | "What does X depend on?" | Downward: callees |
| `impact X` | "What depends on X?" | Upward: callers |

This matters because a normal code-reading loop often turns into
search -> show -> refs -> show-next-function -> repeat. Cymbal collapses that
into fewer, more relevant tool calls with structured output.

## Commands at a Glance

| Command | What it does |
|---|---|
| `investigate` | **Start here.** Kind-adaptive exploration in one call |
| `structure` | Structural overview: entry points, hotspots, central packages |
| `trace` | Downward call graph. Add `--graph` for a visual dependency map |
| `impact` | Upward caller graph. Add `--graph` for a visual blast-radius map |
| `importers` | Reverse import lookup. Add `--graph` for a visual fan-in map |
| `impls` | Find implementers / conformers / extensions. Add `--graph` for a conformance map |
| `search` | Symbol search, or `--text` for grep-style lookup |
| `show` | Display a symbol's source code, or a specific file range |
| `outline` | List symbols in a file |
| `refs` | Find references / call sites. Use `--file` to scope by path |
| `context` | Bundled view: source + types + callers + imports |
| `ls` | File tree, repo list, or `--stats` overview |
| `diff` | Git diff scoped to a symbol's line range |
| `hook` | Agent-integration helpers: `nudge`, `remind`, `install <agent>` |
| `version` | Build info and cached release status |

Commands that accept symbols support batch mode:

```sh
cymbal investigate Foo Bar Baz
```

All commands support `--json` for structured output. For full flags and
examples, see [docs/reference/commands.md](docs/reference/commands.md).

## Graph Mode

Add `--graph` to `trace`, `impact`, `importers`, or `impls` when you want a
high-level relationship map rather than call-site detail.

```sh
cymbal trace handleAuth --graph
cymbal impact handleAuth --graph
cymbal importers internal/auth --graph
cymbal impls io.Reader --graph
```

- Use graph mode for orientation, fan-in/fan-out, blast radius, and
  inheritance / conformance maps.
- Stay with the normal text or JSON output when you need exact call sites,
  source snippets, or line-by-line detail for an edit.
- Mermaid is the default on a TTY. JSON is the default when piped.
- Use `--graph-format mermaid|dot|json` to force a format.
- Use `--graph-limit <n>` to cap dense graphs by degree.
- `impact --graph` defaults to depth `1` unless you explicitly pass `--depth`.
- On symbol graphs, `--include-unresolved` surfaces external relationships as
  dashed `ext:` nodes.

## How It Works

1. **Index** — tree-sitter parses each file into an AST. Cymbal extracts
   symbols, imports, and references into SQLite with FTS5-backed symbol search.
2. **Query** — commands read from the current repo's local index instead of
   reparsing the world every time.
3. **Stay fresh** — before each query, cymbal checks for changed files and
   incrementally reparses only what changed. No daemon or watch process is
   required.

Each repo gets its own database under the OS cache directory:

- Linux: `~/.cache/cymbal/repos/<hash>/index.db`
- macOS: `~/Library/Caches/cymbal/repos/<hash>/index.db`
- Windows: `%LOCALAPPDATA%\cymbal\repos\<hash>\index.db`

Override with `--db <path>` or `CYMBAL_DB` when needed.

## Benchmarks

The benchmark harness lives in `bench/` and runs against a pinned corpus of
real repos across Go, Python, TypeScript, Rust, Java, C, C#, PHP, Lua, and
Bash.

```sh
go run ./bench setup   # clone pinned corpus repos
go run ./bench run     # run full benchmarks -> bench/RESULTS.md
go run ./bench check   # compare current build against baseline
```

Recent corpus results:

- **Accuracy:** 113/113 top-level checks passed
- **Ground truth:** 79/79 passed, with 100% search precision/recall and 100%
  `show` exactness
- **Canonical ranking:** 18/18 correct at rank 1
- **Grep footguns:** 11/11 passed
- **Latency:** most symbol and text query commands on the benchmark corpus
  complete in roughly 10-40ms; hot incremental refresh stays in the low tens
  of milliseconds
- **Agent workflow savings:** focused investigations typically use 40-100% fewer
  tokens than comparable grep-driven flows

## AI Agents

Cymbal is designed to be an agent's code navigation layer, but the README only
summarizes the integration story. The full install snippets and hook wiring
live in the dedicated docs:

- [docs/AGENT_HOOKS.md](docs/AGENT_HOOKS.md) — OpenCode and Claude Code install,
  `nudge`, `remind`, and snippets for other agent runtimes
- [docs/guide/agent-native.md](docs/guide/agent-native.md) — frontmatter output
  format and why it is cheaper than JSON by default

If you are writing agent instructions, the short policy is:

- Start with `cymbal investigate <symbol>`
- Use `cymbal trace <symbol>` for downward dependency flow
- Use `cymbal impact <symbol>` for change risk
- Use `cymbal show <file:L1-L2>` or `cymbal outline <file>` before broad file reads
- Use `cymbal search <query>` before raw grep
- Batch symbol searches as `cymbal search Foo Bar Baz`
- Use `cymbal search --text <pattern> [path ...]` for scoped literal or regex searches
- Add `--graph` when the agent needs topology, not call-site text

OpenCode has a one-line installer:

```sh
cymbal hook install opencode
```

This installs a cymbal-managed OpenCode plugin under the documented plugin
directory for the chosen scope. The plugin refreshes session guidance via
`cymbal hook remind --update=if-stale` and soft-nudges bash grep/find/fd usage
back toward cymbal-first navigation on non-Windows shells. Reminder/update guidance stays fresh
without editing `AGENTS.md`. Cymbal still never self-updates by default; it
only tells the agent or user which explicit update command to run.

Claude Code also has a one-line installer:

```sh
cymbal hook install claude-code
```

## Use as a Library

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go get github.com/1broseidon/cymbal@latest
```

Exported packages:

| Package | What it does |
|---|---|
| `index` | Indexing engine, SQLite store, and public query APIs |
| `lang` | Language registry for names, extensions, special filenames, and parser availability |
| `parser` | Tree-sitter parsing |
| `symbols` | Core data types (`Symbol`, `Import`, `Ref`) |
| `walker` | Concurrent file discovery with language detection |

Small example:

```go
import "github.com/1broseidon/cymbal/index"

stats, _ := index.Index("/path/to/repo", "", index.Options{})
dbPath, _ := index.RepoDBPath("/path/to/repo")
results, _ := index.SearchSymbols(dbPath, index.SearchQuery{Text: "handleAuth"})

_ = stats
_ = results
```

For streaming patterns, lower-level store access, and more complete examples,
see [docs/guide/library.md](docs/guide/library.md).

## Supported Languages

Cymbal currently parses and indexes:

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

Cymbal also recognizes additional file types for classification and CLI path
heuristics even when they are not parseable/indexable, including
`Dockerfile`, `Makefile`, `Jenkinsfile`, `CMakeLists.txt`, JSON, TOML,
Markdown, SQL, Vue, Svelte, Zig, Erlang, Haskell, OCaml, R, and Perl.

Adding a language requires a tree-sitter grammar plus symbol / import / ref
extraction support.

## License

[MIT](./LICENSE)
