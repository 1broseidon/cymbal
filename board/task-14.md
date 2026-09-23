---
id: task-14
title: Repair test coverage and streamline Make startup
column: todo
position: 6
priority: medium
tags:
  - audit
  - testing
  - ci
  - build
  - superseded
relatedFiles:
  - cmd/hook_assets/opencode/cymbal-opencode.test.mjs
  - cmd/hook_assets/opencode/cymbal-opencode.js
  - .github/workflows/ci.yml
  - Makefile
subtasks:
  - id: task-14-1
    title: Retire tests for removed OpenCode update-text parsing
    completed: false
  - id: task-14-2
    title: Test retained behavior through the plugin entry point or an internal module
    completed: false
  - id: task-14-3
    title: Run the repaired OpenCode suite in CI
    completed: false
  - id: task-14-4
    title: Defer and consolidate coverage-package discovery
    completed: false
createdAt: "2026-09-10T03:52:30.006Z"
updatedAt: "2026-09-23T18:00:50.770Z"
---

## Description
Audit findings #10 and #12. Restore an executable OpenCode test suite and stop unrelated Make targets from enumerating coverage packages.

Scope:
- The standalone OpenCode suite imports removed exports and parseUpdateNotice, whose implementation is gone. Retire tests for removed update-text parsing; test retained notification behavior through the actual plugin entry point or a deliberately separated internal helper module. Do not restore incidental entry-point exports solely to satisfy stale tests.
- Add the repaired Node test suite to CI. The current node --check validates plugin syntax but cannot detect broken imports in its test suite.
- Defer MODULE and COVER_PACKAGES discovery until the coverage target needs it. Consolidate enumeration into a single go list operation and keep package exclusions explicit. The root package has no tests today; avoid silently omitting future root tests without an intentional policy.

Evidence:
- node --test cmd/hook_assets/opencode/cymbal-opencode.test.mjs fails before tests run because appleScriptString is not exported.
- Five-run median make -n clean time was 529 ms; a temporary variant deferring the two coverage variables took 12 ms with recipes unchanged.

Completion criteria:
- The OpenCode test suite executes and verifies retained behavior, and CI invokes it in addition to any desired syntax check.
- Unrelated Make targets perform no coverage-package discovery.
- Coverage includes the intended package set with documented exclusions; existing Go checks still pass.
- Record a non-mutating Make startup comparison and validate the repaired Node command.

Source: September 9, 2026 efficiency audit of ac794409a7fa1148e3cdf155991fbb662c3d9487. Full report: /home/george/.codex/visualizations/2026/09/05/01a07147-dfce-7913-b64a-a4535bdd0cc0/cymbal-efficiency-audit-2026-09-09.md

## Log
- 2026-09-23T17:54:14.259Z: [claude] Audit 2026-09-23 against main 047b568: all four subtasks still valid. The OpenCode suite has been broken since 95b0a43 (#61, 2026-06-01), which predates the audit: node --test fails with 'does not provide an export named appleScriptString', so none of its 14 tests run. The plugin exports only CymbalPlugin. appleScriptString, buildNotificationCommand and updateNotifierDisabled still exist unexported; parseUpdateNotice is gone. CI still runs only node --check (#87 did not change that). make -n clean median is 390 ms now, versus 10 ms with MODULE and COVER_PACKAGES deferred, with identical recipe output. The root package still has no tests.
- 2026-09-23T18:00:50.501Z: [claude] Merged into task-18 on 2026-09-23, with the user's decisions: drop 13-4, widen 16 to trace, refs and investigate, and remove the per-file hash. The audit notes above are carried into task-18's description.
