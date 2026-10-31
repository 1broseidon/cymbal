package cmd

import (
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func changedAnalysisRepo(t *testing.T, files map[string]string) string {
	t.Helper()
	repo := t.TempDir()
	for path, content := range files {
		writeFile(t, repo, path, content)
	}
	gitChanged(t, repo, "init")
	gitChanged(t, repo, "config", "user.name", "Test")
	gitChanged(t, repo, "config", "user.email", "test@example.invalid")
	gitChanged(t, repo, "add", ".")
	gitChanged(t, repo, "commit", "-m", "initial")
	return repo
}

func readAnalysis(t *testing.T, repo string, staged bool, base string) *changedAnalysis {
	t.Helper()
	result, err := readChangedAnalysis(repo, staged, base)
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func hasChangedLimitation(a *changedAnalysis, code, path, side string) bool {
	for _, l := range a.Limitations {
		if l.Code == code && l.Path == path && l.Side == side && l.Message != "" {
			return true
		}
	}
	return false
}

func TestChangedAnalysisRetainsDefinitionIdentities(t *testing.T) {
	old := "package p\ntype A struct{}\ntype B struct{}\nfunc (A) Same() int { return 1 }\nfunc (B) Same() int { return 2 }\n"
	repo := changedAnalysisRepo(t, map[string]string{"p.go": old})
	writeFile(t, repo, "p.go", strings.ReplaceAll(old, "return", "return 10 +"))
	a := readAnalysis(t, repo, false, "")
	newSeeds := []changedSeed{}
	for _, seed := range a.Seeds {
		if seed.Side == "new" {
			newSeeds = append(newSeeds, seed)
		}
	}
	if len(newSeeds) != 2 {
		t.Fatalf("new definitions = %+v; want two Same methods", newSeeds)
	}
	for i, seed := range newSeeds {
		s := seed.Symbol
		if s.File != "p.go" || s.Name != "Same" || s.Kind != "method" || s.Language != "go" || s.StartLine != 4+i || s.EndLine != 4+i || seed.Deleted {
			t.Fatalf("lost definition identity: %+v", seed)
		}
	}
	changed, order, deleted, _ := a.legacyProjection()
	if !reflect.DeepEqual(order, []string{"Same"}) || len(changed["Same"]) != 1 || len(deleted) != 0 || a.SkippedFiles != 0 {
		t.Fatalf("legacy projection = %v %v %v; skipped %d", changed, order, deleted, a.SkippedFiles)
	}
}

func TestChangedAnalysisRetainsBinaryPaths(t *testing.T) {
	for _, path := range []string{"space ü.bin", "tab\tquote\".bin", "line\nbreak.bin"} {
		t.Run(path, func(t *testing.T) {
			if runtime.GOOS == "windows" && strings.ContainsAny(path, "\t\n\"") {
				t.Skip("filename characters are not supported on Windows")
			}
			repo := changedAnalysisRepo(t, map[string]string{path: "before\x00"})
			writeFile(t, repo, path, "after\x00")
			a := readAnalysis(t, repo, false, "")
			if len(a.Files) != 1 || a.Files[0].OldPath != path || a.Files[0].NewPath != path || !a.Files[0].Binary || a.SkippedFiles != 1 || len(a.Seeds) != 0 || !hasChangedLimitation(a, "binary", path, "") {
				t.Fatalf("binary input = %+v; limits %+v", a, a.Limitations)
			}
		})
	}
}

func TestChangedAnalysisHonorsGitBinaryAttributes(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{".gitattributes": "*.go binary\n", "source file.go": "package p\nfunc F() int { return 1 }\n"})
	writeFile(t, repo, "source file.go", "package p\nfunc F() int { return 2 }\n")
	a := readAnalysis(t, repo, false, "")
	if len(a.Files) != 1 || !a.Files[0].Binary || len(a.Seeds) != 0 || a.SkippedFiles != 1 {
		t.Fatalf("Git binary attribute lost: %+v", a)
	}
}

func TestChangedAnalysisRetainsPureRenamePaths(t *testing.T) {
	oldPath, newPath := "old ü\tname.go", "new \"name\".go"
	if runtime.GOOS == "windows" {
		oldPath, newPath = "old ü name.go", "new name.go"
	}
	repo := changedAnalysisRepo(t, map[string]string{oldPath: "package p\nfunc F() {}\n"})
	gitChanged(t, repo, "mv", oldPath, newPath)
	a := readAnalysis(t, repo, true, "")
	if len(a.Files) != 1 {
		t.Fatalf("inputs = %+v", a.Files)
	}
	f := a.Files[0]
	if f.OldPath != oldPath || f.NewPath != newPath || f.Status != "R100" || f.HasChangedLines || len(a.Seeds) != 0 || a.SkippedFiles != 0 || !hasChangedLimitation(a, "renamed", newPath, "") {
		t.Fatalf("rename input = %+v; analysis %+v", f, a)
	}
}

func TestChangedAnalysisRenameProjectsLiveSymbolsToNewPath(t *testing.T) {
	old := "package p\n\nfunc Alpha() int { return 1 }\n\nfunc Beta() int { return 2 }\n\nfunc Gamma() int { return 3 }\n"
	repo := changedAnalysisRepo(t, map[string]string{"old file.go": old})
	gitChanged(t, repo, "mv", "old file.go", "new file.go")
	writeFile(t, repo, "new file.go", strings.Replace(old, "return 2", "return 222", 1))
	gitChanged(t, repo, "add", ".")
	a := readAnalysis(t, repo, true, "")
	changed, order, deleted, _ := a.legacyProjection()
	if !reflect.DeepEqual(order, []string{"Beta"}) || !reflect.DeepEqual(sortedKeys(changed["Beta"]), []string{"new file.go"}) || len(deleted) != 0 || !hasChangedLimitation(a, "renamed", "new file.go", "") {
		t.Fatalf("renamed modification = %+v; changed %v %v, deleted %v", a, changed, order, deleted)
	}
}

func TestChangedAnalysisRetainsConflictPaths(t *testing.T) {
	path := "conflict ü\tname.go"
	if runtime.GOOS == "windows" {
		path = "conflict ü name.go"
	}
	repo := changedAnalysisRepo(t, map[string]string{path: "package p\nfunc F() int { return 1 }\n"})
	gitChanged(t, repo, "branch", "other")
	writeFile(t, repo, path, "package p\nfunc F() int { return 2 }\n")
	gitChanged(t, repo, "commit", "-am", "ours")
	gitChanged(t, repo, "checkout", "other")
	writeFile(t, repo, path, "package p\nfunc F() int { return 3 }\n")
	gitChanged(t, repo, "commit", "-am", "theirs")
	// Both fixture branches are local and temporary. Merge must leave a conflict.
	if err := exec.Command("git", "-C", repo, "merge", "-", "--no-edit").Run(); err == nil {
		t.Fatal("expected a merge conflict")
	}
	a := readAnalysis(t, repo, false, "")
	if len(a.Files) != 1 || a.Files[0].OldPath != path || a.Files[0].NewPath != path || !a.Files[0].Conflicted || a.ConflictedFiles != 1 || a.SkippedFiles != 0 || len(a.Seeds) != 0 || !hasChangedLimitation(a, "conflicted", path, "") {
		t.Fatalf("conflict input = %+v; limits %+v", a, a.Limitations)
	}
}

func TestChangedAnalysisReportsBothSideFailures(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"config.txt": "before\n"})
	writeFile(t, repo, "config.txt", "after\n")
	a := readAnalysis(t, repo, false, "")
	for _, side := range []string{"old", "new"} {
		if !hasChangedLimitation(a, "unsupported", "config.txt", side) {
			t.Fatalf("missing %s failure: %+v", side, a.Limitations)
		}
	}
	if len(a.Files) != 1 || a.SkippedFiles != 1 || len(a.Seeds) != 0 {
		t.Fatalf("unsupported input = %+v", a)
	}
}

func TestChangedAnalysisReportsOutsideSymbolLines(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\n\nfunc F() int { return 1 }\n"})
	writeFile(t, repo, "p.go", "package renamed\n\nfunc F() int { return 2 }\n")
	a := readAnalysis(t, repo, false, "")
	for _, side := range []string{"old", "new"} {
		if !hasChangedLimitation(a, "outside_symbols", "p.go", side) {
			t.Fatalf("missing %s outside-symbol reason: %+v", side, a.Limitations)
		}
	}
	_, order, _, _ := a.legacyProjection()
	if !reflect.DeepEqual(order, []string{"F"}) || a.SkippedFiles != 0 {
		t.Fatalf("projection = %v; skipped %d", order, a.SkippedFiles)
	}
}

func TestChangedAnalysisFailedNewSideDoesNotDelete(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\nfunc F() {}\n"})
	writeFile(t, repo, "p.go", strings.Repeat(" ", maxChangedParseBytes+1))
	a := readAnalysis(t, repo, false, "")
	if !hasChangedLimitation(a, "oversized", "p.go", "new") {
		t.Fatalf("new failure missing: %+v", a.Limitations)
	}
	changed, order, deleted, _ := a.legacyProjection()
	if !reflect.DeepEqual(order, []string{"F"}) || len(changed["F"]) != 1 || len(deleted) != 0 || a.SkippedFiles != 0 {
		t.Fatalf("failed new parse manufactured deletion: %+v", a)
	}
	for _, s := range a.Seeds {
		if s.Deleted || s.Side != "old" {
			t.Fatalf("false live or deleted seed: %+v", s)
		}
	}
}

func TestChangedAnalysisFailedOldSideRetainsNewSeed(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": strings.Repeat(" ", maxChangedParseBytes+1)})
	writeFile(t, repo, "p.go", "package p\nfunc F() {}\n")
	a := readAnalysis(t, repo, false, "")
	if !hasChangedLimitation(a, "oversized", "p.go", "old") || a.SkippedFiles != 0 || len(a.Seeds) != 1 || a.Seeds[0].Symbol.Name != "F" || a.Seeds[0].Side != "new" || a.Seeds[0].Deleted {
		t.Fatalf("old failure lost new seed: %+v; limits %+v", a, a.Limitations)
	}
}

func TestChangedAnalysisTypeChangePreservesLegacyDeletion(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\nfunc F() {}\n"})
	path := filepath.Join(repo, "p.go")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("missing.go", path); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	a := readAnalysis(t, repo, false, "")
	if !hasChangedLimitation(a, "unreadable", "p.go", "new") {
		t.Fatalf("unreadable replacement lost: %+v", a.Limitations)
	}
	_, live, _, deleted := a.legacyProjection()
	// Git emits separate deletion/addition blocks for a type change. The old
	// regular file is removed; the unreadable symlink block is a legacy skip.
	if len(a.Files) != 1 || a.Files[0].Status != "T" || len(live) != 0 || !reflect.DeepEqual(deleted, []string{"F"}) || a.SkippedFiles != 1 {
		t.Fatalf("type-change projection lost: %+v; live %v, deleted %v", a, live, deleted)
	}
}

func TestChangedAnalysisDeletedFileRetainsOldIdentity(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"gone.go": "package p\nfunc Gone() {}\n"})
	if err := os.Remove(filepath.Join(repo, "gone.go")); err != nil {
		t.Fatal(err)
	}
	a := readAnalysis(t, repo, false, "")
	if len(a.Files) != 1 || a.Files[0].OldPath != "gone.go" || a.Files[0].NewPath != "" || len(a.Seeds) != 1 {
		t.Fatalf("deleted input = %+v", a)
	}
	s := a.Seeds[0]
	if !s.Deleted || s.Side != "old" || s.Symbol.File != "gone.go" || s.Symbol.Name != "Gone" || s.Symbol.StartLine != 2 || s.Symbol.EndLine != 2 || s.Symbol.Language != "go" {
		t.Fatalf("deleted seed = %+v", s)
	}
}

func TestChangedAnalysisUsesStagedBlobs(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\nfunc Before() {}\n"})
	writeFile(t, repo, "p.go", "package p\nfunc Staged() {}\n")
	gitChanged(t, repo, "add", "p.go")
	writeFile(t, repo, "p.go", "package p\nfunc Working() {}\n")
	for _, tc := range []struct {
		staged           bool
		base, live, gone string
	}{
		{true, "", "Staged", "Before"}, {false, "", "Working", "Staged"}, {false, "HEAD", "Working", "Before"},
	} {
		a := readAnalysis(t, repo, tc.staged, tc.base)
		_, live, _, gone := a.legacyProjection()
		if !reflect.DeepEqual(live, []string{tc.live}) || !reflect.DeepEqual(gone, []string{tc.gone}) {
			t.Fatalf("staged=%v base=%q: live %v, gone %v", tc.staged, tc.base, live, gone)
		}
	}
}

func TestChangedAnalysisEmptyDiffAndGitFailure(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\n"})
	a := readAnalysis(t, repo, false, "")
	if a.Files == nil || a.Seeds == nil || a.Limitations == nil || len(a.Files)+len(a.Seeds)+len(a.Limitations) != 0 {
		t.Fatalf("empty diff = %+v", a)
	}
	if _, err := readChangedAnalysis(repo, false, "refs/heads/does-not-exist"); err == nil {
		t.Fatal("invalid Git ref must return an error")
	}
}

func TestChangedAnalysisModeOnlyDoesNotSkip(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\nfunc F() {}\n"})
	gitChanged(t, repo, "update-index", "--chmod=+x", "p.go")
	a := readAnalysis(t, repo, true, "")
	if len(a.Files) != 1 || a.Files[0].NewPath != "p.go" || a.Files[0].HasChangedLines || len(a.Seeds) != 0 || a.SkippedFiles != 0 || !hasChangedLimitation(a, "metadata_only", "p.go", "") {
		t.Fatalf("mode-only input = %+v", a)
	}
}

func TestChangedAnalysisBinaryRenameRetainsBothPaths(t *testing.T) {
	oldPath, newPath := "old \tname.bin", "new \"name\".bin"
	if runtime.GOOS == "windows" {
		oldPath, newPath = "old name.bin", "new name.bin"
	}
	before := strings.Repeat("same bytes with newline\x00\n", 1000)
	repo := changedAnalysisRepo(t, map[string]string{oldPath: before})
	gitChanged(t, repo, "mv", oldPath, newPath)
	writeFile(t, repo, newPath, before+"changed\x00")
	gitChanged(t, repo, "add", ".")
	a := readAnalysis(t, repo, true, "")
	if len(a.Files) != 1 {
		t.Fatalf("binary rename inputs = %+v", a.Files)
	}
	f := a.Files[0]
	if f.OldPath != oldPath || f.NewPath != newPath || !strings.HasPrefix(f.Status, "R") || !f.Binary || a.SkippedFiles != 1 || !hasChangedLimitation(a, "binary", newPath, "") || !hasChangedLimitation(a, "renamed", newPath, "") {
		t.Fatalf("binary rename = %+v; analysis %+v", f, a)
	}
}

func TestChangedAnalysisSymbolFreeInputRemainsVisible(t *testing.T) {
	repo := changedAnalysisRepo(t, map[string]string{"p.go": "package p\n"})
	writeFile(t, repo, "p.go", "package renamed\n")
	a := readAnalysis(t, repo, false, "")
	if len(a.Files) != 1 || a.Files[0].NewPath != "p.go" || !a.Files[0].HasChangedLines || len(a.Seeds) != 0 || a.SkippedFiles != 0 {
		t.Fatalf("parsed symbol-free input = %+v", a)
	}
	for _, side := range []string{"old", "new"} {
		if !hasChangedLimitation(a, "outside_symbols", "p.go", side) {
			t.Fatalf("missing %s symbol-free limitation: %+v", side, a.Limitations)
		}
	}
}
