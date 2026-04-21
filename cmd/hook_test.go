package cmd

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
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
