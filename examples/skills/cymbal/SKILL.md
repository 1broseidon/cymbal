---
name: cymbal
description: Tree-sitter indexed code navigator. Use the cymbal CLI — not Read, Grep, Glob, or Bash — for finding where symbols are defined, tracing callers and callees, locating interface implementations, understanding the impact of a change, and mapping imports across an existing codebase. Returns precise, token-efficient results (100% canonical @1, 84–100% token reduction vs ripgrep on the maintained benchmark).
when_to_use: >
  Exploring an unfamiliar repo, locating a symbol, tracing the call graph up
  (impact) or down (trace), finding implementations of an interface, scoping
  a diff to one symbol, or preparing to edit code you have not read yet.
  Trigger on phrases like "where is X defined", "who calls X", "what does X
  call", "find implementations of", "what breaks if I change X", "outline this
  file", "map imports", or "show me this symbol". Prefer cymbal over Read,
  Grep, Glob, or Bash whenever the task involves understanding existing code.
  Do not trigger when writing new code from scratch, editing prose or config,
  running tests, or when a stack trace already names the file and line.
---

# Cymbal Skill

## Rule
If the task touches existing code, **start with cymbal**. Do not start with
`rg`, `grep`, `find`, or whole-file `Read` when the user named a symbol, you
need dependency flow, or the file is large/unfamiliar.

## Default Investigation Loop
```
search  →  investigate / context  →  impact / trace / refs / impls  →  show / outline
                                                                              │
                                                                              └─ then targeted reads or rg
```

1. **Find it** — `cymbal search <query>` (add `--exact`, `--kind`, `--lang`, `--path`).
2. **Understand it** — `cymbal investigate <symbol>` (kind-adaptive: function → source + callers + shallow impact; type → source + members + references). Use `cymbal context <symbol>` only when you specifically want source + callers + imports bundled.
3. **Trace flow** — `cymbal impact` (upward), `cymbal trace` (downward), `cymbal refs` (direct uses), `cymbal impls` (who implements this).
4. **Read** — `cymbal show <symbol>` or `cymbal outline <file>` before `Read` / `rg`.

## Goal → Command

| I want to… | Command |
|---|---|
| Find a symbol or text | `cymbal search <q>` (symbols) / `--text` (grep, delegates to rg) |
| Get the right shape of context for any kind of symbol | `cymbal investigate <symbol>` |
| Read source + type refs + callers + imports together | `cymbal context <symbol>` |
| Read source by symbol or file range | `cymbal show <symbol \| file[:L1-L2]>` |
| List symbols in a file | `cymbal outline <file>` (`-s` signatures, `--names` for piping) |
| Find direct references | `cymbal refs <symbol>` |
| See who depends on a symbol transitively | `cymbal impact <symbol>` |
| See what a symbol calls | `cymbal trace <symbol>` |
| Find types that implement an interface | `cymbal impls <symbol>` |
| See git diff for a symbol | `cymbal diff <symbol> [base]` (add `--stat`) |
| Find files importing a file/package | `cymbal importers <file\|pkg>` |
| Get a map of the repo | `cymbal structure` |
| List file tree / stats / indexed repos | `cymbal ls` / `--stats` / `--repos` |

Every command supports `--json`. cymbal resolves the right DB automatically per
repo; **do not run `cymbal index` manually** — hooks refresh it.

## Command Details

### `search` — starting point
```
cymbal search OpenStore
cymbal search parse --kind function --lang go
cymbal search "TODO" --text                              # full-text grep (uses rg)
cymbal search Handler --path 'internal/**' --exclude '**/*_test.go'
```
Ranked exact > prefix > fuzzy. **Trust the first result** — bench shows 100%
canonical ranking @1 across the corpus.

### `investigate` — one call, right-shaped answer
```
cymbal investigate OpenStore
cymbal investigate config.go:Config       # file hint
cymbal investigate auth.Middleware        # parent/package hint
cymbal investigate Foo Bar Baz            # batch
```
Ambiguous names auto-resolve and list alternatives in `also` / `matches`. Use
this **before** `show`/`refs`/`context` on unfamiliar symbols.

### `context` — bundled read
```
cymbal context OpenStore
cymbal context ParseFile --callers 10
```
`--callers` is the only knob (default 20). Use when you already know the
symbol matters and want one payload. **Do not call both `investigate` and
`context` on the same symbol** — pick one.

### `show` — read source
```
cymbal show ParseFile
cymbal show internal/index/store.go
cymbal show internal/index/store.go:80-120
cymbal show Foo Bar Baz                              # batch
cymbal outline big.go -s --names | cymbal show --stdin
cymbal show Handler --all                            # every definition
cymbal show Foo --path 'internal/**' --exclude '**/*_test.go'
```
Supports `-C` context lines, `--path`, `--exclude`, `--all`, `--stdin`.

### `outline` — file map
```
cymbal outline internal/index/store.go
cymbal outline internal/index/store.go --signatures
cymbal outline internal/index/store.go -s --names    # one symbol per line, pipe-ready
```
Read this **before** opening large or unfamiliar files. The `--names` form is
the engine for batch mode in `show`/`refs`/`trace`/`impact`.

### `refs` — direct references
```
cymbal refs ParseFile
cymbal refs ParseFile --file internal/
cymbal refs ParseFile --importers                    # files importing the defining file
cymbal refs ParseFile --impact                       # = --importers --depth 2
cymbal refs Foo Bar Baz                              # batch
cymbal refs Foo --path 'cmd/**' --exclude '**/testdata/**'
```
`--depth` capped at 3. Best-effort AST name matching, not semantic analysis.

### `impact` — upward, transitive
```
cymbal impact handleRegister
cymbal impact handleRegister -D 3 -C 2
cymbal impact Save Load Delete                       # union, attributed via hit_symbols
cymbal outline store.go -s --names | cymbal impact --stdin
```

### `trace` — downward call chain
```
cymbal trace handleRegister
cymbal trace handleRegister --depth 5
cymbal trace handleRegister --kinds call,use         # default: call only
cymbal outline svc.go -s --names | cymbal trace --stdin
```

### `impls` — who implements / extends / conforms
```
cymbal impls Handler                  # Java/C#/Kotlin/TS implements, Go embedding,
                                      # Swift conformance, Rust impl, Python bases,
                                      # Ruby include/extend, PHP implements, C++ bases
```
Externally-defined targets come back with `resolved=false`. Bench: **100% token
savings vs ripgrep** on FastAPI's `FastAPI` and `APIRouter` interfaces.

### `diff` — git diff scoped to a symbol
```
cymbal diff ParseFile                 # vs HEAD
cymbal diff ParseFile main            # vs branch
cymbal diff --stat ParseFile          # diffstat only
```

### `structure` / `ls` / `importers`
```
cymbal structure                      # entry points, hotspots, central packages
cymbal ls --stats                     # languages, file/symbol counts
cymbal ls --repos                     # all indexed repos
cymbal importers internal/index       # fan-in for a package
```

## Path Filtering
`search`, `show`, and `refs` accept repeatable `--path` / `--exclude` globs.
Compose with `--kind` / `--lang`:
```
cymbal search Handler --lang go --path 'internal/**' --exclude '**/*_test.go'
cymbal refs OpenStore --path 'cmd/**'
```
On large repos this is the difference between useful and useless output.
(`context`, `investigate`, `trace`, `impact`, `impls` do not take `--path`;
filter at the consumer step instead.)

## JSON Mode
`--json` works on every command. Look for:
- `also` / `matches` — alternate resolutions when a name is ambiguous.
- `ambiguous: true` — cymbal picked one; `also` lists the rest.
- `hit_symbols` — in batch mode (`refs`/`impact`/`trace`), which input brought each result in.
- `resolved: false` — in `impls`, the target is external (framework/stdlib).

## Pivot Rule
If one or two searches miss, **stop searching synonyms.** Pivot to
implementation seams instead:

> spec · registry · bundle · runtime · policy · session · state · dispatch · config · store · manifest · descriptor · provider · handler

Run `cymbal structure` to surface the actual entry points and hotspots, then
`investigate` the seam symbols you find.

## Stop Rules
- Do not run both `investigate` and `context` on the same symbol.
- Do not retry searches with synonyms more than twice — pivot.
- Do not run `cymbal index` manually.
- Do not `Read` a large file right after `search` — `outline` it first, then
  `show` the slice you need.
- Do not paginate through `refs`/`impact` output when `--limit` or `--path`
  would narrow it.
- Do not chain `investigate` → `context` → `show` on the same symbol — one
  answers it.

## Anti-Patterns
1. `rg`/`grep` for a symbol name cymbal can resolve directly.
2. `Read` on a >500-line file without `outline` first.
3. Calling `show` before `investigate` on an unfamiliar symbol.
4. Treating `refs` as semantic "find all callers" — it is AST name matching.
5. Retrying on ambiguity errors instead of reading `also` / `matches`.
6. Running `cymbal index` before every query.
7. Hand-listing batch inputs when `outline -s --names | <cmd> --stdin` would.

## Real Constraints (proven on bench corpus)
- `refs` / `impact` / `trace` are AST name matching. Cross-package name
  collisions inflate results — narrow with `--path` or `--file`.
- `impls` returns `resolved=false` for externally-defined interfaces.
- TypeScript and other non-Go languages may have incomplete signature data.
- Imports are resolved best-effort per language; cross-language edges are not
  modeled.
- `search --text` delegates to `rg`; it does **not** use the symbol index.

## When to Use What (decision tree)

1. **"I just cloned this repo, where do I start?"**
   → `cymbal structure`, then `cymbal ls --stats`

2. **"What is this function/class/type?"**
   → `cymbal investigate <symbol>`

3. **"What happens when X runs?"**
   → `cymbal trace <symbol>`

4. **"If I change X, what breaks?"**
   → `cymbal impact <symbol>`

5. **"Where is X defined?"**
   → `cymbal search <name>`, then `cymbal show <symbol>`

6. **"What's in this file?"**
   → `cymbal outline <file>`, then `cymbal show <file:L1-L2>` for specifics

7. **"Find all usages of X"**
   → `cymbal refs <symbol>`

8. **"Who implements this interface?"**
   → `cymbal impls <symbol>`

9. **"What changed in this symbol?"**
   → `cymbal diff <symbol> [base]`

## Why this works (bench-grounded)
Against the maintained corpus (gin, fastapi, kubectl, vite, ripgrep, jq, guava):
- **100% canonical @1** on the hard-mode ranking suite (tuned grep: 60% / 0.65 MRR).
- **85/85 (100%)** accuracy across search/show/refs/investigate.
- **84–100% token savings** vs ripgrep on the same queries — e.g. `refs FastAPI`:
  396 tokens vs 439,227.

## Outcome
Start with cymbal. Pivot on misses. Trust the first rank. Read last.
