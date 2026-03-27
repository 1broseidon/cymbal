---
id: task-2
title: "Optimize writer pipeline: prepared statement reuse, read-once, smarter batching"
createdAt: "2026-03-24T19:30:38.123Z"
completedAt: "2026-03-24T19:52:26.646Z"
updatedAt: "2026-03-24T19:52:26.646Z"
---

## Description
Three performance improvements to the serial writer in Index(), identified via syscall profiling and GPT-5.4 review:

1. **Reuse prepared statements across a batch** — InsertFileAllTx currently calls tx.Prepare() 3 times per file (symbols, imports, refs). With batchSize=100, that's 300 prepares per batch instead of 3. Fix: prepare statements once per batch in flushBatch(), pass them into a new InsertFileAllTxStmts() variant. This is the biggest remaining writer speedup.

2. **Read file once, parse and hash from same bytes** — On reindex when mtime changed, the file is read twice: once by parser.ParseFile() and once by HashFile(). Fix: read file in the worker with os.ReadFile, compute hash from those bytes, and pass the bytes to a new parser.ParseSource(src []byte, lang string) function. Eliminates duplicate allocation and I/O.

3. **Batch by row count or bytes, not just file count** — const batchSize=100 files can produce pathological batches when files are symbol-dense (e.g. TS repos with many refs). Fix: flush when files >= 100 OR total rows (symbols+imports+refs) >= 50,000.

Context: The TypeScript compiler repo (81k files) collapsed to 110% CPU and 89ms/file. These optimizations specifically target the writer bottleneck that caused that collapse.

Files: internal/index/index.go, internal/index/store.go, internal/parser/parser.go
Verify: make build && go run ./bench setup && go run ./bench run
