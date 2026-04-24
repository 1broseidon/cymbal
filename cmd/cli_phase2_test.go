package cmd

import (
	"bytes"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

func newPhase2Repo(t *testing.T) (string, string) {
	t.Helper()
	defer index.CloseAll()

	cacheDir := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", cacheDir)

	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.com/cymbaltest\n\ngo 1.25\n")
	writeFile(t, repo, "main.go", `package main

import "example.com/cymbaltest/lib"

type Runner interface {
	Run()
}

type Worker struct{}

func (w Worker) Run() {
	helper()
	lib.Shared()
}

func Execute() {
	helper()
	w := Worker{}
	w.Run()
}

func helper() {}
`)
	writeFile(t, repo, filepath.Join("lib", "lib.go"), `package lib

func Shared() {}
`)
	writeFile(t, repo, filepath.Join("java", "UserService.java"), `package example;

interface Service {
  void handle();
}

class UserService implements Service {
  public void handle() {
    save();
  }

  private void save() {}
}
`)

	runGit(t, repo, "init")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "-c", "user.name=Cymbal Test", "-c", "user.email=cymbal@example.invalid", "commit", "-m", "initial")

	if _, err := index.Index(repo, "", index.Options{Workers: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	dbPath, err := index.RepoDBPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.CloseAll)
	return repo, dbPath
}

func writeFile(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runGit(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func captureProcessOutput(t *testing.T, fn func() error) (string, string, error) {
	t.Helper()

	origStdout := os.Stdout
	origStderr := os.Stderr
	stdoutR, stdoutW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	stderrR, stderrW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}

	os.Stdout = stdoutW
	os.Stderr = stderrW

	var stdoutBuf, stderrBuf bytes.Buffer
	outDone := make(chan struct{})
	errDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&stdoutBuf, stdoutR)
		close(outDone)
	}()
	go func() {
		_, _ = io.Copy(&stderrBuf, stderrR)
		close(errDone)
	}()

	runErr := fn()

	_ = stdoutW.Close()
	_ = stderrW.Close()
	os.Stdout = origStdout
	os.Stderr = origStderr
	<-outDone
	<-errDone
	_ = stdoutR.Close()
	_ = stderrR.Close()

	return stdoutBuf.String(), stderrBuf.String(), runErr
}

func withWorkingDir(t *testing.T, dir string, fn func()) {
	t.Helper()
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	}()
	fn()
}

func requireOutputContains(t *testing.T, got, want string) {
	t.Helper()
	if !strings.Contains(got, want) {
		t.Fatalf("output missing %q:\n%s", want, got)
	}
}

func TestPhase2CommandOutputsForSymbolWorkflows(t *testing.T) {
	_, dbPath := newPhase2Repo(t)

	searchResults, missing, err := searchSymbolQueries(dbPath, []string{"Execute", "UserService"}, "", "", true, false, 20, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(missing) != 0 {
		t.Fatalf("unexpected missing search queries: %v", missing)
	}
	found := map[string]bool{}
	for _, result := range searchResults {
		found[result.Name] = true
	}
	for _, want := range []string{"Execute", "UserService"} {
		if !found[want] {
			t.Fatalf("expected search result %q, got %+v", want, searchResults)
		}
	}

	stdout, stderr, err := captureProcessOutput(t, func() error {
		return showSymbol(dbPath, "Execute", 0, false, false, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	requireOutputContains(t, stdout, "symbol: Execute")
	requireOutputContains(t, stdout, "func Execute()")

	stdout, _, err = captureProcessOutput(t, func() error {
		return refsSymbol(dbPath, "helper", 20, 0, false, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: helper")
	requireOutputContains(t, stdout, "main.go")

	traceRows, _, _, err := mergeTrace(dbPath, []string{"Execute"}, 3, 20, []string{"call"})
	if err != nil {
		t.Fatal(err)
	}
	if !traceHasCallee(traceRows, "helper") {
		t.Fatalf("trace Execute missing helper: %+v", traceRows)
	}

	impactRows, _, _, err := mergeImpact(dbPath, []string{"helper"}, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !impactHasCaller(impactRows, "Execute") {
		t.Fatalf("impact helper missing Execute: %+v", impactRows)
	}

	stdout, _, err = captureProcessOutput(t, func() error {
		return runImplsOne(dbPath, "Service", "", false, 20, "java", nil, nil, false, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "implementors (incoming)")
	requireOutputContains(t, stdout, "UserService")

	stdout, _, err = captureProcessOutput(t, func() error {
		return runImplsOne(dbPath, "UserService", "UserService", false, 20, "java", nil, nil, false, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "implements (outgoing)")
	requireOutputContains(t, stdout, "Service")
}

func TestPhase2CommandOutputsForRepoViews(t *testing.T) {
	repo, _ := newPhase2Repo(t)
	withWorkingDir(t, repo, func() {
		stdout, stderr, err := captureProcessOutput(t, func() error {
			return outlineCmd.RunE(outlineCmd, []string{filepath.Join(repo, "main.go")})
		})
		if err != nil {
			t.Fatal(err)
		}
		if stderr != "" {
			t.Fatalf("unexpected outline stderr: %s", stderr)
		}
		requireOutputContains(t, stdout, "symbol_count:")
		requireOutputContains(t, stdout, "function Execute")

		stdout, _, err = captureProcessOutput(t, func() error {
			return contextCmd.RunE(contextCmd, []string{"helper"})
		})
		if err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, stdout, "symbol: helper")
		requireOutputContains(t, stdout, "# Source")
		requireOutputContains(t, stdout, "# Callers")

		stdout, stderr, err = captureProcessOutput(t, func() error {
			return structureCmd.RunE(structureCmd, nil)
		})
		if err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, stderr, repo)
		requireOutputContains(t, stdout, "Most referenced symbols:")

		stdout, _, err = captureProcessOutput(t, func() error {
			return lsTree(lsCmd, []string{repo}, false)
		})
		if err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, stdout, "main.go")
		requireOutputContains(t, stdout, "UserService.java")

		stdout, _, err = captureProcessOutput(t, func() error {
			return lsStats(lsCmd, false)
		})
		if err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, stdout, "repo:")
		requireOutputContains(t, stdout, "symbols:")
		requireOutputContains(t, stdout, "go")
	})
}

func TestPhase2DiffCommandScopesOutputToSymbol(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)
	cleanJSON, cleanErr, err := captureProcessOutput(t, func() error {
		return runDiff(dbPath, "Execute", "HEAD", false, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	if cleanErr != "" {
		t.Fatalf("unexpected clean diff stderr: %s", cleanErr)
	}
	requireOutputContains(t, cleanJSON, `"diff": ""`)
	if _, _, err := captureProcessOutput(t, func() error {
		return runDiff(dbPath, "Execute", "-bad", false, false)
	}); err == nil {
		t.Fatal("expected invalid diff base error")
	}
	if _, _, err := captureProcessOutput(t, func() error {
		return runDiff(dbPath, "MissingSymbol", "HEAD", false, false)
	}); err == nil {
		t.Fatal("expected missing symbol error")
	}

	path := filepath.Join(repo, "main.go")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(src), "func Execute() {\n\thelper()", "func Execute() {\n\tprintln(\"before helper\")\n\thelper()", 1)
	if updated == string(src) {
		t.Fatal("test fixture replacement did not change source")
	}
	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	stdout, stderr, err := captureProcessOutput(t, func() error {
		return runDiff(dbPath, "Execute", "HEAD", false, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("unexpected diff stderr: %s", stderr)
	}
	requireOutputContains(t, stdout, "symbol: Execute")
	requireOutputContains(t, stdout, "+\tprintln(\"before helper\")")

	stdout, _, err = captureProcessOutput(t, func() error {
		return runDiff(dbPath, "Execute", "HEAD", true, true)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"stat"`)
	requireOutputContains(t, stdout, "main.go")
}

func TestPhase3CommandSearchShowInvestigateAndImporters(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)

	stdout, _, err := captureProcessOutput(t, func() error {
		return searchTextGo(dbPath, "lib.Shared", "go", 10, false, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "query: lib.Shared")
	requireOutputContains(t, stdout, "main.go:")
	stdout, _, err = captureProcessOutput(t, func() error {
		return searchText(dbPath, "lib.Shared", "go", 1, true, []string{"main.go"}, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"rel_path": "main.go"`)
	if _, _, err = captureProcessOutput(t, func() error {
		return searchTextGo(dbPath, "definitely-not-present", "", 10, false, nil, nil)
	}); err == nil {
		t.Fatal("expected text search miss to error")
	}
	if langToRgType("typescript") != "ts" || langToRgType("unknown") != "" {
		t.Fatal("unexpected ripgrep language mapping")
	}

	stdout, _, err = captureProcessOutput(t, func() error {
		return showFile(dbPath, filepath.Join(repo, "main.go")+":16-19", 0, false)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "file:")
	requireOutputContains(t, stdout, "func Execute()")
	stdout, _, err = captureProcessOutput(t, func() error {
		return showSymbol(dbPath, "Execute", 1, true, false, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"symbol"`)
	requireOutputContains(t, stdout, `"lines"`)

	stdout, _, err = captureProcessOutput(t, func() error {
		return showMultiJSON(dbPath, []string{"Execute", filepath.Join(repo, "main.go") + ":16-17", "MissingSymbol"}, 0, false, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"version": "0.1"`)
	requireOutputContains(t, stdout, `"Execute"`)
	requireOutputContains(t, stdout, `"MissingSymbol"`)

	if payload, err := buildShowSymbolPayload(dbPath, "Execute", 0, false, nil, nil); err != nil || payload == nil {
		t.Fatalf("buildShowSymbolPayload Execute = %#v, %v", payload, err)
	}
	if payload, err := buildShowFilePayload(dbPath, filepath.Join(repo, "main.go")+":1-2", 0); err != nil || payload == nil {
		t.Fatalf("buildShowFilePayload main.go = %#v, %v", payload, err)
	}
	path, start, end := parseFileTarget("main.go:L16-L19")
	if path != "main.go" || start != 16 || end != 19 {
		t.Fatalf("parseFileTarget = %q %d %d", path, start, end)
	}
	if root := repoRootForPath(filepath.Join(repo, "main.go")); root == "" {
		t.Fatal("repoRootForPath should resolve indexed repo root")
	}

	investigated := investigateOne(dbPath, "execute")
	if investigated["fuzzy"] != true {
		t.Fatalf("expected fuzzy investigate for lowercase execute: %+v", investigated)
	}
	stdout, _, err = captureProcessOutput(t, func() error {
		return investigateOnePrint(dbPath, "Service", false)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "investigate: type")
	requireOutputContains(t, stdout, "# Implementors")

	stdout, _, err = captureProcessOutput(t, func() error {
		return refsImporters(dbPath, "Shared", 1, 20, false, nil, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: Shared")
	requireOutputContains(t, stdout, "main.go")
}

func TestPhase3CommandGraphAndMultiSymbolHelpers(t *testing.T) {
	_, dbPath := newPhase2Repo(t)

	graphCmd := &cobra.Command{Use: "graph-test"}
	addGraphFlags(graphCmd)
	if graphRequested(graphCmd) {
		t.Fatal("graph should be opt-in")
	}
	if err := graphCmd.Flags().Set("graph-format", "dot"); err != nil {
		t.Fatal(err)
	}
	if !graphRequested(graphCmd) {
		t.Fatal("graph-format should request graph output")
	}
	stdout, _, err := captureProcessOutput(t, func() error {
		return renderAsGraph(graphCmd, dbPath, []string{"Execute"}, index.GraphDirectionDown, 2)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "digraph cymbal")
	requireOutputContains(t, stdout, "Execute")

	g1 := &index.GraphResult{
		Nodes: []index.GraphNode{
			{ID: "a", Label: "A"},
			{ID: "b", Label: "B"},
		},
		Edges: []index.GraphEdge{{From: "a", To: "b", Kind: index.GraphEdgeKindCall, Resolved: true}},
	}
	g2 := &index.GraphResult{
		Nodes:      []index.GraphNode{{ID: "b", Label: "B"}, {ID: "c", Label: "C"}},
		Edges:      []index.GraphEdge{{From: "b", To: "c", Kind: index.GraphEdgeKindCall, Resolved: true}},
		Unresolved: []index.GraphUnresolved{{From: "b", Key: "External", ResolvedAs: "ext:External"}},
	}
	merged := mergeGraphResults(g1, nil, g2)
	if len(merged.Nodes) != 3 || len(merged.Edges) != 2 || len(merged.Unresolved) != 1 {
		t.Fatalf("unexpected merged graph: %+v", merged)
	}
	limited := applyGraphLimit(merged, 2, index.GraphFormatJSON, graphRootIDSet("a"))
	if limited.Truncated == 0 || !strings.Contains(renderGraphMermaid(limited), "truncated") {
		t.Fatalf("expected graph truncation sentinel: %+v", limited)
	}

	stdout, _, err = captureProcessOutput(t, func() error {
		multiSymbolBanner("Second", false)
		multiSymbolHeader("Second")
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "═══ Second ═══")

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = stdinR
	_, _ = stdinW.WriteString("Execute\n# comment\nhelper\nExecute\n")
	_ = stdinW.Close()
	defer func() { os.Stdin = origStdin }()

	stdinCmd := &cobra.Command{Use: "stdin-test"}
	addStdinFlag(stdinCmd)
	if err := stdinCmd.Flags().Set("stdin", "true"); err != nil {
		t.Fatal(err)
	}
	names, err := collectSymbols(stdinCmd, []string{"Service"})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(names, ",") != "Service,Execute,helper" {
		t.Fatalf("collectSymbols = %+v", names)
	}
}

func TestPhase3HookInstallAndNudgeInputRegressions(t *testing.T) {
	dir := t.TempDir()
	withWorkingDir(t, dir, func() {
		hookCmd := &cobra.Command{Use: "hook-install-test"}
		hookCmd.Flags().String("scope", "project", "")
		hookCmd.Flags().Bool("dry-run", false, "")
		var out bytes.Buffer
		hookCmd.SetOut(&out)

		if err := runHookInstall(hookCmd, "claude-code", false); err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, out.String(), "cymbal hooks installed")

		settingsPath := filepath.Join(dir, ".claude", "settings.json")
		data, err := os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), claudeHookMarker) || !strings.Contains(string(data), claudeNudgeCmd) {
			t.Fatalf("installed settings missing cymbal hooks:\n%s", string(data))
		}

		out.Reset()
		if err := runHookInstall(hookCmd, "claude-code", true); err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, out.String(), "cymbal hooks removed")

		data, err = os.ReadFile(settingsPath)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(data), claudeHookMarker) {
			t.Fatalf("uninstall should remove cymbal hooks:\n%s", string(data))
		}

		if err := hookCmd.Flags().Set("dry-run", "true"); err != nil {
			t.Fatal(err)
		}
		out.Reset()
		if err := runHookInstall(hookCmd, "claude", false); err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, out.String(), "[dry-run] would update")
	})

	if _, err := lookupHookAdapter("unknown-agent"); err == nil {
		t.Fatal("unknown hook adapter should error")
	}

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	origStdin := os.Stdin
	os.Stdin = stdinR
	_, _ = stdinW.WriteString(`{"tool_name":"Bash","tool_input":{"command":"rg 'func Execute' cmd"}}`)
	_ = stdinW.Close()
	fields, toolName, err := readNudgeInput(nil)
	os.Stdin = origStdin
	if err != nil {
		t.Fatal(err)
	}
	if toolName != "Bash" || strings.Join(fields, " ") != "rg func Execute cmd" {
		t.Fatalf("readNudgeInput JSON = fields=%q tool=%q", fields, toolName)
	}
	if got := shQuoteIfNeeded("has space"); got != "'has space'" {
		t.Fatalf("shQuoteIfNeeded = %q", got)
	}
}

func TestPhase3CommandOutputFiltersUpdateAndVersion(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)

	if file, sym := parseSymbolArg("main.go:Execute"); file != "main.go" || sym != "Execute" {
		t.Fatalf("parseSymbolArg file hint = %q %q", file, sym)
	}
	if file, sym := parseSymbolArg("Worker.Run"); file != "" || sym != "Worker.Run" {
		t.Fatalf("parseSymbolArg dotted arg = %q %q", file, sym)
	}
	if got := parseKindsFlag(" call, use ,,implements "); strings.Join(got, ",") != "call,use,implements" {
		t.Fatalf("parseKindsFlag = %+v", got)
	}

	symbols, _, err := searchSymbolQueries(dbPath, []string{"Execute", "helper"}, "", "", true, false, 20, false, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	filtered := filterByPath(symbols, func(r index.SymbolResult) string { return r.RelPath }, []string{"main.go"}, []string{"*_test.go"})
	if len(filtered) != len(symbols) {
		t.Fatalf("filterByPath should keep main.go symbols: got %d want %d", len(filtered), len(symbols))
	}
	if allowPath("cmd/main.go", []string{"cmd/**"}, []string{"**/*_test.go"}) != true {
		t.Fatal("allowPath should include cmd/main.go")
	}
	if allowPath("cmd/main_test.go", []string{"cmd/**"}, []string{"**/*_test.go"}) != false {
		t.Fatal("allowPath should exclude test file")
	}

	impactRows, _, _, err := mergeImpact(dbPath, []string{"helper"}, 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	enriched := enrichImpact(impactRows, 1)
	if len(enriched) == 0 || len(enriched[0].Context) == 0 {
		t.Fatalf("enrichImpact missing context: %+v", enriched)
	}
	lines, start := readSourceContext(filepath.Join(repo, "main.go"), 1, 2)
	if start != 1 || len(lines) < 2 {
		t.Fatalf("readSourceContext at file start = start %d lines %+v", start, lines)
	}

	resolved, err := flexResolve(dbPath, "execute")
	if err != nil {
		t.Fatal(err)
	}
	if !resolved.Fuzzy || len(resolved.Results) == 0 {
		t.Fatalf("expected fuzzy flexResolve: %+v", resolved)
	}
	meta := renderShowMeta(resolved.Results[0], append(resolved.Results, resolved.Results[0]), true, 0)
	var sawFuzzy bool
	for _, item := range meta {
		if item.k == "fuzzy" && item.v == "true" {
			sawFuzzy = true
		}
	}
	if !sawFuzzy {
		t.Fatalf("renderShowMeta missing fuzzy flag: %+v", meta)
	}

	cacheDir := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", cacheDir)
	updateDir := filepath.Join(cacheDir, "cymbal")
	if err := os.MkdirAll(updateDir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(updateDir, "update-check.json"), []byte(`{
  "schema_version": 1,
  "last_checked_at": "2026-04-24T10:00:00Z",
  "latest_version": "v9.9.9",
  "release_url": "https://example.test/release",
  "update_available": true,
  "install_type": "go"
}`), 0o600); err != nil {
		t.Fatal(err)
	}

	oldInteractive := interactiveTerminalFn
	oldVersion := version
	version = "v0.1.0"
	interactiveTerminalFn = func(*os.File) bool { return true }
	t.Cleanup(func() {
		interactiveTerminalFn = oldInteractive
		version = oldVersion
	})

	noticeCmd := &cobra.Command{Use: "search", Run: func(cmd *cobra.Command, args []string) {}}
	noticeCmd.Flags().Bool("json", false, "")
	noticeCmd.SetErr(io.Discard)
	if err := prepareUpdateNotice(noticeCmd); err != nil {
		t.Fatal(err)
	}
	var errOut bytes.Buffer
	noticeCmd.SetErr(&errOut)
	emitUpdateNotice(noticeCmd)
	requireOutputContains(t, errOut.String(), "A newer cymbal is available: v9.9.9")

	stdout, _, err := captureProcessOutput(t, func() error {
		return versionCmd.RunE(versionCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "cymbal v0.1.0")

	if versionCmd.Flags().Lookup("json") == nil {
		versionCmd.Flags().Bool("json", false, "")
	}
	if err := versionCmd.Flags().Set("json", "true"); err != nil {
		t.Fatal(err)
	}
	stdout, _, err = captureProcessOutput(t, func() error {
		return versionCmd.RunE(versionCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"version": "v0.1.0"`)
	if err := versionCmd.Flags().Set("json", "false"); err != nil {
		t.Fatal(err)
	}

	version = "dev"
	commit = "abcdef123456"
	if got := shortVersion(); got != "dev (abcdef1)" {
		t.Fatalf("shortVersion() = %q", got)
	}
}

func traceHasCallee(rows []index.TraceResult, name string) bool {
	for _, row := range rows {
		if row.Callee == name {
			return true
		}
	}
	return false
}

func impactHasCaller(rows []index.ImpactResult, name string) bool {
	for _, row := range rows {
		if row.Caller == name {
			return true
		}
	}
	return false
}
