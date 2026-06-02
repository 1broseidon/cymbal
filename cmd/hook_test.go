package cmd

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/internal/updatecheck"
)

// ── detector: positive cases ──

func TestDetectRipgrepSuggestsCymbalSearch(t *testing.T) {
	s := detectSearchCommand([]string{"rg", "-n", "HandleRegister", "."}, "")
	if s.Replacement == "" {
		t.Fatalf("expected a suggestion for 'rg HandleRegister'; got none")
	}
	if !strings.Contains(s.Replacement, "cymbal search HandleRegister") {
		t.Errorf("expected cymbal search suggestion; got %q", s.Replacement)
	}
	if s.Tool != "rg" {
		t.Errorf("expected Tool=rg; got %q", s.Tool)
	}
}

func TestDetectGrepRecursiveSuggestsCymbalSearch(t *testing.T) {
	s := detectSearchCommand([]string{"grep", "-rn", "FindUser", "src/"}, "")
	if s.Replacement == "" || !strings.Contains(s.Replacement, "FindUser") {
		t.Fatalf("expected FindUser suggestion; got %+v", s)
	}
}

func TestDetectGrepMinusE(t *testing.T) {
	s := detectSearchCommand([]string{"grep", "-rn", "-e", "OpenStore", "src/"}, "")
	if !strings.Contains(s.Replacement, "OpenStore") {
		t.Errorf("-e PATTERN should be picked up; got %q", s.Replacement)
	}
}

func TestDetectFindByName(t *testing.T) {
	s := detectSearchCommand([]string{"find", ".", "-name", "UserRepo.go"}, "")
	if s.Replacement == "" || s.Tool != "find" {
		t.Fatalf("expected find suggestion; got %+v", s)
	}
}

func TestDetectFdSourceQuery(t *testing.T) {
	s := detectSearchCommand([]string{"fd", "Server"}, "")
	if s.Replacement == "" || !strings.Contains(s.Replacement, "Server") {
		t.Errorf("expected fd Server to trigger suggestion; got %+v", s)
	}
}

// ── detector: negative cases (things we must NOT nudge on) ──

func TestDetectShortQuerySkipped(t *testing.T) {
	if s := detectSearchCommand([]string{"rg", "-n", "ab", "."}, ""); s.Replacement != "" {
		t.Errorf("2-char query should be skipped; got %q", s.Replacement)
	}
}

func TestDetectLogFileGlobSkipped(t *testing.T) {
	if s := detectSearchCommand([]string{"find", ".", "-name", "*.log"}, ""); s.Replacement != "" {
		t.Errorf("log glob should be skipped; got %q", s.Replacement)
	}
}

func TestDetectHeavyRegexSkipped(t *testing.T) {
	// A real regex — more than half metachars — is a fine rg use case.
	if s := detectSearchCommand([]string{"rg", "-n", `^(foo|bar)+\s*$`, "src/"}, ""); s.Replacement != "" {
		t.Errorf("heavy-regex query should be skipped; got %q", s.Replacement)
	}
}

func TestDetectNonShellToolNameSkipped(t *testing.T) {
	// Claude Code 'Edit' tool should never trigger us even if the command
	// string happens to contain 'rg'.
	if s := detectSearchCommand([]string{"rg", "something"}, "Edit"); s.Replacement != "" {
		t.Errorf("non-shell tool should skip; got %+v", s)
	}
}

func TestDetectEmptyInputSkipped(t *testing.T) {
	if s := detectSearchCommand(nil, ""); s.Replacement != "" {
		t.Errorf("empty input should skip; got %+v", s)
	}
}

func TestDetectCatFileSkipped(t *testing.T) {
	// `cat` isn't in our trigger set.
	if s := detectSearchCommand([]string{"cat", "src/main.go"}, ""); s.Replacement != "" {
		t.Errorf("cat should not trigger; got %+v", s)
	}
}

func TestDetectStopsAtPipe(t *testing.T) {
	// splitShellish is what the stdin path runs; it stops at `|`, so only
	// the first pipeline stage survives and `ls` doesn't match our set.
	fields := splitShellish("ls | wc -l")
	if s := detectSearchCommand(fields, ""); s.Replacement != "" {
		t.Errorf("ls|wc should not trigger; got %+v", s)
	}
}

func TestDetectQuotedLiteralSkipped(t *testing.T) {
	// `grep '"jsx"' file.go` — outer single quotes consumed by the shell,
	// leaving "jsx" as the literal pattern. This is a string-value lookup,
	// not a symbol lookup; cymbal search would be the wrong recommendation.
	if s := detectSearchCommand([]string{"grep", "-n", `"jsx"`, "parser.go"}, ""); s.Replacement != "" {
		t.Errorf("quoted literal query should be skipped; got %q", s.Replacement)
	}
	// Same shape via the stdin path (splitShellish must preserve the inner quotes).
	fields := splitShellish(`grep -n '"jsx"' parser.go`)
	if s := detectSearchCommand(fields, ""); s.Replacement != "" {
		t.Errorf("quoted literal via stdin path should be skipped; got %q (fields=%v)", s.Replacement, fields)
	}
}

func TestDetectMultiWordQuerySkipped(t *testing.T) {
	// Phrases are never symbol names — they're literal text searches.
	if s := detectSearchCommand([]string{"rg", "-n", "hello world", "."}, ""); s.Replacement != "" {
		t.Errorf("multi-word query should be skipped; got %q", s.Replacement)
	}
}

func TestDetectQueryWithNonIdentifierCharsSkipped(t *testing.T) {
	// `=`, `:`, `/`, `<`, `>` and friends never appear in identifiers and
	// strongly signal literal-text intent (config values, URLs, comparisons).
	cases := []string{
		"key=value",
		"http://example.com",
		"foo:bar",
		"x<y",
		"a&&b",
	}
	for _, q := range cases {
		if s := detectSearchCommand([]string{"rg", "-n", q, "."}, ""); s.Replacement != "" {
			t.Errorf("non-identifier query %q should be skipped; got %q", q, s.Replacement)
		}
	}
}

func TestDetectExplicitFilePathsSkipped(t *testing.T) {
	// User named specific source files — they already know where to look.
	// cymbal search is for repo-wide discovery, not line-number lookup.
	if s := detectSearchCommand([]string{"grep", "-n", "FindUser", "parser.go", "registry.go"}, ""); s.Replacement != "" {
		t.Errorf("explicit file targets should suppress the nudge; got %q", s.Replacement)
	}
	if s := detectSearchCommand([]string{"rg", "-n", "Server", "internal/api/server.ts"}, ""); s.Replacement != "" {
		t.Errorf("path/to/file.ts target should suppress the nudge; got %q", s.Replacement)
	}
	if s := detectSearchCommand([]string{"rg", "Foo", "App.tsx"}, ""); s.Replacement != "" {
		t.Errorf("App.tsx target should suppress the nudge; got %q", s.Replacement)
	}
}

func TestDetectDirectoryTargetStillNudges(t *testing.T) {
	// Trailing slash means directory → discovery search → nudge stays.
	if s := detectSearchCommand([]string{"grep", "-rn", "FindUser", "src/"}, ""); s.Replacement == "" {
		t.Errorf("directory target should still trigger nudge; got nothing")
	}
	// Bare `.` is a discovery root.
	if s := detectSearchCommand([]string{"rg", "Foo", "."}, ""); s.Replacement == "" {
		t.Errorf("`.` discovery root should still trigger nudge; got nothing")
	}
	// Glob is discovery.
	if s := detectSearchCommand([]string{"rg", "Foo", "**/*.go"}, ""); s.Replacement == "" {
		t.Errorf("glob target should still trigger nudge; got nothing")
	}
}

func TestDetectSingleCharRegexSignalsSkipped(t *testing.T) {
	// `|` alternation and `^…$` anchors are unambiguous regex signals.
	// The metachar-majority gate only catches metachar-heavy patterns; these
	// otherwise-letter-rich queries would slip through and produce a
	// misleading nudge (`cymbal search 'worktree|git'`).
	cases := []string{
		"worktree|git",
		"foo|bar",
		"^Server$",
		"^FindUser",
		"Handler$",
	}
	for _, q := range cases {
		if s := detectSearchCommand([]string{"rg", "-n", q, "."}, ""); s.Replacement != "" {
			t.Errorf("regex-shaped query %q should be skipped; got %q", q, s.Replacement)
		}
	}
}

func TestDetectMinusEPatternIsNotMistakenForFilePath(t *testing.T) {
	// In `rg -e Foo -e Bar.x src/`, both `Foo` and `Bar.x` are patterns,
	// not paths. The hasExplicitFileTarget check must skip the token
	// following `-e` so it doesn't read `Bar.x` as a file target.
	s := detectSearchCommand([]string{"rg", "-n", "-e", "Foo", "-e", "Bar.x", "src/"}, "")
	if s.Replacement == "" {
		t.Errorf("-e Foo -e Bar.x src/ should still nudge (Bar.x is a pattern, not a path); got nothing")
	}
}

// ── nudge: Claude Code dedicated tools (Grep / Glob / Read) ──
// Issue #47: the matcher previously only knew the Bash schema; Grep/Glob/Read
// hit the hook with structured tool_input fields and went silent. These tests
// pin the new dispatch + the non-code-file skip behavior.

func TestDetectGrepToolSymbolPattern(t *testing.T) {
	s := detectNudge(nudgeInput{toolName: "Grep", pattern: "HandleRegister"})
	if s.Replacement == "" {
		t.Fatalf("Grep with symbol-shaped pattern should nudge; got %+v", s)
	}
	if s.Tool != "Grep" {
		t.Errorf("expected Tool=Grep; got %q", s.Tool)
	}
	if !strings.Contains(s.Replacement, "cymbal search HandleRegister") {
		t.Errorf("expected cymbal search suggestion; got %q", s.Replacement)
	}
}

func TestDetectGrepToolNonCodeGlobSkipped(t *testing.T) {
	// Reporter's case: `Grep --glob '*.json' Foo` is searching data, not code.
	cases := []struct{ glob string }{
		{"*.json"}, {"*.md"}, {"*.log"}, {"*.yaml"}, {"*.txt"},
		{"**/*.jsonl"}, {"src/**/*.md"},
	}
	for _, tc := range cases {
		s := detectNudge(nudgeInput{toolName: "Grep", pattern: "Foo", glob: tc.glob})
		if s.Replacement != "" {
			t.Errorf("non-code glob %q must suppress nudge; got %q", tc.glob, s.Replacement)
		}
	}
}

func TestDetectGrepToolNonCodeTypeSkipped(t *testing.T) {
	for _, tt := range []string{"md", "json", "yaml", "log", "csv"} {
		s := detectNudge(nudgeInput{toolName: "Grep", pattern: "Foo", fileType: tt})
		if s.Replacement != "" {
			t.Errorf("non-code --type %q must suppress nudge; got %q", tt, s.Replacement)
		}
	}
}

func TestDetectGrepToolCodeGlobNudges(t *testing.T) {
	// A code-extension glob (the agent restricting to .go/.ts files) keeps
	// the nudge active — that's exactly when we want it most.
	for _, glob := range []string{"*.go", "**/*.ts", "src/**/*.tsx", "*.py", "*.rs"} {
		s := detectNudge(nudgeInput{toolName: "Grep", pattern: "Handler", glob: glob})
		if s.Replacement == "" {
			t.Errorf("code glob %q should keep nudge active; got nothing", glob)
		}
	}
}

func TestDetectGrepToolLiteralPatternStillSkipped(t *testing.T) {
	// The existing literal-text gates still apply: Grep with a quoted-value
	// or multi-word pattern must skip even when the file scope is code.
	cases := []nudgeInput{
		{toolName: "Grep", pattern: `"jsx"`, glob: "*.go"},
		{toolName: "Grep", pattern: "hello world", glob: "*.go"},
		{toolName: "Grep", pattern: "foo|bar", glob: "*.go"},
		{toolName: "Grep", pattern: "^Server$"},
	}
	for _, in := range cases {
		if s := detectNudge(in); s.Replacement != "" {
			t.Errorf("literal/regex pattern %q must skip; got %q", in.pattern, s.Replacement)
		}
	}
}

func TestDetectGlobToolCodePattern(t *testing.T) {
	s := detectNudge(nudgeInput{toolName: "Glob", pattern: "**/*.go"})
	if s.Replacement == "" {
		t.Fatalf("Glob on code files should nudge; got %+v", s)
	}
	if s.Tool != "Glob" {
		t.Errorf("expected Tool=Glob; got %q", s.Tool)
	}
}

func TestDetectGlobToolNonCodePatternSkipped(t *testing.T) {
	for _, pat := range []string{"*.json", "**/*.md", "logs/*.log", "*.yaml"} {
		if s := detectNudge(nudgeInput{toolName: "Glob", pattern: pat}); s.Replacement != "" {
			t.Errorf("non-code Glob %q must skip; got %q", pat, s.Replacement)
		}
	}
}

func TestDetectReadToolCodeFile(t *testing.T) {
	s := detectNudge(nudgeInput{toolName: "Read", filePath: "/repo/src/handler.go"})
	if s.Replacement == "" {
		t.Fatalf("Read on a code file should nudge with cymbal show; got %+v", s)
	}
	if !strings.Contains(s.Replacement, "cymbal show") {
		t.Errorf("expected cymbal show in suggestion; got %q", s.Replacement)
	}
}

func TestDetectReadToolNonCodeFileSkipped(t *testing.T) {
	for _, p := range []string{"README.md", "package.json", "config.yaml", "/tmp/output.log", "notes.txt"} {
		if s := detectNudge(nudgeInput{toolName: "Read", filePath: p}); s.Replacement != "" {
			t.Errorf("Read of non-code file %q must skip; got %q", p, s.Replacement)
		}
	}
}

func TestDetectBashFallsThroughToShellDetector(t *testing.T) {
	// Bash tool still routes to the legacy shell detector — regression guard
	// for everything shipped in #45 (literal-text/path-target/regex gates).
	s := detectNudge(nudgeInput{toolName: "Bash", fields: []string{"rg", "FindUser", "."}})
	if s.Replacement == "" {
		t.Fatalf("Bash with rg discovery should nudge; got %+v", s)
	}
}

func TestReadNudgeInputParsesGrepToolPayload(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = stdinR
	_, _ = stdinW.WriteString(`{"tool_name":"Grep","tool_input":{"pattern":"WidgetService","path":"/repo","glob":"*.go"}}`)
	_ = stdinW.Close()
	in, err := readNudgeInput(nil)
	os.Stdin = origStdin
	if err != nil {
		t.Fatal(err)
	}
	if in.toolName != "Grep" {
		t.Errorf("expected toolName=Grep; got %q", in.toolName)
	}
	if in.pattern != "WidgetService" {
		t.Errorf("expected pattern=WidgetService; got %q", in.pattern)
	}
	if in.glob != "*.go" {
		t.Errorf("expected glob=*.go; got %q", in.glob)
	}
}

func TestReadNudgeInputParsesReadToolPayload(t *testing.T) {
	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = stdinR
	_, _ = stdinW.WriteString(`{"tool_name":"Read","tool_input":{"file_path":"/repo/src/main.go"}}`)
	_ = stdinW.Close()
	in, err := readNudgeInput(nil)
	os.Stdin = origStdin
	if err != nil {
		t.Fatal(err)
	}
	if in.toolName != "Read" || in.filePath != "/repo/src/main.go" {
		t.Errorf("Read payload not parsed correctly: %+v", in)
	}
}

// ── nudge output shape ──

func TestEmitNudgeClaudeCodeJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	s := Suggestion{
		Tool:        "rg",
		Replacement: "cymbal search Foo",
		Why:         "ranked symbol results",
	}
	if err := emitNudge(&stdout, &stderr, "claude-code", []string{"rg", "Foo"}, s); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("claude-code output must be valid JSON: %v\n%s", err, stdout.String())
	}
	if _, hasDecision := out["decision"]; hasDecision {
		t.Errorf("top-level 'decision' is the deprecated shape and fails Claude Code's schema; got %+v", out)
	}
	if _, hasSysMsg := out["systemMessage"]; hasSysMsg {
		t.Errorf("top-level 'systemMessage' renders as a user warning, not model context; got %+v", out)
	}
	hso, ok := out["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("expected hookSpecificOutput object; got %+v", out)
	}
	if hso["hookEventName"] != "PreToolUse" {
		t.Errorf("expected hookEventName=PreToolUse; got %v", hso["hookEventName"])
	}
	if hso["permissionDecision"] != "allow" {
		t.Errorf("expected permissionDecision=allow; got %v", hso["permissionDecision"])
	}
	ctx, _ := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "cymbal search Foo") {
		t.Errorf("additionalContext missing suggestion; got %q", ctx)
	}
	// The nudge must be advisory, not declarative — the agent should have
	// an explicit "ignore this if your original tool was right" branch so
	// it doesn't reflexively switch tools on every false positive.
	if !strings.Contains(strings.ToLower(ctx), "ignore this") {
		t.Errorf("nudge wording should give the agent an explicit ignore-branch; got %q", ctx)
	}
	if !strings.Contains(strings.ToLower(ctx), "literal text") {
		t.Errorf("nudge wording should name the literal-text case explicitly; got %q", ctx)
	}
}

func TestEmitNudgeTextGoesToStderr(t *testing.T) {
	var stdout, stderr bytes.Buffer
	s := Suggestion{Replacement: "cymbal search X", Why: "why"}
	if err := emitNudge(&stdout, &stderr, "text", []string{"rg", "X"}, s); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 {
		t.Errorf("text mode must leave stdout empty; got %q", stdout.String())
	}
	if !strings.Contains(stderr.String(), "cymbal search X") {
		t.Errorf("expected message on stderr; got %q", stderr.String())
	}
}

func TestEmitNudgeNoSuggestionIsSilent(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := emitNudge(&stdout, &stderr, "claude-code", []string{"ls", "-la"}, Suggestion{}); err != nil {
		t.Fatal(err)
	}
	if stdout.Len() != 0 || stderr.Len() != 0 {
		t.Errorf("no-suggestion must be fully silent; stdout=%q stderr=%q", stdout.String(), stderr.String())
	}
}

// ── remind ──

func TestEmitRemindText(t *testing.T) {
	var buf bytes.Buffer
	if err := emitRemind(&buf, "text"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(buf.String(), "cymbal search") {
		t.Errorf("reminder should mention cymbal search; got %q", buf.String())
	}
}

func TestEmitRemindUpdateModeControlsNetwork(t *testing.T) {
	old := reminderUpdateStatus
	defer func() { reminderUpdateStatus = old }()

	var calls []updatecheck.Options
	reminderUpdateStatus = func(ctx context.Context, opts updatecheck.Options) (updatecheck.Status, error) {
		calls = append(calls, opts)
		return updatecheck.Status{}, nil
	}

	var buf bytes.Buffer
	if err := emitRemindWithUpdate(&buf, "text", "cache"); err != nil {
		t.Fatal(err)
	}
	if err := emitRemindWithUpdate(&buf, "text", "if-stale"); err != nil {
		t.Fatal(err)
	}
	if len(calls) != 2 {
		t.Fatalf("expected 2 update checks, got %d", len(calls))
	}
	if calls[0].AllowNetwork || calls[0].Timeout != 0 {
		t.Fatalf("cache mode should be cache-only, got %+v", calls[0])
	}
	if !calls[1].AllowNetwork || calls[1].Timeout != remindUpdateTimeout {
		t.Fatalf("if-stale mode should allow bounded network, got %+v", calls[1])
	}
}

func TestEmitRemindUpdateModeHonorsNotifierOptOut(t *testing.T) {
	old := reminderUpdateStatus
	defer func() { reminderUpdateStatus = old }()
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "1")

	var got updatecheck.Options
	reminderUpdateStatus = func(ctx context.Context, opts updatecheck.Options) (updatecheck.Status, error) {
		got = opts
		return updatecheck.Status{}, nil
	}

	var buf bytes.Buffer
	if err := emitRemindWithUpdate(&buf, "text", "if-stale"); err != nil {
		t.Fatal(err)
	}
	if got.AllowNetwork || got.Timeout != 0 {
		t.Fatalf("notifier opt-out should suppress live checks, got %+v", got)
	}
}

func TestEmitRemindRejectsUnknownUpdateMode(t *testing.T) {
	var buf bytes.Buffer
	err := emitRemindWithUpdate(&buf, "text", "always")
	if err == nil || !strings.Contains(err.Error(), "unknown --update") {
		t.Fatalf("expected unknown update mode error, got %v", err)
	}
}

func TestEmitRemindJSONShape(t *testing.T) {
	var buf bytes.Buffer
	if err := emitRemind(&buf, "json"); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json mode must emit valid JSON: %v\n%s", err, buf.String())
	}
	if out["systemMessage"] == nil {
		t.Errorf("expected systemMessage key; got %+v", out)
	}
}

func TestEmitRemindClaudeCodeShape(t *testing.T) {
	var buf bytes.Buffer
	if err := emitRemind(&buf, "claude-code"); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("claude-code mode must emit valid JSON: %v\n%s", err, buf.String())
	}
	if _, hasSysMsg := out["systemMessage"]; hasSysMsg {
		t.Errorf("top-level 'systemMessage' renders as a user warning, not model context; got %+v", out)
	}
	hso, ok := out["hookSpecificOutput"].(map[string]any)
	if !ok {
		t.Fatalf("expected hookSpecificOutput object; got %+v", out)
	}
	if hso["hookEventName"] != "SessionStart" {
		t.Errorf("expected hookEventName=SessionStart; got %v", hso["hookEventName"])
	}
	ctx, _ := hso["additionalContext"].(string)
	if !strings.Contains(ctx, "cymbal search") {
		t.Errorf("additionalContext missing reminder body; got %q", ctx)
	}
}

func TestEmitRemindClaudeCodeIncludesCachedUpdateMessage(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v0.11.5", "", ""
	defer func() { version, commit, date = oldVersion, oldCommit, oldDate }()

	cacheBase := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", cacheBase)
	t.Setenv("LOCALAPPDATA", cacheBase)
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	updateDir := filepath.Join(cacheBase, "cymbal")
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := `{
	  "schema_version": 1,
	  "current_version": "v0.11.5",
	  "last_checked_at": "2026-04-21T10:15:00Z",
	  "latest_version": "v0.12.0",
	  "release_url": "https://github.com/1broseidon/cymbal/releases/latest",
	  "update_available": true,
	  "install_type": "powershell",
	  "update_command": "irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex"
	}`
	if err := os.WriteFile(filepath.Join(updateDir, "update-check.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := emitRemind(&buf, "claude-code"); err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("claude-code mode must emit valid JSON: %v\n%s", err, buf.String())
	}
	hookOutput, _ := out["hookSpecificOutput"].(map[string]any)
	ctx, _ := hookOutput["additionalContext"].(string)
	if !strings.Contains(ctx, "cymbal update:") {
		t.Fatalf("expected update paragraph in additionalContext, got %q", ctx)
	}
}

func TestEmitRemindIncludesCachedUpdateMessage(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v0.11.5", "", ""
	defer func() { version, commit, date = oldVersion, oldCommit, oldDate }()

	cacheBase := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", cacheBase)
	t.Setenv("LOCALAPPDATA", cacheBase)
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	updateDir := filepath.Join(cacheBase, "cymbal")
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := `{
	  "schema_version": 1,
	  "current_version": "v0.11.5",
	  "last_checked_at": "2026-04-21T10:15:00Z",
	  "latest_version": "v0.12.0",
	  "release_url": "https://github.com/1broseidon/cymbal/releases/latest",
	  "update_available": true,
	  "install_type": "powershell",
	  "update_command": "irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex"
	}`
	if err := os.WriteFile(filepath.Join(updateDir, "update-check.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := emitRemind(&buf, "text"); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "cymbal update:") {
		t.Fatalf("expected update paragraph, got %q", out)
	}
	if !strings.Contains(out, "irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex") {
		t.Fatalf("expected powershell update command, got %q", out)
	}
}

func TestEmitRemindSkipsUpdateWhenNotifierDisabled(t *testing.T) {
	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "v0.11.5", "", ""
	defer func() { version, commit, date = oldVersion, oldCommit, oldDate }()

	cacheBase := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", cacheBase)
	t.Setenv("LOCALAPPDATA", cacheBase)
	t.Setenv("XDG_CACHE_HOME", cacheBase)
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "1")
	updateDir := filepath.Join(cacheBase, "cymbal")
	if err := os.MkdirAll(updateDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cache := `{
	  "schema_version": 1,
	  "current_version": "v0.11.5",
	  "last_checked_at": "2026-04-21T10:15:00Z",
	  "latest_version": "v0.12.0",
	  "update_available": true,
	  "install_type": "powershell",
	  "update_command": "irm https://raw.githubusercontent.com/1broseidon/cymbal/main/install.ps1 | iex"
	}`
	if err := os.WriteFile(filepath.Join(updateDir, "update-check.json"), []byte(cache), 0o644); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := emitRemind(&buf, "text"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(buf.String(), "cymbal update:") {
		t.Fatalf("expected notifier opt-out to suppress update paragraph, got %q", buf.String())
	}
}

func TestEmitHookNotifyJSONIncludesPayload(t *testing.T) {
	oldStatus, oldShouldNotify, oldMarkNotified := hookNotifyStatus, hookNotifyShouldNotify, hookNotifyMarkNotified
	defer func() {
		hookNotifyStatus = oldStatus
		hookNotifyShouldNotify = oldShouldNotify
		hookNotifyMarkNotified = oldMarkNotified
	}()

	markCalled := false
	hookNotifyStatus = func(ctx context.Context, opts updatecheck.Options) (updatecheck.Status, error) {
		return updatecheck.Status{
			Available:     true,
			LatestVersion: "v0.13.0",
			Command:       "brew upgrade 1broseidon/tap/cymbal",
			ReleaseURL:    "https://github.com/1broseidon/cymbal/releases/latest",
		}, nil
	}
	hookNotifyShouldNotify = func(status updatecheck.Status) bool { return true }
	hookNotifyMarkNotified = func(status updatecheck.Status) error {
		markCalled = true
		return nil
	}

	var buf bytes.Buffer
	if err := emitHookNotify(&buf, "json", "cache"); err != nil {
		t.Fatal(err)
	}
	if !markCalled {
		t.Fatal("expected notification mark to be recorded")
	}
	var out hookNotifyPayload
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json output must be valid: %v\n%s", err, buf.String())
	}
	if !out.Notify || out.LatestVersion != "v0.13.0" || out.Title != "cymbal v0.13.0 is available" {
		t.Fatalf("unexpected payload: %+v", out)
	}
	if out.Body != "Update: brew upgrade 1broseidon/tap/cymbal" {
		t.Fatalf("unexpected body: %+v", out)
	}
	if out.Command != "brew upgrade 1broseidon/tap/cymbal" || out.ReleaseURL != "https://github.com/1broseidon/cymbal/releases/latest" {
		t.Fatalf("unexpected command metadata: %+v", out)
	}
}

func TestEmitHookNotifyJSONFalseWhenThrottled(t *testing.T) {
	oldStatus, oldShouldNotify, oldMarkNotified := hookNotifyStatus, hookNotifyShouldNotify, hookNotifyMarkNotified
	defer func() {
		hookNotifyStatus = oldStatus
		hookNotifyShouldNotify = oldShouldNotify
		hookNotifyMarkNotified = oldMarkNotified
	}()

	markCalled := false
	hookNotifyStatus = func(ctx context.Context, opts updatecheck.Options) (updatecheck.Status, error) {
		return updatecheck.Status{Available: true, LatestVersion: "v0.13.0"}, nil
	}
	hookNotifyShouldNotify = func(status updatecheck.Status) bool { return false }
	hookNotifyMarkNotified = func(status updatecheck.Status) error {
		markCalled = true
		return nil
	}

	var buf bytes.Buffer
	if err := emitHookNotify(&buf, "json", "cache"); err != nil {
		t.Fatal(err)
	}
	if markCalled {
		t.Fatal("mark should not be called when throttled")
	}
	var out hookNotifyPayload
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json output must be valid: %v\n%s", err, buf.String())
	}
	if out.Notify {
		t.Fatalf("expected notify=false when throttled, got %+v", out)
	}
}

func TestEmitHookNotifyTextEmptyWhenNoUpdate(t *testing.T) {
	oldStatus, oldShouldNotify, oldMarkNotified := hookNotifyStatus, hookNotifyShouldNotify, hookNotifyMarkNotified
	defer func() {
		hookNotifyStatus = oldStatus
		hookNotifyShouldNotify = oldShouldNotify
		hookNotifyMarkNotified = oldMarkNotified
	}()

	hookNotifyStatus = func(ctx context.Context, opts updatecheck.Options) (updatecheck.Status, error) {
		return updatecheck.Status{Available: false}, nil
	}
	hookNotifyShouldNotify = func(status updatecheck.Status) bool { return false }
	hookNotifyMarkNotified = func(status updatecheck.Status) error {
		t.Fatal("mark should not be called when no update is available")
		return nil
	}

	var buf bytes.Buffer
	if err := emitHookNotify(&buf, "text", "cache"); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("expected empty text output, got %q", buf.String())
	}
}

func TestEmitHookNotifyHonorsNotifierOptOut(t *testing.T) {
	oldStatus, oldMarkNotified := hookNotifyStatus, hookNotifyMarkNotified
	defer func() {
		hookNotifyStatus = oldStatus
		hookNotifyMarkNotified = oldMarkNotified
	}()

	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "1")
	hookNotifyStatus = func(ctx context.Context, opts updatecheck.Options) (updatecheck.Status, error) {
		return updatecheck.Status{Available: true, LatestVersion: "v0.13.0"}, nil
	}
	hookNotifyMarkNotified = func(status updatecheck.Status) error {
		t.Fatal("mark should not be called when notifier is disabled")
		return nil
	}

	var buf bytes.Buffer
	if err := emitHookNotify(&buf, "json", "if-stale"); err != nil {
		t.Fatal(err)
	}
	var out hookNotifyPayload
	if err := json.Unmarshal(buf.Bytes(), &out); err != nil {
		t.Fatalf("json output must be valid: %v\n%s", err, buf.String())
	}
	if out.Notify {
		t.Fatalf("expected notify=false when notifier disabled, got %+v", out)
	}
}

// ── claude-code install / uninstall round-trip ──

func TestClaudeCodeInstallIsIdempotent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Pre-seed with user-owned hooks that must survive.
	seed := map[string]any{
		"model": "sonnet",
		"hooks": map[string]any{
			"PreToolUse": []any{
				map[string]any{
					"matcher": "Bash",
					"hooks": []any{
						map[string]any{"type": "command", "command": "user-owned-thing"},
					},
				},
			},
		},
	}
	seedBytes, _ := json.Marshal(seed)
	if err := os.WriteFile(path, seedBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	// install twice — must be idempotent.
	for i := 0; i < 2; i++ {
		s, err := loadClaudeSettings(path)
		if err != nil {
			t.Fatal(err)
		}
		mergeClaudeHooks(s)
		if err := writeClaudeSettings(path, s); err != nil {
			t.Fatal(err)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var parsed map[string]any
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("result not valid JSON: %v\n%s", err, got)
	}
	if parsed["model"] != "sonnet" {
		t.Errorf("pre-existing 'model' key was dropped: %+v", parsed)
	}
	hooks, _ := parsed["hooks"].(map[string]any)
	preTool, _ := hooks["PreToolUse"].([]any)
	if len(preTool) != 2 {
		t.Fatalf("expected 2 PreToolUse entries (user + cymbal), got %d: %s", len(preTool), got)
	}
	sessionStart, _ := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Errorf("expected 1 SessionStart entry; got %d", len(sessionStart))
	}
	if !strings.Contains(string(got), "--update=if-stale") {
		t.Fatalf("expected stale-aware reminder command, got %s", got)
	}

	// uninstall and confirm only our entries are removed.
	s, err := loadClaudeSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	removeClaudeHooks(s)
	if err := writeClaudeSettings(path, s); err != nil {
		t.Fatal(err)
	}
	got, _ = os.ReadFile(path)
	_ = json.Unmarshal(got, &parsed)
	if parsed["model"] != "sonnet" {
		t.Errorf("uninstall damaged unrelated keys: %s", got)
	}
	hooks, _ = parsed["hooks"].(map[string]any)
	preTool, _ = hooks["PreToolUse"].([]any)
	if len(preTool) != 1 {
		t.Errorf("expected user's single PreToolUse to survive; got %d entries\n%s", len(preTool), got)
	}
	if _, stillThere := hooks["SessionStart"]; stillThere {
		t.Errorf("SessionStart should have been removed when empty; got %+v", hooks)
	}
}

// TestClaudeCodeInstallMigratesFromUserPromptSubmit verifies the v0.11.2
// reminder hook-point move: users upgrading from 0.11.1 or earlier had
// `cymbal hook remind` wired to UserPromptSubmit (fires every turn), which
// this release moves to SessionStart (fires once per session). A re-install
// must drop the old marker-tagged UserPromptSubmit entry and add a
// SessionStart entry, without touching any non-cymbal entries the user has
// added to either hook point.
func TestClaudeCodeInstallMigratesFromUserPromptSubmit(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "settings.json")

	// Simulate a pre-0.11.2 install: the old UserPromptSubmit entry plus an
	// unrelated user-owned UserPromptSubmit hook that must survive.
	seed := map[string]any{
		"hooks": map[string]any{
			"UserPromptSubmit": []any{
				map[string]any{
					"hooks": []any{
						map[string]any{
							"type":    "command",
							"command": "cymbal hook remind --format=claude-code",
							"marker":  claudeHookMarker,
							"timeout": 5,
						},
					},
				},
				map[string]any{
					"hooks": []any{
						map[string]any{"type": "command", "command": "user-unrelated-hook"},
					},
				},
			},
		},
	}
	seedBytes, _ := json.Marshal(seed)
	if err := os.WriteFile(path, seedBytes, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := loadClaudeSettings(path)
	if err != nil {
		t.Fatal(err)
	}
	mergeClaudeHooks(s)
	if err := writeClaudeSettings(path, s); err != nil {
		t.Fatal(err)
	}

	got, _ := os.ReadFile(path)
	if !strings.Contains(string(got), "--update=if-stale") {
		t.Fatalf("expected migrated SessionStart reminder to use --update=if-stale, got %s", got)
	}
	var parsed map[string]any
	_ = json.Unmarshal(got, &parsed)
	hooks, _ := parsed["hooks"].(map[string]any)

	// Old UserPromptSubmit marker entry must be gone; unrelated user entry stays.
	userPrompt, _ := hooks["UserPromptSubmit"].([]any)
	if len(userPrompt) != 1 {
		t.Fatalf("expected user's unrelated UserPromptSubmit to survive alone; got %d entries\n%s", len(userPrompt), got)
	}
	if hookGroupHasMarker(userPrompt[0], claudeHookMarker) {
		t.Errorf("old cymbal UserPromptSubmit entry should have been removed; got %s", got)
	}

	// New SessionStart entry must exist.
	sessionStart, _ := hooks["SessionStart"].([]any)
	if len(sessionStart) != 1 {
		t.Fatalf("expected 1 SessionStart entry after migration; got %d\n%s", len(sessionStart), got)
	}
	if !hookGroupHasMarker(sessionStart[0], claudeHookMarker) {
		t.Errorf("expected cymbal SessionStart entry; got %s", got)
	}
}

// ── unknown agent hint ──

func TestLookupHookAdapterUnknownAgentMentionsDocs(t *testing.T) {
	_, err := lookupHookAdapter("cursor")
	if err == nil {
		t.Fatal("expected error for unsupported agent")
	}
	if !strings.Contains(err.Error(), "docs/AGENT_HOOKS.md") {
		t.Errorf("unknown-agent error should point users at the docs; got %q", err)
	}
}

func TestLookupHookAdapterOpenCode(t *testing.T) {
	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatalf("expected opencode adapter, got error: %v", err)
	}
	if adapter.install == nil || adapter.uninstall == nil {
		t.Fatalf("expected non-nil install/uninstall funcs, got %+v", adapter)
	}
}

func TestOpenCodeInstallProjectScopeWritesManagedPlugin(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}

	target, _, err := adapter.install("project", false)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	wantTarget := filepath.Join(".opencode", "plugins", "cymbal-opencode.js")
	if target != wantTarget {
		t.Fatalf("unexpected project target: got %q want %q", target, wantTarget)
	}
	absTarget := filepath.Join(dir, wantTarget)
	data, err := os.ReadFile(absTarget)
	if err != nil {
		t.Fatalf("expected managed plugin file at %s: %v", absTarget, err)
	}
	if !strings.Contains(string(data), "cymbal hook remind") || !strings.Contains(string(data), "--update=if-stale") {
		t.Fatalf("expected managed plugin to delegate to remind with stale-aware updates, got %q", string(data))
	}
	if !strings.Contains(string(data), `"tool.execute.before"`) || !strings.Contains(string(data), `cymbal hook nudge --format=json`) {
		t.Fatalf("expected managed plugin to install OpenCode nudge hook, got %q", string(data))
	}
	if !strings.Contains(string(data), `export const CymbalPlugin`) || strings.Contains(string(data), `export default`) || strings.Contains(string(data), `export function`) {
		t.Fatalf("expected managed plugin to expose only the OpenCode plugin export, got %q", string(data))
	}
	if !strings.Contains(string(data), `const nudgableTools = ["bash", "Grep", "Glob"]`) || !strings.Contains(string(data), `tool_name: input.tool === "bash" ? "bash" : input.tool`) {
		t.Fatalf("expected managed plugin to nudge Bash/Grep/Glob through structured input, got %q", string(data))
	}
	if !strings.Contains(string(data), `process.platform === "win32"`) {
		t.Fatalf("expected managed plugin to guard Windows shell rewriting, got %q", string(data))
	}
	if !strings.Contains(string(data), opencodeHookMarker) || !strings.Contains(string(data), currentVersion()) {
		t.Fatalf("expected managed plugin metadata marker/version, got %q", string(data))
	}
}

func TestOpenCodeInstallUserScopeWritesManagedPlugin(t *testing.T) {
	home := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("APPDATA", configRoot)
	t.Setenv("OPENCODE_CONFIG_DIR", "")

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}

	target, _, err := adapter.install("user", false)
	if err != nil {
		t.Fatalf("install failed: %v", err)
	}
	configDir, err := opencodeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	wantTarget := filepath.Join(configDir, "plugins", "cymbal-opencode.js")
	if target != wantTarget {
		t.Fatalf("unexpected user target: got %q want %q", target, wantTarget)
	}
	if _, err := os.Stat(wantTarget); err != nil {
		t.Fatalf("expected managed plugin file at %s: %v", wantTarget, err)
	}
}

func TestOpenCodeConfigDirUsesHomeDotConfigAcrossPlatforms(t *testing.T) {
	home := t.TempDir()
	appData := t.TempDir()
	xdgConfig := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("APPDATA", appData)
	t.Setenv("XDG_CONFIG_HOME", xdgConfig)
	t.Setenv("OPENCODE_CONFIG_DIR", "")

	got, err := opencodeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, ".config", "opencode")
	if got != want {
		t.Fatalf("unexpected OpenCode config dir: got %q want %q", got, want)
	}
}

func TestOpenCodeConfigDirHonorsExplicitOverride(t *testing.T) {
	configured := t.TempDir()
	t.Setenv("OPENCODE_CONFIG_DIR", configured)

	got, err := opencodeConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	if got != configured {
		t.Fatalf("unexpected OpenCode config dir override: got %q want %q", got, configured)
	}
}

func TestOpenCodeInstallWithOverrideRefusesDefaultGlobalManagedPlugin(t *testing.T) {
	home := t.TempDir()
	overrideConfigDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("OPENCODE_CONFIG_DIR", overrideConfigDir)

	defaultConfigDir := filepath.Join(home, ".config", "opencode")
	defaultPlugin := filepath.Join(defaultConfigDir, "plugins", "cymbal-opencode.js")
	if err := os.MkdirAll(filepath.Dir(defaultPlugin), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "// " + opencodeHookMarker + " managed by cymbal\n// cymbal-version: v0.0.1\nexport default async () => ({})\n"
	if err := os.WriteFile(defaultPlugin, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.install("user", false); err == nil {
		t.Fatal("expected user-scope override install to refuse duplicate default global managed plugin")
	}
}

func TestOpenCodeProjectInstallWithOverrideRefusesDefaultGlobalManagedPlugin(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)
	home := t.TempDir()
	overrideConfigDir := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("OPENCODE_CONFIG_DIR", overrideConfigDir)

	defaultConfigDir := filepath.Join(home, ".config", "opencode")
	defaultPlugin := filepath.Join(defaultConfigDir, "plugins", "cymbal-opencode.js")
	if err := os.MkdirAll(filepath.Dir(defaultPlugin), 0o755); err != nil {
		t.Fatal(err)
	}
	content := "// " + opencodeHookMarker + " managed by cymbal\n// cymbal-version: v0.0.1\nexport default async () => ({})\n"
	if err := os.WriteFile(defaultPlugin, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.install("project", false); err == nil {
		t.Fatal("expected project-scope install to refuse duplicate default global managed plugin")
	}
}

func TestOpenCodeInstallAllowsSymlinkedAncestorOutsideConfigRoot(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation is not consistently available on Windows")
	}
	home := t.TempDir()
	base := t.TempDir()
	realRoot := filepath.Join(base, "real")
	if err := os.Mkdir(realRoot, 0o755); err != nil {
		t.Fatal(err)
	}
	linkRoot := filepath.Join(base, "link")
	if err := os.Symlink(realRoot, linkRoot); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	configRoot := filepath.Join(linkRoot, "config")
	if err := os.Mkdir(configRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("APPDATA", configRoot)

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	target, _, err := adapter.install("user", false)
	if err != nil {
		t.Fatalf("install failed through symlinked ancestor outside config root: %v", err)
	}
	if _, err := os.Stat(target); err != nil {
		t.Fatalf("expected managed plugin file at %s: %v", target, err)
	}
}

func TestOpenCodeInstallDryRunDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}

	target, summary, err := adapter.install("project", true)
	if err != nil {
		t.Fatalf("dry-run install failed: %v", err)
	}
	wantTarget := filepath.Join(".opencode", "plugins", "cymbal-opencode.js")
	if target != wantTarget {
		t.Fatalf("unexpected project target: got %q want %q", target, wantTarget)
	}
	if _, err := os.Stat(filepath.Join(dir, wantTarget)); !os.IsNotExist(err) {
		t.Fatalf("dry-run should not write managed plugin file; stat err=%v", err)
	}
	if !strings.Contains(summary, "cymbal hook remind") || !strings.Contains(summary, "--update=if-stale") {
		t.Fatalf("dry-run summary should show stale-aware remind integration, got %q", summary)
	}
	if !strings.Contains(summary, `"tool.execute.before"`) || !strings.Contains(summary, `cymbal hook nudge --format=json`) {
		t.Fatalf("dry-run summary should include OpenCode nudge integration, got %q", summary)
	}
	if !strings.Contains(summary, `export const CymbalPlugin`) || strings.Contains(summary, `export default`) || strings.Contains(summary, `export function`) {
		t.Fatalf("dry-run summary should expose only the OpenCode plugin export, got %q", summary)
	}
	if !strings.Contains(summary, opencodeHookMarker) || !strings.Contains(summary, currentVersion()) {
		t.Fatalf("dry-run summary should include managed plugin metadata, got %q", summary)
	}
}

func TestOpenCodeInstallIsIdempotentAndUninstallPreservesUnrelatedPlugins(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	pluginsDir := filepath.Join(dir, ".opencode", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	userPlugin := filepath.Join(pluginsDir, "user-owned.js")
	if err := os.WriteFile(userPlugin, []byte("export default async () => ({})\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 2; i++ {
		if _, _, err := adapter.install("project", false); err != nil {
			t.Fatalf("install %d failed: %v", i+1, err)
		}
	}

	entries, err := os.ReadDir(pluginsDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("expected exactly 2 plugin files after idempotent reinstall, got %d", len(entries))
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "cymbal-opencode.js")); err != nil {
		t.Fatalf("expected managed plugin file to exist after reinstall: %v", err)
	}

	if _, _, err := adapter.uninstall("project", false); err != nil {
		t.Fatalf("uninstall failed: %v", err)
	}
	if _, err := os.Stat(filepath.Join(pluginsDir, "cymbal-opencode.js")); !os.IsNotExist(err) {
		t.Fatalf("expected managed plugin file to be removed; stat err=%v", err)
	}
	if data, err := os.ReadFile(userPlugin); err != nil {
		t.Fatalf("expected unrelated user plugin to survive: %v", err)
	} else if !strings.Contains(string(data), "export default") {
		t.Fatalf("expected unrelated user plugin contents to survive, got %q", string(data))
	}
}

func TestOpenCodeInstallUpgradesExistingManagedPluginInPlace(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	pluginsDir := filepath.Join(dir, ".opencode", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedPlugin := filepath.Join(pluginsDir, "cymbal-opencode.js")
	oldContent := "// " + opencodeHookMarker + " managed by cymbal\n// cymbal-version: v0.0.1\nexport default async () => ({})\n"
	if err := os.WriteFile(managedPlugin, []byte(oldContent), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.install("project", false); err != nil {
		t.Fatalf("install failed: %v", err)
	}

	data, err := os.ReadFile(managedPlugin)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	if strings.Contains(got, "cymbal-version: v0.0.1") {
		t.Fatalf("expected install to replace stale managed plugin content, got %q", got)
	}
	if !strings.Contains(got, "cymbal-version: "+currentVersion()) {
		t.Fatalf("expected upgraded managed plugin to carry current version, got %q", got)
	}
	if !strings.Contains(got, `"tool.execute.before"`) {
		t.Fatalf("expected upgraded managed plugin to carry current hook logic, got %q", got)
	}
}

func TestOpenCodeInstallRefusesToOverwriteForeignPluginAtManagedPath(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	pluginsDir := filepath.Join(dir, ".opencode", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedPlugin := filepath.Join(pluginsDir, "cymbal-opencode.js")
	foreign := "// user-owned file\nexport default async () => ({ custom: true })\n"
	if err := os.WriteFile(managedPlugin, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.install("project", false); err == nil {
		t.Fatal("expected install to refuse overwriting foreign plugin file")
	}

	data, err := os.ReadFile(managedPlugin)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != foreign {
		t.Fatalf("expected foreign plugin file to remain untouched, got %q", string(data))
	}
}

func TestOpenCodeInstallDryRunRefusesForeignPluginAtManagedPath(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	pluginsDir := filepath.Join(dir, ".opencode", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedPlugin := filepath.Join(pluginsDir, "cymbal-opencode.js")
	foreign := "// user-owned file\nexport default async () => ({ custom: true })\n"
	if err := os.WriteFile(managedPlugin, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.install("project", true); err == nil {
		t.Fatal("expected dry-run install to report overwrite refusal for foreign plugin file")
	}

	data, err := os.ReadFile(managedPlugin)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != foreign {
		t.Fatalf("expected foreign plugin file to remain untouched after dry-run, got %q", string(data))
	}
}

func TestOpenCodeUninstallPreservesForeignPluginAtManagedPath(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	pluginsDir := filepath.Join(dir, ".opencode", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedPlugin := filepath.Join(pluginsDir, "cymbal-opencode.js")
	foreign := "// user-owned file\nexport default async () => ({ custom: true })\n"
	if err := os.WriteFile(managedPlugin, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.uninstall("project", false); err != nil {
		t.Fatalf("unexpected uninstall error for foreign plugin file: %v", err)
	}

	data, err := os.ReadFile(managedPlugin)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != foreign {
		t.Fatalf("expected foreign plugin file to survive uninstall, got %q", string(data))
	}
}

func TestOpenCodeUninstallDryRunPreservesForeignPluginAtManagedPath(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	pluginsDir := filepath.Join(dir, ".opencode", "plugins")
	if err := os.MkdirAll(pluginsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	managedPlugin := filepath.Join(pluginsDir, "cymbal-opencode.js")
	foreign := "// user-owned file\nexport default async () => ({ custom: true })\n"
	if err := os.WriteFile(managedPlugin, []byte(foreign), 0o644); err != nil {
		t.Fatal(err)
	}

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	target, summary, err := adapter.uninstall("project", true)
	if err != nil {
		t.Fatalf("unexpected dry-run uninstall error for foreign plugin file: %v", err)
	}
	if target != filepath.Join(".opencode", "plugins", "cymbal-opencode.js") {
		t.Fatalf("unexpected dry-run uninstall target: %q", target)
	}
	if !strings.Contains(summary, "leave non-cymbal OpenCode plugin untouched") {
		t.Fatalf("expected dry-run uninstall to reflect preservation of foreign plugin file, got %q", summary)
	}

	data, err := os.ReadFile(managedPlugin)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != foreign {
		t.Fatalf("expected foreign plugin file to survive dry-run uninstall, got %q", string(data))
	}
}

func TestOpenCodeInstallRefusesWhenOtherScopeAlreadyHasManagedPlugin(t *testing.T) {
	dir := t.TempDir()
	withTestWorkingDir(t, dir)

	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("OPENCODE_CONFIG_DIR", "")

	adapter, err := lookupHookAdapter("opencode")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := adapter.install("project", false); err != nil {
		t.Fatalf("project-scope install failed: %v", err)
	}
	if _, _, err := adapter.install("user", false); err == nil {
		t.Fatal("expected user-scope install to refuse when project-scope managed plugin already exists")
	}
}

func withTestWorkingDir(t *testing.T, dir string) {
	t.Helper()
	home := t.TempDir()
	configRoot := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("HOMEDRIVE", "")
	t.Setenv("HOMEPATH", "")
	t.Setenv("OPENCODE_CONFIG_DIR", "")
	t.Setenv("XDG_CONFIG_HOME", configRoot)
	t.Setenv("APPDATA", configRoot)
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(wd); err != nil {
			t.Fatal(err)
		}
	})
}
