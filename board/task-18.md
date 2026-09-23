---
id: task-18
title: "Cleanup: empty-result contract, dead code, OpenCode tests, make startup"
column: todo
position: 3
priority: medium
tags:
  - cleanup
  - bug
  - cli
  - json
  - dead-code
  - testing
  - ci
  - build
relatedFiles:
  - cmd/impact.go
  - cmd/trace.go
  - cmd/refs.go
  - cmd/investigate.go
  - index/index.go
  - index/store.go
  - bench/main.go
  - bench/groundtruth.go
  - cmd/graph_render_test.go
  - cmd/hook_assets/opencode/cymbal-opencode.js
  - cmd/hook_assets/opencode/cymbal-opencode.test.mjs
  - .github/workflows/ci.yml
  - Makefile
  - CHANGELOG.md
createdAt: "2026-09-23T18:00:25.638Z"
subtasks:
  - id: task-18-1
    title: Empty results exit 0 with a zero-count result in impact, trace, refs and investigate
    completed: false
  - id: task-18-2
    title: Names the index does not know exit 1 with symbol not found, batches included
    completed: false
updatedAt: "2026-09-23T18:00:32.412Z"
---

## Description
One cleanup pass over three areas, all re-verified against main 047b568 on 2026-09-23. Replaces task-13, task-14 and task-16; their logs hold the audit notes. Sources: the September 9 efficiency audit of ac79440 (/home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-efficiency-audit-2026-09-09.md) and the September 22 impact report.

Part 1 changes exit codes and output, so it needs a CHANGELOG entry. Parts 2 and 3 are internal.

### 1. Empty results are results: impact, trace, refs, investigate

Today:
- `impact` on a symbol nothing calls exits 1 with `Error: no callers found for 'X'`, in text and `--json` (cmd/impact.go:65-70). A missing symbol prints exactly the same, so a script cannot tell "no callers" from a typo.
- `trace` and `refs` exit 0 on an empty result, but `trace --json` prints plain text instead of JSON, and `trace` writes its note to stdout while `refs` writes to stderr (cmd/trace.go:92-94, cmd/refs.go:103).
- `investigate` prints "symbol not found" and exits 0 (cmd/investigate.go:84-88, and `{"error": "not found"}` with exit 0 in `--json`), while `show`, `context` and `search` exit 1.

Contract:
- The index knows the name (a definition, or references to it, such as an external `Println`) but the answer is empty: exit 0, with the same output shape as a full result and a zero count. That means frontmatter such as `total_callers: 0` in text, and the usual `{"version", "results"}` envelope with empty results in `--json`.
- The index knows nothing about the name: exit 1 with `symbol not found: X` on stderr, as `show` and `context` do.
- Batches keep the v0.16 rule: print what succeeded and report missing names through the exit status.
- Index and query failures stay errors.

Tests that pin today's behavior change with it, starting with cmd/cli_codecov_test.go:281-285 (impact on MissingSymbol expects "no callers found").

### 2. Dead code, duplication and the file hash

- `Investigate` (index/index.go:1047) and `InvestigateResolved` (index/index.go:1119) repeat the same kind switch. The only real difference is the source policy: full source, or a 60-line cap for types. Share the switch and pass the policy explicitly. `index.Investigate` is documented in docs/guide/library.md and stays public. The cap lists 9 type kinds but the switch lists 12, so large `protocol`, `record` and `actor` symbols are never capped; use one list.
- `investigateOne` and `investigateOnePrint` (cmd/investigate.go:98, 121) each resolve the name and build the result. Share that. Both callers of `investigateOnePrint` pass `jsonOut=false` (cmd/investigate.go:84, cmd/cli_phase2_test.go:443), so remove the JSON branch and the parameter.
- Delete `containsAll` (bench/main.go:501), `groundTruthImplsResponse` (bench/groundtruth.go:691) and `captureStderr` (cmd/graph_render_test.go:169). These are exactly what `golangci-lint run --default=none --enable=unused --tests=true ./...` reports. Also delete the `processed` closure in `Index` (index/index.go:437-441); progress output comes from `startProgress`.
- Stop computing the per-file hash in `Index`. Workers hash only files already in the index (index/index.go:482-486), so a first index stores an empty hash, a re-parsed file gets one, and nothing compares it: freshness is mtime plus size. Remove the computation and the `parseResult.hash` plumbing, store an empty hash, and fix the comments at index/index.go:449 and 469. Keep the exported `HashFile`, `HashBytes` and `Store.FileHash`, and correct the `FileHash` doc comment, which claims it returns the stored hash for any indexed file. Keep the `hash` column, so no migration.

Dropped: making the walker append directly (old subtask 13-4). Since #81 the walker workers stat and read the `#!` line of extensionless files (walker/walker.go:123-135), so the pool does real I/O.

### 3. OpenCode tests and make startup

- cmd/hook_assets/opencode/cymbal-opencode.test.mjs has not run since 95b0a43 (#61, 2026-06-01): `node --test` fails with "does not provide an export named 'appleScriptString'", so none of its 14 tests run. The plugin exports only `CymbalPlugin` (cymbal-opencode.js:117), which #61 did so OpenCode 1.15.13 would load it, and it ships as one embedded file (cmd/hook_assets_opencode.go:9). So do not add exports back: retire the `parseUpdateNotice` tests (that function is gone) and test `updateNotifierDisabled`, `appleScriptString` and `buildNotificationCommand` (cymbal-opencode.js:5, 10, 22) through `CymbalPlugin`.
- Run `node --test` on the suite in CI's test job. Today it runs only `node --check` (.github/workflows/ci.yml:59).
- `MODULE` and `COVER_PACKAGES` (Makefile:9-10) use `:=`, so every target first runs one `go list` per directory. `make -n clean` takes 390 ms, and 10 ms with the two deferred, with identical recipe output. Defer them to `test-coverage`, list packages with a single `go list`, and keep the bench and root-package exclusions explicit. The root package has no tests today; state whether future root tests should be covered.

### Done when

- `impact`, `trace`, `refs` and `investigate` follow the contract in text and `--json`, with tests for a known name with an empty result, an external name, a missing name, and a batch with one missing name.
- The unused linter reports nothing, and `investigate` output is unchanged apart from the type-cap fix.
- `Index` stores no file hash, and the comments say so.
- `node --test` runs in CI and passes, and `make -n clean` runs no `go list`.
- CI passes on Linux, macOS and Windows.
- CHANGELOG `[Unreleased]` describes the part 1 behavior change.
