# cymbal

A code navigation CLI that indexes a repository with tree-sitter, then answers
structural questions about it — what a symbol is, who calls it, what it calls,
and what breaks if it changes.

For people, it replaces a chain of `grep` and jump-to-definition. For agents, it
turns a dozen file reads into one call with `--json`.

## Overview

Cymbal indexes a repository once, then answers structural questions about it
from that index. Not "which files contain this string" — what is this symbol,
who calls it, what does it call, and what breaks if it changes.

The index is SQLite, per repo, built by tree-sitter parsers. Indexing is a
one-time cost measured in milliseconds for most repos; every query after that
reads the database rather than the filesystem. There is no daemon, no server,
and no model in the loop — every answer is derived from the index.

> For people, it replaces a chain of `grep`, `find` and jump-to-definition. For
> agents, it collapses what would be a dozen file reads into a single call with
> `--json` and documented exit codes.

## Install

Homebrew is the shortest path on macOS and Linux.

```console title="Install"
$ brew install 1broseidon/tap/cymbal
```

If you are pointing a coding agent at a fresh repo, hand it this instead.

```console title="Or hand it to your agent"
Install cymbal and index this repo for me.
1. Run: brew install 1broseidon/tap/cymbal
2. Run `cymbal index .` at the repo root.
3. Run `cymbal structure` and summarise the entry points for me.
4. Run `cymbal hook install claude-code` so you keep using it.
From here on, prefer `cymbal search` over grep for symbol lookup,
`cymbal show <sym>` over reading a whole file, and
`cymbal impact <sym>` before changing anything shared.
```

#### Arch Linux — AUR, community-maintained

```console
$ yay -S cymbal
```

Without an AUR helper, build from
[aur.archlinux.org/packages/cymbal](https://aur.archlinux.org/packages/cymbal).

#### Windows — PowerShell

```console
> irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex
```

To uninstall, run `uninstall.ps1` the same way. It keeps your indexes by
default; pass `-Purge` to also delete everything under
`%LOCALAPPDATA%\cymbal\repos\`.

#### Go — requires CGO for tree-sitter and SQLite

```console
$ CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

The FTS5 flag is not optional — text search depends on it.

#### Docker — no Go toolchain, no CGO setup

```console
$ docker pull ghcr.io/1broseidon/cymbal:latest

# Mount a repo and run cymbal inside the container
$ docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal index .

# Worth aliasing if you use this flow regularly
$ alias cymbal='docker run --rm -v "$(pwd)":/workspace ghcr.io/1broseidon/cymbal'
```

The index lands at `/workspace/.cymbal/index.db` inside the mounted repo. Add
`.cymbal/` to your `.gitignore`.

#### Binaries — direct download

Prebuilt binaries for each platform are attached to every
[release](https://github.com/1broseidon/cymbal/releases).

## Quickstart

Index once, then ask. Indexing a subdirectory updates that subtree in place —
files outside it are neither reparsed nor pruned.

```console
$ cymbal index .
indexed 105 files · 1921 symbols

# "I've never seen this repo — where do I start?"
$ cymbal structure

# Everything about one symbol, shaped to what it is
$ cymbal investigate OpenStore

# What breaks if I change this?
$ cymbal impact handleRegister -D 3
```

Queries refresh the index automatically, so you rarely re-run `index` by hand.
Pass `-f` to force a full re-index.

## Choosing a command

Three commands cover most questions, and they differ only in which way they walk
the call graph. Getting this right is most of the tool.

| The question | Use | Direction |
| --- | --- | --- |
| Tell me about X | `investigate` | adapts to the symbol's kind |
| What does X depend on? | `trace` | downward — X's callees |
| What depends on X? | `impact` | upward — X's callers |
| Just show me the code | `show` | no graph walk |
| Source, types, callers and imports at once | `context` | one bundled read |
| What did I just break? | `changed` | git diff → symbols → impact |

> `investigate` is the one to reach for when you don't know which of the others
> you want. It looks at the symbol's kind and returns the right shape: functions
> get source plus callers plus shallow impact; types get source plus members plus
> references.

## Commands

Nineteen commands. Four flags are global; everything else is per-command.

| Flag | Meaning |
| --- | --- |
| `-d, --db <path>` | override the database path (default: auto-resolved per repo) |
| `--json` | structured output instead of frontmatter + content |
| `--no-federate` | restrict to a single database, no cross-worktree federation |
| `-v, --version` | print version and exit |

Passive update notices are suppressed automatically under `--json`. Set
`CYMBAL_NO_UPDATE_NOTIFIER=1` to disable them entirely.

### Graph output

`trace`, `impact`, `importers` and `impls` accept `--graph` when you want a
relationship map rather than call-site detail. The default format is Mermaid on
a TTY and JSON when piped; `--graph-format mermaid|dot|json` forces one.
`--graph-limit <n>` caps dense graphs by degree, and `impact --graph` defaults
to depth 1 unless you pass `--depth` yourself.

Stay on the normal text or JSON output when you need exact source lines or call
sites you intend to edit against.

#### investigate — Kind-adaptive context for what a symbol is

```console
$ cymbal investigate OpenStore
$ cymbal investigate config.go:Config   # file hint
$ cymbal investigate auth.Middleware    # package hint
$ cymbal investigate Foo Bar Baz        # batch
```

| Flag | Meaning |
| --- | --- |
| `--resolve-scope` | same \| family \| all — cross-language name resolution (default family) |
| `--stdin` | read newline-separated names from stdin |

#### search — Symbols by default, text with --text

Ranked exact > prefix > fuzzy. Trailing path operands act as `--path` filters,
so grep-shaped calls work as written.

```console
$ cymbal search ParseFile
$ cymbal search --text TODO cmd internal/foo.go
$ cymbal search Foo Bar        # independent queries
```

| Flag | Meaning |
| --- | --- |
| `-t, --text` | full-text grep across file contents |
| `-e, --exact` | exact name match only |
| `-i, --ignore-case` | case-insensitive exact (implies --exact; not with --text) |
| `-k, --kind` | filter by symbol kind — function, class, method… |
| `-l, --lang` | filter by language |
| `-n, --limit` | max results (default 20) |
| `--path, --exclude` | glob filters, repeatable |

#### show — Read source by symbol name or file path

```console
$ cymbal show ParseFile
$ cymbal show App.handleSave                  # nested, qualified
$ cymbal show store.go:SearchSymbols          # narrowed by file hint
$ cymbal show internal/index/store.go:80-120  # line range
$ cymbal outline big.go -s --names | cymbal show --stdin
```

| Flag | Meaning |
| --- | --- |
| `--all` | show every matching definition |
| `-C, --context` | lines of context around the target |
| `--stdin` | batch from stdin; JSON returns a map keyed by name |

#### impact — Transitive callers, what breaks if this changes

```console
$ cymbal impact handleRegister
$ cymbal impact Save Load Delete   # union of callers
$ cymbal impact Save -D 3 --graph  # mermaid on a TTY
```

| Flag | Meaning |
| --- | --- |
| `-D, --depth` | max call-chain depth, capped at 5 (default 2) |
| `-C, --context` | lines around each call site (default 1) |
| `--graph` | render as a graph — mermaid on TTY, json when piped |
| `--graph-format` | mermaid \| dot \| json |
| `--graph-limit` | cap at top-N nodes by degree |
| `--no-tests` | exclude callers in test files |
| `--include-unresolved` | keep external calls as dashed ext: nodes |

Multi-symbol runs dedupe callers and attach a `hit_symbols` list recording which
requested symbol brought each one in.

#### trace — Downward call graph, what does this call

Follows invocation edges only by default. Callees that don't resolve to an
indexed symbol — stdlib, third-party, builtins — are filtered out unless you ask
for them.

```console
$ cymbal trace handleRequest
$ cymbal trace pkg/file.go:Name --include-unresolved
```

| Flag | Meaning |
| --- | --- |
| `--kinds` | broaden beyond call edges, e.g. type mentions |
| `--resolve-scope` | same \| family \| all (default family) |
| `--include-unresolved` | keep unresolved callees |

#### context — Source, referenced types, callers and imports in one call

```console
$ cymbal context OpenStore
$ cymbal context ParseFile --callers 10
```

#### impls — What implements, conforms to, or extends a name

Covers Swift protocol conformance, Go interface embedding, Java/C#/Kotlin/TypeScript
implements clauses, Scala with-chains, Rust trait impls, Dart mixins, Python base
classes, Ruby include/extend, PHP implements and C++ base classes.

```console
$ cymbal impls Reader
$ cymbal impls Reader Writer Closer
$ cymbal impls Plugin --lang go
$ cymbal impls --of TimerActivityIntent   # inverse direction
```

Best-effort, based on AST name matching. External framework targets come back
with `resolved=false`.

#### changed — Diff-scoped impact, what your current edits touch

```console
$ cymbal changed              # unstaged, working tree vs index
$ cymbal changed --staged     # staged, index vs HEAD
$ cymbal changed --base main  # working tree vs a ref
```

Changed symbols are attributed by parsing the diffed blobs on both sides, so
whole-symbol deletions are named rather than mis-attributed to a neighbour.
Deleted symbols are listed but have no impact — they no longer exist.

#### diff — Git diff scoped to one symbol's line range

```console
$ cymbal diff ParseFile          # vs HEAD
$ cymbal diff ParseFile main     # vs a branch
$ cymbal diff --stat ParseFile   # diffstat only
```

#### refs — Reference sites, best-effort

| Flag | Meaning |
| --- | --- |
| `--importers` | files importing the defining file |
| `--impact` | shorthand for --importers --depth 2 |
| `--file` | restrict to files including a path fragment |
| `-D, --depth` | import chain depth, max 3 |

#### outline — Symbols defined in a file

```console
$ cymbal outline internal/index/store.go -s
$ cymbal outline big.go --names | cymbal investigate --stdin
```

| Flag | Meaning |
| --- | --- |
| `-s, --signatures` | show full parameter signatures |
| `--names` | one name per line, pipe-friendly |

#### structure — Entry points, hotspots, central packages

Entry points, most-referenced symbols, most-imported files, largest packages —
all derived from the index. No AI, no guessing.

```console
$ cymbal structure -n 20
```

#### ls — File tree, inventory, repos, or stats

```console
$ cymbal ls --stats           # languages, file and symbol counts
$ cymbal ls --names --lang go
$ cymbal ls --repos
```

| Flag | Meaning |
| --- | --- |
| `--names` | flat list of indexed paths |
| `--null` | NUL-terminate for xargs -0 |
| `-D, --depth` | max tree depth |

#### importers — Reverse import lookup

```console
$ cymbal importers internal/index/store.go -D 2
```

#### index — Build or refresh the index

| Flag | Meaning |
| --- | --- |
| `-f, --force` | re-index every file |
| `-w, --workers` | parallel workers (0 = NumCPU) |
| `--exclude` | skip paths matching a glob, repeatable |
| `--include-generated` | index generated files skipped by default |
| `--include-large-files` | index large sources skipped by default |

#### hook — Keep an agent using cymbal instead of grep

```console
$ cymbal hook install claude-code
$ cymbal hook install opencode
```

Subcommands: `install`, `uninstall`, `nudge`, `remind`, `notify`. The
agent-agnostic three can be wired into any harness with hook points.

#### version — Version, commit, build date

```console
$ cymbal version
cymbal v0.15.0
  commit: ac79440
  go:     go1.26.7 linux/amd64
```

## For agents

Cymbal was built to be called by something that isn't a person. Four properties
matter for that.

### Frontmatter, not JSON

The default output is YAML frontmatter followed by a content body — metadata an
agent can parse, then source it can read. JSON quotes every field name and
escapes every string; on the same `refs` result the frontmatter form runs about
a third fewer tokens, which compounds across dozens of calls in one task.

```console
---
symbol: handleAuth
total: 3
groups: 2
---

cmd/server/main.go (1 site):
  > handleAuth(w, r)

internal/api/router.go (2 sites):
  > mux.HandleFunc("/auth", handleAuth)
  > handleAuth(w, r)
```

Identical call sites in the same file are grouped, so the agent sees the pattern
without paying for the repetition.

### --json everywhere

Every command takes the global `--json` flag and returns structured output.
Batch commands key their result map by the name you asked for, so a single call
can dispatch several lookups.

### Batching over round-trips

Most query commands accept multiple symbols, and `--stdin` reads
newline-separated names. One call beats five.

```console
$ cymbal show Foo Bar Baz
$ cymbal outline svc.go -s --names | cymbal investigate --stdin
```

### Hooks that hold the line

Agents drift back to raw `grep` as their context fills. Prompting alone erodes;
two agent-agnostic subcommands wire into whatever hook point a runtime offers.

| Command | What it does |
| --- | --- |
| `cymbal hook nudge` | Inspects a would-be shell command and suggests the cymbal equivalent when it looks like a code search. Never blocks, silent when it has nothing to say |
| `cymbal hook remind` | Prints a reminder block to inject at session start |

Claude Code and OpenCode have first-class installers. Cursor, Windsurf, aider,
Cline, Continue, Zed and the OpenAI Agents SDK wire the two subcommands in by
hand — [HOOKS.md](HOOKS.md) has the snippet for each.

| Instead of | Use |
| --- | --- |
| Reading three files to find what calls `handleAuth` | `cymbal impact handleAuth --json` — one call, with call sites |
| `grep -rn "func ParseFile"`, then opening the file, then scrolling | `cymbal show ParseFile` — the definition, nothing else |

## Languages

Thirty-nine languages are registered, in two tiers. Twenty-two ship a
tree-sitter grammar and are parsed into symbols; the rest are recognised for the
file inventory and text search but produce no symbol graph.

### Parsed to symbols — 22

go · python · javascript · typescript · tsx · rust · ruby · java · c · cpp ·
csharp · dart · swift · kotlin · lua · php · bash · scala · yaml · elixir · hcl ·
protobuf

### Recognised only — 17

apex · zig · toml · json · markdown · sql · erlang · haskell · ocaml · r · perl ·
vue · svelte · make · dockerfile · groovy · cmake

> Name resolution is scoped to a language family by default — JVM groups
> java/kotlin/scala, JS groups javascript/typescript/tsx, C groups c/cpp. Use
> `--resolve-scope same` for exact-language only, or `all` to resolve across
> everything.

## Notes

### References are name-scoped

Cymbal resolves references by name against the AST, not by full semantic
analysis. When a name has several definitions, counts may span them — that is
reported as `definition_count` rather than hidden. Treat `refs` and `impls` as
high-quality leads, not proofs.

### The index follows the working tree

`changed` answers "what is affected now", so its impact data comes from the
working-tree index. Arbitrary commit ranges whose new side isn't the working
tree aren't supported.

### Federation across worktrees

Symbols federate across worktrees of the same repo by default, so a query in one
worktree can resolve into another. Pass `--no-federate` to restrict to a single
database.
