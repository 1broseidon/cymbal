---
id: task-17
title: Record the git commit the index reflects
priority: low
tags:
  - indexing
  - provenance
  - git
  - json
  - wontfix
relatedFiles:
  - index/index.go
  - index/store.go
  - cmd/output.go
createdAt: "2026-09-23T03:04:19.542Z"
updatedAt: "2026-09-23T17:49:06.205Z"
completedAt: "2026-09-23T17:49:06.205Z"
---

## Description
The meta table only holds repo_root, index_exclude, index_include_generated and index_include_large_files (index/index.go:368, 608-614). Nothing records which commit, or whether the tree was dirty, when the index was last refreshed, so a saved blast radius from `impact` or `changed` cannot be dated or re-checked later.

Store HEAD and a dirty flag in meta on each index/refresh (cheap: read .git/HEAD and the ref, no git subprocess needed if avoidable), and emit them in the --json envelope (for example `"index": {"head": "...", "dirty": true}`) and in `cymbal structure`/`ls --stats`.

## Log
- 2026-09-23T17:49:05.689Z: [claude] Closed as won't-implement (2026-09-23). body_hash (task-15, shipped in v0.16.2) already lets an agent check whether a remembered symbol changed, which was most of the reason to record the indexed commit. What's left, dating an index by HEAD and dirty state, isn't worth a meta field and a JSON envelope change for now.
