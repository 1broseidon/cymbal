---
id: task-16
title: "impact: no callers is a result, not an error"
column: todo
position: 5
priority: medium
tags:
  - bug
  - cli
  - scripting
  - json
  - superseded
relatedFiles:
  - cmd/impact.go
  - main.go
  - cmd/output.go
createdAt: "2026-09-23T03:04:19.327Z"
updatedAt: "2026-09-23T18:00:51.555Z"
---

## Description
`cymbal impact <sym>` on a symbol with no callers prints `Error: no callers found for '<sym>'` to stderr and exits 1, in both text and --json mode (reproduced 2026-09-22 on v0.15.0 with a one-function Go repo). "Nothing calls this" is a valid, useful answer, and scripts and agents cannot tell it apart from a real failure (bad symbol, missing index).

Return exit 0 with an empty result: `total_callers: 0` in the frontmatter, and in --json the usual {"version","results"} envelope with total_callers 0 and results []. Keep exit 1 for real errors (symbol not found, index failure) and consider distinguishing "symbol not found" from other errors.

Done when: impact on an uncalled symbol exits 0 with total_callers 0 in both modes; impact on a nonexistent symbol still fails.

## Log
- 2026-09-23T17:54:14.508Z: [claude] Audit 2026-09-23 against main 047b568: still valid. impact on an uncalled symbol exits 1 with 'no callers found' in both text and --json, and a nonexistent symbol gives identical output. The error comes from cmd/impact.go:65-70. cmd/cli_codecov_test.go:281-285 pins it using MissingSymbol, so that test changes with the fix. Related: trace and refs exit 0 on empty results, but trace --json prints plain text rather than JSON, refs writes its message to stderr while trace writes to stdout, and none of the three distinguishes a missing symbol.
- 2026-09-23T18:00:51.298Z: [claude] Merged into task-18 on 2026-09-23, with the user's decisions: drop 13-4, widen 16 to trace, refs and investigate, and remove the per-file hash. The audit notes above are carried into task-18's description.
