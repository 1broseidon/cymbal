package cmd

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

func TestCommandsRejectFailedRefresh(t *testing.T) {
	db := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(db, []byte("not a sqlite database"), 0600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.CloseAll)
	for _, tc := range []struct {
		name    string
		run     func(*cobra.Command, []string) error
		command *cobra.Command
		args    []string
	}{
		{"outline", outlineCmd.RunE, newOutlineTestCommand(db), []string{"file.go"}},
		{"refs", refsCmd.RunE, newRefsTestCommand(db), []string{"Target"}},
		{"investigate", investigateCmd.RunE, commandWithDB(db), []string{"Target"}},
		{"search", searchCmd.RunE, newSearchTestCommand(db), []string{"Target"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			stdout, _, err := captureProcessOutput(t, func() error { return tc.run(tc.command, tc.args) })
			if err == nil || !strings.Contains(err.Error(), "refreshing index") {
				t.Fatalf("error=%v", err)
			}
			if stdout != "" {
				t.Fatalf("query ran after failed refresh: %q", stdout)
			}
		})
	}
}

func TestInvestigateBatchPreservesSuccessOnQueryError(t *testing.T) {
	_, db := newPhase2Repo(t)
	for _, jsonOut := range []bool{false, true} {
		command := commandWithDB(db)
		if jsonOut {
			setTestFlag(t, command, "json", "true")
		}
		stdout, _, err := captureProcessOutput(t, func() error { return investigateCmd.RunE(command, []string{"Execute", `"`}) })
		if err == nil {
			t.Fatal("invalid FTS query must cause a nonzero result")
		}
		requireOutputContains(t, stdout, "Execute")
		if jsonOut {
			requireOutputContains(t, stdout, `"error":`)
		}
	}
}

func TestOutlineBatchPreservesSuccessOnPathError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cannot remove the current directory on Windows")
	}
	repo, db := newPhase2Repo(t)
	// An absolute target remains usable after cwd is removed; relative resolution fails.
	for _, mode := range []string{"text", "names", "json"} {
		t.Run(mode, func(t *testing.T) {
			cwd := t.TempDir()
			withWorkingDir(t, cwd, func() {
				if err := os.Remove(cwd); err != nil {
					t.Fatal(err)
				}
				// macOS still reports the removed directory's path, so relative
				// resolution succeeds there and no target fails.
				if _, err := filepath.Abs("relative.go"); err == nil {
					t.Skip("relative paths still resolve after the working directory is removed")
				}
				command := newOutlineTestCommand(db)
				if mode != "text" {
					setTestFlag(t, command, mode, "true")
				}
				stdout, _, err := captureProcessOutput(t, func() error {
					return outlineCmd.RunE(command, []string{filepath.Join(repo, "main.go"), "relative.go"})
				})
				if err == nil {
					t.Fatal("failed target must cause a nonzero result")
				}
				requireOutputContains(t, stdout, "Execute")
			})
		})
	}
}
