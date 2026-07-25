# Commands

All commands support `--json` for structured output. Each repo gets its own database in the OS cache directory, auto-resolved from your working directory (`~/.cache/cymbal/repos/<hash>/index.db` on Linux, `~/Library/Caches/cymbal/repos/<hash>/index.db` on macOS, `%LOCALAPPDATA%\cymbal\repos\<hash>\index.db` on Windows).

Path/language heuristics recognize some special filenames such as `Dockerfile`, `Makefile`, `Jenkinsfile`, and `CMakeLists.txt` even though they are not parseable/indexable languages.

## Global Flags

| Flag | Description |
|------|-------------|
| `-d, --db <path>` | Override path to cymbal database (default: auto-resolved per repo) |
| `--json` | Output as JSON instead of frontmatter+content |

Passive update notices are suppressed automatically for `--json` output. Set `CYMBAL_NO_UPDATE_NOTIFIER=1` to disable passive update notices entirely.

---

## Graph Output

Add `--graph` to `trace`, `impact`, `importers`, or `impls` when you want a
high-level relationship map rather than call-site detail.

- Default format is Mermaid on a TTY and JSON when piped.
- Use `--graph-format mermaid|dot|json` to force a specific format.
- Use `--graph-limit <n>` to cap dense graphs by degree.
- `impact --graph` defaults to depth `1` unless you explicitly pass `--depth`.
- `--include-unresolved` is useful on symbol graphs when external relationships matter.

Stay with the normal text or JSON output when you need exact source lines,
call sites, or detail you will edit against.

---

## `cymbal index`

Index a directory for symbol discovery.

```sh
cymbal index [path] [flags]
```

| Flag | Description |
|------|-------------|
| `--exclude <glob>` | Exclude files whose path matches this glob during indexing (repeatable) |
| `-f, --force` | Force re-index all files |
| `--include-generated` | Index generated files that cymbal skips by default |
| `--include-large-files` | Index large source files that cymbal skips by default |
| `-w, --workers <n>` | Number of parallel workers (0 = NumCPU) |

```sh
# Index current directory
cymbal index .

# Force re-index with 8 workers
cymbal index . --force --workers 8

# Skip an additional generated subtree
cymbal index . --exclude 'src/generated/**'
```

---

## `cymbal version`

Print build/version information and cached release status.

```sh
cymbal version [--json]
```

- Human output includes the current build information and, when available, a suggested update command.
- `--json` adds a structured `update` object with `checked_at`, `cache_stale`, `available`, `latest_version`, `install_type`, `command`, `release_url`, and `source`.
- `cymbal --version` stays terse and only prints the installed version.

---

## `cymbal ls`

Show file tree, repo list, or repo statistics.

```sh
cymbal ls [path] [flags]
```

| Flag | Description |
|------|-------------|
| `-D, --depth <n>` | Max tree depth (0 = unlimited) |
| `--repos` | List all indexed repositories |
| `--stats` | Show repo overview (languages, file/symbol counts) |

```sh
# File tree
cymbal ls

# Top-level only
cymbal ls --depth 1

# Repo stats
cymbal ls --stats

# All indexed repos
cymbal ls --repos
```

---

## `cymbal outline`

Show symbols defined in a file.

```sh
cymbal outline <file> [file2 ...] [flags]
```

| Flag | Description |
|------|-------------|
| `-s, --signatures` | Show full parameter signatures |

```sh
cymbal outline internal/auth/handler.go
cymbal outline internal/auth/handler.go --signatures
cymbal outline internal/auth/handler.go internal/auth/service.go --names
```

---

## `cymbal search`

Search symbols by name, or use `--text` for full-text grep. Results are ranked: exact match > prefix > fuzzy.

```sh
cymbal search <query> [path ...] [flags]
```

Trailing path operands are accepted as `--path` filters, which matches common
`rg` usage: `cymbal search --text <pattern> cmd internal/foo.go`.
In symbol mode, multiple query arguments are searched independently:
`cymbal search Foo Bar Baz`.

| Flag | Description |
|------|-------------|
| `-t, --text` | Full-text grep across file contents |
| `-e, --exact` | Exact name match only |
| `-k, --kind <type>` | Filter by symbol kind (function, class, method, etc.) |
| `-l, --lang <name>` | Filter by language (go, python, typescript, etc.) |
| `-n, --limit <n>` | Max results (default: 20) |
| `--path <glob>` | Include only results whose path matches this glob or path fragment (repeatable) |
| `--exclude <glob>` | Exclude results whose path matches this glob or path fragment (repeatable) |
| `--stdin` | Read additional symbol queries from stdin |

```sh
# Symbol search
cymbal search handleAuth

# Batch symbol search
cymbal search PatchMulti MultiEdit EditTool PatchTool

# Full-text grep
cymbal search "TODO" --text

# Full-text grep scoped to files/directories, rg-style
cymbal search --text 'os\.WriteFile\(' tools/file.go tools/patch.go

# Only Go functions
cymbal search parse --kind function --lang go
```

---

## `cymbal show`

Read source code by symbol name or file path.

```sh
cymbal show <symbol|file[:L1-L2|:Symbol]> [flags]
```

| Flag | Description |
|------|-------------|
| `-C, --context <n>` | Lines of context around the target |

`file.go:SymbolName` looks up a symbol with a file hint: results are narrowed to files whose path ends with (or contains) the hint, falling back to global results if nothing matches. Otherwise, an argument containing `/` or ending with a known extension is treated as a file path; anything else is a symbol name.

```sh
# Show a symbol's source
cymbal show handleAuth

# Narrow a common name by file hint
cymbal show store.go:SearchSymbols

# Show a file
cymbal show internal/auth/handler.go

# Show specific lines
cymbal show internal/auth/handler.go:80-120

# Show with surrounding context
cymbal show handleAuth -C 5
```

---

## `cymbal importers`

Find files that import a given file or package.

```sh
cymbal importers <file|package> [flags]
```

| Flag | Description |
|------|-------------|
| `-D, --depth <n>` | Import chain depth (max 3, default: 1) |
| `-n, --limit <n>` | Max results (default: 50) |
| `--graph` | Render target's fan-in as a visual graph |
| `--graph-format <fmt>` | `mermaid`, `dot`, or `json` (implies `--graph`) |
| `--graph-limit <n>` | Cap the graph size by degree (0 for no cap) |

```sh
cymbal importers internal/auth
cymbal importers internal/auth --graph
```

---

## `cymbal impls`

Find types that implement an interface, or elements an explicit type implements.

```sh
cymbal impls <symbol> [flags]
```

| Flag | Description |
|------|-------------|
| `-n, --limit <n>` | Max results (default: 50) |
| `--of <type>` | Inverse: find interfaces that the given `<type>` implements |
| `--unresolved` | Only show external / unresolved targets |
| `--graph` | Render inheritance as a visual graph |
| `--graph-format <fmt>` | `mermaid`, `dot`, or `json` (implies `--graph`) |
| `--include-unresolved`| Graph unresolved external nodes as dashed `ext:` boxes |
| `--graph-limit <n>` | Cap the graph size by degree (0 for no cap) |

```sh
cymbal impls io.Reader
cymbal impls --of MyStruct --graph --include-unresolved
```

---

## `cymbal refs`

Find references to a symbol across indexed files.

```sh
cymbal refs <symbol> [flags]
```

| Flag | Description |
|------|-------------|
| `-n, --limit <n>` | Max results (default: 50) |
| `--importers` | Find files that import the defining file |
| `--impact` | Transitive impact analysis (`--importers --depth 2`) |
| `-D, --depth <n>` | Import chain depth for `--importers` (max 3, default: 1) |

References are best-effort based on AST name matching, not semantic analysis. Results are deduplicated — identical call sites in the same file are grouped.

```sh
# Direct references
cymbal refs handleAuth

# Who imports this package?
cymbal refs handleAuth --importers

# Transitive impact
cymbal refs handleAuth --impact
```

---

## `cymbal investigate`

Kind-adaptive investigation — returns the right shape of context for whatever a
symbol is, so you don't have to choose between `search`, `show`, `refs`, and
`impact`.

```sh
cymbal investigate <symbol> [symbol2 ...] [flags]
```

| Flag | Description |
|------|-------------|
| `--stdin` | Read additional symbol names (newline-separated) from stdin |
| `--resolve-scope <s>` | `same` \| `family` \| `all` (default: family) |

What you get back depends on the symbol's kind:

- function / method → source + callers + shallow impact
- class / struct / type / interface → source + members + references
- ambiguous name → auto-resolves to the best match and notes the alternatives

Disambiguate with a file or parent hint (`config.go:Config`, `auth.Middleware`),
and pass several names (or pipe newline-separated names via `--stdin`) to
investigate a batch.

```sh
cymbal investigate OpenStore
cymbal investigate config.go:Config     # file hint
cymbal investigate Foo Bar Baz          # batch
cymbal outline svc.go -s --names | cymbal investigate --stdin
```

---

## `cymbal trace`

Downward call trace — what does this symbol call? Complementary to `impact`,
which traces callers upward.

```sh
cymbal trace <symbol> [symbol2 ...] [flags]
```

| Flag | Description |
|------|-------------|
| `--depth <n>` | Max traversal depth (default: 3) |
| `-n, --limit <n>` | Max results per symbol (default: 50) |
| `--kinds <list>` | Comma-separated ref kinds to follow: `call`, `use`, `implements` (default: `call`) |
| `--stdin` | Read additional symbol names (newline-separated) from stdin |
| `--resolve-scope <s>` | `same` \| `family` \| `all` (default: family) |
| `--include-unresolved` | Keep callees that don't resolve to an indexed symbol (stdlib, third-party, builtins); dashed `ext:` nodes under `--graph` |
| `--graph` | Render the call tree as a visual graph |
| `--graph-format <fmt>` | `mermaid`, `dot`, or `json` (implies `--graph`) |
| `--graph-limit <n>` | Cap the graph size by degree (0 for no cap) |

By default `trace` follows only invocation edges (`call`) and drops callees that
don't resolve to an indexed symbol. `--include-unresolved` keeps those external
callees in the text/JSON output (and renders them as dashed `ext:` nodes under
`--graph`). Like `impact`, `trace` reports `truncated` when a per-symbol
`--limit` is hit. Pass several names (or pipe via `--stdin`) for the union of
callees, deduplicated with a `hit_symbols` attribution list.

```sh
cymbal trace handleRegister                      # call chain (depth 3)
cymbal trace handleRegister --depth 5            # deeper trace
cymbal trace Save Load Delete                    # union of callees
cymbal trace handleRegister --include-unresolved # keep stdlib/external calls
cymbal outline svc.go -s --names | cymbal trace --stdin
```

---

## `cymbal impact`

Transitive caller analysis — what is impacted if a symbol changes.

```sh
cymbal impact <symbol> [symbol2 ...] [flags]
```

| Flag | Description |
|------|-------------|
| `-D, --depth <n>` | Max call-chain depth (max 5, default: 2) |
| `-n, --limit <n>` | Max callers per symbol (default: 50) |
| `--no-tests` | Exclude callers in test files (keeps production + unknown) |
| `--test-path <pat>` | Classify matching paths as test code, in addition to the built-in conventions (substring, or glob with `**`; repeatable) |
| `--resolve-scope <s>` | `same` \| `family` \| `all` (default: family) |

Callers are classified by file path as **production**, **test**, or **unknown**,
so the header reports the real blast radius rather than a bare count:

```sh
$ cymbal impact Index --depth 1
total_callers: 29 (5 production, 24 test)
references: 39 (8 production, 31 test) in 12 (4 production, 8 test)
```

- `truncated: true` is shown (and emitted in `--json`) when a per-symbol
  `--limit` was hit, so a partial caller set is never presented as complete.
- The `references` line / JSON `metrics` block are exact, un-truncated counts of
  references to the symbol (reference sites and distinct files, split by class) —
  true breadth even when callers are capped. They are name-scoped: an ambiguous
  name (several definitions) is flagged via `definition_count` / `definitions`.
- `--no-tests` drops test-file callers; classification happens during traversal,
  so test callers never consume the `--limit` budget ahead of production ones.
  In `--graph` mode, hidden test nodes are contracted: a production caller
  reachable only through a test helper stays connected to the seed via a dashed
  edge marked `"indirect": true` in the JSON.
- `--test-path` extends the built-in test classification with repo-specific
  patterns (e.g. `--test-path qa/ --test-path '**/*_it.go'`) for layouts the
  conventions don't cover. It affects the caller split, `--no-tests`, and the
  `metrics` block on both `impact` and `changed`.

```sh
# only the production callers worth inspecting
cymbal impact Index --no-tests

# treat the qa/ tree as test code too
cymbal impact Index --no-tests --test-path qa/
```

`trace` likewise reports `truncated` when its `--limit` is hit.

---

## `cymbal changed`

Diff-scoped impact — map a git diff to the symbols it touches and report each
one's references and transitive impact in a single call.

```sh
cymbal changed [flags]
```

| Flag | Description |
|------|-------------|
| `--staged` | Diff the staged changes (index vs HEAD) instead of the working tree |
| `--base <ref>` | Diff the working tree against another single ref (e.g. `main`) |
| `-D, --depth <n>` | Max call-chain depth for impact (max 5, default: 2) |
| `-n, --limit <n>` | Max callers per changed symbol (default: 50) |
| `--max-symbols <n>` | Max changed symbols to analyze (default: 40, 0 = unlimited) |
| `--max-impact <n>` | Soft cap on total caller rows across symbols (default: 500) |
| `--no-tests` | Exclude callers in test files from impact |
| `--test-path <pat>` | Classify matching paths as test code, in addition to the built-in conventions (substring, or glob with `**`; repeatable) |
| `--resolve-scope <s>` | `same` \| `family` \| `all` (default: family) |

Defaults to **unstaged** working-tree changes (`git diff`). Changed symbols are
attributed by parsing the actual diffed blobs on both sides: added/modified lines
map to symbols in the new version, deleted lines to symbols in the old version.
So whole-symbol deletions are **named** (listed under `deleted`), `--staged`
attribution matches the staged content even with unstaged edits present, and
each changed line maps to its enclosing navigable definition.

```sh
# what does my uncommitted edit affect?
$ cymbal changed
changed_symbols: 1
base: working tree
---
# ClassifyPath  (index/classify.go)
  references: 6 (4 production, 2 test) in 6 (4 production, 2 test)
  impact: 14 (12 production, 2 test) callers
```

```sh
cymbal changed --staged        # staged changes (index vs HEAD)
cymbal changed --base main     # working tree vs your branch point
cymbal changed --json          # full structured payload for agents
```

Operates only on the current worktree. Counts are name-scoped (cymbal resolves
references by name); arbitrary commit ranges (`a..b`) and deleted-symbol impact
are out of scope. Deleted/binary/unsupported files are reported, not silently
dropped.

---

## `cymbal hook`

Agent-integration helpers for session reminders, shell nudges, and supported
agent installers.

```sh
cymbal hook <subcommand> [flags]
```

### `cymbal hook remind`

Print the short persistent guidance block agents should treat as system
context.

```sh
cymbal hook remind [flags]
```

| Flag | Description |
|------|-------------|
| `--format <fmt>` | `text` (default), `json`, or `claude-code` |
| `--update <mode>` | `cache` (default) or `if-stale` |

- `--update=cache` uses cached update status only.
- `--update=if-stale` performs a bounded live update check only when cache is stale or missing.
- Reminder output can surface update guidance, but cymbal still never self-updates by default.

### `cymbal hook notify`

Emit a structured update notification payload for agent plugins that want to
surface update notices outside hidden system context.

```sh
cymbal hook notify [flags]
```

| Flag | Description |
|------|-------------|
| `--format <fmt>` | `json` (default) or `text` |
| `--update <mode>` | `cache` (default) or `if-stale` |

- Returns `{"notify": true, ...}` with version and command when an update is available and the notification throttle allows it.
- Returns `{"notify": false}` when no update is available or the user was already notified recently.
- `text` format prints a plain notice; empty output when no notice is due.
- Respects `CYMBAL_NO_UPDATE_NOTIFIER`.
- Uses cymbal's per-version notification throttle (24h TTL).

### `cymbal hook nudge`

Inspect a would-be shell command and, if it looks like code navigation through
`rg`, `grep`, `find`, or `fd`, emit a cymbal suggestion.

```sh
cymbal hook nudge [--format <fmt>] [-- <command> [args...]]
```

| Flag | Description |
|------|-------------|
| `--format <fmt>` | `claude-code` (default), `text`, or `json` |

- Exit code is always `0`.
- `text` writes the suggestion to stderr.
- `json` emits a generic `suggest` / `why` payload for non-Claude integrations.

### `cymbal hook install`

Install cymbal-managed integration for a supported agent.

```sh
cymbal hook install <agent> [flags]
```

| Flag | Description |
|------|-------------|
| `--scope <scope>` | `user` (default) or `project` |
| `--dry-run` | Print the target path and intended managed content without writing |

Supported agents:

- `opencode`
- `claude-code`

Examples:

```sh
cymbal hook install opencode
cymbal hook install opencode --scope project
cymbal hook install opencode --scope project --dry-run
cymbal hook install claude-code
```

For `opencode`, re-running install upgrades the existing cymbal-managed plugin
file in place. The managed plugin is written with OpenCode's single `CymbalPlugin`
export shape and nudges Bash, Grep, and Glob tool calls toward cymbal-first code
navigation. If a non-cymbal file already exists at cymbal's target path, install
refuses to overwrite it.

Only one cymbal-managed OpenCode scope is supported at a time. If a managed
plugin already exists in the other scope, install refuses until that scope is
uninstalled.

### `cymbal hook uninstall`

Remove cymbal-managed integration for a supported agent.

```sh
cymbal hook uninstall <agent> [flags]
```

The same `--scope` and `--dry-run` flags apply.

For `opencode`, uninstall removes only the cymbal-managed plugin file and
leaves unrelated user-owned files untouched.
