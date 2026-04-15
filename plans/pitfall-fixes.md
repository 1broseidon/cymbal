# Cymbal Pitfall Fixes — Spec & Sprint Plan

## Context

Cymbal is now excellent at definition discovery, canonical ranking, and token-efficient search. Benchmarks prove 100% ground-truth precision/recall and 100% canonical @1. But a comprehensive architecture review surfaced 12 pitfalls where cymbal is **worse** than grep or brute-force file-reading agent workflows.

This plan organises fixes into four priority sprints, each independently shippable and testable.

---

## Sprint 0 — P0: Silent data loss and performance traps

> These are cases where cymbal gives **wrong or incomplete answers** without signalling failure, or where the architecture imposes avoidable overhead on every single command.

### P0-A: Ranking before LIMIT (silent truncation)

**Problem**: `SearchSymbols` runs SQL with `LIMIT ?` and returns whatever SQLite gives first (insertion order). `rankSymbols` only reorders that truncated set. If the canonical definition wasn't in the first N rows, it's silently dropped — the user never knows it existed.

**Example**: A repo with 50 `Builder` definitions. Limit is 20. Canonical `Builder` at row 25 in SQLite order is invisible.

**Fix**:

1. Fetch a larger internal window (e.g. `min(limit * 5, 500)`) from SQLite
2. Apply `rankSymbols` on the full window
3. Truncate to the user-requested limit after ranking
4. Alternative: push scoring into SQL with an `ORDER BY` that approximates the ranking heuristics (kind priority + path depth), then apply fine-grained Go reranking on the fetched set

**Files**: `index/store.go` (`SearchSymbols`, `SearchSymbolsCI`), `cmd/output.go` (`flexResolve`), `cmd/search.go`

**Acceptance criteria**:
- [ ] `search --exact --limit 5 Builder` in a repo with 50 matches returns the canonical definition
- [ ] Existing bench ground-truth stays 100%
- [ ] New bench case: symbol with many definitions, canonical not in first 20 DB rows

**Risks**:
- Fetching 500 rows is fine for SQLite — it's in-process
- Must not regress search latency beyond 2x on current benchmark repos

---

### P0-B: DB open/close per query (death by a thousand opens)

**Problem**: Every public index function (`SymbolsByName`, `SearchSymbolsFlex`, `FindReferences`, `InvestigateResolved`, etc.) opens the SQLite store, runs one query, and closes it. A single `flexResolve` call opens the DB **twice** (once for `SymbolsByName`, once for `SearchSymbolsFlex` on miss). `investigate` opens it **5+ times**. `context` even more.

An agent workflow of `search → show → refs → investigate` does 10+ open/close cycles. Each open involves file locking, WAL recovery check, and schema validation.

**Fix**:

Option A — **Command-scoped store** (recommended):
1. Open the store once per cobra command invocation
2. Pass `*Store` (or `dbPath + lazy singleton`) through to all sub-calls
3. Close on command exit via `defer`
4. This means changing public index API from `func Foo(dbPath, ...) → func (s *Store) Foo(...)` — many already exist as methods, just the top-level wrappers open/close

Option B — **Connection pool**:
1. A `sync.Pool` or `sync.Once`-based store cache keyed by dbPath
2. Lower code churn but harder to reason about lifetime

**Files**: `index/index.go` (all `func Foo(dbPath string, ...)` wrappers), `cmd/output.go`, `cmd/show.go`, `cmd/search.go`, `cmd/refs.go`, `cmd/investigate.go`, `cmd/context.go`, `cmd/impact.go`, `cmd/trace.go`

**Acceptance criteria**:
- [ ] `investigate` opens the DB exactly once
- [ ] `context` opens the DB exactly once
- [ ] No behaviour change in any command output
- [ ] Bench speed regression check passes (should improve)

**Risks**:
- Large refactor surface — many files touched
- Must not break the python bridge (`pythonbridge/`) which also calls these functions
- Must preserve concurrent safety (currently not an issue since each invocation is single-threaded)
- Recommend: do Option A, keep the old `func(dbPath)` wrappers as thin facades that open-once internally, so python bridge doesn't break

---

## Sprint 1 — P1: Agent workflow failures

> These are cases where an agent using cymbal hits a dead end and has to fall back to grep, when cymbal had enough information to give a useful answer.

### P1-A: Ambiguity error instead of results

**Problem**: `SymbolContext` returns `AmbiguousError` when multiple matches exist. The agent gets an error and **zero results**. This is worse than grep, which would at least return something.

**Current code**: `index/index.go:SymbolContext()` returns early with `AmbiguousError` when `len(results) > 1` and no file hint.

**Fix**:
1. When ambiguous, apply `rankSymbols` and use the top result instead of erroring
2. Include a `"matches": N` or `"ambiguous": true` field in the output so the agent knows there were alternatives
3. Keep `AmbiguousError` only as an internal signal — never surface it to CLI output

**Files**: `index/index.go` (`SymbolContext`), `cmd/context.go`

**Acceptance criteria**:
- [ ] `cymbal context Context` in gin returns the struct definition, not an error
- [ ] Output includes `matches: 5` metadata
- [ ] Agent can see the top-ranked result without error recovery

**Risks**:
- Low risk — this is strictly more useful than the current behaviour
- Edge case: if ranking is bad, the agent sees the wrong context silently. Mitigated by P0-A (better ranking)

---

### P1-B: Path filtering on search

**Problem**: There's `--kind` and `--lang` but no `--path` or `--exclude-path`. An agent can't say "search for Config but only in `src/`" or "exclude `test/`". The ranking heuristics help but aren't user-controllable.

**Fix**:
1. Add `--path <glob>` flag — only return results whose `rel_path` matches the glob
2. Add `--exclude <glob>` flag — exclude results whose `rel_path` matches
3. Apply filters after DB query but before ranking/limit
4. Support both on `search`, `refs`, `show`

**Files**: `cmd/search.go`, `cmd/refs.go`, `cmd/show.go`, `index/store.go` (optional: push `WHERE rel_path LIKE` into SQL for perf)

**Acceptance criteria**:
- [ ] `cymbal search --path 'src/*' Config` only returns source-tree results
- [ ] `cymbal search --exclude '*_test.go' Context` excludes test definitions
- [ ] `cymbal refs --exclude 'vendor/*' FastAPI` excludes vendored references
- [ ] Flags compose with `--kind`, `--lang`, `--exact`

**Risks**:
- Glob matching in Go is cheap (`filepath.Match` or `strings.Contains`)
- SQL push-down optional — can do in-memory filter first, optimise later

---

### P1-C: Show should expose alternatives usefully

**Problem**: `show` picks `res.Results[0]` and renders it. The frontmatter mentions "matches: 3 (also: foo.go:12)" but an agent has to parse that string, construct a new command, and retry. This is more friction than grep, which shows everything inline.

**Fix** (two parts):

1. **`--all` flag**: show all matched definitions with `---` separators between them (up to a reasonable cap, e.g. 10)
2. **Better `also` metadata**: emit each alternative as structured data in JSON mode:
   ```json
   { "also": [{"file": "foo.go", "line": 12, "kind": "struct"}, ...] }
   ```
   so agents can follow up without string parsing

**Files**: `cmd/show.go` (`showSymbol`)

**Acceptance criteria**:
- [ ] `cymbal show --all createServer` in vite shows all 10 definitions
- [ ] `cymbal --json show createServer` includes structured `also` array
- [ ] Default (no `--all`) behaviour unchanged — shows best match only

**Risks**:
- `--all` output can be long — cap at 10 or add `--limit` for show
- JSON schema change is additive, not breaking

---

## Sprint 2 — P2: Noise reduction and edge cases

> These are cases where cymbal works but produces unnecessary noise or misses easy wins.

### P2-A: Generated code detection

**Problem**: `*.pb.go`, `*_generated.go`, `*_pb2.py`, `*.g.dart`, etc. are indexed and ranked the same as hand-written code. The path penalties help slightly but there's no explicit generated-file signal.

**Fix**:
1. Add a `generatedFilePatterns` list in `cmd/output.go` (or `lang/lang.go`)
2. Apply a `-70` score penalty in `symbolScore` for generated files
3. Patterns: `_generated.go`, `.pb.go`, `_pb2.py`, `_pb2_grpc.py`, `.g.dart`, `.generated.`, `__generated__`, `_gen.go`, `.gen.ts`
4. Also check first few bytes of file for `// Code generated` / `# Generated` markers (optional, higher effort)

**Files**: `cmd/output.go` (`symbolScore`)

**Acceptance criteria**:
- [ ] `search MessageType` in a protobuf project ranks hand-written types above generated
- [ ] No false positives on files like `generator.go` or `generate_test.go`

**Risks**:
- Pattern list needs to be conservative — false positives are worse than misses
- First-line marker check adds I/O; defer to later if needed

---

### P2-B: Refs scope context

**Problem**: `FindReferences` matches by string name. `refs Context` returns both `gin.Context` struct usages AND `context.Context()` stdlib method calls AND `Context` variable names. For common names this is noisier than grep because the output has less surrounding text.

**Fix** (two complementary):

1. **Add `--file` to refs**: `cymbal refs --file context.go Context` — only show refs in files that also import/reference the defining file. This is a poor-man's scope filter.
2. **Show file path prominently in output**: ensure each ref line shows `file:line: <source_line>` so agents can visually/programmatically filter. (May already be the case — verify.)
3. **Future**: use import graph to filter refs that couldn't possibly reference the target definition. (Larger effort, defer.)

**Files**: `cmd/refs.go`, `index/store.go` (`FindReferences`)

**Acceptance criteria**:
- [ ] `cymbal refs --file context.go Context` in gin returns only refs from files that reference `context.go`
- [ ] Default refs output remains unchanged

**Risks**:
- Import-graph filtering is the real solution but high effort — the `--file` flag is a useful 80% solution

---

### P2-C: Text search — don't compete with rg

**Problem**: `TextSearch` reads every indexed file sequentially in Go. ripgrep uses mmap + SIMD. For `--text` queries cymbal is strictly worse.

**Fix**:
1. When `rg` is available on `$PATH`, shell out to `rg --no-heading -n <pattern> <repo_root>` and parse the output
2. Fall back to current Go implementation only when `rg` is not available
3. Document this in `--help`: "Uses ripgrep when available for performance"

**Files**: `index/store.go` (`TextSearch` or new wrapper), `cmd/search.go`

**Acceptance criteria**:
- [ ] `cymbal search --text foo` with rg available completes in ~same time as raw `rg foo`
- [ ] Without rg, falls back gracefully
- [ ] Output format unchanged

**Risks**:
- Adds external dependency (optional)
- Output parsing: rg format is stable and well-known
- Must handle rg not finding results (exit code 1) as "no results" not error

---

## Sprint 3 — P3: Structural improvements

> These are architectural improvements that pay off over time but don't fix immediate agent-facing bugs.

### P3-A: EnsureFresh optimisation

**Problem**: Before every query, `ensureFresh` walks the entire repo checking file hashes. For large repos this is a meaningful cold-start penalty on every command. Grep has zero startup cost.

**Fix** (progressive):

1. **Mtime cache**: store `(path, mtime, size)` tuples in the DB. Only re-hash files whose mtime or size changed. Reduces full-walk from O(N × hash) to O(N × stat).
2. **Directory-level mtime**: check directory mtimes first. If a directory hasn't changed, skip all files in it. Reduces to O(dirs).
3. **Background/async**: move freshness check to background goroutine. Return stale results immediately, refresh in parallel. Add `--fresh` flag for explicit refresh.
4. **inotify/fsnotify** (future): watch for changes instead of polling. Linux-only initially.

**Files**: `index/index.go` (`EnsureFresh`), `index/store.go` (`AllFileChecks`, `FileHash`)

**Acceptance criteria**:
- [ ] Second invocation of any command in an unchanged repo takes <5ms for freshness check (currently ~50-200ms depending on repo size)
- [ ] Modified files are still detected and re-indexed
- [ ] `--fresh` flag forces full re-check

**Risks**:
- Mtime is not reliable on all filesystems (NFS, Docker bind mounts)
- Must handle clock skew gracefully
- Background refresh changes semantics — agent might see stale results

---

### P3-B: Signature extraction improvement

**Problem**: Many languages have incomplete or empty signature fields. A search result like `function createServer packages/vite/src/node/server/index.ts:408` doesn't show parameters. The agent has to follow up with `show`.

**Fix**:
1. Audit each tree-sitter grammar query for signature capture
2. For Go: capture full `func Name(params) (returns)` — currently works
3. For TypeScript: capture `function name(params): ReturnType` — may be incomplete
4. For Python: capture `def name(params) -> ReturnType` — may be incomplete
5. For Rust: capture `fn name(params) -> ReturnType`
6. For Java: capture method/constructor signatures including modifiers
7. For C: capture function declarations including return type

**Files**: `parser/queries/*.scm` (tree-sitter query files), `parser/parser.go`

**Acceptance criteria**:
- [ ] `search --exact createServer` in vite shows `createServer(inlineConfig?: InlineConfig): Promise<ViteDevServer>` in signature field
- [ ] `search --exact FastAPI` in fastapi shows `class FastAPI(Starlette)` or similar
- [ ] Non-regression: existing Go/Rust signatures remain correct

**Risks**:
- Tree-sitter query differences across language grammars
- Some languages have very long signatures — may need truncation
- Per-language effort; prioritise by usage: TS > Python > Java > C > Rust

---

## Sprint Checklist

### Sprint 0 — P0 ✅
- [x] P0-A: Ranking before LIMIT — fetch wider window, rank, then truncate
- [x] P0-B: DB open/close — command-scoped store lifecycle

### Sprint 1 — P1 ✅
- [x] P1-A: Ambiguity error → ranked results in context command
- [x] P1-B: `--path` / `--exclude` flags on search, refs, show
- [x] P1-C: `show --all` and structured `also` in JSON

### Sprint 2 — P2 (in progress)
- [ ] P2-A: Generated code detection in ranking
- [ ] P2-B: Refs scope filtering with `--file`
- [ ] P2-C: Text search delegation to rg

### Sprint 3 — P3 (do when capacity allows)
- [ ] P3-A: EnsureFresh mtime cache
- [ ] P3-B: Signature extraction improvement

---

## Bench integration

Each sprint should add bench cases that would have caught the problem:

| Sprint | Bench additions |
|---|---|
| P0-A | Symbol with 50+ definitions, canonical not in first 20 |
| P0-B | Latency benchmark for investigate/context (should improve) |
| P1-A | `context Context` in gin — must not error |
| P1-B | `search --path 'src/*'` ground truth |
| P1-C | `show --all` result count validation |
| P2-A | Search in protobuf project — generated vs hand-written ranking |
| P2-B | `refs --file` precision vs unscoped refs |
| P2-C | `search --text` latency comparison |
| P3-A | Cold-start latency on 10k+ file repo |
| P3-B | Signature completeness audit per language |

---

## Sequencing rationale

**P0 first** because these cause silent wrong answers — the worst failure mode. An agent that gets a wrong-but-plausible answer is worse off than one that gets nothing.

**P1 second** because these cause agent dead ends — the second-worst failure mode. The agent falls back to grep, which works but is slower and noisier.

**P2 third** because these are noise/polish — cymbal works but could work better.

**P3 last** because these are architectural investments — important but not blocking current usage.
