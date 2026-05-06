package index

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func createTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	// Go file
	goContent := `package main

import "fmt"

type Server struct {
	Port int
}

func NewServer(port int) *Server {
	return &Server{Port: port}
}

func (s *Server) Start() error {
	fmt.Println("starting")
	return nil
}

func main() {
	s := NewServer(8080)
	s.Start()
}
`
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte(goContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Python file
	pyContent := `class Calculator:
    def __init__(self):
        self.result = 0

    def add(self, a, b):
        return a + b

def main():
    calc = Calculator()
    print(calc.add(1, 2))
`
	if err := os.WriteFile(filepath.Join(dir, "calc.py"), []byte(pyContent), 0644); err != nil {
		t.Fatal(err)
	}

	// JavaScript file
	jsContent := `function greet(name) {
    return "Hello, " + name;
}

class UserService {
    getUser(id) {
        return { id, name: "test" };
    }
}

const helper = (x) => x * 2;
`
	if err := os.WriteFile(filepath.Join(dir, "app.js"), []byte(jsContent), 0644); err != nil {
		t.Fatal(err)
	}

	return dir
}

func TestFeatureIndexBasicSymbolCounts(t *testing.T) {
	dir := createTestRepo(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")

	stats, err := Index(dir, dbPath, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	if stats.FilesIndexed != 3 {
		t.Errorf("expected 3 files indexed, got %d", stats.FilesIndexed)
	}
	if stats.SymbolsFound == 0 {
		t.Error("expected some symbols to be found")
	}
	if stats.Errors != 0 {
		t.Errorf("expected 0 errors, got %d", stats.Errors)
	}

	// Verify we can search for symbols
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	// Should find Server struct
	results, err := store.SearchSymbols("Server", "", "", true, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected to find Server symbol after indexing")
	}

	// Should find Calculator class
	results, err = store.SearchSymbols("Calculator", "", "", true, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) == 0 {
		t.Error("expected to find Calculator symbol after indexing")
	}
}

func TestFeatureIndexIncrementalSkipsUnchanged(t *testing.T) {
	dir := createTestRepo(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// First index
	stats1, err := Index(dir, dbPath, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats1.FilesIndexed == 0 {
		t.Fatal("first index should have indexed files")
	}

	// Second index without changes - should skip all
	stats2, err := Index(dir, dbPath, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.FilesIndexed != 0 {
		t.Errorf("incremental reindex should index 0 files, got %d", stats2.FilesIndexed)
	}
	if stats2.FilesSkipped != stats1.FilesIndexed {
		t.Errorf("expected %d skipped files, got %d", stats1.FilesIndexed, stats2.FilesSkipped)
	}
}

func TestFeatureIndexForceReindex(t *testing.T) {
	dir := createTestRepo(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// First index
	stats1, err := Index(dir, dbPath, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	// Force reindex
	stats2, err := Index(dir, dbPath, Options{Workers: 2, Force: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.FilesIndexed != stats1.FilesIndexed {
		t.Errorf("force reindex should reindex all %d files, got %d", stats1.FilesIndexed, stats2.FilesIndexed)
	}
	if stats2.FilesSkipped != 0 {
		t.Errorf("force reindex should skip 0 files, got %d", stats2.FilesSkipped)
	}
}

func TestFeatureIndexStalePruning(t *testing.T) {
	dir := createTestRepo(t)
	dbPath := filepath.Join(t.TempDir(), "test.db")

	// First index
	_, err := Index(dir, dbPath, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	// Delete a file
	if err := os.Remove(filepath.Join(dir, "calc.py")); err != nil {
		t.Fatal(err)
	}

	// Reindex - should prune the stale file
	stats2, err := Index(dir, dbPath, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}
	if stats2.StaleRemoved != 1 {
		t.Errorf("expected 1 stale file removed, got %d", stats2.StaleRemoved)
	}

	// Verify Calculator is gone
	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	results, err := store.SearchSymbols("Calculator", "", "", true, false, 50)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 0 {
		t.Error("expected Calculator to be removed after file deletion")
	}
}

func TestFeatureIndexGeneratedFilesAreExcludedByDefault(t *testing.T) {
	defer CloseAll()

	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc Indexed() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "service.pb.go"), []byte("package main\nfunc GeneratedOnly() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	stats, err := Index(dir, dbPath, Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesIndexed != 1 {
		t.Fatalf("expected only ordinary source file indexed, got %+v", stats)
	}
	if stats.FilesExcluded != 1 || stats.BytesExcluded == 0 {
		t.Fatalf("expected generated file exclusion to be reported, got %+v", stats)
	}

	found, err := SearchSymbols(dbPath, SearchQuery{Text: "GeneratedOnly", Exact: true, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("generated symbol should not be indexed by default, got %+v", found)
	}

	includeDB := filepath.Join(t.TempDir(), "include.db")
	stats, err = Index(dir, includeDB, Options{Workers: 1, IncludeGenerated: true})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesExcluded != 0 {
		t.Fatalf("include-generated should disable default generated excludes, got %+v", stats)
	}
	found, err = SearchSymbols(includeDB, SearchQuery{Text: "GeneratedOnly", Exact: true, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 {
		t.Fatalf("expected generated symbol with IncludeGenerated, got %+v", found)
	}
}

func TestFeatureIndexCustomExcludesPrunePreviouslyIndexedFiles(t *testing.T) {
	defer CloseAll()

	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")

	if err := os.MkdirAll(filepath.Join(dir, "app"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "main.go"), []byte("package app\nfunc KeepMe() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app", "private.go"), []byte("package app\nfunc DropMe() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Index(dir, dbPath, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	if found, err := SearchSymbols(dbPath, SearchQuery{Text: "DropMe", Exact: true, Limit: 5}); err != nil || len(found) != 1 {
		t.Fatalf("expected DropMe before exclude, got %+v err=%v", found, err)
	}

	stats, err := Index(dir, dbPath, Options{Workers: 1, Exclude: []string{"app/private.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesExcluded != 1 || stats.StaleRemoved != 1 {
		t.Fatalf("expected excluded file to be pruned from existing index, got %+v", stats)
	}
	if found, err := SearchSymbols(dbPath, SearchQuery{Text: "DropMe", Exact: true, Limit: 5}); err != nil || len(found) != 0 {
		t.Fatalf("expected DropMe to be pruned after exclude, got %+v err=%v", found, err)
	}
}

func TestFeatureEnsureFreshHonorsStoredIndexExcludes(t *testing.T) {
	defer CloseAll()

	dir := t.TempDir()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	if err := os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main\nfunc KeepMe() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "skip.go"), []byte("package main\nfunc DropMe() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Index(dir, dbPath, Options{Workers: 1, Exclude: []string{"skip.go"}}); err != nil {
		t.Fatal(err)
	}
	if refreshed := EnsureFresh(dbPath); refreshed != 0 {
		t.Fatalf("expected excluded file to stay excluded during EnsureFresh, refreshed=%d", refreshed)
	}

	found, err := SearchSymbols(dbPath, SearchQuery{Text: "DropMe", Exact: true, Limit: 5})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 0 {
		t.Fatalf("EnsureFresh should not index stored exclude, got %+v", found)
	}
}

func TestFeatureIndexPerRepoIsolation(t *testing.T) {
	dir1 := createTestRepo(t)
	dir2 := t.TempDir()

	// Create different content in dir2
	if err := os.WriteFile(filepath.Join(dir2, "other.go"), []byte(`package other
func UniqueOther() {}
`), 0644); err != nil {
		t.Fatal(err)
	}

	dbPath1 := filepath.Join(t.TempDir(), "repo1.db")
	dbPath2 := filepath.Join(t.TempDir(), "repo2.db")

	_, err := Index(dir1, dbPath1, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	_, err = Index(dir2, dbPath2, Options{Workers: 2})
	if err != nil {
		t.Fatal(err)
	}

	// Verify isolation: repo1 has Server, repo2 doesn't
	store1, err := OpenStore(dbPath1)
	if err != nil {
		t.Fatal(err)
	}
	defer store1.Close()

	store2, err := OpenStore(dbPath2)
	if err != nil {
		t.Fatal(err)
	}
	defer store2.Close()

	r1, _ := store1.SearchSymbols("Server", "", "", true, false, 50)
	if len(r1) == 0 {
		t.Error("repo1 should have Server")
	}

	r2, _ := store2.SearchSymbols("Server", "", "", true, false, 50)
	if len(r2) != 0 {
		t.Error("repo2 should NOT have Server")
	}

	r3, _ := store2.SearchSymbols("UniqueOther", "", "", true, false, 50)
	if len(r3) == 0 {
		t.Error("repo2 should have UniqueOther")
	}

	r4, _ := store1.SearchSymbols("UniqueOther", "", "", true, false, 50)
	if len(r4) != 0 {
		t.Error("repo1 should NOT have UniqueOther")
	}
}

func TestFindGitRootWorktree(t *testing.T) {
	// Simulate a worktree: .git is a file containing "gitdir: <path>".
	dir := t.TempDir()
	worktreeDir := filepath.Join(dir, "worktree")
	if err := os.MkdirAll(worktreeDir, 0755); err != nil {
		t.Fatal(err)
	}
	// Write a .git file (as worktrees have).
	gitFile := filepath.Join(worktreeDir, ".git")
	if err := os.WriteFile(gitFile, []byte("gitdir: /some/path/.git/worktrees/mybranch\n"), 0644); err != nil {
		t.Fatal(err)
	}

	root, err := FindGitRoot(worktreeDir)
	if err != nil {
		t.Fatalf("FindGitRoot should find worktree root, got error: %v", err)
	}
	if root != worktreeDir {
		t.Errorf("expected %s, got %s", worktreeDir, root)
	}

	// Subdirectory of worktree should resolve to worktree root.
	subDir := filepath.Join(worktreeDir, "pkg", "sub")
	if err := os.MkdirAll(subDir, 0755); err != nil {
		t.Fatal(err)
	}
	root, err = FindGitRoot(subDir)
	if err != nil {
		t.Fatalf("FindGitRoot from subdirectory should work, got error: %v", err)
	}
	if root != worktreeDir {
		t.Errorf("expected %s, got %s", worktreeDir, root)
	}
}

func TestFindGitRootRegularRepo(t *testing.T) {
	// Standard repo: .git is a directory.
	dir := t.TempDir()
	gitDir := filepath.Join(dir, ".git")
	if err := os.MkdirAll(gitDir, 0755); err != nil {
		t.Fatal(err)
	}

	root, err := FindGitRoot(dir)
	if err != nil {
		t.Fatalf("FindGitRoot should find regular repo, got error: %v", err)
	}
	if root != dir {
		t.Errorf("expected %s, got %s", dir, root)
	}
}

func TestFindGitRootNoRepo(t *testing.T) {
	dir := t.TempDir()
	_, err := FindGitRoot(dir)
	if err == nil {
		t.Fatal("expected error for directory without .git")
	}
}

func TestFeatureIndexRepoDBPathDeterministic(t *testing.T) {
	path1, err := RepoDBPath("/home/user/myrepo")
	if err != nil {
		t.Fatal(err)
	}
	path2, err := RepoDBPath("/home/user/myrepo")
	if err != nil {
		t.Fatal(err)
	}
	if path1 != path2 {
		t.Errorf("expected same DB path for same repo, got %q and %q", path1, path2)
	}

	// Different repos get different paths
	path3, err := RepoDBPath("/home/user/otherrepo")
	if err != nil {
		t.Fatal(err)
	}
	if path1 == path3 {
		t.Error("expected different DB paths for different repos")
	}
}

func TestFeatureEnsureFreshDetectsOrdinaryFileEdits(t *testing.T) {
	defer CloseAll()

	dir := t.TempDir()
	filePath := filepath.Join(dir, "main.go")
	if err := os.WriteFile(filePath, []byte("package main\nfunc OldName() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	dbPath := filepath.Join(t.TempDir(), "test.db")
	if _, err := Index(dir, dbPath, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	time.Sleep(20 * time.Millisecond)
	if err := os.WriteFile(filePath, []byte("package main\nfunc NewName() {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	if refreshed := EnsureFresh(dbPath); refreshed == 0 {
		t.Fatal("expected EnsureFresh to reindex the edited file")
	}

	store, err := OpenStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()

	gotNew, err := store.SearchSymbols("NewName", "", "", true, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotNew) != 1 {
		t.Fatalf("expected NewName after refresh, got %d matches", len(gotNew))
	}

	gotOld, err := store.SearchSymbols("OldName", "", "", true, false, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(gotOld) != 0 {
		t.Fatalf("expected OldName to be removed after refresh, got %d matches", len(gotOld))
	}
}

func TestFeatureListReposUsesCacheDirOverride(t *testing.T) {
	defer CloseAll()

	cacheDir := t.TempDir()
	t.Setenv("CYMBAL_CACHE_DIR", cacheDir)

	repoDir := createTestRepo(t)
	dbPath, err := RepoDBPath(repoDir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := Index(repoDir, dbPath, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	repos, err := ListRepos()
	if err != nil {
		t.Fatal(err)
	}

	found := false
	for _, repo := range repos {
		if repo.DBPath == dbPath && strings.Contains(repo.DBPath, filepath.Join(cacheDir, "cymbal")) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected ListRepos to return %s from cache override, got %+v", dbPath, repos)
	}
}
