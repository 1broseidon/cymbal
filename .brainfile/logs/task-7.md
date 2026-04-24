---
id: task-7
title: "Coverage Phase 2: CLI golden regressions for user-facing commands"
priority: high
tags:
  - coverage
  - tests
  - cli
  - regression
updatedAt: "2026-04-24T15:26:00.730Z"
completedAt: "2026-04-24T15:26:00.730Z"
---

# Coverage Phase 2

Add regression-focused CLI tests for the commands users and agents actually depend on. Prefer temp repositories plus real indexing over isolated line coverage.

## Rule

All local verification that runs Go tests must preserve SQLite FTS5 support:

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go test ./...
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage
```

Do not snapshot huge outputs unless formatting is the contract. Prefer targeted assertions on sections, metadata, JSON shape, ordering, and safety behavior.

## Context

Most of `cmd/` is low coverage even though it is the primary product surface. This is the best place to catch real regressions: wrong symbol resolution, broken JSON output, missing sections, unsafe path handling, bad dedupe, and incorrect multi-symbol behavior.

## Work

Build a small shared CLI test harness that:

- creates temp repos with representative Go/Python/etc. files.
- indexes them with test-local DBs.
- captures stdout/stderr safely.
- resets global command flags/state between tests.
- keeps assertions concise and behavior-driven.

Cover these command contracts:

1. `investigate`
   - function vs type output shape.
   - ambiguous match metadata.
   - fuzzy match metadata.
   - source, callers, impact, members, implementors sections where applicable.
2. `context`
   - source section.
   - caller context.
   - file imports.
   - implementors/implements sections for type-like symbols.
3. `trace` and `impact`
   - single-symbol output.
   - multi-symbol dedupe.
   - `hit_symbols` attribution in JSON.
   - empty-result behavior.
4. `impls`
   - incoming implementors.
   - `--of` inverse direction.
   - `--resolved`, `--unresolved`, `--lang`, `--path`, `--exclude`.
5. `refs`
   - normal refs.
   - `--importers`.
   - `--impact`.
   - `--file` and path filters.
6. `show`, `outline`, `ls`, `structure`
   - symbol and file-range output.
   - JSON shape.
   - type member output.
   - stable tree/stats/outline behavior.
7. `diff`
   - temp git repo with edited symbol.
   - symbol-scoped hunk filtering.
   - `--stat`.
   - invalid base rejection.

## Acceptance Criteria

- CLI tests cover real command workflows rather than private helpers by default.
- Each command above has at least one regression test around its core contract.
- `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage` passes.
- Product coverage is in the low/mid 70s before Phase 3.
