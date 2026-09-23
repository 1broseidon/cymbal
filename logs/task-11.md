---
id: task-11
title: Preserve filesystem permissions and report failures correctly
priority: high
tags:
  - audit
  - correctness
  - storage
  - cli
relatedFiles:
  - index/store.go
  - index/index.go
  - cmd/root.go
  - cmd/outline.go
  - cmd/refs.go
  - cmd/investigate.go
subtasks:
  - id: task-11-1
    title: Preserve permissions on existing custom database parent directories
    completed: true
  - id: task-11-2
    title: Expose refresh failures separately from the no-op change count
    completed: true
  - id: task-11-3
    title: Propagate single-target failures and define partial-batch exit status
    completed: true
  - id: task-11-4
    title: Test directory modes and operational failure exit codes
    completed: true
createdAt: "2026-09-10T03:52:29.368Z"
updatedAt: "2026-09-10T04:24:00.877Z"
completedAt: "2026-09-10T04:24:00.877Z"
---

## Description
Audit findings #3 and #4. Opening a custom database can modify an unrelated existing directory, and command loops can report successful exit status after operational failures.

Scope:
- OpenStore currently chmods the database parent to 0700 unconditionally. Preserve permissions on an existing user-selected parent while retaining restrictive creation modes for new private cache directories and database files.
- Propagate single-target query failures in outline and inspect the corresponding text loops in refs and investigate. Accumulate batch errors while retaining successful output and define partial-batch exit behavior explicitly.
- Give refresh failures a distinct error path so they cannot be mistaken for a successful no-op. Preserve compatibility of public APIs when introducing the internal error-aware path.

Evidence:
- Running index with --db ./custom.db changes a disposable repository directory from 0755 to 0700.
- Outline against a plain-text corrupt.db prints a SQLite schema error but exits with status 0.

Completion criteria:
- Existing parent-directory permissions remain unchanged; newly created private cache and database permissions retain their intended protection.
- Operational refresh/query failures produce nonzero exits, including when a batch contains successful results.
- Successful batch output is preserved and normal no-match behavior remains explicitly defined.
- Regression tests check filesystem modes, stderr, successful output, and exit status.

Source: September 9, 2026 efficiency audit of ac794409a7fa1148e3cdf155991fbb662c3d9487. Full report: /home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-efficiency-audit-2026-09-09.md

## Log
- 2026-09-10T04:24:00.673Z: [codex] Implemented locally. OpenStore preserves existing custom-database parent permissions while creating private cache directories and database files. EnsureFreshWithError reports database, metadata, discovery, and partial write/read failures separately from the change count; the legacy EnsureFresh signature remains available. Every existing command freshness call now propagates errors. Failed file discovery cannot prune from an incomplete inventory. Outline, refs, and investigate aggregate operational batch failures while preserving successful output; ordinary no-match behavior remains nonfatal for those commands.

Validation: full go test ./... passed with FTS5 and GOFLAGS=-p=1 using an isolated TMPDIR; go test -race ./cmd ./index ./walker passed; go vet ./... and git diff --check passed. End-to-end checks confirmed nonzero exit for a corrupt database, unchanged 0755 parent permissions with --db ./custom.db, and successful JSON entries retained alongside a failing batch item. New regression files: cmd/operational_errors_test.go and index/freshness_errors_test.go. Documentation and CHANGELOG.md Unreleased updated.

The initial /tmp test-environment Git-parent issue was avoided with an isolated temporary directory outside that Git tree. Staticcheck retains seven pre-existing warnings, with no new findings. Implementation is in the working tree and has not been committed.
