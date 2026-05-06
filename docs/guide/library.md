# Library Guide

cymbal exposes five Go packages for embedding code indexing and navigation into your own tools.

## Install

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go get github.com/1broseidon/cymbal@latest
```

> **CGO required.** cymbal uses tree-sitter (C) for parsing and SQLite (C) for storage. The `SQLITE_ENABLE_FTS5` flag enables full-text search.

## Packages

| Package | Import | Purpose |
|---------|--------|---------|
| `index` | `github.com/1broseidon/cymbal/index` | Indexing engine, SQLite store, and all query APIs |
| `lang` | `github.com/1broseidon/cymbal/lang` | Unified language registry for names, extensions, special filenames, and parser availability |
| `parser` | `github.com/1broseidon/cymbal/parser` | Tree-sitter parsing for 22 languages |
| `symbols` | `github.com/1broseidon/cymbal/symbols` | Core data types: `Symbol`, `Import`, `Ref`, `ParseResult` |
| `walker` | `github.com/1broseidon/cymbal/walker` | Concurrent file discovery with language detection |

Most consumers only need `index`. The other packages are useful if you want to parse files without indexing, walk directories with custom filters, or work with raw symbol data.

---

## Language registry

The `lang` package is the canonical source of truth for language support in cymbal. It models both:

- **known / recognized languages** — files cymbal can classify by extension or special filename
- **supported / parseable languages** — files with a tree-sitter grammar that can be parsed and indexed

```go
import "github.com/1broseidon/cymbal/lang"

fmt.Println(lang.Default.Supported("go"))            // true
fmt.Println(lang.Default.Known("dockerfile"))       // true
fmt.Println(lang.Default.LangForFile("Dockerfile")) // "dockerfile"

l := lang.Default.ForFile("notes.toml")
fmt.Println(l.Name)        // "toml"
fmt.Println(l.Parseable()) // false
```

Use `lang.Default.Supported` when you need the parseable subset for indexing or parsing. Use `Known` / `LangForFile` when classification alone is enough.

---

## Indexing

### Index a repository

```go
import "github.com/1broseidon/cymbal/index"

stats, err := index.Index("/path/to/repo", "", index.Options{})
// stats.FilesIndexed, stats.SymbolsFound, stats.StaleRemoved, etc.
```

**Parameters:**
- `root` — absolute path to the repo root
- `dbPath` — path to the SQLite database. Pass `""` to auto-compute from the repo root (stored under the OS cache directory)
- `opts.Workers` — number of parallel parse workers. `0` defaults to `runtime.NumCPU()`
- `opts.Force` — if `true`, re-index all files regardless of mtime
- `opts.Exclude` — repeatable repo-relative path patterns to skip while indexing
- `opts.IncludeGenerated` — if `true`, disables default generated-file skips
- `opts.IncludeLargeFiles` — if `true`, disables default large-source-file skips

**What it does:**
1. Walks the directory tree, skipping dot-dirs, `node_modules`, `vendor`, generated/large files, and any configured excludes
2. Compares each file's mtime+size against the stored index — skips unchanged files
3. Parses changed files with tree-sitter, extracting symbols, imports, and references
4. Writes results to SQLite in batched transactions
5. Prunes stale entries for deleted/renamed files

### Resolve the database path

```go
dbPath, err := index.RepoDBPath("/path/to/repo")
// e.g. ~/.cache/cymbal/repos/a1b2c3d4e5f6g7h8/index.db
```

Each repo gets its own database, keyed by a SHA-256 hash of the repo root path.

### Keep the index fresh

```go
refreshed := index.EnsureFresh(dbPath)
```

Call this before queries. It runs an incremental reindex, re-parsing only files that changed since the last index. Returns the number of files refreshed (0 if nothing changed). Errors are swallowed — a stale read is better than a failed query.

If the database doesn't exist yet, `EnsureFresh` auto-indexes from the current working directory's git root.

---

## Querying

All query functions take a `dbPath` string and return typed results. They open/close the database internally — no connection management needed.

### Search symbols

```go
results, err := index.SearchSymbols(dbPath, index.SearchQuery{
    Text:     "handleAuth",
    Kind:     "function",  // optional: filter by kind
    Language: "go",        // optional: filter by language
    Exact:    false,       // true = exact name match only
    Limit:    50,
})

for _, r := range results {
    fmt.Printf("%s %s %s:%d\n", r.Kind, r.Name, r.RelPath, r.StartLine)
}
```

Search is ranked: exact match > prefix > fuzzy (via FTS5).

### Flexible search

```go
results, err := index.SearchSymbolsFlex(dbPath, "HandleAuth", 50)
```

Tries case-insensitive exact match first, then falls back to FTS prefix match. Useful when user input may not match the exact casing.

### Investigate a symbol

```go
inv, err := index.Investigate(dbPath, "handleAuth")
```

Returns a kind-adaptive result:
- **Functions/methods** → source + callers (refs) + transitive impact
- **Types/structs/classes** → source + members + references

```go
type InvestigateResult struct {
    Symbol  SymbolResult   `json:"symbol"`
    Source  string         `json:"source"`
    Kind    string         `json:"investigate_kind"` // "function" or "type"
    Refs    []RefResult    `json:"refs,omitempty"`
    Impact  []ImpactResult `json:"impact,omitempty"`
    Members []SymbolResult `json:"members,omitempty"`
}
```

Use `InvestigateOpts` to disambiguate when multiple symbols share a name:

```go
inv, err := index.Investigate(dbPath, "Config", index.InvestigateOpts{
    FileHint: "auth/config.go",
})
```

### Find references

```go
refs, err := index.FindReferences(dbPath, "handleAuth", 50)

for _, r := range refs {
    fmt.Printf("%s:%d\n", r.RelPath, r.Line)
}
```

Returns call sites and usages of a symbol across the indexed codebase. Based on AST name matching, not semantic analysis.

### Trace (downward call graph)

```go
trace, err := index.FindTrace(dbPath, "handleAuth", 3, 50)

for _, t := range trace {
    fmt.Printf("[%d] %s → %s  %s:%d\n", t.Depth, t.Caller, t.Callee, t.RelPath, t.Line)
}
```

Follows the call graph downward: what does this function call, what do those call, etc. `depth` controls how many hops to follow (max recommended: 3-4).

### Impact (upward call graph)

```go
impact, err := index.FindImpact(dbPath, "handleAuth", 2, 100)

for _, i := range impact {
    fmt.Printf("[%d] %s called by %s  %s:%d\n", i.Depth, i.Symbol, i.Caller, i.RelPath, i.Line)
}
```

Follows the call graph upward: what calls this function, what calls those callers. Answers "what breaks if I change this?"

### Find importers

```go
// By symbol name — finds files that import the file containing a symbol
importers, err := index.FindImporters(dbPath, "handleAuth", 2, 50)

// By file/package path directly
importers, err := index.FindImportersByPath(dbPath, "internal/auth", 2, 50)
```

`depth` controls transitive import analysis (1 = direct importers only, 2 = importers of importers).

### Full context bundle

```go
ctx, err := index.SymbolContext(dbPath, "handleAuth", 20)
// ctx.Symbol   — the resolved symbol
// ctx.Source   — full source code
// ctx.TypeRefs — type symbols referenced in this function
// ctx.Callers  — who calls this
// ctx.FileImports — imports in the same file
```

### Structural overview

```go
structure, err := index.Structure(dbPath, 10)
// structure.EntryPoints    — main/init/handler functions
// structure.TopByRefs      — most-referenced symbols
// structure.TopByImportFan — most-imported files
// structure.TopPackages    — largest packages
```

### File outline

```go
syms, err := index.FileOutline(dbPath, "/absolute/path/to/file.go")

for _, s := range syms {
    indent := strings.Repeat("  ", s.Depth)
    fmt.Printf("%s%s %s (L%d-%d)\n", indent, s.Kind, s.Name, s.StartLine, s.EndLine)
}
```

### Text search (grep)

```go
results, err := index.TextSearch(dbPath, "TODO", "go", 50)
// Pass "" for lang to search all languages
```

### List indexed repos

```go
repos, err := index.ListRepos()
for _, r := range repos {
    fmt.Printf("%s — %d files, %d symbols\n", r.Path, r.FileCount, r.SymbolCount)
}
```

---

## Lower-level: Store

For advanced use cases, open the store directly:

```go
store, err := index.OpenStore(dbPath)
defer store.Close()

// Direct store methods
results, err := store.SearchSymbols("handleAuth", "function", "go", true, 50)
refs, err := store.FindReferences("handleAuth", 50)
members, err := store.ChildSymbols("UserService", 50, "/path/to/file.go")
trace, err := store.FindTrace("handleAuth", 3, 50)
impact, err := store.FindImpact("handleAuth", 2, 100)
imports, err := store.FileImports("/path/to/file.go")
stats, err := store.RepoStats()

// Metadata
root, err := store.GetMeta("repo_root")
err = store.SetMeta("key", "value")
```

This avoids repeated open/close overhead when running multiple queries in sequence. The store holds a `*sql.DB` connection with WAL mode and busy timeout configured.

---

## Parsing without indexing

Use the `parser` package to extract symbols from a single file without touching SQLite:

```go
import (
    "github.com/1broseidon/cymbal/parser"
    "github.com/1broseidon/cymbal/symbols"
)

// From a file path
result, err := parser.ParseFile("/path/to/handler.go", "go")

// From bytes (avoids re-reading the file)
src, _ := os.ReadFile("/path/to/handler.go")
result, err := parser.ParseBytes(src, "/path/to/handler.go", "go")

// result.Symbols — []symbols.Symbol
// result.Imports — []symbols.Import
// result.Refs    — []symbols.Ref

for _, sym := range result.Symbols {
    fmt.Printf("%s %s L%d-%d\n", sym.Kind, sym.Name, sym.StartLine, sym.EndLine)
}
```

Check language support:

```go
import "github.com/1broseidon/cymbal/lang"

if parser.SupportedLanguage("go") {
    // ...
}

if lang.Default.Known("dockerfile") {
    // recognized for classification, but not parseable/indexable
}
```

`parser.SupportedLanguage` delegates to `lang.Default.Supported`.

### Supported languages

Parseable/indexed languages:

- Go
- Python (`.py`, `.pyw`)
- JavaScript (`.js`, `.jsx`, `.mjs`, `.cjs`)
- TypeScript (`.ts`, `.tsx`, `.mts`, `.cts`)
- Rust
- C / C++
- C#
- Java
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

Recognized but not parseable/indexable examples: `Dockerfile`, `Makefile`, `Jenkinsfile`, `CMakeLists.txt`, Apex, JSON, TOML, Markdown, SQL, Vue, Svelte, Zig, Erlang, Haskell, OCaml, R, and Perl.

---

## File discovery

Use the `walker` package to find source files with concurrent directory traversal:

```go
import (
    "github.com/1broseidon/cymbal/lang"
    "github.com/1broseidon/cymbal/walker"
)

// Walk only parseable/indexable files
files, err := walker.Walk("/path/to/repo", 0, lang.Default.Supported)

for _, f := range files {
    fmt.Printf("%s (%s, %d bytes)\n", f.RelPath, f.Language, f.Size)
}
```

`Walk` skips dot-directories, `node_modules`, `vendor`, `__pycache__`, build output, etc. Pass `nil` for the language filter to include all recognized file types, including non-parseable ones such as `Dockerfile` and `Makefile`.

Detect a file's language:

```go
lang := walker.LangForFile("handler.go") // "go"
lang := walker.LangForFile("Dockerfile") // "dockerfile"
lang := walker.LangForFile("styles.css") // "" (unrecognized)
```

Build a directory tree (for `cymbal ls`-style output):

```go
tree, err := walker.BuildTree("/path/to/repo", 3) // maxDepth 3, 0 = unlimited
walker.PrintTree(os.Stdout, tree, "")
```

---

## Result types

All result types have JSON struct tags and serialize cleanly.

### SymbolResult

```go
type SymbolResult struct {
    Name      string `json:"name"`
    Kind      string `json:"kind"`       // function, method, struct, class, etc.
    File      string `json:"file"`       // absolute path
    RelPath   string `json:"rel_path"`   // relative to repo root
    StartLine int    `json:"start_line"`
    EndLine   int    `json:"end_line"`
    Parent    string `json:"parent,omitempty"`    // enclosing type/class
    Depth     int    `json:"depth"`               // nesting depth (0 = top-level)
    Signature string `json:"signature,omitempty"` // parameter list
    Language  string `json:"language"`
}
```

### RefResult

```go
type RefResult struct {
    File    string `json:"file"`
    RelPath string `json:"rel_path"`
    Line    int    `json:"line"`
    Name    string `json:"name"`
}
```

### TraceResult

```go
type TraceResult struct {
    Caller  string `json:"caller"`    // the function making the call
    Callee  string `json:"callee"`    // the function being called
    File    string `json:"file"`
    RelPath string `json:"rel_path"`
    Line    int    `json:"line"`
    Depth   int    `json:"depth"`     // hop distance from root
}
```

### ImpactResult

```go
type ImpactResult struct {
    Symbol  string `json:"symbol"`    // the callee
    Caller  string `json:"caller"`    // the calling function
    File    string `json:"file"`
    RelPath string `json:"rel_path"`
    Line    int    `json:"line"`
    Depth   int    `json:"depth"`     // hop distance from original
}
```

### ImporterResult

```go
type ImporterResult struct {
    File    string `json:"file"`
    RelPath string `json:"rel_path"`
    Import  string `json:"import"`
    Depth   int    `json:"depth"`
}
```

---

## CGO and build notes

cymbal requires CGO for tree-sitter and SQLite:

```sh
# Build
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go build ./...

# Test
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go test ./...

# Install
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go install github.com/1broseidon/cymbal@latest
```

Without the `SQLITE_ENABLE_FTS5` flag, the database will fail to create the FTS5 virtual table and all queries will error on first use.

Cross-compilation requires a C cross-compiler for the target platform.
