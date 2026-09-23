---
id: task-13
title: Consolidate builders and retire dead code
column: todo
position: 5
priority: medium
tags:
  - audit
  - maintainability
  - duplication
  - dead-code
relatedFiles:
  - index/index.go
  - index/store.go
  - cmd/investigate.go
  - bench/main.go
  - bench/groundtruth.go
  - cmd/graph_render_test.go
  - walker/walker.go
subtasks:
  - id: task-13-1
    title: Share investigate result construction while preserving public semantics
    completed: false
  - id: task-13-2
    title: Remove unreachable JSON handling and verified unused helpers
    completed: false
  - id: task-13-3
    title: Resolve ineffective internal hash computation with an explicit policy
    completed: false
  - id: task-13-4
    title: Replace walker metadata dispatch with direct append and verify parity
    completed: false
createdAt: "2026-09-10T03:52:29.797Z"
---

## Description
Audit findings #9 and #11. Merge repeated result construction and retire code that has no useful work, while preserving public behavior.

Scope:
- Share result assembly between Investigate and InvestigateResolved after symbol resolution. Pass source-reading policy explicitly so ambiguity and truncation behavior do not change accidentally.
- Share resolved data between investigateOne and investigateOnePrint. Every caller passes false for the printer's JSON flag: remove the unreachable JSON branch and boolean parameter.
- Delete the unused processed closure in Index and its obsolete progress comments.
- Remove staticcheck-confirmed unused containsAll (bench/main.go), groundTruthImplsResponse (bench/groundtruth.go), and captureStderr (cmd/graph_render_test.go).
- Resolve the ineffective hash path: Index hashes existing files after parsing but never compares hashes. Either implement a justified content check before parsing or remove unnecessary internal work and correct comments. Preserve exported HashFile/FileHash APIs unless a separate compatibility decision authorizes removal.
- Simplify WalkWithOptions: discovery, filtering, and stat calls already run serially; workers only copy metadata and append under a mutex. Append directly in the callback and retain filtering and sorting. Keep the real parser worker pool and preserve the public worker argument for compatibility.

Evidence: a direct-append walker prototype produced identical entries on a 4,000-file fixture and reduced median time from 16.70 ms to 14.53 ms. The main objective is simpler ownership, not a claimed large end-to-end speedup.

Completion criteria:
- Public result schemas, ambiguity, truncation, and walker output remain compatible.
- The identified unused code and unreachable branch are removed; relevant staticcheck U1000 findings are gone.
- Hash behavior and comments agree, with focused coverage for any behavior change.
- Existing tests pass and the walker comparison still demonstrates parity.

Source: September 9, 2026 efficiency audit of ac794409a7fa1148e3cdf155991fbb662c3d9487. Full report: /home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-efficiency-audit-2026-09-09.md

## Log
- 2026-09-23T15:27:58.141Z: [claude] Subtask task-13-4's premise changed: #81 (merged in #83) gives the walker's worker pool real work, a stat plus a 256-byte #! read for each extensionless file. Removing the worker dispatch would move those reads back into the serial WalkDir callback. Re-measure before doing it, or drop the subtask. Subtask task-13-3: task-15 (PR #84) leaves the per-file hash alone and adds its own per-symbol hash, so the per-file hash policy question is still open.
