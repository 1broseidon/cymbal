---
id: task-12
title: Remove repeated query and rendering work
priority: medium
tags:
  - audit
  - performance
  - query
  - rendering
relatedFiles:
  - cmd/output.go
  - cmd/refs.go
  - cmd/impact.go
  - cmd/investigate.go
  - index/store.go
  - index/graph.go
subtasks:
  - id: task-12-1
    title: Share source snippets across JSON and text rendering
    completed: true
  - id: task-12-2
    title: Batch enclosing-symbol resolution for impact call sites
    completed: true
  - id: task-12-3
    title: Fetch graph metadata only for relevant names
    completed: true
  - id: task-12-4
    title: Benchmark reduced work and verify output equivalence
    completed: true
createdAt: "2026-09-10T03:52:29.580Z"
updatedAt: "2026-09-10T05:11:57.281Z"
completedAt: "2026-09-10T05:11:57.281Z"
---

## Description
Audit findings #6, #7, and #8. Small result sets incur repeated source reads, per-reference SQL queries, and whole-repository graph preparation.

Scope:
- Collect requested source ranges by file and read each file once up to the highest requested line. Share typed snippet data between renderers and derive the central line from loaded context. Keep caching command-scoped.
- Replace one EnclosingSymbol SQL lookup per reference with per-file interval loading or a batched query. Preserve innermost enclosure, language scope, caller deduplication, and hide-but-traverse test behavior.
- Collect graph root/traversed names before fetching metadata. Load all relevant definitions and languages for those names and use a composite-key set for exact duplicate suppression. Preserve ambiguity and unresolved diagnostics.

Measured baseline:
- strace observed 60 opens of one source file to display 20 references.
- With the same 20 impact callers, median lookup time grew from 0.80 ms for 20 call sites to 35.03 ms for 1,000 call sites.
- A one-node graph with 10,000 unrelated symbols took 28.92 ms and allocated about 3.49 MB; plain trace remained about 0.066 ms. These are local synthetic measurements, not production guarantees.

Completion criteria:
- Existing text grouping, JSON envelopes, traversal semantics, and diagnostics remain equivalent.
- Source-file reads, enclosing-symbol queries, and irrelevant graph allocations are reduced with evidence from representative fixtures.
- Add meaningful behavior tests and focused benchmarks without introducing a daemon, storage rewrite, or persistent stale cache.

Source: September 9, 2026 efficiency audit of ac794409a7fa1148e3cdf155991fbb662c3d9487. Full report: /home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-efficiency-audit-2026-09-09.md

## Log
- 2026-09-10T05:11:57.090Z: [codex] Implemented source-range batching shared by text/JSON, per-file enclosing-symbol intervals for impact, and graph metadata queries restricted to root/traversed names (500-name batches with exact duplicate sets). Verified 60 to 1 source opens for 20 refs; 14 CLI output/exit comparisons identical. Three-run benchmark medians: enrichment 0.433 to 0.048 ms; 1,000 impact call sites 36.855 to 1.947 ms; one-node graph with 10,000 unrelated symbols 27.793 to 0.092 ms. Full Go tests, affected-package race tests, build, vet, formatting pass; same 7 pre-existing staticcheck findings. Updated changelog and local go install. Corpus harness: 113/113 accuracy, 79/79 ground truth, 18/18 ranking, 154/154 timings; existing Vite grep-noise fixture mismatch unchanged. Changes remain local and uncommitted. Evidence: /home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-task12-results-2026-09-10.md
