---
name: cymbal
description: >
  Use the cymbal CLI — not Read, Grep, Glob, or Bash — for all code
  navigation, exploration, and comprehension. cymbal is a tree-sitter indexed
  code navigator that returns precise, token-efficient results.
  TRIGGER when: exploring an unfamiliar codebase, finding where a function/class/type
  is defined, understanding what calls what, assessing the impact of a change, reading
  specific symbols or line ranges, searching for symbols or text patterns, getting an
  overview of a repo's structure, tracing execution flow, investigating a bug by
  locating relevant code, or preparing to modify code you haven't read yet. Even for
  simple lookups like "where is X defined" or "what does function Y do", use cymbal —
  it's faster and cheaper than reading whole files.
  DO NOT TRIGGER when: writing new code from scratch, running tests, updating
  dependencies, creating PRs, or fixing type/runtime errors where the error message
  already points to the exact location.
---

# cymbal — Code Navigation CLI

cymbal is a tree-sitter-powered code indexer and navigator. It understands
symbols, call graphs, and file structure across languages. The index builds
automatically on first use and refreshes incrementally — no setup needed.

The reason to prefer cymbal over raw file reads or grep: it returns only the
relevant context (a symbol's source, its callers, its members) instead of
entire files. This dramatically cuts token usage and gets you to the answer
faster.

## Choosing the right command

Start from what you're trying to do:

| Goal | Command |
|---|---|
| Orient yourself in an unfamiliar repo | `cymbal structure` |
| Understand a symbol (function, class, type) | `cymbal investigate <symbol>` |
| See what a function calls (downward) | `cymbal trace <symbol>` |
| See what breaks if a symbol changes (upward) | `cymbal impact <symbol>` |
| Read a symbol's source code | `cymbal show <symbol>` |
| Read specific lines of a file | `cymbal show <file:L1-L2>` |
| See what's defined in a file | `cymbal outline <file>` |
| Find a symbol by name | `cymbal search <query>` |
| Find text in file contents | `cymbal search <query> --text` |
| Browse the file tree | `cymbal ls [path]` |
| Get repo stats (languages, counts) | `cymbal ls --stats` |
| Find what imports a file/package | `cymbal importers <file>` |
| Get bundled context for a symbol | `cymbal context <symbol>` |
| See git diff scoped to a symbol | `cymbal diff <symbol> [base]` |
| Find references to a symbol | `cymbal refs <symbol>` |

## Command details

### `cymbal structure`

Start here when you're new to a repo. Returns entry points, most-referenced
symbols, most-imported files, and largest packages — all derived from the
index, no guessing.

```
cymbal structure            # default: top 10 per section
cymbal structure -n 5       # limit items per section
```

### `cymbal investigate <symbol>`

The go-to command for understanding any symbol. It adapts based on what the
symbol is:

- **function/method** — returns source + callers + shallow impact
- **class/struct/type/interface** — returns source + members + references
- **ambiguous** — auto-resolves to best match, notes alternatives

Supports batch mode and disambiguation:

```
cymbal investigate OpenStore              # auto-picks best match
cymbal investigate config.go:Config       # disambiguate with file hint
cymbal investigate auth.Middleware         # disambiguate with package hint
cymbal investigate Foo Bar Baz            # batch: multiple symbols at once
```

This is the most useful command — when in doubt, use `investigate`.

### `cymbal trace <symbol>`

Follows the call graph downward: what does this symbol call, and what do
those call? Use this to understand execution flow.

```
cymbal trace handleRegister              # 3 levels deep (default)
cymbal trace handleRegister --depth 5    # go deeper
cymbal trace handleRegister -n 20        # limit result count
```

### `cymbal impact <symbol>`

Follows the call graph upward: what calls this symbol, and what calls those
callers? Use this to assess risk before changing something.

```
cymbal impact ParseFile                  # 2 levels deep (default)
cymbal impact ParseFile -D 3             # deeper analysis
cymbal impact ParseFile -C 2             # more context lines around call sites
```

### `cymbal show <symbol|file[:lines]>`

Read source code precisely — either a symbol's definition or a file range.
If the argument contains `/` or a file extension, it's treated as a path.

```
cymbal show ParseFile                    # show symbol source
cymbal show internal/index/store.go      # show full file
cymbal show internal/index/store.go:80-120  # lines 80-120
cymbal show Foo Bar Baz                  # batch: multiple symbols
cymbal show -C 5 ParseFile              # 5 extra context lines
```

Prefer `cymbal show <symbol>` over reading a whole file with Read — you get
just the definition without loading hundreds of irrelevant lines.

### `cymbal outline <file>`

Lists all symbols defined in a file — functions, classes, types, constants.
Use this before reading a file to know what's in it and jump to the right
spot.

```
cymbal outline src/index.ts
cymbal outline src/index.ts --signatures  # include parameter signatures
```

### `cymbal search <query>`

Search for symbols by name (default) or text across file contents (`--text`).
Symbol search ranks results: exact > prefix > fuzzy.

```
cymbal search Config                     # find symbols named Config
cymbal search Config --kind class        # only classes
cymbal search Config --lang typescript   # only TypeScript
cymbal search "error handling" --text    # grep across file contents
cymbal search Config --exact             # exact name match only
cymbal search Config -n 50              # more results
```

### `cymbal ls`

Browse the file tree or get a repo overview.

```
cymbal ls                    # file tree of current directory
cymbal ls src/               # tree of a subdirectory
cymbal ls -D 2               # limit tree depth
cymbal ls --stats            # repo overview: languages, file/symbol counts
cymbal ls --repos            # list all indexed repos
```

### `cymbal context <symbol>`

Returns bundled context: source code, referenced types, callers, and imports
of the defining file. Heavier than `investigate` but gives you everything
in one call.

```
cymbal context OpenStore
cymbal context OpenStore --callers 10    # limit caller count
```

### `cymbal refs <symbol>`

Find references (call sites) to a symbol. Best-effort AST name matching.

```
cymbal refs ParseFile                    # call-site references
cymbal refs ParseFile --importers        # files importing the defining file
cymbal refs ParseFile --impact           # transitive import impact
cymbal refs Foo Bar Baz                  # batch mode
cymbal refs ParseFile -C 3              # more context around each site
```

### `cymbal importers <file|package>`

Find files that import a given file or package.

```
cymbal importers src/utils.ts
cymbal importers src/utils.ts -D 2       # transitive (2 levels)
```

### `cymbal diff <symbol> [base]`

Show git diff scoped to a symbol's definition only — filters hunks to those
overlapping the symbol's line range.

```
cymbal diff ParseFile                    # diff vs HEAD
cymbal diff ParseFile main               # diff vs main branch
cymbal diff ParseFile --stat             # diffstat only
```

## When to use what (decision tree)

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

8. **"What changed in this symbol?"**
   → `cymbal diff <symbol>`

## Tips

- **Batch mode**: `investigate`, `show`, and `refs` accept multiple symbols
  in one invocation. Use this instead of running the command repeatedly.
- **Disambiguation**: If a symbol name is ambiguous, qualify it with
  `file.go:Symbol` or `package.Symbol`.
- **JSON output**: All commands support `--json` for structured output when
  you need to parse results programmatically.
- **No setup required**: The index builds automatically on first use and
  refreshes incrementally on subsequent queries.
