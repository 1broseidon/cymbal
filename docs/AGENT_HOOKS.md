# Agent Hooks

Prompting alone works at first, then erodes as context grows — agents slide
back to `grep`/`find` (see [issue #23](https://github.com/1broseidon/cymbal/issues/23)).
Cymbal ships two small, agent-agnostic subcommands that any agent runtime
can wire into its native hook point:

| Command | What it does |
|---|---|
| `cymbal hook nudge` | Inspect a would-be shell command; if it looks like a code search, emit a short suggestion for the cymbal equivalent. Never blocks. |
| `cymbal hook remind` | Print a short reminder block to inject at session start or on demand. |

Claude Code has a first-class installer:

```bash
cymbal hook install claude-code              # ~/.claude/settings.json
cymbal hook install claude-code --scope project
cymbal hook uninstall claude-code
```

Everyone else can wire the two subcommands in by hand. Snippets below.

---

## Output formats

Both subcommands accept `--format`:

| Format | Shape | Use it when |
|---|---|---|
| `claude-code` | JSON: `{"decision":"allow","systemMessage":"..."}` | The agent speaks Claude Code's hook protocol. |
| `json` | Generic: `{"suggest":"...","why":"...","tool":"..."}` (nudge) / `{"systemMessage":"..."}` (remind) | The agent reads structured JSON but not the Claude protocol. |
| `text` | Plain text. `nudge` writes to **stderr**; `remind` writes to stdout. | Rules-file injection, logging, simple shell hooks. |

`nudge` is silent (no stdout, no stderr, exit 0) when it has nothing to
say. That's intentional — the hook never gets in the way of commands it
doesn't recognize.

---

## Claude Code

Use the installer:

```bash
cymbal hook install claude-code
```

What it writes (merged into your existing `~/.claude/settings.json`):

```json
{
  "hooks": {
    "PreToolUse": [
      {
        "matcher": "Bash",
        "hooks": [
          {"type": "command", "command": "cymbal hook nudge --format=claude-code", "marker": "cymbal-hook", "timeout": 5}
        ]
      }
    ],
    "SessionStart": [
      {
        "hooks": [
          {"type": "command", "command": "cymbal hook remind --format=claude-code", "marker": "cymbal-hook", "timeout": 5}
        ]
      }
    ]
  }
}
```

- `PreToolUse` on `Bash` injects the nudge whenever the model is about to
  shell out. The nudge returns `decision: allow` so the command still
  runs — it just gets an inline system message next to it.
- `SessionStart` injects the reminder block exactly once at the start of
  each session — the agent sees the cymbal primer up front and keeps the
  context, without paying ~700 B of re-injected tokens on every user turn.
  (Earlier versions wired this to `UserPromptSubmit`; re-running
  `cymbal hook install claude-code` migrates old installs automatically.)
- Both entries carry `marker: cymbal-hook` so `cymbal hook uninstall
  claude-code` finds and removes them without touching anything else
  you've added.

The installer is idempotent and preserves unrelated settings.

---

## Cursor

Cursor doesn't have pre-tool hooks, so the integration is
reminder-only. Drop a rules file into your repo:

```bash
mkdir -p .cursor/rules
cymbal hook remind > .cursor/rules/cymbal.md
```

Or for user scope:

```bash
cymbal hook remind > ~/.cursor/rules/cymbal.md
```

Cursor loads this as persistent context. Re-run to refresh when cymbal's
reminder text changes between releases.

---

## Windsurf

Same idea as Cursor — persistent rules file, reminder only:

```bash
cymbal hook remind > .windsurfrules
```

Windsurf loads `.windsurfrules` automatically. Append instead of overwrite
if you already have rules:

```bash
{ echo; echo "# cymbal"; cymbal hook remind; } >> .windsurfrules
```

---

## aider

aider has no hook API, but it takes a `--read` file of persistent
context. Generate one and reference it:

```bash
cymbal hook remind > .aider.cymbal.md
aider --read .aider.cymbal.md
```

Add to `.aider.conf.yml` for permanence:

```yaml
read:
  - .aider.cymbal.md
```

---

## Cline / Roo Code

Cline loads `.clinerules` as persistent context:

```bash
cymbal hook remind > .clinerules
```

Append to existing rules if present.

---

## Continue

Continue supports `rules` in `~/.continue/config.yaml`. Capture the
reminder once and reference it:

```bash
cymbal hook remind > ~/.continue/rules/cymbal.md
```

Then in `config.yaml`:

```yaml
rules:
  - path: ~/.continue/rules/cymbal.md
```

---

## Zed

Zed has slash commands and assistant rules (`~/.config/zed/settings.json`
→ `assistant.default_model_prompt` or per-project
`.zed/rules.md`). Drop the reminder in:

```bash
mkdir -p .zed
cymbal hook remind > .zed/rules.md
```

---

## Opencode

Opencode reads `AGENTS.md` as its primary rules file (project root or
`~/.config/opencode/AGENTS.md` for global scope) and also composes extra
files via `opencode.json` → `instructions`. Reminder-only integration:

**Project scope — append to `AGENTS.md`:**

```bash
{ echo; echo "# cymbal"; cymbal hook remind; } >> AGENTS.md
```

Opencode also falls back to `CLAUDE.md` when no `AGENTS.md` is present, so
repos already set up for Claude Code pick up the same reminder without
duplication.

**Composable alternative — dedicated file via `opencode.json`:**

This avoids collisions when `AGENTS.md` is shared with other agents
(Codex, Cursor 0.42+, etc.) or already lives under version control:

```bash
mkdir -p .opencode/instructions
cymbal hook remind > .opencode/instructions/cymbal.md
```

Then add to `opencode.json` (or the global
`~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "instructions": [".opencode/instructions/cymbal.md"]
}
```

Opencode supports globs and remote URLs in `instructions`, so teams can
point at a shared source of truth instead of committing the reminder
into every repo.

---

## Codex / OpenAI Agents SDK

The Agents SDK has `before_tool_call` / `after_tool_call` hooks. Wire
`nudge` into the `before_tool_call` handler for the shell/bash tool:

```python
from agents import Agent, RunContext
import subprocess, json

def before_tool_call(ctx: RunContext, tool_name: str, tool_args: dict) -> None:
    if tool_name.lower() not in {"bash", "shell", "run"}:
        return
    payload = json.dumps({"tool_name": tool_name, "tool_input": {"command": tool_args.get("command", "")}})
    out = subprocess.run(
        ["cymbal", "hook", "nudge", "--format=json"],
        input=payload, capture_output=True, text=True, timeout=5,
    )
    if out.stdout.strip():
        data = json.loads(out.stdout)
        ctx.add_system_message(f"{data['suggest']} — {data['why']}")
```

---

## Generic shell hook

Any agent that can exec a shell command on a pre-tool event can shell out:

```bash
cymbal hook nudge --format=text -- <the agent's would-be command>
```

Exit code is always 0. Stderr carries the suggestion when there is one,
stays silent otherwise.

For JSON consumers:

```bash
echo '{"tool_name":"Bash","tool_input":{"command":"rg -n FindUser ."}}' \
  | cymbal hook nudge --format=json
```

---

## Design notes

- **Never blocks.** The nudge always returns `allow`. Hard-stops on
  grep usage are too brittle across agents; a soft reminder next to
  the offending call is what actually changes behavior.
- **Detection is deliberately narrow.** Triggers only on `rg`, `grep`,
  `egrep`, `fgrep`, `ack`, `ag`, `find -name/-iname/-path`, `fd`,
  `fdfind`, with queries that are ≥3 chars, contain a letter, aren't
  file globs (`*.log`), and aren't mostly regex metacharacters. False
  positives are worse than false negatives — a nagging hook is exactly
  the thing #23 complains about.
- **Per-agent installers.** Only `claude-code` is auto-installable.
  For every other agent, a rules-file or config-file snippet is two
  lines — no reason for cymbal to maintain a config adapter for every
  agent in the ecosystem.

If your agent needs a first-class installer, open an issue with the
config shape and we'll add it.
