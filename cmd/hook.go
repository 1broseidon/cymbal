package cmd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/1broseidon/cymbal/internal/updatecheck"
	"github.com/spf13/cobra"
)

// Agent hooks. See issue #23.
//
// Problem: coding agents ignore the "use cymbal first" prompt as context
// grows and fall back to rg/grep/find. We give them two small, agent-
// agnostic subcommands that integration layers can wire into their native
// hook points:
//
//   cymbal hook nudge    read a shell command (stdin or argv), detect if
//                        it's a code search, emit a short system message
//                        suggesting the cymbal equivalent. Never blocks.
//
//   cymbal hook remind   print a short system block an agent can inject at
//                        session start or on reminders.
//
// The nudge/remind surface is agent-agnostic. For the most popular agent we
// also ship a one-liner installer:
//
//   cymbal hook install / uninstall claude-code
//                        wire the above into ~/.claude/settings.json.
//
// Other agents (Cursor, Windsurf, aider, Cline, Continue, Zed, etc.) can
// consume the same subcommands — see docs/AGENT_HOOKS.md for copy-paste
// snippets per agent. Auto-installers for those are intentionally out of
// scope so we don't maintain config adapters for every agent in the world.

var hookCmd = &cobra.Command{
	Use:   "hook",
	Short: "Agent-integration hooks (nudge, remind, install)",
	Long: `Hooks that keep coding agents using cymbal instead of sliding back to
raw grep/find as context grows. See https://github.com/1broseidon/cymbal/issues/23.

The three primitive subcommands are agent-agnostic. Use 'hook install <agent>'
to wire them into your agent's native hook points.`,
}

var hookNudgeCmd = &cobra.Command{
	Use:   "nudge [-- <command> [args...]]",
	Short: "Suggest a cymbal equivalent when an agent is about to grep",
	Long: `Inspect a would-be shell command and, if it looks like a code search,
emit a short system message suggesting the cymbal equivalent.

Input: the command can come from positional args or a Claude Code-style
JSON payload on stdin. Non-code-search commands are allowed through silently
(exit 0, no output).

Output formats:
  --format=claude-code  (default) JSON Claude Code's PreToolUse hook accepts:
                        {"hookSpecificOutput":{"hookEventName":"PreToolUse",
                        "permissionDecision":"allow","additionalContext":"..."}}
  --format=text         Plain text suggestion to stderr; exit 0 always.
  --format=json         Generic {"suggest":"...","why":"..."} shape.

nudge never blocks. Agents that want a hard stop on repeated grep usage can
pipe nudge into their own policy layer.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		fields, toolName, err := readNudgeInput(args)
		if err != nil {
			return err
		}
		suggestion := detectSearchCommand(fields, toolName)
		return emitNudge(cmd.OutOrStdout(), cmd.ErrOrStderr(), format, fields, suggestion)
	},
}

var hookRemindCmd = &cobra.Command{
	Use:   "remind",
	Short: "Print a short reminder block an agent can inject as a system message",
	Long: `Print a concise reminder that the current project is indexed by cymbal
and which commands to reach for. Intended for session-start injection or
periodic re-reminders.

Formats:
  --format=text         (default) plain text
  --format=json         {"systemMessage": "..."} for agents that want JSON
  --format=claude-code  SessionStart shape:
                        {"hookSpecificOutput":{"hookEventName":"SessionStart",
                        "additionalContext":"..."}}

Update checks:
  --update=cache        (default) use cached update status only
  --update=if-stale     refresh update status with a bounded live check only
                        when the cache is stale or missing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		updateMode, _ := cmd.Flags().GetString("update")
		return emitRemindWithUpdate(cmd.OutOrStdout(), format, updateMode)
	},
}

var hookInstallCmd = &cobra.Command{
	Use:   "install <agent>",
	Short: "Install cymbal hooks into the given agent (claude-code, opencode)",
	Long: `Wire the nudge and remind hooks into the named agent's config.

Supported agents:
	  claude-code   ~/.claude/settings.json (or --scope project for .claude/settings.json)
	  opencode      <user-config-dir>/opencode/plugins/cymbal-opencode.js (or --scope project for .opencode/plugins/cymbal-opencode.js)

For other agents (Cursor, Windsurf, aider, Cline, Continue, Zed, ...), see
docs/AGENT_HOOKS.md for copy-paste snippets that wire 'cymbal hook nudge'
and 'cymbal hook remind' into each agent's native hook point.

Use --dry-run to see the changes without writing. Use --scope=project to
install into the current repo instead of the user home.`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHookInstall(cmd, args[0], false)
	},
}

var hookUninstallCmd = &cobra.Command{
	Use:   "uninstall <agent>",
	Short: "Remove cymbal hooks from the given agent",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		return runHookInstall(cmd, args[0], true)
	},
}

func init() {
	hookNudgeCmd.Flags().String("format", "claude-code", "output format: claude-code, text, json")
	hookRemindCmd.Flags().String("format", "text", "output format: text, json, claude-code")
	hookRemindCmd.Flags().String("update", "cache", "update check mode: cache, if-stale")
	hookInstallCmd.Flags().String("scope", "user", "install scope: user (default) or project")
	hookInstallCmd.Flags().Bool("dry-run", false, "show intended changes without writing")
	hookUninstallCmd.Flags().String("scope", "user", "uninstall scope: user (default) or project")
	hookUninstallCmd.Flags().Bool("dry-run", false, "show intended changes without writing")

	hookCmd.AddCommand(hookNudgeCmd)
	hookCmd.AddCommand(hookRemindCmd)
	hookCmd.AddCommand(hookInstallCmd)
	hookCmd.AddCommand(hookUninstallCmd)
	rootCmd.AddCommand(hookCmd)
}

// ── nudge: detection ──────────────────────────────────────────────

// Suggestion is the structured result of looking at a would-be command.
// Empty Replacement means "no suggestion — let it through."
type Suggestion struct {
	// Replacement is the cymbal command we suggest, e.g. "cymbal search Foo".
	Replacement string `json:"suggest,omitempty"`
	// Why explains the swap. One short sentence.
	Why string `json:"why,omitempty"`
	// Tool is the detected outer tool (rg, grep, find, etc.), informational.
	Tool string `json:"tool,omitempty"`
}

// detectSearchCommand inspects an already-tokenized argv (first element is
// the tool, subsequent elements are its arguments) and returns a Suggestion
// if it looks like a code search agents should be running through cymbal.
//
// Input is argv, not a shell-joined string, because the stdin path already
// tokenizes via splitShellish and the argv path (via `--`) preserves the
// caller's quoting. Re-joining argv and re-lexing corrupts regex queries
// containing shell metacharacters like `|` and `;`.
//
// We deliberately keep detection narrow. False positives are worse than
// false negatives here: a nagging hook that fires on unrelated commands is
// the exact thing the issue complains about.
//
// Triggered tools: rg, grep, egrep, fgrep, ack, ag, find, fd, fdfind.
// We also honor the caller-supplied toolName when a structured hook payload
// passes it (e.g. Bash tool_input.tool_name on Claude Code).
func detectSearchCommand(fields []string, toolName string) Suggestion {
	if len(fields) == 0 && toolName == "" {
		return Suggestion{}
	}
	// If the invoking agent exposed the tool name directly, use it. Only
	// suggest on shell-like tools; file-edit tools etc. are noise.
	if toolName != "" && !isShellToolName(toolName) {
		return Suggestion{}
	}
	if len(fields) == 0 {
		return Suggestion{}
	}
	tool := filepath.Base(strings.TrimSpace(fields[0]))
	switch tool {
	case "rg", "grep", "egrep", "fgrep", "ack", "ag":
		q := extractSearchQuery(fields[1:])
		if q == "" || !looksLikeCodeQuery(q) {
			return Suggestion{}
		}
		return Suggestion{
			Tool:        tool,
			Replacement: fmt.Sprintf("cymbal search %s", shQuoteIfNeeded(q)),
			Why:         "Keep grep-style tools for literal text or regex.",
		}
	case "find":
		name := extractFindNameArg(fields[1:])
		if name == "" || !looksLikeCodeQuery(name) {
			return Suggestion{}
		}
		return Suggestion{
			Tool:        "find",
			Replacement: fmt.Sprintf("cymbal search %s", shQuoteIfNeeded(name)),
			Why:         "Keep find for raw filesystem traversal.",
		}
	case "fd", "fdfind":
		q := extractSearchQuery(fields[1:])
		if q == "" || !looksLikeCodeQuery(q) {
			return Suggestion{}
		}
		return Suggestion{
			Tool:        tool,
			Replacement: fmt.Sprintf("cymbal search %s", shQuoteIfNeeded(q)),
			Why:         "Keep fd-style tools for raw filesystem discovery.",
		}
	}
	return Suggestion{}
}

// isShellToolName returns true for tool names that typically wrap shell
// commands in agent frameworks. Claude Code's "Bash" is the canonical one.
func isShellToolName(name string) bool {
	n := strings.ToLower(name)
	return n == "bash" || n == "shell" || n == "sh" || n == "terminal" || n == "run"
}

// splitShellish tokenizes a command line into whitespace-separated fields,
// respecting single/double quotes. It isn't a full POSIX shell lexer — we
// just need to find the tool name and a plausible query string.
func splitShellish(s string) []string {
	var out []string
	var cur strings.Builder
	var quote rune
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, r := range s {
		switch {
		case quote != 0:
			if r == quote {
				quote = 0
				continue
			}
			cur.WriteRune(r)
		case r == '\'' || r == '"':
			quote = r
		case r == ' ' || r == '\t':
			flush()
		case r == '|' || r == ';' || r == '&':
			// Stop at pipes/chains — we only look at the first command.
			flush()
			return out
		default:
			cur.WriteRune(r)
		}
	}
	flush()
	return out
}

// extractSearchQuery walks a tool's args and returns the first positional
// argument that isn't a flag. Handles the common `-e PATTERN`, `--regexp=PATTERN`,
// and `--pattern PATTERN` shapes too.
func extractSearchQuery(args []string) string {
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" {
			continue
		}
		if a == "-e" || a == "--regexp" || a == "--pattern" {
			if i+1 < len(args) {
				return args[i+1]
			}
			continue
		}
		if strings.HasPrefix(a, "--regexp=") {
			return strings.TrimPrefix(a, "--regexp=")
		}
		if strings.HasPrefix(a, "--pattern=") {
			return strings.TrimPrefix(a, "--pattern=")
		}
		if strings.HasPrefix(a, "-") {
			// Skip flags. This is crude: we don't know which flags take
			// values, so patterns passed as `-e PAT` are handled above and
			// everything else falls through to the first non-flag token.
			continue
		}
		return a
	}
	return ""
}

// extractFindNameArg handles `find DIR -name PATTERN`, `-iname PATTERN`,
// `-path PATTERN`. Returns PATTERN if present.
func extractFindNameArg(args []string) string {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "-name", "-iname", "-path", "-ipath":
			if i+1 < len(args) {
				return args[i+1]
			}
		}
	}
	return ""
}

// looksLikeCodeQuery filters noise. We only nudge when the query plausibly
// targets source code, not arbitrary strings. Heuristics:
//   - contains identifier characters (letters/digits/underscore)
//   - at least 3 chars
//   - not a pure wildcard or a pure regex metachar blob
func looksLikeCodeQuery(q string) bool {
	q = strings.Trim(q, `'"`)
	q = strings.TrimSpace(q)
	if len(q) < 3 {
		return false
	}
	// Reject obvious binary/text-file globs like "*.log", "*.md".
	if strings.HasPrefix(q, "*.") {
		return false
	}
	// Require at least one letter. "123", "---", "()" aren't code queries.
	hasLetter := false
	for _, r := range q {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || r == '_' {
			hasLetter = true
			break
		}
	}
	if !hasLetter {
		return false
	}
	// Skip heavy regex metachar spam: if more than half the characters are
	// metachars, the user is doing a real regex and cymbal isn't the right
	// replacement.
	meta := 0
	for _, r := range q {
		switch r {
		case '(', ')', '[', ']', '{', '}', '|', '^', '$', '+', '?', '*', '\\':
			meta++
		}
	}
	if meta*2 > len(q) {
		return false
	}
	return true
}

// shQuoteIfNeeded wraps a string in single quotes when it contains shell
// metacharacters. Used to make our suggestion text copy-pasteable.
func shQuoteIfNeeded(s string) string {
	if s == "" {
		return "''"
	}
	safe := regexp.MustCompile(`^[A-Za-z0-9_./\-]+$`)
	if safe.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// ── nudge: input parsing ──────────────────────────────────────────

// readNudgeInput returns the would-be command as an argv slice, plus an
// optional tool name hint. Two input paths:
//
//   - argv (after `--`): used verbatim, preserving the caller's quoting.
//     This is the 99% case from Claude Code hook settings and from any
//     agent that execs us with a ready-made command list.
//   - stdin: parsed as Claude Code's PreToolUse payload if it's JSON;
//     otherwise treated as a shell command line and tokenized via
//     splitShellish. Regex queries passed this way can still get
//     truncated at unquoted pipes, but that matches real shell behavior
//     — if an agent runs `rg foo|bar` literally, it really did just run
//     two commands, and we should only inspect the first.
func readNudgeInput(args []string) (fields []string, toolName string, err error) {
	if len(args) > 0 {
		return args, "", nil
	}
	// Avoid blocking on stdin when there's nothing to read (e.g. TTY).
	stat, serr := os.Stdin.Stat()
	if serr != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
		return nil, "", nil
	}
	data, rerr := io.ReadAll(os.Stdin)
	if rerr != nil {
		return nil, "", fmt.Errorf("reading stdin: %w", rerr)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nil, "", nil
	}
	// Try Claude Code's PreToolUse shape first.
	if text[0] == '{' {
		var payload struct {
			ToolName  string `json:"tool_name"`
			ToolInput struct {
				Command string `json:"command"`
			} `json:"tool_input"`
		}
		if jerr := json.Unmarshal([]byte(text), &payload); jerr == nil {
			if payload.ToolInput.Command != "" || payload.ToolName != "" {
				return splitShellish(payload.ToolInput.Command), payload.ToolName, nil
			}
		}
	}
	return splitShellish(text), "", nil
}

// ── nudge: output ─────────────────────────────────────────────────

const nudgeTemplate = "This project is indexed by cymbal. This looks like code navigation, so start with `%s`, then use `cymbal show` or `cymbal investigate` on the result. Batch related symbols in one cymbal call when possible. %s"

func emitNudge(stdout, stderr io.Writer, format string, fields []string, s Suggestion) error {
	cmdLine := strings.Join(fields, " ")
	if s.Replacement == "" {
		// No suggestion — stay silent on stdout so we don't pollute hook
		// pipelines. Claude Code treats empty stdout as "allow".
		return nil
	}
	msg := fmt.Sprintf(nudgeTemplate, s.Replacement, s.Why)
	switch format {
	case "", "claude-code":
		// Claude Code PreToolUse: decision + context live inside
		// hookSpecificOutput. Top-level decision/systemMessage is
		// deprecated and rejected by current schema validation.
		out := map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":            "PreToolUse",
				"permissionDecision":       "allow",
				"permissionDecisionReason": "cymbal nudge",
				"additionalContext":        msg,
			},
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	case "text":
		fmt.Fprintln(stderr, msg)
		return nil
	case "json":
		out := map[string]any{
			"suggest": s.Replacement,
			"why":     s.Why,
			"tool":    s.Tool,
			"command": cmdLine,
		}
		enc := json.NewEncoder(stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	default:
		return fmt.Errorf("unknown --format %q (want: claude-code, text, json)", format)
	}
}

// ── remind ────────────────────────────────────────────────────────

// reminderText is the short, tone-calibrated system block we ask agents
// to treat as persistent context. Short by design.
const reminderText = `This project is indexed by cymbal. Treat cymbal as the default code-navigation interface.

Default workflow:
  1. ` + "`cymbal search <name>`" + ` to locate a symbol or file by name.
  2. ` + "`cymbal show <sym>`" + ` to read source, or ` + "`cymbal investigate <sym>`" + ` for a quick summary.
  3. ` + "`cymbal impact <sym>`" + `, ` + "`cymbal trace <sym>`" + `, and ` + "`cymbal impls <sym>`" + ` to follow callers, dependencies, and implementations.

Batch related lookups in one call when possible: ` + "`cymbal search Foo Bar`" + `, ` + "`cymbal show Foo Bar`" + `, ` + "`cymbal investigate Foo Bar`" + `, or pipe newline-separated symbols via ` + "`--stdin`" + `.

Use ` + "`cymbal search --text <pattern>`" + ` only for literal text or regex. Prefer cymbal before rg/grep/find/fd unless the user explicitly wants raw text search or raw filesystem traversal.`

const (
	remindUpdateCache   = "cache"
	remindUpdateIfStale = "if-stale"
	remindUpdateTimeout = 800 * time.Millisecond
)

var reminderUpdateStatus = updatecheck.GetStatus

func emitRemind(w io.Writer, format string) error {
	return emitRemindWithUpdate(w, format, remindUpdateCache)
}

func emitRemindWithUpdate(w io.Writer, format, updateMode string) error {
	allowNetwork, timeout, err := reminderUpdateOptions(updateMode)
	if err != nil {
		return err
	}
	message := reminderText
	status, _ := reminderUpdateStatus(context.Background(), updatecheck.Options{
		CurrentVersion: currentVersion(),
		AllowNetwork:   allowNetwork,
		Timeout:        timeout,
	})
	message = updatecheck.AugmentReminder(message, status)
	switch format {
	case "", "text":
		fmt.Fprintln(w, message)
		return nil
	case "json":
		out := map[string]any{"systemMessage": message}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	case "claude-code":
		// SessionStart injects persistent context via additionalContext
		// inside hookSpecificOutput. Top-level systemMessage would
		// render as a user-facing warning, not model context.
		out := map[string]any{
			"hookSpecificOutput": map[string]any{
				"hookEventName":     "SessionStart",
				"additionalContext": message,
			},
		}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	default:
		return fmt.Errorf("unknown --format %q (want: text, json, claude-code)", format)
	}
}

func reminderUpdateOptions(mode string) (bool, time.Duration, error) {
	switch strings.TrimSpace(strings.ToLower(mode)) {
	case "", remindUpdateCache:
		return false, 0, nil
	case remindUpdateIfStale:
		if updatecheck.Disabled() {
			return false, 0, nil
		}
		return true, remindUpdateTimeout, nil
	default:
		return false, 0, fmt.Errorf("unknown --update %q (want: cache, if-stale)", mode)
	}
}

// ── install / uninstall ───────────────────────────────────────────

func runHookInstall(cmd *cobra.Command, agent string, uninstall bool) error {
	scope, _ := cmd.Flags().GetString("scope")
	dryRun, _ := cmd.Flags().GetBool("dry-run")
	if scope != "user" && scope != "project" {
		return fmt.Errorf("--scope must be 'user' or 'project'")
	}
	adapter, err := lookupHookAdapter(agent)
	if err != nil {
		return err
	}
	action := adapter.install
	verb := "installed"
	if uninstall {
		action = adapter.uninstall
		verb = "removed"
	}
	target, summary, err := action(scope, dryRun)
	if err != nil {
		return err
	}
	if dryRun {
		fmt.Fprintf(cmd.OutOrStdout(), "[dry-run] would update %s\n---\n%s\n", target, summary)
		return nil
	}
	fmt.Fprintf(cmd.OutOrStdout(), "cymbal hooks %s for %s (%s scope) → %s\n", verb, agent, scope, target)
	return nil
}

// hookAdapter is the per-agent installer. install/uninstall return the
// target path, a summary of the change (for --dry-run display), and any
// error. Adapters are intentionally tiny; the agent-agnostic work lives
// in the surrounding shared helpers.
type hookAdapter struct {
	install   func(scope string, dryRun bool) (target, summary string, err error)
	uninstall func(scope string, dryRun bool) (target, summary string, err error)
}

func lookupHookAdapter(name string) (hookAdapter, error) {
	switch strings.ToLower(name) {
	case "claude-code", "claudecode", "claude":
		return hookAdapter{install: installClaudeCode, uninstall: uninstallClaudeCode}, nil
	case "opencode":
		return hookAdapter{install: installOpenCode, uninstall: uninstallOpenCode}, nil
	}
	return hookAdapter{}, fmt.Errorf("unknown agent %q (supported: claude-code, opencode). "+
		"For other agents see docs/AGENT_HOOKS.md — 'cymbal hook nudge' and "+
		"'cymbal hook remind' can be wired by hand into any agent's hook point.", name)
}

// ── Claude Code adapter ──

type claudeSettings struct {
	// We only touch the Hooks field. Everything else is preserved
	// verbatim via raw JSON merge so we don't clobber user settings.
	raw map[string]any
}

const (
	claudeHookMarker = "cymbal-hook"
	claudeNudgeCmd   = "cymbal hook nudge --format=claude-code"
	claudeRemindCmd  = "cymbal hook remind --format=claude-code --update=if-stale"
)

func claudeSettingsPath(scope string) (string, error) {
	if scope == "project" {
		return ".claude/settings.json", nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".claude", "settings.json"), nil
}

func loadClaudeSettings(path string) (*claudeSettings, error) {
	s := &claudeSettings{raw: map[string]any{}}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}
	if len(data) == 0 {
		return s, nil
	}
	if err := json.Unmarshal(data, &s.raw); err != nil {
		return nil, fmt.Errorf("parsing %s: %w", path, err)
	}
	return s, nil
}

func writeClaudeSettings(path string, s *claudeSettings) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	data, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWriteJSON(path, data, mode)
}

// claudeHookKeys lists every Claude Code hook point we've *ever* installed
// into. Listed here (not inlined) so removeClaudeHooks sweeps old locations
// too, which makes `install` safely migrate users who installed an earlier
// version. v0.11.1 and earlier wired the reminder to UserPromptSubmit (fires
// every turn); v0.11.2+ uses SessionStart (fires once per session).
var claudeHookKeys = []string{"PreToolUse", "SessionStart", "UserPromptSubmit"}

// claudeHookEntries returns the two hook entries we want installed:
// PreToolUse on Bash (the nudge) and SessionStart (the reminder at session
// start — fires once, not per turn). Marker field lets uninstall find us
// without matching command strings exactly.
func claudeHookEntries() (preTool, sessionStart map[string]any) {
	preTool = map[string]any{
		"matcher": "Bash",
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": claudeNudgeCmd,
				"marker":  claudeHookMarker,
				"timeout": 5,
			},
		},
	}
	sessionStart = map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": claudeRemindCmd,
				"marker":  claudeHookMarker,
				"timeout": 5,
			},
		},
	}
	return preTool, sessionStart
}

// mergeClaudeHooks installs our entries into settings.raw["hooks"]. It first
// strips any prior cymbal-marked entries (including those from older hook
// points like UserPromptSubmit) so a re-install migrates cleanly and stays
// idempotent. Unrelated entries are preserved.
func mergeClaudeHooks(s *claudeSettings) {
	removeClaudeHooks(s)
	preTool, sessionStart := claudeHookEntries()
	hooks, _ := s.raw["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["PreToolUse"] = appendUniqueHookGroup(hooks["PreToolUse"], preTool)
	hooks["SessionStart"] = appendUniqueHookGroup(hooks["SessionStart"], sessionStart)
	s.raw["hooks"] = hooks
}

// removeClaudeHooks drops any hook entries carrying our marker. Leaves other
// user-added hooks untouched. Sweeps every hook point in claudeHookKeys so
// older installs (UserPromptSubmit-based) are migrated away.
func removeClaudeHooks(s *claudeSettings) {
	hooks, _ := s.raw["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for _, key := range claudeHookKeys {
		arr, _ := hooks[key].([]any)
		if arr == nil {
			continue
		}
		filtered := arr[:0]
		for _, entry := range arr {
			if hookGroupHasMarker(entry, claudeHookMarker) {
				continue
			}
			filtered = append(filtered, entry)
		}
		if len(filtered) == 0 {
			delete(hooks, key)
		} else {
			hooks[key] = filtered
		}
	}
	if len(hooks) == 0 {
		delete(s.raw, "hooks")
	}
}

func atomicWriteJSON(path string, data []byte, mode os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, mode)
}

// appendUniqueHookGroup appends `group` to the existing array (creating one
// if nil) unless a group with our marker already exists.
func appendUniqueHookGroup(existing any, group map[string]any) []any {
	arr, _ := existing.([]any)
	for _, entry := range arr {
		if hookGroupHasMarker(entry, claudeHookMarker) {
			return arr
		}
	}
	return append(arr, group)
}

func hookGroupHasMarker(entry any, marker string) bool {
	m, ok := entry.(map[string]any)
	if !ok {
		return false
	}
	hooks, _ := m["hooks"].([]any)
	for _, h := range hooks {
		hm, _ := h.(map[string]any)
		if hm == nil {
			continue
		}
		if hm["marker"] == marker {
			return true
		}
	}
	return false
}

func installClaudeCode(scope string, dryRun bool) (string, string, error) {
	path, err := claudeSettingsPath(scope)
	if err != nil {
		return "", "", err
	}
	s, err := loadClaudeSettings(path)
	if err != nil {
		return path, "", err
	}
	mergeClaudeHooks(s)
	data, _ := json.MarshalIndent(s.raw, "", "  ")
	if dryRun {
		return path, string(data), nil
	}
	if err := writeClaudeSettings(path, s); err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

func uninstallClaudeCode(scope string, dryRun bool) (string, string, error) {
	path, err := claudeSettingsPath(scope)
	if err != nil {
		return "", "", err
	}
	s, err := loadClaudeSettings(path)
	if err != nil {
		return path, "", err
	}
	removeClaudeHooks(s)
	data, _ := json.MarshalIndent(s.raw, "", "  ")
	if dryRun {
		return path, string(data), nil
	}
	if err := writeClaudeSettings(path, s); err != nil {
		return path, "", err
	}
	return path, string(data), nil
}

// ── OpenCode adapter ──

const (
	opencodeManagedPluginFile = "cymbal-opencode.js"
	opencodeHookMarker        = "cymbal-hook"
)

const opencodeManagedHeaderPrefix = "// " + opencodeHookMarker + " managed by cymbal\n// cymbal-version: "

func opencodePluginPath(scope string) (string, error) {
	if scope == "project" {
		return filepath.Join(".opencode", "plugins", opencodeManagedPluginFile), nil
	}
	configRoot, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configRoot, "opencode", "plugins", opencodeManagedPluginFile), nil
}

func opencodePluginContents() string {
	return renderOpenCodePlugin(opencodeHookMarker, currentVersion())
}

func writeManagedFile(path, content string) error {
	if managed, err := openCodeManagedFileState(path); err != nil {
		return err
	} else if !managed {
		return fmt.Errorf("refusing to overwrite non-cymbal OpenCode plugin at %s", path)
	}
	if err := ensureSafeManagedTarget(path); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	mode := os.FileMode(0o644)
	if info, err := os.Stat(path); err == nil {
		mode = info.Mode().Perm()
	}
	return atomicWriteFile(path, []byte(content), mode)
}

func installOpenCode(scope string, dryRun bool) (string, string, error) {
	path, err := opencodePluginPath(scope)
	if err != nil {
		return "", "", err
	}
	otherScope := "user"
	if scope == "user" {
		otherScope = "project"
	}
	otherPath, err := opencodePluginPath(otherScope)
	if err != nil {
		return path, "", err
	}
	otherManaged, err := opencodeManagedFileExists(otherPath)
	if err != nil {
		return path, "", err
	}
	if otherManaged {
		return path, "", fmt.Errorf("cymbal-managed OpenCode plugin already exists in %s scope at %s; uninstall it before installing %s scope", otherScope, otherPath, scope)
	}
	managed, err := openCodeManagedFileState(path)
	if err != nil {
		return path, "", err
	}
	content := opencodePluginContents()
	if dryRun {
		if !managed {
			return path, "", fmt.Errorf("would refuse to overwrite non-cymbal OpenCode plugin at %s", path)
		}
		return path, content, nil
	}
	if err := writeManagedFile(path, content); err != nil {
		return path, "", err
	}
	return path, content, nil
}

func uninstallOpenCode(scope string, dryRun bool) (string, string, error) {
	path, err := opencodePluginPath(scope)
	if err != nil {
		return "", "", err
	}
	managed, err := openCodeManagedFileState(path)
	if err != nil {
		return path, "", err
	}
	if dryRun {
		if !managed {
			return path, "leave non-cymbal OpenCode plugin untouched", nil
		}
		return path, "remove managed OpenCode plugin", nil
	}
	if !managed {
		return path, "leave non-cymbal OpenCode plugin untouched", nil
	}
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return path, "", err
	}
	return path, "remove managed OpenCode plugin", nil
}

func openCodeManagedFileState(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, err
	}
	return strings.HasPrefix(string(data), opencodeManagedHeaderPrefix), nil
}

func opencodeManagedFileExists(path string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return strings.HasPrefix(string(data), opencodeManagedHeaderPrefix), nil
}

func atomicWriteFile(path string, data []byte, mode os.FileMode) error {
	dir := filepath.Dir(path)
	tmpFile, err := os.CreateTemp(dir, filepath.Base(path)+".*.tmp")
	if err != nil {
		return err
	}
	tmp := tmpFile.Name()
	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		_ = os.Remove(tmp)
		return err
	}
	if err := tmpFile.Close(); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		_ = os.Remove(tmp)
		return err
	}
	return os.Chmod(path, mode)
}

func ensureSafeManagedTarget(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write through symlinked OpenCode plugin path %s", path)
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}

	for dir := filepath.Dir(path); dir != ""; dir = filepath.Dir(dir) {
		info, err := os.Lstat(dir)
		if errors.Is(err, os.ErrNotExist) {
			next := filepath.Dir(dir)
			if next == dir {
				break
			}
			continue
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("refusing to write inside symlinked OpenCode plugin directory %s", dir)
		}
		next := filepath.Dir(dir)
		if next == dir {
			break
		}
	}
	return nil
}
