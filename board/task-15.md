---
id: task-15
title: "Per-symbol body hash: detect whether a symbol changed"
column: in-progress
position: 0
priority: medium
tags:
  - agent-flow
  - indexing
  - json
  - identity
relatedFiles:
  - index/store.go
  - index/index.go
  - cmd/show.go
  - cmd/investigate.go
  - cmd/output.go
createdAt: "2026-09-23T02:59:45.046Z"
updatedAt: "2026-09-23T15:27:57.596Z"
---

## Description
cymbal has no way to say "this function is the same as it was". The internal SymbolID (index/store.go:650-653, rel_path:lang:kind:name:start_line) is never output and breaks on any edit above the symbol; symbols.id is an autoincrement rebuilt on reindex; the only content hash is per file.

Add a body_hash per symbol: sha256 of the symbol's source span (start..end), normalized for line endings and trailing whitespace, computed at index time and stored in the symbols table. Emit it in --json for show, investigate, search, outline and context. Optionally add `cymbal changed --since-hash` later.

Why it is worth it on its own: an agent (or a script) can record `file:Name` + body_hash and later ask "did this symbol's body change?" in one call, which survives edits elsewhere in the file and line shifts. Anything that wants to anchor to a symbol (a decision log, a review note, a test-impact cache) can do it without cymbal knowing about it.

Coordinate with task-13's hash subtask ("Resolve ineffective internal hash computation with an explicit policy"): file hashes are computed but never compared; the per-symbol hash should follow the same policy decision.

Done when: `cymbal show Foo --json` includes body_hash; editing a different function in the same file leaves Foo's hash unchanged; editing Foo's body changes it; whitespace-only line-ending changes do not.

## Log
- 2026-09-23T15:27:57.873Z: [claude] PR #84 open (branch feat/symbol-body-hash). body_hash = first 16 hex of sha256 over whole lines start..end, CRLF and trailing whitespace normalized; emitted in search/show/outline/context/investigate/structure JSON. New meta index_format=1 forces a one-time full reparse of older indexes. Stdlib bench: no measurable index-time change, DB +4.6%.
