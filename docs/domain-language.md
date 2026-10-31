# Cymbal Domain Language

Cymbal provides code navigation for people and AI agents. It identifies code symbols and their relationships in repositories and Git worktrees.

## Language

### Code and relationships

**Symbol**: A named code element, such as a function, method, type, or variable. A symbol has a kind, a source location, and a language. It can have a parent symbol and members.

**Reference**: A site where code refers to a name. References are matched by name, not by full semantic analysis. A matching reference is a lead, not proof that it refers to one specific definition. _Avoid_: Verified dependency.

**Call**: A reference that invokes a function, method, or constructor.

**Use**: A reference that is not a call or an implements relationship. Type mentions are uses.

**Implements relationship**: A declared inheritance, conformance, extension, or embedding relationship between a type and another named type. It does not prove implicit structural conformance. _Avoid_: Complete interface implementation detection.

**Import**: A source statement that names a module, package, or file used by the source file.

**Caller**: A symbol that contains a call to another name.

**Callee**: The name invoked by a call in a caller.

**Member**: A symbol nested in, or associated with, a parent type.

**Body hash**: A value that identifies the source text within a symbol's line range. Moving the symbol without changing that text does not change its body hash.

### Languages and resolution

**Recognized language**: A language that Cymbal can identify from a file extension, a special filename, or a supported shebang. Recognition alone does not mean that Cymbal can extract symbols from the file.

**Supported language**: A recognized language for which Cymbal can parse source and extract symbols and relationships. _Avoid_: Recognized language when parse support is required.

**Language family**: A group of languages that can share name resolution. The families are JavaScript/TypeScript/TSX, Java/Kotlin/Scala, and C/C++.

**Resolution scope**: The set of languages allowed when matching names. `same` allows the source language, `family` also allows its language family, and `all` allows all languages. The default is `family`.

**Unresolved name**: A referenced name with no matching indexed definition in the query's resolution scope. This can be an external name or a name excluded by the scope. _Avoid_: Missing code as a definite conclusion.

### Repositories and indexes

**Repository**: The source tree that Cymbal indexes and queries.

**Index**: Cymbal's local record of files, symbols, imports, and references for a source tree.

**Incremental refresh**: An update that reparses changed files and removes stale entries without reparsing unchanged files.

**Scoped indexing**: An index update limited to a subtree. It does not remove or reparse files outside that subtree.

**Worktree**: A Git checkout with its own working files and Cymbal index. Sibling worktrees share a Git common directory.

**Federation**: A lookup across indexed sibling worktrees of the same repository. Results keep their worktree identity. Relationship traversal stays in the worktree that owns the seed symbol. _Avoid_: Merged cross-branch call graph.

### Navigation queries

**Investigate**: A query shaped by the symbol's kind. Functions and methods receive source, references, and impact. Types receive source, members, and references.

**Trace**: A downward traversal from a symbol to its callees. By default, it follows call references only. _Avoid_: Impact for downward traversal.

**Impact**: An upward traversal from a symbol to its callers and their callers. Results distinguish production callers from test callers. _Avoid_: Trace for upward traversal, guaranteed breakage.

**Importer**: A file that imports the file or package associated with a query target. Importer queries can follow more than one import step.

**Changed symbol**: A symbol touched by the selected Git diff. Cymbal uses source from both sides of the diff to identify it. Deleted symbols can be listed, but have no current impact result.

**Affected-test selection**: An advisory query that maps changed code or files to ranked test-code paths. It uses references, imports, and colocated filename conventions. It does not run tests or prove that unselected tests are safe to skip. _Avoid_: Safe-to-skip list, Coverage guarantee.

**Evidence path**: An ordered explanation from a changed definition or file to a selected test path. Steps identify actual reference sites, heuristic import matches, changed-file facts, or naming matches. A naming-only match has no dependency distance.

**Confidence**: The strength of a selection's best evidence: high, medium, or low. It is not the probability of test coverage or a correctness guarantee.

**Analysis status**: The state of an affected-test scan: complete, partial, or unavailable. Complete means the supported static scan finished without a detected gap, not that test coverage is complete. Partial and unavailable results recommend running the full suite.

**Graph**: A relationship view with nodes and edges. It can show calls, imports, or implements relationships. It is distinct from a list of reference sites.

**Outline**: The symbols defined in a source file, with their locations and nesting.

**Context**: A query bundle with a symbol's source, referenced types, callers, and file imports.

### Agent integration

**Agent hook**: An integration that supplies Cymbal guidance to an AI agent runtime. It is not a Git hook.

**Nudge**: A non-blocking suggestion to use a Cymbal query instead of a recognized code-search command.

**Reminder**: A block of Cymbal usage guidance supplied at session start or on request.

## Open Language Questions

None identified.
