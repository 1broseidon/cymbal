# Cymbal Skill

An [Agent Skill](https://code.claude.com/docs/en/skills) that teaches Claude
(and any agent runtime that reads the same format) to reach for the `cymbal`
CLI first when navigating existing code, instead of falling back to Read,
Grep, Glob, or raw Bash.

This is a reference example — drop it into your agent's skills directory and
adapt it to your project if you want to tune the triggers, add project-specific
commands, or benchmark a different style.

## Why a skill (instead of just a prompt)

The frontmatter `description` and `when_to_use` stay permanently in the
agent's context; the body only loads when the skill is triggered. That makes
a skill resilient to context rot in long sessions — the place where "always
use cymbal first" prompts tend to erode. See
[issue #23](https://github.com/1broseidon/cymbal/issues/23) for the problem
statement.

## Installation

Copy the `SKILL.md` file (directory name becomes the skill name) to the
location your agent reads from:

| Agent | Path |
|---|---|
| Claude Code (user scope) | `~/.claude/skills/cymbal/SKILL.md` |
| Claude Code (project scope) | `.claude/skills/cymbal/SKILL.md` |
| Other runtimes that read Agent Skill format | their documented skills dir |

For agents that do *not* load skills, use the `cymbal hook remind` integrations
in [`docs/AGENT_HOOKS.md`](../../../docs/AGENT_HOOKS.md) instead — same
content, different wiring.

## Iterating

This skill was shaped against the maintained benchmark corpus (gin, fastapi,
kubectl, vite, ripgrep, jq, guava) and tuned for token efficiency and correct
rank-1 results. If you want to adapt it to a specific codebase, the
[Anthropic Skill Creator](https://github.com/anthropics/skills/tree/main/skills/skill-creator)
can generate benchmarks for your repo and iterate on the skill against them.

## What's inside

- **Hard rule** — if the task touches existing code, start with cymbal.
- **Investigation loop** — `search → investigate → impact/trace/refs/impls → show/outline`.
- **Goal → command table** — one-line lookup from "I want to…" to the right subcommand.
- **Command details** with path-filter, stdin-batch, and `--json` examples.
- **Pivot rule, stop rules, anti-patterns, real constraints** — the
  field-manual bits that keep agent behavior stable under context pressure.
- **Decision tree** for first-time users.
- **Bench numbers** so the agent has a concrete reason to trust first results.
