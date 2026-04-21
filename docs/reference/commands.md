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
cymbal search <query> [flags]
```

| Flag | Description |
|------|-------------|
| `-t, --text` | Full-text grep across file contents |
| `-e, --exact` | Exact name match only |
| `-k, --kind <type>` | Filter by symbol kind (function, class, method, etc.) |
| `-l, --lang <name>` | Filter by language (go, python, typescript, etc.) |
| `-n, --limit <n>` | Max results (default: 50) |

```sh
# Symbol search
cymbal search handleAuth

# Full-text grep
cymbal search "TODO" --text

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
