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
relatedFiles:
  - cmd/impact.go
  - main.go
  - cmd/output.go
createdAt: "2026-09-23T03:04:19.327Z"
---

## Description
`cymbal impact <sym>` on a symbol with no callers prints `Error: no callers found for '<sym>'` to stderr and exits 1, in both text and --json mode (reproduced 2026-09-22 on v0.15.0 with a one-function Go repo). "Nothing calls this" is a valid, useful answer, and scripts and agents cannot tell it apart from a real failure (bad symbol, missing index).

Return exit 0 with an empty result: `total_callers: 0` in the frontmatter, and in --json the usual {"version","results"} envelope with total_callers 0 and results []. Keep exit 1 for real errors (symbol not found, index failure) and consider distinguishing "symbol not found" from other errors.

Done when: impact on an uncalled symbol exits 0 with total_callers 0 in both modes; impact on a nonexistent symbol still fails.
