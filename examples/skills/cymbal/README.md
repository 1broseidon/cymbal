# Cymbal Skill

This skill has been created, iterated and benchmarked with the [Anthropic Skill Creator](https://github.com/anthropics/skills/tree/main/skills/skill-creator).

The skill creator is able to create a custom skill benchmarks, specific to your project, and 
iteratively improve a skill. Use it to adapt this skill to your project, to achieve maximum
token efficiency.


## Installation

### Copy Skill

Depending on your setup, copy the `SKILL.md` file to:

- Claude Code: `~/.claude/skills/cymbal/SKILL.md`
- GitHub Copilot: `~/.github/skills/cymbal/SKILL.md`
- Generic: `~/.agents/skills/cymbal/SKILL.md`

### Extend your Prompt

Extend your system prompt `CLAUDE.md` or `AGENTS.md` with a hint to the skill:

```
Use the `cymbal` skill and CLI — not Read, Grep, Glob or Bash — for all code navigation and comprehension.
cymbal is a tree-sitter indexed code navigator returning precise, token-efficient results.
```
