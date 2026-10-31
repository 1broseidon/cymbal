package cmd

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

func TestAffectedTestsInvalidFlagsBeforeRefresh(t *testing.T) {
	// Not even a Git repository: validation must precede Git discovery and DB
	// creation. A bad DB would otherwise hide the requested flag diagnostic.
	cwd := t.TempDir()
	for _, tc := range []struct {
		name string
		args []string
		want string
	}{
		{"json_quiet", []string{"--json", "--quiet"}, "mutually exclusive"},
		{"staged_base", []string{"--staged", "--base", "HEAD"}, "mutually exclusive"},
		{"file_staged", []string{"source.go", "--staged"}, "file inputs"},
		{"stdin_base", []string{"--stdin", "--base", "HEAD"}, "file inputs"},
		{"explicit_false_staged", []string{"source.go", "--staged=false"}, "file inputs"},
		{"explicit_empty_base", []string{"source.go", "--base="}, "file inputs"},
		{"option_ref", []string{"--base=--help"}, "invalid base ref"},
		{"range_ref", []string{"--base", "HEAD~1..HEAD"}, "single ref"},
		{"three_dot_ref", []string{"--base", "HEAD...other"}, "single ref"},
		{"zero_depth", []string{"--depth", "0"}, "must be positive"},
		{"negative_depth", []string{"--depth", "-1"}, "must be positive"},
		{"zero_limit", []string{"--limit", "0"}, "must be positive"},
		{"negative_limit", []string{"--limit", "-1"}, "must be positive"},
		{"negative_seeds", []string{"--max-symbols", "-1"}, "must not be negative"},
		{"negative_evidence", []string{"--max-evidence", "-1"}, "must not be negative"},
		{"invalid_scope", []string{"--resolve-scope", "none"}, "resolve-scope"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := filepath.Join(t.TempDir(), "not-created.db")
			out, _, err := runAffectedCLI(t, cwd, strings.NewReader(""), append(tc.args, "--db", db)...)
			if err == nil || !strings.Contains(err.Error(), tc.want) || out != "" {
				t.Fatalf("out=%q err=%v, want %q", out, err, tc.want)
			}
			if _, err := os.Stat(db); !os.IsNotExist(err) {
				t.Fatalf("invalid flags touched DB: %v", err)
			}
		})
	}
}

type affectedFailReader struct{}

func (affectedFailReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestAffectedTestsOperationalErrorsHaveNoSuccessOutput(t *testing.T) {
	repo, db := newAffectedCLIRepo(t, affectedBasicFiles())
	for _, tc := range []struct {
		name  string
		cwd   string
		stdin io.Reader
		args  []string
		want  string
	}{
		{"not_git", t.TempDir(), nil, []string{"--json"}, "not a git repository"},
		{"missing_ref", repo, nil, []string{"--base", "missing-ref", "--json"}, "git diff"},
		{"outside", repo, nil, []string{filepath.Join(t.TempDir(), "outside.go"), "--json"}, "outside repository"},
		{"foreign_windows_path", repo, nil, []string{`C:\outside.go`, "--json"}, "outside repository"},
		{"directory", repo, nil, []string{".", "--json"}, "not a directory"},
		{"read_error", repo, affectedFailReader{}, []string{"--stdin", "--json"}, "reading file paths"},
		{"line_too_long", repo, strings.NewReader(strings.Repeat("x", 1024*1024+1)), []string{"--stdin", "--json"}, "reading file paths"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := runAffectedCLI(t, tc.cwd, tc.stdin, tc.args...)
			if err == nil || !strings.Contains(err.Error(), tc.want) || out != "" {
				t.Fatalf("out=%q err=%v, want %q", out, err, tc.want)
			}
			if tc.name == "read_error" && !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("reader error not preserved: %v", err)
			}
		})
	}
	t.Run("symlink_escape_missing_descendant", func(t *testing.T) {
		if err := os.Symlink(t.TempDir(), filepath.Join(repo, "escape")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		for _, path := range []string{"escape/missing/source.go", `escape\missing\source.go`} {
			out, _, err := runAffectedCLI(t, repo, nil, path, "--json")
			if err == nil || !strings.Contains(err.Error(), "outside repository") || out != "" {
				t.Fatalf("path=%q out=%q err=%v", path, out, err)
			}
		}
	})
	t.Run("invalid_database", func(t *testing.T) {
		bad := filepath.Join(t.TempDir(), "invalid.db")
		if err := os.WriteFile(bad, []byte("not a SQLite database"), 0600); err != nil {
			t.Fatal(err)
		}
		out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--db", bad, "--json")
		if err == nil || out != "" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})
	t.Run("refresh_failure", func(t *testing.T) {
		// A real database with corrupt persisted refresh options passes the root
		// check, then fails inside EnsureFreshWithError rather than the selector.
		store, err := index.OpenStore(db)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.SetMeta("index_exclude", "{"); err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--json")
		if err == nil || !strings.Contains(err.Error(), "refreshing index") || !strings.Contains(err.Error(), "exclusions") || out != "" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})
}

func TestAffectedTestsDatabaseRootCheckedBeforeRefresh(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	foreign := t.TempDir()
	writeFile(t, foreign, "foreign.go", "package p\nfunc Foreign() {}\n")
	gitChanged(t, foreign, "init")
	foreignDB := filepath.Join(t.TempDir(), "foreign.db")
	if _, err := index.Index(foreign, foreignDB, index.Options{Workers: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	writeFile(t, foreign, "foreign.go", "package p\nfunc MustNotBeRefreshed() {}\n")
	for _, mode := range []string{"flag", "environment"} {
		t.Run(mode, func(t *testing.T) {
			args := []string{"source.go", "--json"}
			if mode == "flag" {
				args = append(args, "--db", foreignDB)
			} else {
				t.Setenv("CYMBAL_DB", foreignDB)
			}
			out, _, err := runAffectedCLI(t, repo, nil, args...)
			if err == nil || !strings.Contains(err.Error(), "does not match input repository") || out != "" {
				t.Fatalf("out=%q err=%v", out, err)
			}
			index.CloseAll()
			found, err := index.SymbolsByName(foreignDB, "MustNotBeRefreshed")
			if err != nil || len(found) != 0 {
				t.Fatalf("foreign DB refreshed before rejection: %v %+v", err, found)
			}
			found, err = index.SymbolsByName(foreignDB, "Foreign")
			if err != nil || len(found) != 1 {
				t.Fatalf("foreign DB changed: %v %+v", err, found)
			}
		})
	}
	t.Run("unowned_existing_database", func(t *testing.T) {
		db := filepath.Join(t.TempDir(), "empty.db")
		store, err := index.OpenStore(db)
		if err != nil {
			t.Fatal(err)
		}
		if err := store.Close(); err != nil {
			t.Fatal(err)
		}
		out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--db", db, "--json")
		if err == nil || !strings.Contains(err.Error(), "does not match") || out != "" {
			t.Fatalf("out=%q err=%v", out, err)
		}
	})
}

func TestAffectedTestsCurrentDatabaseOverrides(t *testing.T) {
	repo, db := newAffectedCLIRepo(t, affectedBasicFiles())
	// An existing corrupt environment DB must not override the valid flag DB.
	bad := filepath.Join(t.TempDir(), "invalid.db")
	if err := os.WriteFile(bad, []byte("not a SQLite database"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("CYMBAL_DB", bad)
	out, _, err := runAffectedCLI(t, repo, nil, "direct_test.go", "--db", db, "--json")
	affectedCLIPaths(t, affectedCLIResult(t, out, err), "direct_test.go")
	t.Setenv("CYMBAL_DB", db)
	out, _, err = runAffectedCLI(t, repo, nil, "direct_test.go", "--json")
	affectedCLIPaths(t, affectedCLIResult(t, out, err), "direct_test.go")
}

func TestAffectedTestsMissingDatabaseIsBuiltForCurrentRoot(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	db := filepath.Join(t.TempDir(), "new", "index.db")
	out, stderr, err := runAffectedCLI(t, repo, nil, "direct_test.go", "--db", db, "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r, "direct_test.go")
	requireOutputContains(t, stderr, "Building index")
	store, err := index.OpenStore(db)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	root, err := store.GetMeta("repo_root")
	if err != nil || !pathsEqual(root, repo) {
		t.Fatalf("created index root = %q: %v", root, err)
	}
}
