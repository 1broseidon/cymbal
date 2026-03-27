---
id: task-1
title: "Fix index correctness: stale files, partial commits, stats accuracy"
createdAt: "2026-03-24T19:30:24.430Z"
completedAt: "2026-03-24T19:43:22.645Z"
updatedAt: "2026-03-24T19:43:22.645Z"
---

## Description
Three related correctness bugs found during GPT-5.4 review of the indexing pipeline:

1. **Deleted files never pruned** — renamed/deleted files remain in the index indefinitely. After walker.Walk() returns the current file set, diff against stored paths in DB and DELETE stale rows in batch. This is a real accuracy bug.

2. **Partial file data on write error** — InsertFileAllTx does multiple statements per file (delete + insert file + insert symbols + insert imports + insert refs). If it fails mid-file, partial data commits with the batch. Fix: use SAVEPOINT per file inside the batch transaction, or abort the whole batch on any error.

3. **Stats inflated on commit failure** — indexed.Add(1) and found.Add() happen before tx.Commit() in flushBatch(). If commit fails, stats are wrong. Fix: accumulate local counters in flushBatch and publish only after successful commit.

4. **Parse errors counted as skipped** — parser.ParseFile errors increment skipped.Add(1) which hides real failures. Split into separate counters: unchanged, unsupported, parse_error, write_error.

Files: internal/index/index.go, internal/index/store.go
Verify: go build ./... && make test
