---
id: task-4
title: "Benchmark Harness v2: Accuracy, Speed, Token Savings, JIT Freshness"
column: todo
priority: high
tags: [bench, accuracy, tokens, freshness]
---

# Benchmark Harness v2

Extend bench/ to measure all three axes — accuracy, speed, and token savings — plus JIT freshness overhead. Everything automated, reproducible, no human evaluation needed.

## Context

Current bench/main.go measures speed and raw output size against ripgrep and ctags. Missing: accuracy verification, proper token savings ratios, JIT freshness benchmarks, and agent-workflow simulation.

## Design

### corpus.yaml changes

Add ground-truth assertions per symbol:

```yaml
symbols:
  - name: NewCmdRoot
    file_contains: "cmd/root.go"
    kind: function
    show_contains: "func NewCmdRoot"
    refs_min: 1
```

### Phase 1: Speed + Token Efficiency (improve existing)

- Keep existing speed tables (median of 10 runs, warmup)
- Add `investigate` as a benchmarked operation
- Add savings ratio column: `1 - (cymbal_tokens / baseline_tokens)`
- Baseline = ripgrep output for same query (already captured)
- Token estimate: bytes/4 is fine for relative comparison

### Phase 2: Accuracy (new)

For each symbol × operation, verify:
- **search**: result contains expected file path, correct kind
- **show**: output contains expected signature string
- **refs**: at least refs_min callers found
- **investigate**: source section contains expected signature

Report: `correct / total` across all queries per repo and overall.
Fail loudly if accuracy drops below threshold (e.g., 95%).

### Phase 3: JIT Freshness (new)

Measure query latency in four states:
1. Index current (baseline) — should be ~2ms overhead
2. After `touch` on 1 file — JIT reindex 1 file
3. After `touch` on 5 files — JIT reindex 5 files
4. After deleting 1 file — JIT prune

Report: absolute latency per state + overhead vs baseline.
Proves "always correct, always fast" claim.

### Phase 4: Agent Workflow Simulation (new)

For each symbol, compare:
- **Without cymbal**: rg search + rg -A30 (show) + rg refs = 3 tool calls, total output bytes
- **With cymbal**: `cymbal investigate` = 1 call, total output bytes

Report:
- Tool calls: 3 vs 1
- Total tokens: baseline vs cymbal
- Savings ratio

### RESULTS.md output

Add new sections to the generated report:
- Accuracy table (per repo, overall)
- JIT Freshness table
- Agent Workflow Savings table
- Keep existing speed + output size tables

### Implementation notes

- All changes in bench/main.go + corpus.yaml
- No external dependencies beyond what's already used (yaml, exec)
- Token counting stays bytes/4 (good enough for relative claims, no tiktoken dep needed)
- Accuracy checks are string-contains assertions, not semantic matching
- JIT freshness uses os.Chtimes / os.Remove on real indexed files

## Acceptance criteria

- `go run ./bench run` produces all four sections in RESULTS.md
- Accuracy is ≥95% across all repos
- JIT freshness overhead is <50ms for 1-file touch on any corpus repo
- Agent workflow section shows clear call reduction and token savings
