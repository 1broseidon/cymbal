---
id: task-9
title: Worktree federation for symbol lookup (#44)
column: todo
position: 1
priority: high
tags:
  - worktree
  - indexing
  - issue-44
  - agent-flow
relatedFiles:
  - cmd/root.go
  - cmd/search.go
  - cmd/show.go
  - cmd/investigate.go
  - cmd/impact.go
  - cmd/trace.go
  - cmd/impls.go
  - cmd/refs.go
  - index/index.go
subtasks:
  - id: task-9-1
    title: index.RepoCommonDir + EnumerateWorktrees + unit tests
    completed: true
  - id: task-9-2
    title: cmd.resolveDBs + --no-federate flag
    completed: true
  - id: task-9-3
    title: federate cmd/search.go
    completed: true
  - id: task-9-4
    title: federate cmd/show.go + cmd/investigate.go (seed-only fan-out)
    completed: true
  - id: task-9-5
    title: federate cmd/impact.go + trace + impls + refs (seed-only)
    completed: true
  - id: task-9-6
    title: feature tests covering hard regression guarantees
    completed: true
  - id: task-9-7
    title: CHANGELOG entry + README mention
    completed: true
contract:
  status: ready
  deliverables:
    - type: file
      path: index/index.go
      description: RepoCommonDir + EnumerateWorktrees helpers
    - type: file
      path: cmd/root.go
      description: resolveDBs + DBPlan + --no-federate flag
    - type: file
      path: cmd/search.go
      description: federation in search with worktree label
    - type: file
      path: cmd/show.go
      description: seed-only federation in show
    - type: file
      path: cmd/investigate.go
      description: seed-only federation in investigate
    - type: file
      path: cmd/impact.go
      description: seed-only federation in impact
    - type: file
      path: cmd/trace.go
      description: seed-only federation in trace
    - type: file
      path: cmd/impls.go
      description: seed-only federation in impls
    - type: file
      path: cmd/refs.go
      description: federation in refs
    - type: test
      path: index/index_worktree_test.go
      description: RepoCommonDir + EnumerateWorktrees parser tests
    - type: test
      path: cmd/cli_worktree_test.go
      description: 6 feature tests covering hard regression guarantees
    - type: docs
      path: CHANGELOG.md
      description: next-release entry
  validation:
    commands:
      - make build-check
      - make lint
      - CGO_CFLAGS=-DSQLITE_ENABLE_FTS5 go test ./cmd/... ./index/...
      - CGO_CFLAGS=-DSQLITE_ENABLE_FTS5 go test ./... -run TestWorktree -v
  constraints:
    - No schema migration — per-worktree DBs remain the storage unit
    - No cross-worktree graph traversal (impact/trace/impls/refs stay within seed's DB)
    - No auto-index of foreign worktrees — unindexed siblings emit a stderr note and are skipped
    - "--db / $CYMBAL_DB / --no-federate disable federation completely"
    - Non-worktree repos must run no git worktree commands (zero overhead path)
    - Federation fan-out capped at 32 worktrees with stderr note
  metrics:
    readyAt: "2026-05-19T16:41:09.238Z"
createdAt: "2026-05-19T16:41:09.244Z"
---

## Description
## Problem

Cymbal scopes each repo's DB by workdir (`git rev-parse --show-toplevel` equivalent — `FindGitRoot` walks up to a `.git` dir or file). For git worktrees, `.git` is a *file* with `gitdir:` pointing into the main repo, so the worktree's path is unique and hashes to its own DB.

This is correct for isolation, but it breaks the agent flow described in issue #44: AI agents routinely keep cwd at the **main** repo while editing files inside a sibling worktree. From main's cwd, `cymbal search Foo` only opens main's DB, so symbols added in the worktree are invisible — agents fall back to grep, defeating the indexer in exactly the multi-branch workflows worktrees are designed for.

## Solution (paired A + B)

Two complementary changes, both shipping in one task because together they fully resolve the agent flow:

**A — Path-aware DB routing.** Extend the existing `repoRootForPath` pattern (already used by `cymbal show`) to every command that accepts a path argument. When the caller passes `cymbal search Foo /path/to/wt` or `--path /path/to/wt`, the DB lookup walks from that path, not cwd.

**B — Federated symbol lookup across worktrees.** When cwd is inside a repo with worktrees (detected via `git rev-parse --git-common-dir` differing from `--show-toplevel`, or via `git worktree list --porcelain` enumeration), `cymbal search` / `show` / `investigate` / `impact` / `trace` / `impls` / `refs` open every *already-indexed* worktree's DB belonging to the same common-dir and merge results. Non-current-worktree hits get a `worktree=<basename> (<branch>)` label. Federation is **default-on** with `--no-federate` escape hatch.

## Non-goals (hard, with tests)

1. **No cross-worktree graph traversal.** Federation locates the *seed* symbol across worktrees, but callers/impact/trace/impls stay within the worktree that owned the seed. Different branches = different code; connecting them produces nonsense.
2. **No auto-index of foreign worktrees.** If a worktree hasn't been indexed yet, it's silently skipped with a one-line stderr note. Never pay N×EnsureFresh on a single command.
3. **No schema migration.** Per-worktree DBs stay the source of truth. Federation is a query-time fan-out, not a storage change.

## Hard regression guarantees (explicit tests required)

1. Inside a non-worktree repo → no `git worktree list` call, behavior byte-identical to today.
2. Single worktree (no siblings) → opens exactly 1 DB, behavior identical to today.
3. `--db` or `$CYMBAL_DB` set → federation off, exact DB used.
4. JSON output: existing fields untouched; `worktree` field added only when a result came from a non-current worktree.
5. `cymbal ls` / `outline` / `structure` / `diff` / `index` → unchanged (these are "tell me about my current checkout" commands).

## Architecture

New helpers in `index/index.go`:
- `RepoCommonDir(repoRoot string) (string, error)` — runs `git rev-parse --git-common-dir` to detect worktree relationships. Returns the common dir path; if it equals `<repoRoot>/.git`, no federation needed.
- `EnumerateWorktrees(commonDir string) ([]Worktree, error)` — parses `git worktree list --porcelain`. Returns `{Path, Branch, IsCurrent}`. Implementation parses the porcelain format directly (no extra deps).

`getDBPath` in `cmd/root.go` evolves:
- Current: `func getDBPath(cmd *cobra.Command) string`
- New: keep `getDBPath` returning primary DB for back-compat. Add `func resolveDBs(cmd *cobra.Command, paths []string) DBPlan` that returns `{Primary string, Federated []DBEntry, FederationOff bool}`. Each `DBEntry` has `{Path, Root, Branch, IsCurrent}`.
- `--db` / `$CYMBAL_DB` set → `FederationOff=true`, only Primary populated.
- Positional path or `--path` given → Primary routes to that path's repo via `repoRootForPath`; Federated still populated for the same common-dir family if applicable.
- `--no-federate` flag → `FederationOff=true`.

Each consuming command (`search`, `show`, `investigate`, `impact`, `trace`, `impls`, `refs`) calls `resolveDBs`, iterates `Federated`, queries each DB with the existing per-DB query function, merges results with worktree labels.

For `show` / `investigate` / `impact` / `trace` / `impls`: federation only fans out the **seed lookup**. Once the seed is located in DB X, all downstream operations (source read, refs, impact graph) run only against DB X. This enforces non-goal #1 and keeps performance sane.

## Output shape

JSON: add optional `worktree` field at the top of each result. Omitted when the result came from cwd's DB. Format: `"worktree": "foo (feat/foo)"` (basename + branch when they differ, basename alone when they match).

Text: prefix non-current hits with `[worktree:foo]` so humans see the source. Current-cwd hits unchanged.

## Performance budget

- Federation cost = N × (open DB + run query). N is small (typical worktree count 1–5; reporter's example uses 1).
- Hard cap: federation skipped if N > 32 (with stderr note). Pathological repos with hundreds of worktrees fall back to single-DB behavior.
- `RepoCommonDir` cached per process invocation (called once, not per command iteration).
- DBs opened read-only and closed after the command; no long-lived federation pool needed.

## Implementation order

1. `index.RepoCommonDir` + `index.EnumerateWorktrees` + unit tests (table-driven, no real git invocation — parser tests use canned porcelain output).
2. `cmd.resolveDBs` + `--no-federate` flag wired into root command. Existing `getDBPath` stays as a thin shim over `resolveDBs(...).Primary`.
3. Federation in `cmd/search.go` first (simplest payload). Add `worktree` field to search result struct.
4. Federation in `cmd/show.go`, `cmd/investigate.go`. Seed-only fan-out; downstream uses single DB.
5. Federation in `cmd/impact.go`, `cmd/trace.go`, `cmd/impls.go`, `cmd/refs.go`. Same seed-only pattern.
6. Feature tests:
   - `TestWorktreeFederationSearchFromMainCwd` — reproduces the issue's repro verbatim, asserts symbol found.
   - `TestWorktreeFederationNonGoalCrossGraph` — asserts impact/trace don't cross worktree boundaries.
   - `TestWorktreeNoFederationFlag` — `--no-federate` restores old behavior.
   - `TestWorktreeNoFederationWithDBOverride` — `$CYMBAL_DB` set → single DB even with siblings.
   - `TestWorktreeFederationSkipsUnindexedSiblings` — un-indexed worktree → stderr note, no auto-index.
   - `TestWorktreeNoRegressionInPlainRepo` — non-worktree repo runs no `git worktree list`.
7. CHANGELOG entry + README mention.

## Out of scope (file follow-up issues if they bite)

- Unified DB keyed by common-dir (reporter's literal option (a)). Stays on table if federation perf collapses at scale.
- Cross-worktree `impact` / `trace`. Deliberate non-goal — connecting different branches produces wrong graphs.
- Auto-indexing of all worktrees on first command. Deliberate — explicit `cymbal index` per worktree, or first cd-in trigger.
