---
id: task-5
title: Plugin system for external language packs
column: todo
position: 1
priority: high
tags:
  - design
  - architecture
createdAt: "2026-04-06T04:44:57.884Z"
description: |-
  ## Plugin System for External Language Packs — Design Spec

  ### Problem

  Cymbal hardcodes language support in core. Niche languages (e.g., Salesforce Apex) bring heavy dependencies and scope creep. PR #6 showed that a single vendored parser can add 239k lines to the repo. Community contributors need a clean path to add languages without bloating core.

  ### Principle

  Core languages must either already exist in the official tree-sitter Go bindings or add zero new dependency surface. Everything else is a plugin.
---

## Log
- 2026-04-06T06:02:17.032Z: ## Go WASM Gap — Implementation Constraint

Research confirmed that no prebuilt WASM exists for tree-sitter-sfapex (Apex/Salesforce). The upstream repo only ships native C sources. However, any standard tree-sitter grammar can produce a parser.wasm via 'tree-sitter build --wasm' using the WASI SDK.

The real constraint is on the Go side: smacker/go-tree-sitter (which cymbal uses) does NOT support loading WASM languages at runtime. It only supports compile-time linked C grammars via cgo. There is no LoadLanguageFromWASM() equivalent.

### Implementation options for WASM loading in Go:

1. **Embed a pure-Go WASM runtime (e.g., wazero)** — load parser.wasm files at runtime, bridge parse trees back into cymbal's data model. No new C dependencies. Most aligned with the spec.

2. **Use tree-sitter's C WASM loader (ts_parser_set_language_from_wasm)** — requires linking against tree-sitter's wasmtime engine. Adds a heavy C dependency, partially defeating the purpose.

3. **Sidecar fallback for v1** — skip WASM-in-Go entirely. Shell out to a small process that loads the WASM grammar and returns structured JSON. Simplest path but adds process overhead and a second binary.

### Recommendation:
Option 1 (wazero) is the cleanest path. It keeps cymbal as a single binary, adds no C dependencies, and aligns with the WASM sandbox security model in the spec. The bridge layer between wazero's WASM execution and cymbal's symbol model is the main engineering work.
