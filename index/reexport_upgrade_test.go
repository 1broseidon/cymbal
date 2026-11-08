package index

import (
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"testing"
)

// Only fixture setup uses SQL: model an older writer that had no re-export
// imports, without changing source bytes, size, or mtime.
func reExportUpgradeFixture(t *testing.T) (string, string, string) {
	t.Helper()
	repo, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	t.Cleanup(CloseAll)
	var err error
	repo, err = filepath.EvalSymlinks(repo)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(repo, "barrel.ts")
	if err := os.WriteFile(path, []byte("export { leaf } from './leaf';\nexport * from './other';\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Index(repo, db, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	assertBarrelImports(t, db, path, []string{"./leaf", "./other"})
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`DELETE FROM imports`); err != nil {
		t.Fatal(err)
	}
	if err := store.SetMeta(metaIndexFormat, "2"); err != nil {
		t.Fatal(err)
	}
	assertBarrelImports(t, db, path, []string{})
	return repo, db, path
}

func assertBarrelImports(t *testing.T, db, path string, want []string) {
	t.Helper()
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.FileImports(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) == 0 && len(want) == 0 {
		return
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("barrel imports = %v, want %v", got, want)
	}
}

func assertReExportFormat(t *testing.T, db, want string) {
	t.Helper()
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	got, err := store.GetMeta(metaIndexFormat)
	if err != nil || got != want {
		t.Fatalf("format = %q, %v; want %q", got, err, want)
	}
}

func TestIndexFormat3RefreshesReExportImports(t *testing.T) {
	repo, db, path := reExportUpgradeFixture(t)
	sub := filepath.Join(repo, "sub")
	if err := os.Mkdir(sub, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sub, "other.ts"), []byte("export const other = 1;\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Index(repo, db, Options{Workers: 1, Scope: sub}); err != nil {
		t.Fatal(err)
	}
	assertReExportFormat(t, db, "2")
	assertBarrelImports(t, db, path, []string{})
	if count, err := EnsureFreshWithError(db); err != nil || count != 2 {
		t.Fatalf("upgrade count = %d, error = %v; want 2 files reparsed", count, err)
	}
	assertReExportFormat(t, db, indexFormat)
	assertBarrelImports(t, db, path, []string{"./leaf", "./other"})
	rows, err := FindImportersByPath(db, "leaf", 1, 10)
	if err != nil || len(rows) != 1 || rows[0].RelPath != "barrel.ts" || rows[0].Import != "./leaf" {
		t.Fatalf("importer edge after upgrade = %+v, %v", rows, err)
	}
	if count, err := EnsureFreshWithError(db); err != nil || count != 0 {
		t.Fatalf("second refresh count = %d, error = %v; want no reparse", count, err)
	}
}

func TestIndexFormat3FailedWriteRetriesReExportUpgrade(t *testing.T) {
	_, db, path := reExportUpgradeFixture(t)
	store, err := OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	if _, err := store.db.Exec(`CREATE TRIGGER fail_imports BEFORE INSERT ON imports BEGIN SELECT RAISE(FAIL, 'test write failure'); END`); err != nil {
		t.Fatal(err)
	}
	if _, err := EnsureFreshWithError(db); err == nil {
		t.Fatal("failed refresh must return an error")
	}
	assertReExportFormat(t, db, "2")
	assertBarrelImports(t, db, path, []string{})
	if _, err := store.db.Exec(`DROP TRIGGER fail_imports`); err != nil {
		t.Fatal(err)
	}
	if count, err := EnsureFreshWithError(db); err != nil || count != 1 {
		t.Fatalf("retry count = %d, error = %v", count, err)
	}
	assertReExportFormat(t, db, indexFormat)
	assertBarrelImports(t, db, path, []string{"./leaf", "./other"})
}

func TestIndexFormat3FailedReadRetriesReExportUpgrade(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires Unix file permissions")
	}
	_, db, path := reExportUpgradeFixture(t)
	if err := os.Chmod(path, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
	if _, err := os.ReadFile(path); err == nil {
		t.Skip("current user can read files without permission")
	}
	if _, err := EnsureFreshWithError(db); err == nil {
		t.Fatal("failed refresh must return an error")
	}
	assertReExportFormat(t, db, "2")
	if err := os.Chmod(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if count, err := EnsureFreshWithError(db); err != nil || count != 1 {
		t.Fatalf("retry count = %d, error = %v", count, err)
	}
	assertReExportFormat(t, db, indexFormat)
	assertBarrelImports(t, db, path, []string{"./leaf", "./other"})
}
