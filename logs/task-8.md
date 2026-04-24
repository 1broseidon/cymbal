---
id: task-8
title: "Coverage Phase 3: Index facade, diff, and update notifier regressions to reach 80%"
priority: high
tags:
  - coverage
  - tests
  - index
  - updatecheck
updatedAt: "2026-04-24T15:34:44.424Z"
completedAt: "2026-04-24T15:34:44.424Z"
---

# Coverage Phase 3

Close the remaining gap to 80% by testing public library contracts and small behavior-heavy utilities. This phase should stabilize APIs, not chase private branches.

## Rule

All local verification that runs Go tests must preserve SQLite FTS5 support:

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go test ./...
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage
```

Do not add tests for `init()` or trivial wrappers unless they protect a public behavior. Prefer tests that would catch a real regression in the CLI, library API, or release/update workflow.

## Context

Several exported functions in `index/index.go` are currently uncovered because existing tests exercise lower-level `Store` methods directly. These exported functions are the API consumers embed, so they are valid coverage targets. `diff` and update notification logic also encode user-visible behavior that should not regress.

## Work

1. Add public `index` facade tests:
   - `SearchSymbols`
   - `SearchSymbolsFlex`
   - `FileOutline`
   - `RepoStats`
   - `Structure`
   - `SymbolContext`
   - `Investigate` / `InvestigateResolved`
   - `FindReferences`
   - `FindImporters`
   - `FindImportersByPath`
   - `FindTrace`
   - `FindImpact`
   - `BuildGraph`
2. Add ranking/selection regressions where they affect API behavior:
   - canonical source preferred over tests/examples/vendor/generated files.
   - exact search over-fetch does not truncate duplicate symbol candidates.
3. Add focused diff tests:
   - `filterDiffHunks` overlap behavior for added, changed, and deleted hunks.
   - `parseHunkHeader` edge cases.
   - end-to-end `runDiff` and `runDiffStat` against a temp git repo where useful.
4. Add update notifier tests:
   - `FormatNotice`.
   - `AugmentReminder`.
   - `MarkNotified`.
   - stale cache behavior.
   - failure backoff.
   - install method command rendering.

## Acceptance Criteria

- Public index APIs have direct regression tests around their promised behavior.
- Diff and update notifier behavior is covered without network dependence.
- `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage` passes.
- Product coverage reaches or exceeds 80%.
