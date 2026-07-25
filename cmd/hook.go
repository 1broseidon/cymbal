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
	"github.com/1broseidon/cymbal/lang"
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
//   cymbal hook notify   emit a structured update notification payload for
//                        agent plugins that want to surface update notices.
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
	Short: "Agent-integration hooks (nudge, remind, notify, install)",
	Long: `Hooks that keep coding agents using cymbal instead of sliding back to
raw grep/find as context grows. See https://github.com/1broseidon/cymbal/issues/23.

The agent-agnostic subcommands are nudge, remind, and notify. Use 'hook install <agent>'
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
		in, err := readNudgeInput(args)
		if err != nil {
			return err
		}
		suggestion := detectNudge(in)
		return emitNudge(cmd.OutOrStdout(), cmd.ErrOrStderr(), format, in.fields, suggestion)
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

var hookNotifyCmd = &cobra.Command{
	Use:   "notify [--format=json|text] [--update=cache|if-stale]",
	Short: "Emit a structured update notification payload for agent plugins",
	Long: `Emit a structured update notification payload when cymbal has an
available update and the notification throttle allows it.

Formats:
  --format=json         (default) structured payload for agent plugins
  --format=text         plain text notice; empty output when no notice is due

Update checks:
  --update=cache        (default) use cached update status only
  --update=if-stale     refresh update status with a bounded live check only
                        when the cache is stale or missing`,
	RunE: func(cmd *cobra.Command, args []string) error {
		format, _ := cmd.Flags().GetString("format")
		updateMode, _ := cmd.Flags().GetString("update")
		return emitHookNotify(cmd.OutOrStdout(), format, updateMode)
	},
}

var hookInstallCmd = &cobra.Command{
	Use:   "install <agent>",
	Short: "Install cymbal hooks into the given agent (claude-code, opencode)",
	Long: `Wire the nudge and remind hooks into the named agent's config.

Supported agents:
	  claude-code   ~/.claude/settings.json (or --scope project for .claude/settings.json)
	  opencode      ~/.config/opencode/plugins/cymbal-opencode.js, or $OPENCODE_CONFIG_DIR/plugins/cymbal-opencode.js when set
	                (or --scope project for .opencode/plugins/cymbal-opencode.js)

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
	hookNotifyCmd.Flags().String("format", "json", "output format: json, text")
	hookNotifyCmd.Flags().String("update", "cache", "update check mode: cache, if-stale")
	hookInstallCmd.Flags().String("scope", "user", "install scope: user (default) or project")
	hookInstallCmd.Flags().Bool("dry-run", false, "show intended changes without writing")
	hookUninstallCmd.Flags().String("scope", "user", "uninstall scope: user (default) or project")
	hookUninstallCmd.Flags().Bool("dry-run", false, "show intended changes without writing")

	hookCmd.AddCommand(hookNudgeCmd)
	hookCmd.AddCommand(hookRemindCmd)
	hookCmd.AddCommand(hookNotifyCmd)
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

// nudgeInput holds everything readNudgeInput could extract from one hook
// invocation. For the legacy Bash path, fields + toolName are populated and
// the dedicated-tool fields are zero. For Claude Code's Grep/Glob/Read tools
// the dedicated fields below are populated instead; fields stays empty.
type nudgeInput struct {
	toolName string
	fields   []string

	// Dedicated-tool fields (Claude Code Grep/Glob/Read).
	pattern  string // Grep pattern, Glob pattern
	glob     string // Grep --glob filter
	fileType string // Grep --type filter
	filePath string // Read file_path
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
		// "User already knows where to look" — explicit file paths in the
		// remaining args mean this is a line-number lookup, not discovery.
		// Cymbal search would be wrong here; stay silent.
		if hasExplicitFileTarget(fields[1:], q) {
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
		if hasExplicitFileTarget(fields[1:], q) {
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

// hasExplicitFileTarget reports whether the args contain at least one
// positional that looks like a specific source file (e.g. parser.go,
// path/to/file.ts), as opposed to a discovery root (., src/, **/*.go).
//
// When the caller names files directly, they already know where to look —
// suggesting cymbal search would be a regression, not an improvement.
// The query string is passed in so we can skip it (it would otherwise be
// the first non-flag arg).
func hasExplicitFileTarget(args []string, query string) bool {
	seenQuery := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if a == "" {
			continue
		}
		// Skip `-e PATTERN`, `--regexp PATTERN`, `--pattern PATTERN` pairs:
		// the next token is a pattern, not a path.
		if a == "-e" || a == "--regexp" || a == "--pattern" {
			i++
			continue
		}
		if strings.HasPrefix(a, "-") {
			continue
		}
		// First positional is the query — skip it.
		if !seenQuery && a == query {
			seenQuery = true
			continue
		}
		if looksLikeFilePath(a) {
			return true
		}
	}
	return false
}

// looksLikeFilePath returns true for args that name a specific file rather
// than a discovery root. We use shape heuristics (extension, no trailing
// slash, no globs) rather than stat — the hook must run without touching
// the filesystem.
func looksLikeFilePath(a string) bool {
	if a == "." || a == ".." {
		return false
	}
	if strings.HasSuffix(a, "/") || strings.HasSuffix(a, `\`) {
		return false
	}
	if strings.ContainsAny(a, "*?[") {
		return false
	}
	// Need a `name.ext` tail where ext is 1-5 alphanumeric chars. This
	// catches parser.go, App.tsx, path/to/file.py, but not directories
	// like node_modules or extensionless binaries.
	idx := strings.LastIndex(a, ".")
	if idx <= 0 || idx == len(a)-1 {
		return false
	}
	// Reject leading-dot files like ".env" where idx == 0 (already handled
	// by idx <= 0) and trailing-slash-then-dot edge cases.
	if strings.ContainsRune(a[idx+1:], '/') || strings.ContainsRune(a[idx+1:], '\\') {
		return false
	}
	ext := a[idx+1:]
	if len(ext) > 5 {
		return false
	}
	for _, r := range ext {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')) {
			return false
		}
	}
	return true
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

// detectNudge dispatches on tool name. Claude Code's dedicated Grep/Glob/Read
// tools carry structured input, not a command string — those go through
// per-tool detectors. Bash and any other shell tool fall through to
// detectSearchCommand against the tokenized argv.
//
// Returns an empty Suggestion when nothing should fire — every detector
// errs on the side of staying silent (see the design notes on
// detectSearchCommand).
func detectNudge(in nudgeInput) Suggestion {
	switch in.toolName {
	case "Grep":
		return detectGrepToolSearch(in.pattern, in.glob, in.fileType)
	case "Glob":
		return detectGlobToolSearch(in.pattern)
	case "Read":
		return detectReadToolSearch(in.filePath)
	}
	return detectSearchCommand(in.fields, in.toolName)
}

// detectGrepToolSearch fires when Claude Code's Grep tool is called with a
// symbol-shaped pattern against a code-file scope. The literal/regex gates
// from detectSearchCommand still apply (multi-word phrases, embedded quotes,
// `|` alternation, etc.); the only new responsibility here is recognising
// that an explicit `glob`/`type` filter targeting non-code files (markdown,
// JSON, logs, etc.) means the caller is reading data, not navigating code.
func detectGrepToolSearch(pattern, glob, fileType string) Suggestion {
	if pattern == "" || !looksLikeCodeQuery(pattern) {
		return Suggestion{}
	}
	if glob != "" && !globTargetsCode(glob) {
		return Suggestion{}
	}
	if fileType != "" && !rgTypeIsCode(fileType) {
		return Suggestion{}
	}
	return Suggestion{
		Tool:        "Grep",
		Replacement: fmt.Sprintf("cymbal search %s", shQuoteIfNeeded(pattern)),
		Why:         "Keep the Grep tool for literal text or regex across non-code files.",
	}
}

// detectGlobToolSearch fires when the Glob tool's pattern targets code files.
// The natural cymbal equivalent is `cymbal ls --names` (file inventory) or a
// scoped `cymbal search --path <pattern> <symbol>` once a symbol is in hand —
// the nudge points at the broader navigation surface rather than a single
// command, since Glob alone doesn't carry symbol intent.
func detectGlobToolSearch(pattern string) Suggestion {
	if pattern == "" {
		return Suggestion{}
	}
	if !globTargetsCode(pattern) {
		return Suggestion{}
	}
	return Suggestion{
		Tool:        "Glob",
		Replacement: "cymbal ls --names",
		Why:         "The inventory lists the code files cymbal indexed, with skip rules applied.",
	}
}

// detectReadToolSearch fires when Claude Code's Read tool targets a code
// file. Reading a whole file when you only need a symbol is wasteful; the
// nudge points at `cymbal show <file>:<Symbol>` or `cymbal outline <file>`
// as alternatives that return targeted context.
func detectReadToolSearch(filePath string) Suggestion {
	if filePath == "" {
		return Suggestion{}
	}
	if !looksLikeCodeFile(filePath) {
		return Suggestion{}
	}
	return Suggestion{
		Tool:        "Read",
		Replacement: fmt.Sprintf("cymbal outline %s", shQuoteIfNeeded(filePath)),
		Why:         "Use `cymbal show <file>:<Symbol>` for a specific function/type in this file, or `cymbal show <file>:L1-L2` for a line range.",
	}
}

// nonCodeExtensions enumerates file extensions cymbal explicitly does not
// want to nudge for, even when cymbal indexes them as a language (yaml,
// json, etc. are indexed but agents grep them as data, not as code).
//
// Source: issue #47's suggested skip list, plus a few common siblings.
var nonCodeExtensions = map[string]bool{
	".md": true, ".markdown": true, ".rst": true, ".adoc": true,
	".json": true, ".jsonl": true, ".jsonc": true, ".json5": true,
	".yaml": true, ".yml": true, ".toml": true,
	".log": true, ".ndjson": true,
	".txt": true, ".csv": true, ".tsv": true, ".xml": true, ".html": true,
	".ini": true, ".conf": true, ".env": true, ".lock": true, ".properties": true,
}

// looksLikeCodeFile is the "should the nudge fire?" test for a filename.
// Returns false when the extension is on our explicit non-code list, OR
// when the language registry doesn't recognise the file at all. Returns
// true when cymbal knows about the file and it isn't on the deny list —
// the agent is reading code we could navigate symbolically.
func looksLikeCodeFile(name string) bool {
	if name == "" {
		return false
	}
	ext := strings.ToLower(filepath.Ext(name))
	if ext != "" && nonCodeExtensions[ext] {
		return false
	}
	return lang.Default.ForFile(name) != nil
}

// globTargetsCode reports whether a glob unambiguously targets non-code
// files. Returns false only when we can isolate a single extension and
// that extension is on the non-code list; everything else returns true so
// the existing literal/regex gates can decide.
func globTargetsCode(glob string) bool {
	if glob == "" {
		return true
	}
	// Brace expansion → can't trivially isolate; assume code.
	if strings.ContainsAny(glob, "{}") {
		return true
	}
	g := glob
	if idx := strings.LastIndex(g, "/"); idx >= 0 {
		g = g[idx+1:]
	}
	g = strings.TrimLeft(g, "*?")
	if !strings.HasPrefix(g, ".") {
		return true
	}
	return !nonCodeExtensions[strings.ToLower(g)]
}

// rgTypeIsCode classifies ripgrep --type names against the same non-code
// set as the extension check, so `Grep --type yaml` and `Grep --glob '*.yaml'`
// are treated identically.
func rgTypeIsCode(t string) bool {
	t = strings.ToLower(t)
	switch t {
	case "md", "markdown", "json", "jsonl", "yaml", "yml", "toml",
		"log", "txt", "csv", "tsv", "xml", "ini", "env", "lock":
		return false
	}
	return true
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
	// Inspect the raw value for quote characters first — any embedded
	// quote signals literal-text intent (`"jsx"`, `'foo'`), regardless
	// of whether the surrounding shell parsing already trimmed an outer
	// pair. This catches Grep-tool patterns like `"jsx"` where there's
	// no sibling file-path arg to rescue us.
	if strings.ContainsAny(q, `"'`) {
		return false
	}
	q = strings.TrimSpace(q)
	if len(q) < 3 {
		return false
	}
	// Reject obvious binary/text-file globs like "*.log", "*.md".
	if strings.HasPrefix(q, "*.") {
		return false
	}
	if strings.ContainsAny(q, " \t\n") {
		return false
	}
	// Characters that can't appear in any supported-language identifier
	// (or in a useful `cymbal search` query) — strong literal-text signal.
	// `.` is intentionally permitted (qualified names like pkg.Func) and
	// `-` is permitted (some search inputs use hyphens). Regex metachars
	// are handled by the metachar-majority gate below.
	if strings.ContainsAny(q, `/\:;=<>!@&#,~`+"`") {
		return false
	}
	// Unambiguous regex signals — even one of these means the user is running
	// a real regex search, not a symbol lookup. The metachar-majority gate
	// below only catches metachar-heavy patterns; these single-char signals
	// (`worktree|git`, `^Server$`) would otherwise slip through.
	if strings.ContainsRune(q, '|') {
		return false
	}
	if strings.HasPrefix(q, "^") || strings.HasSuffix(q, "$") {
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
func readNudgeInput(args []string) (nudgeInput, error) {
	if len(args) > 0 {
		return nudgeInput{fields: args}, nil
	}
	// Avoid blocking on stdin when there's nothing to read (e.g. TTY).
	stat, serr := os.Stdin.Stat()
	if serr != nil || (stat.Mode()&os.ModeCharDevice) != 0 {
		return nudgeInput{}, nil
	}
	data, rerr := io.ReadAll(os.Stdin)
	if rerr != nil {
		return nudgeInput{}, fmt.Errorf("reading stdin: %w", rerr)
	}
	text := strings.TrimSpace(string(data))
	if text == "" {
		return nudgeInput{}, nil
	}
	// Try Claude Code's PreToolUse shape first. We capture every field any
	// of the dedicated tools could emit — unused fields stay zero.
	if text[0] == '{' {
		var payload struct {
			ToolName  string `json:"tool_name"`
			ToolInput struct {
				Command  string `json:"command"`
				Pattern  string `json:"pattern"`
				Glob     string `json:"glob"`
				Type     string `json:"type"`
				FilePath string `json:"file_path"`
			} `json:"tool_input"`
		}
		if jerr := json.Unmarshal([]byte(text), &payload); jerr == nil {
			in := nudgeInput{toolName: payload.ToolName}
			switch payload.ToolName {
			case "Grep":
				in.pattern = payload.ToolInput.Pattern
				in.glob = payload.ToolInput.Glob
				in.fileType = payload.ToolInput.Type
				return in, nil
			case "Glob":
				in.pattern = payload.ToolInput.Pattern
				return in, nil
			case "Read":
				in.filePath = payload.ToolInput.FilePath
				return in, nil
			}
			// Bash/shell or unknown tool: tokenize the command.
			if payload.ToolInput.Command != "" || payload.ToolName != "" {
				in.fields = splitShellish(payload.ToolInput.Command)
				return in, nil
			}
		}
	}
	return nudgeInput{fields: splitShellish(text)}, nil
}

// ── nudge: output ─────────────────────────────────────────────────

// The nudge templates are advisory, not declarative. Each carries an explicit
// "ignore this note" branch naming the case where the agent's original tool
// was correct, so it doesn't reflexively switch tools (and apologize) for
// valid use cases.
const nudgeSearchTemplate = "Cymbal note: this project is indexed. If your target is a symbol name (function/class/variable/constant) and you're searching repo-wide, try `%s` for ranked results with file:line in one call. If you're searching for literal text, a value inside a string, or you already know which file to read, your current command is the right tool — ignore this note. %s"

const nudgeReadTemplate = "Cymbal note: this project is indexed. If you need a specific symbol or section rather than the whole file, try `%s` to see what's defined. %s If you genuinely need the whole file, your Read call is the right tool — ignore this note."

const nudgeGlobTemplate = "Cymbal note: this project is indexed. If you're looking for code files, try `%s` for the indexed file inventory. %s If you need exact glob matching or non-code files, your Glob call is the right tool — ignore this note."

func nudgeMessage(s Suggestion) string {
	switch s.Tool {
	case "Read":
		return fmt.Sprintf(nudgeReadTemplate, s.Replacement, s.Why)
	case "Glob":
		return fmt.Sprintf(nudgeGlobTemplate, s.Replacement, s.Why)
	default:
		return fmt.Sprintf(nudgeSearchTemplate, s.Replacement, s.Why)
	}
}

func emitNudge(stdout, stderr io.Writer, format string, fields []string, s Suggestion) error {
	cmdLine := strings.Join(fields, " ")
	if s.Replacement == "" {
		// No suggestion — stay silent on stdout so we don't pollute hook
		// pipelines. Claude Code treats empty stdout as "allow".
		return nil
	}
	msg := nudgeMessage(s)
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
const reminderText = `This project is indexed by cymbal. Use cymbal for structural code navigation when you know (or can guess) the symbol name.

Typical flow: ` + "`cymbal search <name>`" + ` to locate -> ` + "`cymbal context/show/investigate <sym>`" + ` to read -> ` + "`cymbal impact/trace/impls <sym>`" + ` to follow callers, dependencies, and implementations.

Orientation (new repo or file):
  - ` + "`cymbal structure`" + ` for entry points, hotspots, and most-referenced symbols.
  - ` + "`cymbal outline <file>`" + ` for the symbol map of a file before editing.
  - ` + "`cymbal ls --stats`" + ` for repo overview (languages, file/symbol counts).

Reading source:
  - ` + "`cymbal context <sym>`" + ` for source + callers + imports in one call.
  - ` + "`cymbal show <sym>`" + ` to read a function/type by name; ` + "`cymbal show Parent.child`" + ` for nested symbols; ` + "`cymbal show file.ts:80-120`" + ` for a line range.
  - ` + "`cymbal investigate <sym>`" + ` for a kind-adaptive summary.

Following relationships:
  - ` + "`cymbal impact <sym>`" + ` for transitive callers, ` + "`cymbal trace <sym>`" + ` for the call graph downward, ` + "`cymbal impls <sym>`" + ` for implementations.
  - ` + "`cymbal refs <sym>`" + ` for reference sites (best-effort), ` + "`cymbal importers <file>`" + ` for reverse import lookup.

Assessing change risk:
  - ` + "`cymbal changed`" + ` for diff-scoped impact of unstaged edits; ` + "`--staged`" + ` for staged edits, ` + "`--base <ref>`" + ` for working tree vs a base ref.
  - ` + "`cymbal diff <sym> [base]`" + ` for the git diff scoped to one symbol's line range.

Finding things:
  - ` + "`cymbal search <name>`" + ` for symbol lookup (ranked: exact > prefix > fuzzy).
  - ` + "`cymbal search --text <pattern>`" + ` for literal text or regex grep.

Batch related lookups in one call when possible: ` + "`cymbal search Foo Bar`" + `, ` + "`cymbal show Foo Bar`" + `, ` + "`cymbal investigate Foo Bar`" + `, or pipe newline-separated symbols via ` + "`--stdin`" + `.

Prefer ` + "`cymbal search`" + ` over the Grep tool (or rg/grep) for symbol/function/class lookup. Prefer ` + "`cymbal show`" + ` over Read when reading source by symbol name. Prefer ` + "`cymbal investigate / impact / trace / impls`" + ` over manual cross-referencing.

Use Grep/Glob/Read (or rg/grep/find) for: literal text in non-code files (markdown, JSON, logs, config), files outside the indexed repo, and direct path reads when you already know the path.

Cymbal is for structural queries (known names, call graphs, references). For semantic/conceptual queries ("find code related to X") where you don't know the exact symbol name, prefer a semantic search tool if one is configured.`

const (
	remindUpdateCache   = "cache"
	remindUpdateIfStale = "if-stale"
	remindUpdateTimeout = 800 * time.Millisecond
)

var (
	reminderUpdateStatus   = updatecheck.GetStatus
	hookNotifyStatus       = updatecheck.GetStatus
	hookNotifyShouldNotify = updatecheck.ShouldNotify
	hookNotifyMarkNotified = updatecheck.MarkNotified
)

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

type hookNotifyPayload struct {
	Notify        bool   `json:"notify"`
	LatestVersion string `json:"latestVersion,omitempty"`
	Title         string `json:"title,omitempty"`
	Body          string `json:"body,omitempty"`
	Command       string `json:"command,omitempty"`
	ReleaseURL    string `json:"releaseURL,omitempty"`
}

func emitHookNotify(w io.Writer, format, updateMode string) error {
	allowNetwork, timeout, err := reminderUpdateOptions(updateMode)
	if err != nil {
		return err
	}
	status, _ := hookNotifyStatus(context.Background(), updatecheck.Options{
		CurrentVersion: currentVersion(),
		AllowNetwork:   allowNetwork,
		Timeout:        timeout,
	})
	shouldNotify := hookNotifyShouldNotify(status)
	if !shouldNotify {
		switch format {
		case "", "json":
			enc := json.NewEncoder(w)
			enc.SetIndent("", "  ")
			return enc.Encode(hookNotifyPayload{Notify: false})
		case "text":
			return nil
		default:
			return fmt.Errorf("unknown --format %q (want: json, text)", format)
		}
	}
	payload := hookNotifyPayload{
		Notify:        true,
		LatestVersion: status.LatestVersion,
		Title:         fmt.Sprintf("cymbal %s is available", status.LatestVersion),
		Body:          fmt.Sprintf("Update: %s", status.Command),
		Command:       status.Command,
		ReleaseURL:    status.ReleaseURL,
	}
	var writeErr error
	switch format {
	case "", "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		writeErr = enc.Encode(payload)
	case "text":
		_, writeErr = fmt.Fprintln(w, payload.Title)
		if writeErr == nil {
			_, writeErr = fmt.Fprintln(w, payload.Body)
		}
	default:
		return fmt.Errorf("unknown --format %q (want: json, text)", format)
	}
	if writeErr != nil {
		return writeErr
	}
	_ = hookNotifyMarkNotified(status)
	return nil
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
	configDir, err := opencodeConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(configDir, "plugins", opencodeManagedPluginFile), nil
}

func opencodeConfigDir() (string, error) {
	if configured := strings.TrimSpace(os.Getenv("OPENCODE_CONFIG_DIR")); configured != "" {
		return configured, nil
	}
	return opencodeDefaultConfigDir()
}

func opencodeDefaultConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "opencode"), nil
}

func opencodeUserPluginPaths() ([]string, error) {
	configDir, err := opencodeConfigDir()
	if err != nil {
		return nil, err
	}
	paths := []string{filepath.Join(configDir, "plugins", opencodeManagedPluginFile)}

	defaultConfigDir, err := opencodeDefaultConfigDir()
	if err != nil {
		return nil, err
	}
	defaultPath := filepath.Join(defaultConfigDir, "plugins", opencodeManagedPluginFile)
	if !samePath(defaultPath, paths[0]) {
		paths = append(paths, defaultPath)
	}
	return paths, nil
}

func samePath(a, b string) bool {
	return filepath.Clean(a) == filepath.Clean(b)
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
	conflicts, err := opencodeManagedInstallConflicts(scope, path)
	if err != nil {
		return path, "", err
	}
	if len(conflicts) > 0 {
		return path, "", fmt.Errorf("cymbal-managed OpenCode plugin already exists at %s; uninstall it before installing %s scope", conflicts[0], scope)
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

func opencodeManagedInstallConflicts(scope, targetPath string) ([]string, error) {
	var candidates []string
	if scope == "project" {
		userPaths, err := opencodeUserPluginPaths()
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, userPaths...)
	} else {
		projectPath, err := opencodePluginPath("project")
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, projectPath)
		userPaths, err := opencodeUserPluginPaths()
		if err != nil {
			return nil, err
		}
		candidates = append(candidates, userPaths...)
	}

	var conflicts []string
	seen := map[string]bool{}
	for _, candidate := range candidates {
		clean := filepath.Clean(candidate)
		if seen[clean] || samePath(candidate, targetPath) {
			continue
		}
		seen[clean] = true
		managed, err := opencodeManagedFileExists(candidate)
		if err != nil {
			return nil, err
		}
		if managed {
			conflicts = append(conflicts, candidate)
		}
	}
	return conflicts, nil
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
		// Stop at the first existing, non-symlink ancestor. This still catches
		// symlinks inside the managed plugin path, while allowing platform-level
		// aliases such as macOS /var -> /private/var.
		return nil
	}
	return nil
}
