package cmd

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

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
  --format=claude-code  (default) JSON that Claude Code's PreToolUse hook
                        understands: {"decision":"allow","systemMessage":"..."}
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
  --format=claude-code  same as json, Claude Code accepts it via
                        UserPromptSubmit hook additionalContext.`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		return emitRemind(cmd.OutOrStdout(), format)
	},
}

var hookInstallCmd = &cobra.Command{
	Use:   "install <agent>",
	Short: "Install cymbal hooks into the given agent (claude-code)",
	Long: `Wire the nudge and remind hooks into the named agent's config.

Supported agents:
  claude-code   ~/.claude/settings.json (or --scope project for .claude/settings.json)

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
			Why:         "Ranked symbol results with file+line, file-scoped with --file, JSON with --json. Faster than scanning every match.",
		}
	case "find":
		name := extractFindNameArg(fields[1:])
		if name == "" || !looksLikeCodeQuery(name) {
			return Suggestion{}
		}
		return Suggestion{
			Tool:        "find",
			Replacement: fmt.Sprintf("cymbal search %s", shQuoteIfNeeded(name)),
			Why:         "cymbal search also matches by name and returns symbol locations, not just paths.",
		}
	case "fd", "fdfind":
		q := extractSearchQuery(fields[1:])
		if q == "" || !looksLikeCodeQuery(q) {
			return Suggestion{}
		}
		return Suggestion{
			Tool:        tool,
			Replacement: fmt.Sprintf("cymbal search %s", shQuoteIfNeeded(q)),
			Why:         "cymbal indexes symbols by name; for file discovery use `cymbal ls --stats`.",
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

const nudgeTemplate = "cymbal can answer this faster: `%s`. %s"

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
		// Claude Code PreToolUse allowed decisions. "allow" with a
		// systemMessage injects a note without blocking.
		out := map[string]any{
			"decision":      "allow",
			"systemMessage": msg,
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
const reminderText = `This project is indexed by cymbal. Prefer these commands before falling
back to grep/find:

  cymbal search <name>        ranked symbol search (add --file, --kind, --lang)
  cymbal show <sym>           source for a symbol (or file:L1-L2)
  cymbal investigate <sym>    kind-adaptive summary
  cymbal impact <sym>         who depends on this?
  cymbal trace <sym>          what does this depend on?
  cymbal impls <sym>          who implements this interface/protocol?

Multi-symbol: all of the above accept several names in one call, or pipe
newline-separated names via --stdin. JSON output is available on every
command with --json.

Use 'cymbal search --text <pattern>' only for literal text matches cymbal
can't resolve by symbol.`

func emitRemind(w io.Writer, format string) error {
	switch format {
	case "", "text":
		fmt.Fprintln(w, reminderText)
		return nil
	case "json", "claude-code":
		out := map[string]any{"systemMessage": reminderText}
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(out)
	default:
		return fmt.Errorf("unknown --format %q (want: text, json, claude-code)", format)
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
	}
	return hookAdapter{}, fmt.Errorf("unknown agent %q (supported: claude-code). "+
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
	claudeRemindCmd  = "cymbal hook remind --format=claude-code"
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
	data, err := json.MarshalIndent(s.raw, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

// claudeHookEntries returns the two hook entries we want installed:
// PreToolUse on Bash (the nudge) and UserPromptSubmit (the reminder at
// session start / user prompt). Marker field lets uninstall find us without
// matching command strings exactly.
func claudeHookEntries() (preTool, userPrompt map[string]any) {
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
	userPrompt = map[string]any{
		"hooks": []any{
			map[string]any{
				"type":    "command",
				"command": claudeRemindCmd,
				"marker":  claudeHookMarker,
				"timeout": 5,
			},
		},
	}
	return preTool, userPrompt
}

// mergeClaudeHooks installs our entries into settings.raw["hooks"],
// skipping entries that already carry our marker so installs are idempotent.
func mergeClaudeHooks(s *claudeSettings) {
	preTool, userPrompt := claudeHookEntries()
	hooks, _ := s.raw["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	hooks["PreToolUse"] = appendUniqueHookGroup(hooks["PreToolUse"], preTool)
	hooks["UserPromptSubmit"] = appendUniqueHookGroup(hooks["UserPromptSubmit"], userPrompt)
	s.raw["hooks"] = hooks
}

// removeClaudeHooks drops any hook entries carrying our marker. Leaves other
// user-added hooks untouched.
func removeClaudeHooks(s *claudeSettings) {
	hooks, _ := s.raw["hooks"].(map[string]any)
	if hooks == nil {
		return
	}
	for _, key := range []string{"PreToolUse", "UserPromptSubmit"} {
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
