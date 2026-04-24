package cmd

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

func commandWithDB(dbPath string) *cobra.Command {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("db", dbPath, "")
	cmd.Flags().Bool("json", false, "")
	return cmd
}

func setTestFlag(t *testing.T, cmd *cobra.Command, name, value string) {
	t.Helper()
	if err := cmd.Flags().Set(name, value); err != nil {
		t.Fatalf("set flag %s=%s: %v", name, value, err)
	}
}

func newImportersTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().IntP("depth", "D", 1, "")
	cmd.Flags().IntP("limit", "n", 50, "")
	addGraphFlags(cmd)
	return cmd
}

func newImpactTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().IntP("depth", "D", 2, "")
	cmd.Flags().IntP("limit", "n", 50, "")
	cmd.Flags().IntP("context", "C", 1, "")
	addStdinFlag(cmd)
	addGraphFlags(cmd)
	return cmd
}

func newTraceTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().Int("depth", 3, "")
	cmd.Flags().IntP("limit", "n", 50, "")
	cmd.Flags().String("kinds", "call", "")
	addStdinFlag(cmd)
	addGraphFlags(cmd)
	return cmd
}

func newImplsTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().IntP("limit", "n", 50, "")
	cmd.Flags().StringP("lang", "l", "", "")
	cmd.Flags().StringArray("path", nil, "")
	cmd.Flags().StringArray("exclude", nil, "")
	cmd.Flags().String("of", "", "")
	cmd.Flags().Bool("resolved", false, "")
	cmd.Flags().Bool("unresolved", false, "")
	addStdinFlag(cmd)
	addGraphFlags(cmd)
	return cmd
}

func newSearchTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().StringP("kind", "k", "", "")
	cmd.Flags().IntP("limit", "n", 20, "")
	cmd.Flags().StringP("lang", "l", "", "")
	cmd.Flags().BoolP("exact", "e", false, "")
	cmd.Flags().BoolP("ignore-case", "i", false, "")
	cmd.Flags().BoolP("text", "t", false, "")
	cmd.Flags().StringArray("path", nil, "")
	cmd.Flags().StringArray("exclude", nil, "")
	addStdinFlag(cmd)
	return cmd
}

func newRefsTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().Bool("importers", false, "")
	cmd.Flags().Bool("impact", false, "")
	cmd.Flags().IntP("depth", "D", 1, "")
	cmd.Flags().IntP("limit", "n", 20, "")
	cmd.Flags().IntP("context", "C", 1, "")
	cmd.Flags().StringArray("path", nil, "")
	cmd.Flags().StringArray("exclude", nil, "")
	cmd.Flags().String("file", "", "")
	addStdinFlag(cmd)
	return cmd
}

func newShowTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().IntP("context", "C", 0, "")
	cmd.Flags().Bool("all", false, "")
	cmd.Flags().StringArray("path", nil, "")
	cmd.Flags().StringArray("exclude", nil, "")
	addStdinFlag(cmd)
	return cmd
}

func newContextTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().IntP("callers", "n", 20, "")
	return cmd
}

func newOutlineTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().BoolP("signatures", "s", false, "")
	cmd.Flags().Bool("names", false, "")
	return cmd
}

func newLsTestCommand(dbPath string) *cobra.Command {
	cmd := commandWithDB(dbPath)
	cmd.Flags().Bool("repos", false, "")
	cmd.Flags().Bool("stats", false, "")
	cmd.Flags().IntP("depth", "D", 0, "")
	return cmd
}

func TestCodecovCLIIndexRunERegressions(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", t.TempDir())
	writeFile(t, repo, "go.mod", "module example.com/indexrun\n\ngo 1.25\n")
	writeFile(t, repo, "main.go", `package main

func Indexed() {
	helper()
}

func helper() {}
`)

	dbPath := filepath.Join(t.TempDir(), "cymbal.db")
	cmd := commandWithDB(dbPath)
	cmd.Flags().IntP("workers", "w", 0, "")
	cmd.Flags().BoolP("force", "f", false, "")
	setTestFlag(t, cmd, "workers", "1")
	setTestFlag(t, cmd, "force", "true")

	stdout, stderr, err := captureProcessOutput(t, func() error {
		return indexCmd.RunE(cmd, []string{repo})
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Fatalf("unexpected stdout: %s", stdout)
	}
	requireOutputContains(t, stderr, "Indexing ")
	requireOutputContains(t, stderr, "Done in")

	results, err := index.SearchSymbols(dbPath, index.SearchQuery{Text: "Indexed", Exact: true, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Name != "Indexed" {
		t.Fatalf("indexed command did not write expected symbol: %+v", results)
	}

	if _, _, err := captureProcessOutput(t, func() error {
		return indexCmd.RunE(cmd, []string{filepath.Join(repo, "missing")})
	}); err == nil {
		t.Fatal("expected missing path error")
	}

	envDB := filepath.Join(t.TempDir(), "env-index.db")
	t.Setenv("CYMBAL_DB", envDB)
	envCmd := commandWithDB("")
	envCmd.Flags().IntP("workers", "w", 0, "")
	envCmd.Flags().BoolP("force", "f", false, "")
	setTestFlag(t, envCmd, "workers", "1")
	if _, _, err := captureProcessOutput(t, func() error {
		return indexCmd.RunE(envCmd, []string{repo})
	}); err != nil {
		t.Fatal(err)
	}
	if results, err := index.SearchSymbols(envDB, index.SearchQuery{Text: "Indexed", Exact: true, Limit: 5}); err != nil || len(results) == 0 {
		t.Fatalf("index command did not honor CYMBAL_DB: results=%+v err=%v", results, err)
	}
}

func TestCodecovCLIImportersRunEModes(t *testing.T) {
	_, dbPath := newPhase2Repo(t)

	stdout, _, err := captureProcessOutput(t, func() error {
		return importersCmd.RunE(newImportersTestCommand(dbPath), []string{"example.com/cymbaltest/lib"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "target: example.com/cymbaltest/lib")
	requireOutputContains(t, stdout, "main.go")

	jsonCmd := newImportersTestCommand(dbPath)
	setTestFlag(t, jsonCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return importersCmd.RunE(jsonCmd, []string{"example.com/cymbaltest/lib"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"rel_path": "main.go"`)

	graphCmd := newImportersTestCommand(dbPath)
	setTestFlag(t, graphCmd, "graph-format", "dot")
	stdout, _, err = captureProcessOutput(t, func() error {
		return importersCmd.RunE(graphCmd, []string{"example.com/cymbaltest/lib"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "digraph cymbal")
	requireOutputContains(t, stdout, "main.go")

	if _, _, err = captureProcessOutput(t, func() error {
		return importersCmd.RunE(newImportersTestCommand(dbPath), []string{"example.com/missing"})
	}); err == nil || !strings.Contains(err.Error(), "no importers found") {
		t.Fatalf("expected no importers error, got %v", err)
	}
}

func TestCodecovCLIImpactRunEModes(t *testing.T) {
	_, dbPath := newPhase2Repo(t)

	cmd := newImpactTestCommand(dbPath)
	setTestFlag(t, cmd, "context", "0")
	stdout, _, err := captureProcessOutput(t, func() error {
		return impactCmd.RunE(cmd, []string{"helper"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: helper")
	requireOutputContains(t, stdout, "main.go")

	jsonCmd := newImpactTestCommand(dbPath)
	setTestFlag(t, jsonCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return impactCmd.RunE(jsonCmd, []string{"helper", "Shared"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"symbols":`)
	requireOutputContains(t, stdout, `"hit_symbols":`)

	graphCmd := newImpactTestCommand(dbPath)
	setTestFlag(t, graphCmd, "graph-format", "json")
	stdout, _, err = captureProcessOutput(t, func() error {
		return impactCmd.RunE(graphCmd, []string{"helper"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"nodes":`)

	if _, _, err = captureProcessOutput(t, func() error {
		return impactCmd.RunE(newImpactTestCommand(dbPath), []string{"MissingSymbol"})
	}); err == nil || !strings.Contains(err.Error(), "no callers found") {
		t.Fatalf("expected no callers error, got %v", err)
	}
}

func TestCodecovCLITraceRunEModes(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)

	cmd := newTraceTestCommand(dbPath)
	stdout, _, err := captureProcessOutput(t, func() error {
		return traceCmd.RunE(cmd, []string{"main.go:Execute"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: Execute")
	requireOutputContains(t, stdout, "Execute")
	requireOutputContains(t, stdout, "helper")

	jsonCmd := newTraceTestCommand(dbPath)
	setTestFlag(t, jsonCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return traceCmd.RunE(jsonCmd, []string{"Execute", "Worker.Run"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"direction": "downward (callees)"`)
	requireOutputContains(t, stdout, `"hit_symbols":`)

	graphCmd := newTraceTestCommand(dbPath)
	setTestFlag(t, graphCmd, "graph-format", "json")
	stdout, _, err = captureProcessOutput(t, func() error {
		return traceCmd.RunE(graphCmd, []string{"Execute"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"edges":`)

	stdout, _, err = captureProcessOutput(t, func() error {
		return traceCmd.RunE(newTraceTestCommand(dbPath), []string{"Shared"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "No outgoing calls found for 'Shared'.")

	if file, sym := parseSymbolArg(filepath.Join(repo, "main.go") + ":Execute"); file == "" || sym != "Execute" {
		t.Fatalf("parseSymbolArg should keep file hint and symbol: %q %q", file, sym)
	}
}

func TestCodecovCLIImplsRunEModes(t *testing.T) {
	_, dbPath := newPhase2Repo(t)

	multiCmd := newImplsTestCommand(dbPath)
	stdout, stderr, err := captureProcessOutput(t, func() error {
		return implsCmd.RunE(multiCmd, []string{"Service", "MissingInterface"})
	})
	if err != nil {
		t.Fatal(err)
	}
	if stderr != "" {
		t.Fatalf("unexpected stderr: %s", stderr)
	}
	requireOutputContains(t, stdout, "implementors (incoming)")
	requireOutputContains(t, stdout, "UserService")
	requireOutputContains(t, stdout, "No implementors found for 'MissingInterface'.")

	jsonCmd := newImplsTestCommand(dbPath)
	setTestFlag(t, jsonCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return implsCmd.RunE(jsonCmd, []string{"Service", "MissingInterface"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"Service":`)
	requireOutputContains(t, stdout, `"MissingInterface":`)

	ofCmd := newImplsTestCommand(dbPath)
	setTestFlag(t, ofCmd, "of", "UserService")
	stdout, _, err = captureProcessOutput(t, func() error {
		return implsCmd.RunE(ofCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "implements (outgoing)")
	requireOutputContains(t, stdout, "Service")

	graphCmd := newImplsTestCommand(dbPath)
	setTestFlag(t, graphCmd, "of", "UserService")
	setTestFlag(t, graphCmd, "graph-format", "json")
	stdout, _, err = captureProcessOutput(t, func() error {
		return implsCmd.RunE(graphCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"nodes":`)
	requireOutputContains(t, stdout, "UserService")

	conflictCmd := newImplsTestCommand(dbPath)
	setTestFlag(t, conflictCmd, "of", "UserService")
	if _, _, err = captureProcessOutput(t, func() error {
		return implsCmd.RunE(conflictCmd, []string{"Service"})
	}); err == nil || !strings.Contains(err.Error(), "pass either positional symbols") {
		t.Fatalf("expected --of conflict error, got %v", err)
	}
}

func TestCodecovCLISearchRefsShowContextRunEModes(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)

	searchCmdLocal := newSearchTestCommand(dbPath)
	setTestFlag(t, searchCmdLocal, "exact", "true")
	stdout, stderr, err := captureProcessOutput(t, func() error {
		return searchCmd.RunE(searchCmdLocal, []string{"Execute", "MissingSymbol"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "query: Execute MissingSymbol")
	requireOutputContains(t, stdout, "function Execute")
	requireOutputContains(t, stderr, "MissingSymbol: no results found")

	withWorkingDir(t, repo, func() {
		textCmd := newSearchTestCommand(dbPath)
		setTestFlag(t, textCmd, "text", "true")
		setTestFlag(t, textCmd, "json", "true")
		stdout, _, err = captureProcessOutput(t, func() error {
			return searchCmd.RunE(textCmd, []string{"lib.Shared", "main.go"})
		})
		if err != nil {
			t.Fatal(err)
		}
		requireOutputContains(t, stdout, `"rel_path": "main.go"`)
	})

	refsCmdLocal := newRefsTestCommand(dbPath)
	stdout, _, err = captureProcessOutput(t, func() error {
		return refsCmd.RunE(refsCmdLocal, []string{"helper", "MissingSymbol"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: helper")

	importersCmdLocal := newRefsTestCommand(dbPath)
	setTestFlag(t, importersCmdLocal, "importers", "true")
	setTestFlag(t, importersCmdLocal, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return refsCmd.RunE(importersCmdLocal, []string{"Shared"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"rel_path": "main.go"`)

	showCmdLocal := newShowTestCommand(dbPath)
	stdout, stderr, err = captureProcessOutput(t, func() error {
		return showCmd.RunE(showCmdLocal, []string{"Execute", filepath.Join(repo, "main.go") + ":16-18", "MissingSymbol"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "═══ Execute ═══")
	requireOutputContains(t, stdout, "func Execute()")
	requireOutputContains(t, stdout, "file:")
	requireOutputContains(t, stderr, "MissingSymbol:")

	showJSONCmd := newShowTestCommand(dbPath)
	setTestFlag(t, showJSONCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return showCmd.RunE(showJSONCmd, []string{"Execute", filepath.Join(repo, "main.go") + ":1-3"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"Execute":`)
	requireOutputContains(t, stdout, `"lines":`)

	contextCmdLocal := newContextTestCommand(dbPath)
	stdout, _, err = captureProcessOutput(t, func() error {
		return contextCmd.RunE(contextCmdLocal, []string{"Service", "Execute"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: Service")
	requireOutputContains(t, stdout, "# Implementors")
	requireOutputContains(t, stdout, "symbol: Execute")

	contextJSONCmd := newContextTestCommand(dbPath)
	setTestFlag(t, contextJSONCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return contextCmd.RunE(contextJSONCmd, []string{"Execute"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"symbol"`)
}

func TestCodecovCLIOutlineLsInvestigateRunEModes(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)

	outlineNamesCmd := newOutlineTestCommand(dbPath)
	setTestFlag(t, outlineNamesCmd, "names", "true")
	stdout, _, err := captureProcessOutput(t, func() error {
		return outlineCmd.RunE(outlineNamesCmd, []string{filepath.Join(repo, "main.go")})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "Execute")

	outlineJSONCmd := newOutlineTestCommand(dbPath)
	setTestFlag(t, outlineJSONCmd, "json", "true")
	setTestFlag(t, outlineJSONCmd, "signatures", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return outlineCmd.RunE(outlineJSONCmd, []string{filepath.Join(repo, "main.go")})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"name": "Execute"`)

	emptyFile := filepath.Join(repo, "empty.go")
	writeFile(t, repo, "empty.go", "package main\n")
	stdout, stderr, err := captureProcessOutput(t, func() error {
		return outlineCmd.RunE(newOutlineTestCommand(dbPath), []string{emptyFile})
	})
	if err != nil {
		t.Fatal(err)
	}
	if stdout != "" {
		t.Fatalf("unexpected outline stdout: %s", stdout)
	}
	requireOutputContains(t, stderr, "No symbols found")

	lsTreeCmd := newLsTestCommand(dbPath)
	setTestFlag(t, lsTreeCmd, "depth", "1")
	stdout, _, err = captureProcessOutput(t, func() error {
		return lsCmd.RunE(lsTreeCmd, []string{repo})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "main.go")

	lsStatsCmd := newLsTestCommand(dbPath)
	setTestFlag(t, lsStatsCmd, "stats", "true")
	setTestFlag(t, lsStatsCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return lsCmd.RunE(lsStatsCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"symbol_count"`)

	lsReposCmd := newLsTestCommand(dbPath)
	setTestFlag(t, lsReposCmd, "repos", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return lsCmd.RunE(lsReposCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, repo)

	lsReposJSONCmd := newLsTestCommand(dbPath)
	setTestFlag(t, lsReposJSONCmd, "repos", "true")
	setTestFlag(t, lsReposJSONCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return lsCmd.RunE(lsReposJSONCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"path":`)

	investigateJSONCmd := commandWithDB(dbPath)
	setTestFlag(t, investigateJSONCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return investigateCmd.RunE(investigateJSONCmd, []string{"Execute", "MissingSymbol"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"result"`)
	requireOutputContains(t, stdout, `"error": "not found"`)

	stdout, stderr, err = captureProcessOutput(t, func() error {
		return investigateCmd.RunE(commandWithDB(dbPath), []string{"Service", "MissingSymbol"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "investigate: type")
	requireOutputContains(t, stderr, "MissingSymbol:")
}

func TestCodecovCLIRootDiffVersionUpdateRegressions(t *testing.T) {
	repo, dbPath := newPhase2Repo(t)

	flagCmd := commandWithDB(dbPath)
	if got := getDBPath(flagCmd); got != dbPath {
		t.Fatalf("getDBPath flag = %q, want %q", got, dbPath)
	}

	envDB := filepath.Join(t.TempDir(), "env.db")
	t.Setenv("CYMBAL_DB", envDB)
	if got := getDBPath(commandWithDB("")); got != envDB {
		t.Fatalf("getDBPath env = %q, want %q", got, envDB)
	}
	t.Setenv("CYMBAL_DB", "")
	withWorkingDir(t, repo, func() {
		if got := getDBPath(commandWithDB("")); got == "" || !strings.HasSuffix(got, "index.db") {
			t.Fatalf("getDBPath git repo returned %q", got)
		}
	})
	withWorkingDir(t, t.TempDir(), func() {
		_, stderr, _ := captureProcessOutput(t, func() error {
			if got := getDBPath(commandWithDB("")); got == "" {
				t.Fatal("fallback DB path should not be empty")
			}
			return nil
		})
		requireOutputContains(t, stderr, "not inside a git repository")
	})
	if got := fallbackDBPath(); got == "" || !strings.HasSuffix(got, "index.db") {
		t.Fatalf("fallbackDBPath returned %q", got)
	}

	mainPath := filepath.Join(repo, "main.go")
	src, err := os.ReadFile(mainPath)
	if err != nil {
		t.Fatal(err)
	}
	updated := strings.Replace(string(src), "func Execute() {\n\thelper()", "func Execute() {\n\tprintln(\"diff stat\")\n\thelper()", 1)
	if updated == string(src) {
		t.Fatal("test fixture replacement did not change source")
	}
	if err := os.WriteFile(mainPath, []byte(updated), 0o644); err != nil {
		t.Fatal(err)
	}

	diffCmdLocal := commandWithDB(dbPath)
	diffCmdLocal.Flags().Bool("stat", false, "")
	setTestFlag(t, diffCmdLocal, "stat", "true")
	setTestFlag(t, diffCmdLocal, "json", "true")
	stdout, _, err := captureProcessOutput(t, func() error {
		return diffCmd.RunE(diffCmdLocal, []string{"Execute", "HEAD"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"stat"`)
	requireOutputContains(t, stdout, "main.go")

	humanDiffCmd := commandWithDB(dbPath)
	humanDiffCmd.Flags().Bool("stat", false, "")
	stdout, _, err = captureProcessOutput(t, func() error {
		return diffCmd.RunE(humanDiffCmd, []string{"Execute", "HEAD"})
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "symbol: Execute")
	requireOutputContains(t, stdout, "+\tprintln(\"diff stat\")")

	oldVersion, oldCommit, oldDate := version, commit, date
	version, commit, date = "dev", "abcdef1234567890", "2026-04-24T12:00:00Z"
	t.Cleanup(func() {
		version, commit, date = oldVersion, oldCommit, oldDate
	})

	stdout, _, err = captureProcessOutput(t, func() error {
		return versionCmd.RunE(commandWithDB(""), nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "cymbal dev")
	requireOutputContains(t, stdout, "commit: abcdef1234567890")
	requireOutputContains(t, stdout, "built:  2026-04-24T12:00:00Z")

	rootCmd.SetArgs([]string{"version"})
	t.Cleanup(func() { rootCmd.SetArgs(nil) })
	stdout, _, err = captureProcessOutput(t, Execute)
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, "cymbal dev")

	versionJSONCmd := commandWithDB("")
	setTestFlag(t, versionJSONCmd, "json", "true")
	stdout, _, err = captureProcessOutput(t, func() error {
		return versionCmd.RunE(versionJSONCmd, nil)
	})
	if err != nil {
		t.Fatal(err)
	}
	requireOutputContains(t, stdout, `"commit": "abcdef1234567890"`)

	tempFile, err := os.CreateTemp(t.TempDir(), "stderr-*")
	if err != nil {
		t.Fatal(err)
	}
	defer tempFile.Close()
	if isInteractiveTerminal(tempFile) {
		t.Fatal("regular temp file should not be an interactive terminal")
	}

	if !shouldSkipPassiveUpdateNotice(nil) {
		t.Fatal("nil command should skip passive update notice")
	}

	oldInteractive := interactiveTerminalFn
	interactiveTerminalFn = func(*os.File) bool { return true }
	t.Cleanup(func() { interactiveTerminalFn = oldInteractive })

	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "1")
	if !shouldSkipPassiveUpdateNotice(&cobra.Command{Use: "search", Run: func(cmd *cobra.Command, args []string) {}}) {
		t.Fatal("disabled notifier should skip passive update notice")
	}
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "")
	if !shouldSkipPassiveUpdateNotice(&cobra.Command{Use: "version", Run: func(cmd *cobra.Command, args []string) {}}) {
		t.Fatal("version command should skip passive update notice")
	}
	if !shouldSkipPassiveUpdateNotice(&cobra.Command{Use: "group"}) {
		t.Fatal("non-runnable command should skip passive update notice")
	}
	t.Setenv("CI", "true")
	if !shouldSkipPassiveUpdateNotice(&cobra.Command{Use: "search", Run: func(cmd *cobra.Command, args []string) {}}) {
		t.Fatal("CI command should skip passive update notice")
	}
	t.Setenv("CI", "")
	jsonNoticeCmd := commandWithDB("")
	jsonNoticeCmd.Use = "search"
	jsonNoticeCmd.Run = func(cmd *cobra.Command, args []string) {}
	setTestFlag(t, jsonNoticeCmd, "json", "true")
	if !shouldSkipPassiveUpdateNotice(jsonNoticeCmd) {
		t.Fatal("json command should skip passive update notice")
	}
}
