package cmd

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Build once, then execute the registered CLI for every input mode. In-process
// tests cover the remaining branches without replacing Git, parsing, or SQLite.
func TestAffectedTestsExecutableModes(t *testing.T) {
	binary := filepath.Join(t.TempDir(), "cymbal")
	if runtime.GOOS == "windows" {
		binary += ".exe"
	}
	build := exec.Command("go", "build", "-o", binary, "..")
	build.Env = append(os.Environ(), "CGO_CFLAGS=-DSQLITE_ENABLE_FTS5")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	// Alpha differs in the staged blob; Gamma differs only on disk. The final
	// journey also removes staged Alpha from the live graph.
	writeFile(t, repo, "source.go", "package p\nfunc Alpha() int { return 11 }\n\nfunc Gamma() int { return 3 }\n")
	gitChanged(t, repo, "add", "source.go")
	writeFile(t, repo, "source.go", "package p\nfunc Alpha() int { return 11 }\n\nfunc Gamma() int { return 33 }\n")
	writeFile(t, repo, "untracked.go", "package p\nfunc Untracked() {}\n")
	writeFile(t, repo, "untracked_test.go", "package p\nfunc TestUntracked() { Untracked() }\n")
	run := func(t *testing.T, stdin string, args ...string) (string, string, error) {
		t.Helper()
		cmd := exec.Command(binary, append([]string{"affected-tests"}, args...)...)
		cmd.Dir = repo
		cmd.Stdin = strings.NewReader(stdin)
		var out, stderr bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &stderr
		err := cmd.Run()
		return out.String(), stderr.String(), err
	}
	for _, tc := range []struct {
		name      string
		args      []string
		stdin     string
		want      []string
		precision string
	}{
		{"unstaged", nil, "", []string{"gamma_test.go"}, "hunk"},
		{"staged", []string{"--staged"}, "", []string{"direct_test.go", "transitive_test.go"}, "hunk"},
		{"base", []string{"--base", "HEAD"}, "", []string{"direct_test.go", "gamma_test.go", "transitive_test.go"}, "hunk"},
		{"file", []string{"source.go"}, "", []string{"direct_test.go", "gamma_test.go", "transitive_test.go"}, "file"},
		{"stdin", []string{"--stdin"}, "source.go\n", []string{"direct_test.go", "gamma_test.go", "transitive_test.go"}, "file"},
		{"union", []string{"--stdin", "source.go"}, "source.go\r\nsource.go\n", []string{"direct_test.go", "gamma_test.go", "transitive_test.go"}, "file"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, stderr, err := run(t, tc.stdin, append(tc.args, "--json")...)
			if err != nil {
				t.Fatalf("%v: %v\n%s", tc.args, err, stderr)
			}
			r := affectedCLIResult(t, out, err)
			affectedCLIPaths(t, r, tc.want...)
			if r.InputPrecision != tc.precision {
				t.Fatalf("precision = %s", r.InputPrecision)
			}
			if tc.precision == "hunk" {
				if r.Status != "complete" || r.Fallback.Recommended {
					t.Fatalf("precise bounded scan: %+v", r)
				}
				for _, test := range r.Tests {
					want := "high"
					if test.Path == "transitive_test.go" {
						want = "medium"
					}
					if test.Confidence != want {
						t.Errorf("%s confidence = %s, want %s", test.Path, test.Confidence, want)
					}
					if len(test.ChangedSymbols) != 1 || test.ChangedSymbols[0].Path != "source.go" || test.ChangedSymbols[0].StartLine == 0 {
						t.Fatalf("missing definition identity: %+v", test)
					}
					if test.Path == "direct_test.go" {
						e := test.Evidence[0]
						if test.Distance == nil || *test.Distance != 1 || e.Kind != "direct_reference" || len(e.Steps) != 2 || e.Steps[0].Name != "Alpha" || e.Steps[1].Path != "direct_test.go" || e.Steps[1].Line != 2 || e.Steps[1].ReferenceKind != "call" {
							t.Fatalf("direct reference must point to the actual call: %+v", test)
						}
					}
				}
			}
		})
	}
	t.Run("staged_missing_live_seed", func(t *testing.T) {
		writeFile(t, repo, "source.go", "package p\nfunc DiskOnly() int { return 11 }\n\nfunc Gamma() int { return 33 }\n")
		out, stderr, err := run(t, "", "--staged", "--json")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		r := affectedCLIResult(t, out, err)
		affectedCLIGap(t, r, "missing_live_seed", "source.go")
		affectedCLIAdvice(t, r)
		for _, test := range r.Tests {
			for _, seed := range test.ChangedSymbols {
				if seed.Name == "DiskOnly" || seed.Name == "Gamma" {
					t.Fatalf("disk seed substituted for staged Alpha: %+v", seed)
				}
			}
		}
	})
	t.Run("error_has_no_success_output", func(t *testing.T) {
		out, stderr, err := run(t, "", "--base", "missing-ref", "--json")
		if err == nil || out != "" || !strings.Contains(stderr, "git diff") {
			t.Fatalf("out=%q stderr=%q err=%v", out, stderr, err)
		}
	})
	t.Run("base_ancestor_not_head", func(t *testing.T) {
		writeFile(t, repo, "source.go", "package p\nfunc Alpha() int { return 11 }\n\nfunc Gamma() int { return 33 }\n")
		gitChanged(t, repo, "add", "source.go")
		gitChanged(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "new head")
		out, stderr, err := run(t, "", "--base", "HEAD~1", "--json")
		if err != nil {
			t.Fatalf("%v\n%s", err, stderr)
		}
		r := affectedCLIResult(t, out, err)
		affectedCLIPaths(t, r, "direct_test.go", "gamma_test.go", "transitive_test.go")
		if r.InputPrecision != "hunk" || r.Status != "complete" {
			t.Fatalf("ancestor-ref scan: %+v", r)
		}
	})
}
