---
id: task-6
title: "Coverage Phase 1: Define product denominator and cover parser/walker regressions"
priority: high
tags:
  - coverage
  - tests
  - parser
  - walker
updatedAt: "2026-04-24T15:24:12.737Z"
completedAt: "2026-04-24T15:24:12.737Z"
---

# Coverage Phase 1

Define the product coverage denominator honestly, then raise coverage with regression tests around parser and walker behavior that Cymbal explicitly supports.

## Rule

All local verification that runs Go tests must preserve SQLite FTS5 support:

```sh
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" go test ./...
CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage
```

Do not add tests only to execute lines. Each test must lock down supported behavior, a previously observed bug class, or a public contract.

## Context

Current Codecov coverage includes `bench/`, which is an internal benchmark/evaluation harness and pulls the reported number down significantly. Product coverage excluding `bench/` is the meaningful target unless `bench/` is treated as supported product behavior.

Parser coverage is already decent, but there are clear gaps in languages Cymbal advertises as supported. Walker tree rendering is mostly untested despite backing `ls`/tree-style behavior.

## Work

1. Exclude `github.com/1broseidon/cymbal/bench` from the product coverage target or Codecov coverage config.
2. Add parser fixture/table tests for real supported-language regressions:
   - HCL/Terraform blocks and names.
   - Protobuf messages/services/imports.
   - Ruby imports, refs, include/extend behavior.
   - Elixir imports/refs.
   - Scala `extends` / `with` conformance.
   - Ruby implementor extraction.
   - `ParseBytes`, `ParseFile`, unknown language handling.
3. Add walker tests for filesystem behavior:
   - skip `.git`, hidden dirs, `node_modules`, `vendor`, symlinks.
   - respect `maxDepth` in `BuildTree`.
   - produce stable sorted tree output.
   - keep `PrintTree` formatting stable for nested files.

## Acceptance Criteria

- Coverage denominator no longer treats `bench/` as product coverage.
- New tests fail before the intended behavior exists or if it regresses.
- `CGO_CFLAGS="-DSQLITE_ENABLE_FTS5" make test-coverage` passes.
- `go tool cover -func=coverage.txt` shows product coverage moving materially toward 80%.
