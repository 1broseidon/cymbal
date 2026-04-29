# Agent Hooks

Prompting alone works at first, then erodes as context grows — agents slide
back to `grep`/`find` (see [issue #23](https://github.com/1broseidon/cymbal/issues/23)).
Cymbal ships two small, agent-agnostic subcommands that any agent runtime
can wire into its native hook point:

| Command | What it does |
|---|---|
| `cymbal hook nudge` | Inspect a would-be shell command; if it looks like a code search, emit a short suggestion for the cymbal equivalent. Never blocks. |
| `cymbal hook remind` | Print a short reminder block to inject at session start or on demand. Add `--update=if-stale` when the hook should refresh stale update status with a bounded live check. |

OpenCode and Claude Code have first-class installers:

```bash
cymbal hook install opencode                 # <user-config-dir>/opencode/plugins/cymbal-opencode.js
cymbal hook install opencode --scope project
cymbal hook uninstall opencode

cymbal hook install claude-code              # ~/.claude/settings.json
cymbal hook install claude-code --scope project
cymbal hook uninstall claude-code
```

Other runtimes can wire the two subcommands in by hand. Snippets below.

---

## Output formats

Both subcommands accept `--format`:

| Format | Shape | Use it when |
|---|---|---|
| `claude-code` | JSON nested under `hookSpecificOutput` — `permissionDecision`+`additionalContext` for nudge (PreToolUse), `additionalContext` for remind (SessionStart). | The agent speaks Claude Code's hook protocol. |
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
          {"type": "command", "command": "cymbal hook remind --format=claude-code --update=if-stale", "marker": "cymbal-hook", "timeout": 5}
        ]
      }
    ]
  }
}
```

- `PreToolUse` on `Bash` injects the nudge whenever the model is about to
  shell out. The nudge returns `hookSpecificOutput.permissionDecision: allow`
  so the command still runs — it just gets the suggestion attached as
  `additionalContext` for the model to read.
- `SessionStart` injects the reminder block exactly once at the start of
  each session — the agent sees the cymbal primer up front and keeps the
  context, without paying ~700 B of re-injected tokens on every user turn.
  (Earlier versions wired this to `UserPromptSubmit`; re-running
  `cymbal hook install claude-code` migrates old installs automatically.)
- The installer uses `cymbal hook remind --update=if-stale`, so SessionStart
  refreshes stale or missing update status with a bounded live check before
  emitting the reminder. If a newer release is available, agents that can run
  shell commands should run the exact update command immediately; otherwise
  they should tell the user exactly what to run.
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

The main supported OpenCode path is now a first-class installer:

```sh
cymbal hook install opencode
```

What it does:

- **User scope** installs a cymbal-managed plugin at
  `<user-config-dir>/opencode/plugins/cymbal-opencode.js`
- **Project scope** installs a cymbal-managed plugin at
  `.opencode/plugins/cymbal-opencode.js`
- The plugin refreshes startup guidance by calling
  `cymbal hook remind --format=text --update=if-stale`
- The plugin soft-nudges bash `rg` / `grep` / `find` / `fd`-style commands
  back toward cymbal-first navigation before the shell runs them on
  non-Windows shells
- Update guidance stays fresh automatically, but **cymbal still never
  self-updates by default** — it only surfaces the explicit update command to
  run

Examples:

```bash
cymbal hook install opencode
cymbal hook install opencode --scope project
cymbal hook uninstall opencode
```

Upgrade / ownership behaviour:

- Re-running `cymbal hook install opencode` upgrades an existing
  **cymbal-managed** OpenCode plugin in place.
- `cymbal hook uninstall opencode` removes only the cymbal-managed plugin file.
- If a different user-owned file already exists at cymbal's target path,
  cymbal refuses to overwrite or remove it.
- Cymbal manages **one OpenCode scope at a time**. If a cymbal-managed plugin
  already exists in the other scope, install refuses and asks you to uninstall
  the other scope first.

This is the preferred setup because it keeps OpenCode's cymbal guidance managed
by cymbal itself instead of baking stale text into `AGENTS.md`.

**Legacy / fallback bootstrap via `AGENTS.md`:**

Use this only when plugins are unavailable or you explicitly want a manual
instructions-only setup.

```bash
cat >> AGENTS.md <<'EOF'

# cymbal
At session start, run `cymbal hook remind --update=if-stale` and treat its
output as persistent navigation guidance. If shell access is unavailable,
prefer cymbal for symbol navigation and ask the user to run the command when
update guidance is needed.
EOF
```

**Legacy / fallback bootstrap via `opencode.json` instructions:**

This avoids collisions when `AGENTS.md` is shared with other agents
(Codex, Cursor 0.42+, etc.) or already lives under version control:

```bash
mkdir -p .opencode/instructions
cat > .opencode/instructions/cymbal.md <<'EOF'
# cymbal
At session start, run `cymbal hook remind --update=if-stale` and treat its
output as persistent navigation guidance. If shell access is unavailable,
prefer cymbal for symbol navigation and ask the user to run the command when
update guidance is needed.
EOF
```

Then add to `opencode.json` (or the global
`~/.config/opencode/opencode.json`):

```json
{
  "$schema": "https://opencode.ai/config.json",
  "instructions": [".opencode/instructions/cymbal.md"]
}
```

**Windows global instructions path (PowerShell):**

```powershell
New-Item -ItemType Directory -Force "$HOME\.config\opencode\instructions" | Out-Null
@'
# cymbal
At session start, run `cymbal hook remind --update=if-stale` and treat its
output as persistent navigation guidance. If shell access is unavailable,
prefer cymbal for symbol navigation and ask the user to run the command when
update guidance is needed.
'@ | Set-Content -Encoding UTF8 "$HOME\.config\opencode\instructions\cymbal.md"
```

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
