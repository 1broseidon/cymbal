package index

import (
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestIndexBodyHashAcrossEdits(t *testing.T) {
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	// Registered after TempDir so it runs first: Windows cannot remove an open index.db.
	t.Cleanup(CloseAll)
	path := filepath.Join(repo, "main.go")
	mtime := time.Now().Add(-time.Hour)
	// write reindexes after each edit; a distinct mtime guarantees the edit is seen.
	write := func(src string) map[string]string {
		t.Helper()
		if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
			t.Fatal(err)
		}
		mtime = mtime.Add(time.Second)
		if err := os.Chtimes(path, mtime, mtime); err != nil {
			t.Fatal(err)
		}
		if _, err := Index(repo, db, Options{Workers: 1}); err != nil {
			t.Fatal(err)
		}
		syms, err := FileOutline(db, path)
		if err != nil {
			t.Fatal(err)
		}
		out := map[string]string{}
		for _, s := range syms {
			out[s.Name] = s.BodyHash
		}
		return out
	}

	orig := "package p\n\nfunc Foo() int {\n\treturn 1\n}\n\nfunc Bar() int {\n\treturn 2\n}\n"
	base := write(orig)
	if base["Foo"] == "" || base["Bar"] == "" {
		t.Fatalf("hashes not stored: %v", base)
	}
	barEdited := write(strings.Replace(orig, "return 2", "return 3", 1))
	if barEdited["Foo"] != base["Foo"] || barEdited["Bar"] == base["Bar"] {
		t.Fatalf("editing Bar: before %v, after %v", base, barEdited)
	}
	fooEdited := write(strings.Replace(orig, "return 1", "return 10", 1))
	if fooEdited["Foo"] == base["Foo"] {
		t.Fatalf("editing Foo left its hash at %s", base["Foo"])
	}
	crlf := write(strings.ReplaceAll(strings.Replace(orig, "return 1", "return 10", 1), "\n", "\r\n"))
	if crlf["Foo"] != fooEdited["Foo"] || crlf["Bar"] != fooEdited["Bar"] {
		t.Fatalf("CRLF conversion: before %v, after %v", fooEdited, crlf)
	}
}

func TestIndexStoresNoFileHash(t *testing.T) {
	repo, db := freshnessFixture(t)
	t.Cleanup(CloseAll)
	// Reparsing a file that is already indexed is where a hash used to be stored.
	path, later := filepath.Join(repo, "main.go"), time.Now().Add(time.Hour)
	if err := os.WriteFile(path, []byte("package fixture\nfunc Edited() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if stats, err := Index(repo, db, Options{Workers: 1}); err != nil || stats.FilesIndexed != 1 {
		t.Fatalf("reindex: %+v, %v", stats, err)
	}
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var files, hashed int
	if err := store.db.QueryRow(`SELECT COUNT(*), COUNT(NULLIF(hash, '')) FROM files`).Scan(&files, &hashed); err != nil {
		t.Fatal(err)
	}
	if files != 1 || hashed != 0 {
		t.Fatalf("files %d, with a hash %d; want 1 and 0", files, hashed)
	}
}

func TestIndexFormatUpgradeReparsesUnchangedFilesOnce(t *testing.T) {
	repo, db := freshnessFixture(t)
	t.Cleanup(CloseAll)
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "sub.go"), []byte("package sub\nfunc Nested() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Index(repo, db, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}

	// Simulate an index written before format 1: no marker and no hashes.
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`UPDATE symbols SET body_hash = ''`); err != nil {
		t.Fatal(err)
	}
	if _, err := store.db.Exec(`DELETE FROM meta WHERE key = ?`, metaIndexFormat); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	format := func() string {
		t.Helper()
		store, err := OpenStore(db)
		if err != nil {
			t.Fatal(err)
		}
		defer store.Close()
		value, err := store.GetMeta(metaIndexFormat)
		if err != nil {
			t.Fatal(err)
		}
		return value
	}

	// A scoped run reparses only its subtree, so it leaves the marker unset.
	if _, err := Index(repo, db, Options{Workers: 1, Scope: sub}); err != nil {
		t.Fatal(err)
	}
	if got := format(); got != "" {
		t.Fatalf("scoped run set index_format to %q", got)
	}

	stats, err := Index(repo, db, Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesIndexed != 2 || stats.FilesSkipped != 0 {
		t.Fatalf("upgrade run: indexed %d, skipped %d; want every file reparsed", stats.FilesIndexed, stats.FilesSkipped)
	}
	if got := format(); got != indexFormat {
		t.Fatalf("index_format = %q, want %q", got, indexFormat)
	}
	syms, err := FileOutline(db, filepath.Join(repo, "main.go"))
	if err != nil || len(syms) == 0 || syms[0].BodyHash == "" {
		t.Fatalf("after upgrade: %+v, %v", syms, err)
	}

	stats, err = Index(repo, db, Options{Workers: 1})
	if err != nil {
		t.Fatal(err)
	}
	if stats.FilesIndexed != 0 || stats.FilesSkipped != 2 {
		t.Fatalf("run after upgrade: indexed %d, skipped %d; want nothing reparsed", stats.FilesIndexed, stats.FilesSkipped)
	}
}

func TestOpenStoreAddsBodyHashToLegacySymbolsTable(t *testing.T) {
	db := filepath.Join(t.TempDir(), "legacy.db")
	raw, err := sql.Open("sqlite3", db)
	if err != nil {
		t.Fatal(err)
	}
	// The symbols table as created before body_hash existed.
	for _, stmt := range []string{
		`CREATE TABLE symbols (
			id INTEGER PRIMARY KEY AUTOINCREMENT, file_id INTEGER NOT NULL,
			name TEXT NOT NULL, kind TEXT NOT NULL, start_line INTEGER NOT NULL,
			end_line INTEGER NOT NULL, start_col INTEGER, end_col INTEGER,
			parent TEXT, depth INTEGER DEFAULT 0, signature TEXT, language TEXT NOT NULL)`,
		`INSERT INTO symbols (file_id, name, kind, start_line, end_line, language) VALUES (1, 'Old', 'function', 1, 1, 'go')`,
	} {
		if _, err := raw.Exec(stmt); err != nil {
			t.Fatal(err)
		}
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}

	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	var hash string
	if err := store.db.QueryRow(`SELECT body_hash FROM symbols WHERE name = 'Old'`).Scan(&hash); err != nil || hash != "" {
		t.Fatalf("legacy row body_hash = %q, err %v; want empty string", hash, err)
	}
}
