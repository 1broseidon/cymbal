package index

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/1broseidon/cymbal/symbols"
)

func newPhase3Repo(t *testing.T) (string, string) {
	t.Helper()
	t.Setenv("CYMBAL_CACHE_DIR", t.TempDir())
	repo := t.TempDir()
	writePhase3File(t, repo, "go.mod", "module example.com/phase3\n\ngo 1.25\n")
	writePhase3File(t, repo, "main.go", `package main

import "example.com/phase3/lib"

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
	writePhase3File(t, repo, filepath.Join("lib", "lib.go"), `package lib

func Shared() {}
`)
	writePhase3File(t, repo, filepath.Join("java", "UserService.java"), `package example;

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
	runPhase3Git(t, repo, "init")
	runPhase3Git(t, repo, "add", ".")
	runPhase3Git(t, repo, "-c", "user.name=Cymbal Test", "-c", "user.email=cymbal@example.invalid", "commit", "-m", "initial")

	stats, err := Index(repo, "", Options{Workers: 1, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesIndexed == 0 || stats.SymbolsFound == 0 {
		t.Fatalf("index stats should show work: %+v", stats)
	}
	dbPath, err := RepoDBPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseAll)
	return repo, dbPath
}

func writePhase3File(t *testing.T, root, rel, content string) {
	t.Helper()
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func runPhase3Git(t *testing.T, repo string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, string(out))
	}
}

func TestPhase3IndexFacadeQueries(t *testing.T) {
	repo, dbPath := newPhase3Repo(t)

	stats, err := RepoStats(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Path != repo || stats.SymbolCount == 0 || stats.Languages["go"] == 0 {
		t.Fatalf("unexpected repo stats: %+v", stats)
	}
	structure, err := Structure(dbPath, 10)
	if err != nil {
		t.Fatal(err)
	}
	if structure.RepoRoot != repo || structure.Symbols == 0 {
		t.Fatalf("Structure() = %+v", structure)
	}

	repos, err := ListRepos()
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 1 || repos[0].Path != repo {
		t.Fatalf("ListRepos() = %+v, want repo %s", repos, repo)
	}

	outline, err := FileOutline(dbPath, filepath.Join(repo, "main.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !symbolsContain(outline, "Execute") || !symbolsContain(outline, "Worker") {
		t.Fatalf("outline missing Execute or Worker: %+v", outline)
	}

	search, err := SearchSymbols(dbPath, SearchQuery{Text: "Execute", Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(search) != 1 || search[0].Name != "Execute" {
		t.Fatalf("SearchSymbols Execute = %+v", search)
	}

	flex, err := SearchSymbolsFlex(dbPath, "execute", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !symbolsContain(flex, "Execute") {
		t.Fatalf("SearchSymbolsFlex execute missing Execute: %+v", flex)
	}

	text, err := TextSearch(dbPath, "lib.Shared", "go", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(text) == 0 || text[0].RelPath != "main.go" {
		t.Fatalf("TextSearch lib.Shared = %+v", text)
	}
	importers, err := FindImporters(dbPath, "Shared", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(importers) == 0 || importers[0].RelPath != "main.go" {
		t.Fatalf("FindImporters Shared = %+v", importers)
	}
	importersByPath, err := FindImportersByPath(dbPath, "example.com/phase3/lib", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(importersByPath) == 0 || importersByPath[0].RelPath != "main.go" {
		t.Fatalf("FindImportersByPath lib = %+v", importersByPath)
	}

	refs, err := FindReferences(dbPath, "helper", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) < 2 {
		t.Fatalf("FindReferences helper = %+v", refs)
	}

	impact, err := FindImpact(dbPath, "helper", 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !impactContainsCaller(impact, "Execute") {
		t.Fatalf("FindImpact helper missing Execute: %+v", impact)
	}

	trace, err := FindTrace(dbPath, "Execute", 2, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !traceContainsCallee(trace, "helper") {
		t.Fatalf("FindTrace Execute missing helper: %+v", trace)
	}

	implementors, err := FindImplementors(dbPath, "Service", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !implementorsContain(implementors, "UserService", "Service") {
		t.Fatalf("FindImplementors Service = %+v", implementors)
	}

	implements, err := FindImplements(dbPath, "UserService", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !implementorsContain(implements, "UserService", "Service") {
		t.Fatalf("FindImplements UserService = %+v", implements)
	}

	byName, err := SymbolsByName(dbPath, "Execute")
	if err != nil {
		t.Fatal(err)
	}
	if len(byName) != 1 {
		t.Fatalf("SymbolsByName Execute = %+v", byName)
	}
	if root := RepoRootFromDB(dbPath); root != repo {
		t.Fatalf("RepoRootFromDB() = %q, want %q", root, repo)
	}
}

func TestPhase3ContextInvestigateAndGraphFacades(t *testing.T) {
	_, dbPath := newPhase3Repo(t)

	ctx, err := SymbolContext(dbPath, "helper", 20)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(ctx.Source, "func helper") || len(ctx.Callers) == 0 {
		t.Fatalf("SymbolContext helper = %+v", ctx)
	}

	investigateFn, err := Investigate(dbPath, "Execute")
	if err != nil {
		t.Fatal(err)
	}
	if investigateFn.Kind != "function" || !strings.Contains(investigateFn.Source, "func Execute") {
		t.Fatalf("Investigate Execute = %+v", investigateFn)
	}

	investigateType, err := Investigate(dbPath, "Service")
	if err != nil {
		t.Fatal(err)
	}
	if investigateType.Kind != "type" || len(investigateType.Implementors) == 0 {
		t.Fatalf("Investigate Service = %+v", investigateType)
	}

	if _, err := Investigate(dbPath, "MissingSymbol"); err == nil {
		t.Fatal("expected missing symbol error")
	}

	resolved, err := InvestigateResolved(dbPath, investigateType.Symbol)
	if err != nil {
		t.Fatal(err)
	}
	if resolved.Kind != "type" || len(resolved.Implementors) == 0 {
		t.Fatalf("InvestigateResolved Service = %+v", resolved)
	}

	graph, err := BuildGraph(dbPath, GraphQuery{Symbol: "Execute", Direction: GraphDirectionDown, Depth: 2, Limit: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(graph.Nodes) == 0 || len(graph.Edges) == 0 {
		t.Fatalf("BuildGraph Execute = %+v", graph)
	}
	if GraphNodeIDFor(investigateFn.Symbol.Name) == "" {
		t.Fatal("GraphNodeIDFor should return a stable non-empty id")
	}
}

func TestPhase3EnsureFreshAutoDetectsGitRepo(t *testing.T) {
	repo := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", t.TempDir())
	writePhase3File(t, repo, "go.mod", "module example.com/autofresh\n\ngo 1.25\n")
	writePhase3File(t, repo, "main.go", "package main\n\nfunc AutoFresh() {}\n")
	runPhase3Git(t, repo, "init")
	runPhase3Git(t, repo, "add", ".")
	runPhase3Git(t, repo, "-c", "user.name=Cymbal Test", "-c", "user.email=cymbal@example.invalid", "commit", "-m", "initial")

	dbPath := filepath.Join(t.TempDir(), "auto.db")
	old, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(repo); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if err := os.Chdir(old); err != nil {
			t.Errorf("restore cwd: %v", err)
		}
	}()

	if refreshed := EnsureFresh(dbPath); refreshed == 0 {
		t.Fatal("EnsureFresh should auto-index an uninitialized db inside a git repo")
	}
	results, err := SearchSymbols(dbPath, SearchQuery{Text: "AutoFresh", Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("AutoFresh search = %+v", results)
	}
}

func TestPhase3RankSymbolsOrdersExactAndExportedMatches(t *testing.T) {
	results := []SymbolResult{
		{Name: "Execute", Kind: "method", RelPath: "internal/run_test.go"},
		{Name: "Execute", Kind: "class", RelPath: "src/docs/run.go"},
		{Name: "Execute", Kind: "function", RelPath: "cmd/run.go"},
	}
	RankSymbols(results)
	if results[0].Name != "Execute" || results[0].Kind != "function" || results[0].RelPath != "cmd/run.go" {
		t.Fatalf("RankSymbols did not prefer product function over test/doc matches: %+v", results)
	}
	if got := rankFetchWindow(0, false); got != 500 {
		t.Fatalf("rankFetchWindow(0, false) = %d, want 500", got)
	}
	if got := rankFetchWindow(20, true); got != 0 {
		t.Fatalf("rankFetchWindow(20, true) = %d, want 0", got)
	}
}

func TestPhase3StoreInsertFileAllAndHashHelpers(t *testing.T) {
	store, dbPath := newTestStore(t)
	filePath := filepath.Join(t.TempDir(), "manual.go")
	content := []byte("package manual\n\nfunc Manual() { helper() }\n")
	if err := os.WriteFile(filePath, content, 0o644); err != nil {
		t.Fatal(err)
	}
	hash, err := HashFile(filePath)
	if err != nil {
		t.Fatal(err)
	}
	if hash != HashBytes(content) {
		t.Fatalf("HashFile = %q, want HashBytes", hash)
	}
	if got, err := store.FileHash(filePath); err != nil || got != "" {
		t.Fatalf("FileHash before insert = %q, %v", got, err)
	}

	err = store.InsertFileAll(
		filePath,
		"manual.go",
		"go",
		hash,
		time.Now(),
		int64(len(content)),
		[]symbols.Symbol{{Name: "Manual", Kind: "function", File: filePath, StartLine: 3, EndLine: 3, Language: "go"}},
		[]symbols.Import{{RawPath: "fmt", Language: "go"}},
		[]symbols.Ref{{Name: "helper", Line: 3, Language: "go", Kind: symbols.RefKindCall}},
	)
	if err != nil {
		t.Fatal(err)
	}
	if got, err := store.FileHash(filePath); err != nil || got != hash {
		t.Fatalf("FileHash after insert = %q, %v", got, err)
	}
	results, err := SearchSymbols(dbPath, SearchQuery{Text: "Manual", Exact: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 {
		t.Fatalf("manual symbol search = %+v", results)
	}
	refs, err := FindReferences(dbPath, "helper", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(refs) != 1 || refs[0].RelPath != "manual.go" {
		t.Fatalf("manual refs = %+v", refs)
	}
}

func symbolsContain(results []SymbolResult, name string) bool {
	for _, result := range results {
		if result.Name == name {
			return true
		}
	}
	return false
}

func impactContainsCaller(results []ImpactResult, caller string) bool {
	for _, result := range results {
		if result.Caller == caller {
			return true
		}
	}
	return false
}

func traceContainsCallee(results []TraceResult, callee string) bool {
	for _, result := range results {
		if result.Callee == callee {
			return true
		}
	}
	return false
}

func implementorsContain(results []ImplementorResult, implementer, target string) bool {
	for _, result := range results {
		if result.Implementer == implementer && result.Target == target {
			return true
		}
	}
	return false
}
