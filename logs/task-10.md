---
id: task-10
title: Fix search and traversal completeness
priority: high
tags:
  - audit
  - correctness
  - search
  - traversal
relatedFiles:
  - cmd/pathfilters.go
  - cmd/search.go
  - cmd/refs.go
  - index/index.go
  - index/store.go
subtasks:
  - id: task-10-1
    title: Apply path constraints before accepted-result limits
    completed: true
  - id: task-10-2
    title: Retain resolved short callees in trace and graph traversal
    completed: true
  - id: task-10-3
    title: Align regex and file eligibility semantics across text-search backends
    completed: true
  - id: task-10-4
    title: Add regression coverage for scoped results and backend parity
    completed: true
createdAt: "2026-09-10T03:52:29.161Z"
updatedAt: "2026-09-10T04:23:59.692Z"
completedAt: "2026-09-10T04:23:59.692Z"
---

## Description
Audit findings #1, #2, and #5. Normal queries currently omit valid results or change meaning depending on whether ripgrep is installed.

Scope:
- Replace bounded overfetch followed by path filtering in symbol search and references. Apply limits to accepted results, using retrieval-time constraints or pagination. Include the Go text fallback and preserve ranking and repeated glob behavior.
- Remove the blanket exclusion of one- and two-character callees from call traversal. Use reference kind and resolution evidence instead of name length.
- Establish consistent regex and file eligibility semantics for ripgrep and the Go fallback. Compile fallback patterns once and surface invalid-pattern diagnostics instead of silently switching to literal matching.

Evidence:
- In 151 packages defining Handle, unrestricted search finds all definitions but a default-limit search scoped to a package outside the first 100 candidates reports no results.
- For Caller calling Do and Longer, refs Do finds the call while trace Caller returns only Longer.
- Text search for Alpha|Beta returns both definitions with ripgrep and no results without it.

Completion criteria:
- A scoped definition or reference beyond the initial candidate window is found without raising the user limit.
- Both short and longer real callees appear in trace and graphs; ordinary variable references remain excluded by default.
- Supported regex patterns, errors, filters, and file eligibility agree across backends.
- Add focused regression tests and preserve existing output schemas.

Source: September 9, 2026 efficiency audit of ac794409a7fa1148e3cdf155991fbb662c3d9487. Full report: /home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-efficiency-audit-2026-09-09.md

## Log
- 2026-09-10T04:23:59.498Z: [codex] Implemented locally. PathFilter now applies during symbol/reference retrieval before accepted-result limits; short real callees remain visible in trace and graphs. Text search uses the indexed inventory and deterministic path/line ordering. Literal queries can use ripgrep; regex syntax uses the native Go matcher, compiled once, avoiding dialect drift and whole-file JSON serialization. Invalid patterns and file-read failures propagate. Existing literal TextSearch API and JSON envelope shapes remain available.

Validation: full go test ./... passed with FTS5 and GOFLAGS=-p=1 using an isolated TMPDIR; go test -race ./cmd ./index ./walker passed; the final backend-selection adjustment also passed focused race tests. go vet ./... and git diff --check passed. End-to-end checks on a built CLI confirmed a late scoped symbol, short callees, actual PATH-based fallback, and valid JSON output. New regressions: cmd/search_completeness_test.go and TestTraceIncludesShortCalls in index/freshness_errors_test.go. Documentation and CHANGELOG.md Unreleased updated.

Staticcheck still reports the same seven pre-existing audit warnings, with no new findings. Identified cleanup remains in the medium-priority tasks. Implementation is in the working tree and has not been committed.
