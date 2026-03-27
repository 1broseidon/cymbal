---
id: task-3
title: "Improve change detection: mtime_ns + size, walker language filtering"
createdAt: "2026-03-24T19:30:49.805Z"
completedAt: "2026-03-24T20:17:12.580Z"
updatedAt: "2026-03-24T20:17:12.580Z"
---

## Description
Harden the mtime-based skip logic and reduce wasted work in the walker, per GPT-5.4 review:

1. **Store mtime_ns + size instead of just mtime** — Current check uses time.Time comparison which may lose nanosecond precision depending on filesystem/DB. Edge cases where mtime-only fails: coarse FS timestamp resolution (FAT32, some NFS), tools that preserve mtime while changing content, clock skew. Fix: store mtime as INTEGER (UnixNano) and add a size INTEGER column to files table. Skip only when both match. Migration needed for existing DBs.

2. **Filter unsupported languages in the walker** — walker.extToLang maps ~40 extensions but parser.SupportedLanguage only handles ~15 languages. Files like .json, .md, .toml, .sql get discovered, built into FileEntry structs, sent to parse workers, then immediately skipped. Fix: have walker check parser.SupportedLanguage before emitting, or maintain one authoritative supported-language list shared between walker and parser. Reduces channel traffic and allocations.

Files: internal/index/store.go (schema + migration), internal/index/index.go (skip logic), internal/walker/walker.go (language filtering)
Verify: make build && make test
