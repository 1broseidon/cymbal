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
| `-f, --force` | Force re-index all files |
| `-w, --workers <n>` | Number of parallel workers (0 = NumCPU) |

```sh
# Index current directory
cymbal index .

# Force re-index with 8 workers
cymbal index . --force --workers 8
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
cymbal outline <file> [flags]
```

| Flag | Description |
|------|-------------|
| `-s, --signatures` | Show full parameter signatures |

```sh
cymbal outline internal/auth/handler.go
cymbal outline internal/auth/handler.go --signatures
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
cymbal show <symbol|file[:L1-L2]> [flags]
```

| Flag | Description |
|------|-------------|
| `-C, --context <n>` | Lines of context around the target |

If the argument contains `/` or ends with a known extension, it's treated as a file path. Otherwise, it's treated as a symbol name.

```sh
# Show a symbol's source
cymbal show handleAuth

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
file in place. If a non-cymbal file already exists at cymbal's target path,
install refuses to overwrite it.

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
