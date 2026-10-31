package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
)

func TestAffectedTestsScopeAndTestPathFlags(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, map[string]string{
		"source.ts":        "export function target() { return 1; }\n",
		"collision.js":     "function target() {}\n",
		"collision.py":     "def target():\n    pass\n",
		"checks/custom.ts": "target();\n",
		"verify/custom.js": "target();\n",
		"audit/custom.py":  "target()\n",
	})
	writeFile(t, repo, "source.ts", "export function target() { return 2; }\n")
	out, _, err := runAffectedCLI(t, repo, nil, "--json")
	affectedCLIPaths(t, affectedCLIResult(t, out, err))
	for _, tc := range []struct {
		scope      string
		paths      []string
		confidence string
	}{
		{"same", []string{"checks/custom.ts"}, "high"},
		{"family", []string{"checks/custom.ts", "verify/custom.js"}, "low"},
		{"all", []string{"audit/custom.py", "checks/custom.ts", "verify/custom.js"}, "low"},
	} {
		t.Run(tc.scope, func(t *testing.T) {
			out, _, err := runAffectedCLI(t, repo, nil, "--json", "--resolve-scope", tc.scope, "--test-path", "checks", "--test-path", "verify", "--test-path", "audit")
			r := affectedCLIResult(t, out, err)
			affectedCLIPaths(t, r, tc.paths...)
			if string(r.Bounds.ResolveScope) != tc.scope || !reflect.DeepEqual(r.Bounds.TestPaths, []string{"audit", "checks", "verify"}) {
				t.Fatalf("effective options: %+v", r.Bounds)
			}
			for _, test := range r.Tests {
				if test.Confidence != tc.confidence {
					t.Fatalf("scoped confidence: %+v", test)
				}
			}
			if tc.scope == "same" {
				if r.Status != "complete" {
					t.Fatalf("unambiguous precise scan: %+v", r)
				}
			} else {
				affectedCLIGap(t, r, "ambiguous_definitions", "source.ts")
				affectedCLIAdvice(t, r)
			}
		})
	}
}

func TestAffectedTestsCapFlagsAndWarnings(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	for _, tc := range []struct {
		name, flag, value, gap string
		bound                  func(index.AffectedTestsOptions) int
	}{
		{"seeds", "--max-symbols", "1", "seed_limit", func(o index.AffectedTestsOptions) int { return o.MaxSymbols }},
		{"evidence", "--max-evidence", "1", "evidence_limit", func(o index.AffectedTestsOptions) int { return o.MaxEvidence }},
		{"results", "-n", "1", "result_limit", func(o index.AffectedTestsOptions) int { return o.Limit }},
		{"depth", "-D", "1", "depth_limit", func(o index.AffectedTestsOptions) int { return o.Depth }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--json", tc.flag, tc.value)
			r := affectedCLIResult(t, out, err)
			if tc.bound(r.Bounds) != 1 {
				t.Fatalf("ignored option: %+v", r.Bounds)
			}
			found := false
			for _, gap := range r.Limitations {
				if gap.Code == tc.gap {
					found = true
				}
			}
			if !found {
				t.Fatalf("missing %s: %+v", tc.gap, r.Limitations)
			}
			affectedCLIAdvice(t, r)
			if tc.name == "results" && len(r.Tests) != 1 {
				t.Fatalf("limit not applied: %+v", r.Tests)
			}
			if tc.name == "depth" {
				affectedCLIPaths(t, r, "direct_test.go", "gamma_test.go")
			}
			out, stderr, err := runAffectedCLI(t, repo, nil, "source.go", "--quiet", tc.flag, tc.value)
			if err != nil {
				t.Fatal(err)
			}
			requireOutputContains(t, stderr, tc.gap)
			requireOutputContains(t, stderr, "full suite")
			if strings.Contains(out, tc.gap) {
				t.Fatalf("warning on quiet stdout: %s", out)
			}
		})
	}
	t.Run("unlimited_and_depth_clamp", func(t *testing.T) {
		out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--json", "--max-symbols", "0", "--max-evidence", "0", "--depth", "99")
		r := affectedCLIResult(t, out, err)
		if r.Bounds.Depth != 5 || r.Bounds.MaxSymbols != 0 || r.Bounds.MaxEvidence != 0 {
			t.Fatalf("effective bounds: %+v", r.Bounds)
		}
		affectedCLIPaths(t, r, "direct_test.go", "gamma_test.go", "transitive_test.go")
		for _, l := range r.Limitations {
			if strings.HasSuffix(l.Code, "_limit") {
				t.Fatalf("false cutoff: %+v", l)
			}
		}
	})
}

func TestAffectedTestsInputLimitationsRetainPaths(t *testing.T) {
	for _, tc := range []struct {
		name, path, before, after, code string
		explicit                        bool
	}{
		{"missing", "missing.go", "", "", "missing_file", true},
		{"unsupported", "unsupported.xyz", "old\n", "new\n", "unsupported", false},
		{"binary", "image.bin", "old\x00image", "new\x00image", "binary", false},
		{"config", "config.json", "{\"enabled\":false}\n", "{\"enabled\":true}\n", "no_symbol_seed", false},
		{"outside_symbols", "source.go", "package p\n// before\nfunc Alpha() {}\n", "package p\n// after\nfunc Alpha() {}\n", "outside_symbols", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{"unrelated_test.go": "package p\nfunc TestUnrelated() {}\n"}
			if tc.before != "" {
				files[tc.path] = tc.before
			}
			repo, _ := newAffectedCLIRepo(t, files)
			if tc.after != "" {
				writeFile(t, repo, tc.path, tc.after)
			}
			args := []string{"--json"}
			if tc.explicit {
				args = append(args, tc.path)
			}
			out, _, err := runAffectedCLI(t, repo, nil, args...)
			r := affectedCLIResult(t, out, err)
			affectedCLIGap(t, r, tc.code, tc.path)
			affectedCLIAdvice(t, r)
			affectedCLIPaths(t, r)
			if r.Status != "unavailable" {
				t.Fatalf("wholly unanalyzable input: %+v", r)
			}
		})
	}
	t.Run("deleted", func(t *testing.T) {
		repo, _ := newAffectedCLIRepo(t, map[string]string{"source.go": "package p\nfunc Alpha() {}\n", "direct_test.go": "package p\nfunc TestAlpha() { Alpha() }\n"})
		if err := os.Remove(filepath.Join(repo, "source.go")); err != nil {
			t.Fatal(err)
		}
		out, _, err := runAffectedCLI(t, repo, nil, "--json")
		r := affectedCLIResult(t, out, err)
		affectedCLIGap(t, r, "deleted_seed", "source.go")
		affectedCLIGap(t, r, "missing_file", "source.go")
		affectedCLIAdvice(t, r)
		affectedCLIPaths(t, r, "direct_test.go")
		if len(r.Tests[0].ChangedSymbols) != 1 || !r.Tests[0].ChangedSymbols[0].Deleted || r.Tests[0].Confidence != "low" {
			t.Fatalf("false live deleted seed: %+v", r.Tests[0])
		}
	})
	t.Run("rename", func(t *testing.T) {
		repo, _ := newAffectedCLIRepo(t, map[string]string{"old name.go": "package p\nfunc Alpha() {}\n"})
		gitChanged(t, repo, "mv", "old name.go", "new name.go")
		out, _, err := runAffectedCLI(t, repo, nil, "--staged", "--json")
		r := affectedCLIResult(t, out, err)
		affectedCLIGap(t, r, "renamed", "new name.go")
		affectedCLIAdvice(t, r)
		requireOutputContains(t, out, "old name.go")
	})
	t.Run("unreadable_symlink_blob", func(t *testing.T) {
		repo, _ := newAffectedCLIRepo(t, map[string]string{"source.go": "package p\nfunc Alpha() {}\n", "target.go": "package p\nfunc Other() {}\n"})
		if err := os.Remove(filepath.Join(repo, "source.go")); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink("target.go", filepath.Join(repo, "source.go")); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
		out, _, err := runAffectedCLI(t, repo, nil, "--json")
		r := affectedCLIResult(t, out, err)
		affectedCLIGap(t, r, "unreadable", "source.go")
		affectedCLIAdvice(t, r)
	})
}

func TestAffectedTestsConflictedInputRetainsPath(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, map[string]string{"conflict.go": "package p\nfunc Alpha() int { return 1 }\n"})
	gitChanged(t, repo, "checkout", "-b", "other")
	writeFile(t, repo, "conflict.go", "package p\nfunc Alpha() int { return 2 }\n")
	gitChanged(t, repo, "add", ".")
	gitChanged(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "other")
	gitChanged(t, repo, "checkout", "-")
	writeFile(t, repo, "conflict.go", "package p\nfunc Alpha() int { return 3 }\n")
	gitChanged(t, repo, "add", ".")
	gitChanged(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "main")
	merge := exec.Command("git", "-C", repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "merge", "other")
	if out, err := merge.CombinedOutput(); err == nil || !strings.Contains(string(out), "CONFLICT") {
		t.Fatalf("expected real conflict: %v\n%s", err, out)
	}
	out, _, err := runAffectedCLI(t, repo, nil, "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIGap(t, r, "conflicted", "conflict.go")
	affectedCLIAdvice(t, r)
}

func TestAffectedTestsSiblingWorktreeDoesNotLeak(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, map[string]string{"source.go": "package p\nfunc Alpha() {}\n"})
	sibling := filepath.Join(t.TempDir(), "sibling")
	gitChanged(t, repo, "worktree", "add", "-b", "sibling", sibling)
	writeFile(t, sibling, "only_sibling_test.go", "package p\nfunc TestSibling() { Alpha() }\n")
	if _, err := index.Index(sibling, "", index.Options{Workers: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	// No --db or --no-federate: the default behavior itself must be isolated.
	out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r)
	if strings.Contains(out, "only_sibling_test.go") {
		t.Fatalf("sibling candidate leaked: %s", out)
	}
}
