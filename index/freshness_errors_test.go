package index

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOpenStorePreservesExistingParentPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix permission bits")
	}
	parent := t.TempDir()
	if err := os.Chmod(parent, 0755); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(parent, "custom.db")
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for path, mode := range map[string]os.FileMode{parent: 0755, db: 0600} {
		info, err := os.Stat(path)
		if err != nil || info.Mode().Perm() != mode {
			t.Fatalf("%s: info=%v error=%v want=%o", path, info, err, mode)
		}
	}
	private := filepath.Join(parent, "private", "cache")
	store, err = OpenStore(filepath.Join(private, "index.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(private)
	if err != nil || info.Mode().Perm() != 0700 {
		t.Fatalf("private directory: %v %v", info, err)
	}
}

func freshnessFixture(t *testing.T) (string, string) {
	t.Helper()
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package fixture\nfunc Original() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Index(repo, db, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	return repo, db
}

func TestEnsureFreshReportsFailureWithoutPruning(t *testing.T) {
	repo, db := freshnessFixture(t)
	if count, err := EnsureFreshWithError(db); err != nil || count != 0 {
		t.Fatalf("unchanged: count=%d err=%v", count, err)
	}
	if err := os.Rename(repo, repo+"-moved"); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(repo + "-moved") })
	if _, err := EnsureFreshWithError(db); err == nil {
		t.Fatal("missing repository must fail refresh")
	}
	if count := EnsureFresh(db); count != 0 {
		t.Fatalf("legacy API count=%d", count)
	}
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	files, err := store.AllFiles("")
	if err != nil || len(files) != 1 {
		t.Fatalf("failed discovery pruned the index: files=%v error=%v", files, err)
	}
}

func TestEnsureFreshReportsWriteFailure(t *testing.T) {
	repo, db := freshnessFixture(t)
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	_, err = store.db.Exec(`CREATE TRIGGER fail_symbols BEFORE INSERT ON symbols BEGIN SELECT RAISE(FAIL, 'test write failure'); END`)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte("package fixture\nfunc ChangedName() {}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureFreshWithError(db); err == nil || !strings.Contains(err.Error(), "1 write errors") {
		t.Fatalf("error=%v", err)
	}
}

func TestEnsureFreshRejectsCorruptMetadata(t *testing.T) {
	_, db := freshnessFixture(t)
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.SetMeta(metaIndexExclude, "["); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureFreshWithError(db); err == nil || !strings.Contains(err.Error(), "reading index exclusions") {
		t.Fatalf("error=%v", err)
	}
}

func TestTraceIncludesShortCalls(t *testing.T) {
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	source := "package fixture\nfunc Do() {}\nfunc Longer() {}\nfunc Caller() { Do(); Longer(); var x int; _ = x }\n"
	if err := os.WriteFile(filepath.Join(repo, "main.go"), []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Index(repo, db, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseAll)
	rows, err := FindTrace(db, "Caller", 1, 20)
	if err != nil {
		t.Fatal(err)
	}
	if !traceContainsCallee(rows, "Do") || !traceContainsCallee(rows, "Longer") || len(rows) != 2 {
		t.Fatalf("trace=%+v", rows)
	}
	graph, err := BuildGraph(db, GraphQuery{Symbol: "Caller", Direction: GraphDirectionDown, Depth: 1})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, node := range graph.Nodes {
		if node.Symbol == "Do" {
			found = true
		}
	}
	if !found {
		t.Fatalf("graph missing Do: %+v", graph)
	}
}
