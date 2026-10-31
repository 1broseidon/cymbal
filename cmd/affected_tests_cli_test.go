package cmd

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

// These fixtures run serially: the command's existing renderers use process
// stdout/stderr, and repository discovery uses the process working directory.
func newAffectedCLIRepo(t *testing.T, files map[string]string) (string, string) {
	t.Helper()
	t.Setenv("CYMBAL_CACHE_DIR", t.TempDir())
	t.Setenv("CYMBAL_DB", "")
	t.Setenv("CYMBAL_NO_UPDATE_NOTIFIER", "1")
	repo := t.TempDir()
	writeFile(t, repo, "go.mod", "module example.org/affected\n\ngo 1.25\n")
	for p, src := range files {
		writeFile(t, repo, p, src)
	}
	gitChanged(t, repo, "init")
	gitChanged(t, repo, "add", ".")
	gitChanged(t, repo, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "initial")
	if _, err := index.Index(repo, "", index.Options{Workers: 1, Force: true}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(index.CloseAll)
	db, err := index.RepoDBPath(repo)
	if err != nil {
		t.Fatal(err)
	}
	return repo, db
}

func runAffectedCLI(t *testing.T, cwd string, stdin io.Reader, args ...string) (string, string, error) {
	t.Helper()
	// A fresh root and child prevent Cobra flag values/Changed bits leaking
	// between journeys. The executable smoke test covers real root registration.
	root := &cobra.Command{Use: "cymbal", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().Bool("json", false, "")
	root.PersistentFlags().String("db", "", "")
	root.AddCommand(newAffectedTestsCommand())
	root.SetArgs(append([]string{"affected-tests"}, args...))
	root.SetIn(stdin)
	var out, stderr string
	var err error
	withWorkingDir(t, cwd, func() {
		out, stderr, err = captureProcessOutput(t, func() error {
			root.SetOut(os.Stdout)
			root.SetErr(os.Stderr)
			return root.Execute()
		})
	})
	return out, stderr, err
}

func affectedCLIResult(t *testing.T, out string, err error) index.AffectedTestsResult {
	t.Helper()
	if err != nil {
		t.Fatalf("affected-tests: %v\n%s", err, out)
	}
	var env struct {
		Version string                    `json:"version"`
		Results index.AffectedTestsResult `json:"results"`
	}
	if err := json.Unmarshal([]byte(out), &env); err != nil {
		t.Fatalf("invalid JSON: %v\n%s", err, out)
	}
	if env.Version != "0.1" || env.Results.SchemaVersion != 1 {
		t.Fatalf("version contract: %s", out)
	}
	return env.Results
}

func affectedCLIPaths(t *testing.T, r index.AffectedTestsResult, want ...string) {
	t.Helper()
	got := make([]string, 0, len(r.Tests))
	for i, test := range r.Tests {
		got = append(got, test.Path)
		if test.Rank != i+1 {
			t.Errorf("rank = %d, want %d", test.Rank, i+1)
		}
	}
	if !reflect.DeepEqual(got, append([]string{}, want...)) {
		t.Fatalf("paths = %v, want %v; result: %+v", got, want, r)
	}
}

func affectedCLIGap(t *testing.T, r index.AffectedTestsResult, code, path string) {
	t.Helper()
	for _, gap := range r.Limitations {
		if gap.Code == code && gap.Path == path && gap.Message != "" {
			return
		}
	}
	t.Fatalf("missing %s limitation for %q: %+v", code, path, r.Limitations)
}

func affectedCLIAdvice(t *testing.T, r index.AffectedTestsResult) {
	t.Helper()
	if r.Status == "complete" || !r.Fallback.Recommended || !strings.Contains(strings.ToLower(r.Fallback.Reason), "full suite") || !strings.Contains(r.Fallback.Advisory, "safe to skip") {
		t.Fatalf("missing uncertainty/full-suite advice: %+v", r)
	}
}

func affectedBasicFiles() map[string]string {
	return map[string]string{
		"source.go":          "package p\nfunc Alpha() int { return 1 }\n\nfunc Gamma() int { return 3 }\n",
		"direct_test.go":     "package p\nfunc TestAlpha() { Alpha() }\n",
		"gamma_test.go":      "package p\nfunc TestGamma() { Gamma() }\n",
		"helper.go":          "package p\nfunc Bridge() { Alpha() }\n",
		"transitive_test.go": "package p\nfunc TestBridge() { Bridge() }\n",
		"unrelated_test.go":  "package p\nfunc TestUnrelated() {}\n",
	}
}

func TestAffectedTestsFilePrecisionConfidenceAndText(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	out, _, err := runAffectedCLI(t, repo, nil, "source.go", "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r, "direct_test.go", "gamma_test.go", "transitive_test.go")
	if r.InputPrecision != "file" {
		t.Fatalf("precision = %s", r.InputPrecision)
	}
	affectedCLIGap(t, r, "file_precision", "")
	affectedCLIAdvice(t, r)
	for _, test := range r.Tests {
		if test.Confidence != "medium" || test.Distance == nil {
			t.Fatalf("file confidence: %+v", test)
		}
	}
	chain := r.Tests[2].Evidence[0]
	if *r.Tests[2].Distance != 2 || len(chain.Steps) != 3 || chain.Steps[0].Path != "source.go" || chain.Steps[1].Path != "helper.go" || chain.Steps[2].Path != "transitive_test.go" || chain.Steps[2].Line != 2 {
		t.Fatalf("disconnected explanation: %+v", chain)
	}
	out, _, err = runAffectedCLI(t, repo, nil, "source.go")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"1. direct_test.go [medium]", "direct_reference", "source.go:2 Alpha", "direct_test.go:2 TestAlpha [call]", "transitive_reference", "helper.go:2 Bridge", "full suite", "safe to skip"} {
		requireOutputContains(t, out, want)
	}
}

func TestAffectedTestsPathUnionNormalization(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, map[string]string{
		"sub/# check.test.ts":   "check();\n",
		"sub/ spaced ü.test.ts": "check();\n",
		"other.test.ts":         "other();\n",
		"positional.test.ts":    "positional();\n",
	})
	out, _, err := runAffectedCLI(t, filepath.Join(repo, "sub"), strings.NewReader("# check.test.ts\r\n spaced ü.test.ts\r\n\r\n..\\other.test.ts\r\n# check.test.ts\n"), "--stdin", "../positional.test.ts", "../other.test.ts", "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r, "other.test.ts", "positional.test.ts", "sub/ spaced ü.test.ts", "sub/# check.test.ts")
	for _, test := range r.Tests {
		if len(test.Evidence) != 1 || test.Evidence[0].Kind != "changed_test" || len(test.ChangedSymbols) != 0 {
			t.Fatalf("duplicate input inflated support: %+v", test)
		}
	}
	// Union and duplicates have exactly the same serialized result as unique files.
	unique, _, err := runAffectedCLI(t, repo, nil, "other.test.ts", "positional.test.ts", "sub/ spaced ü.test.ts", "sub/# check.test.ts", "--json")
	if err != nil || unique != out {
		t.Fatalf("union differs from unique inputs: %v\n%s\n%s", err, out, unique)
	}
}

func TestAffectedTestsEmptyStdinIsNotDiff(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	writeFile(t, repo, "source.go", "package p\nfunc Alpha() int { return 99 }\n\nfunc Gamma() int { return 3 }\n")
	out, _, err := runAffectedCLI(t, repo, strings.NewReader("\r\n\n"), "--stdin", "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r)
	if r.Status != "complete" || r.InputPrecision != "file" || len(r.Limitations) != 0 || r.Fallback.Recommended {
		t.Fatalf("empty stdin: %+v", r)
	}
	for _, key := range []string{`"tests": []`, `"limitations": []`} {
		requireOutputContains(t, out, key)
	}
}

func TestAffectedTestsJSONContract(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	out, _, err := runAffectedCLI(t, repo, nil, "direct_test.go", "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r, "direct_test.go")
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal([]byte(out), &envelope); err != nil {
		t.Fatal(err)
	}
	var result map[string]json.RawMessage
	if err := json.Unmarshal(envelope["results"], &result); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"schema_version", "status", "input_precision", "bounds", "tests", "limitations", "fallback", "examined_evidence"} {
		if len(result[key]) == 0 {
			t.Errorf("missing results.%s", key)
		}
	}
	for _, key := range []string{"tests", "limitations"} {
		if !strings.HasPrefix(string(result[key]), "[") {
			t.Errorf("%s is not an array: %s", key, result[key])
		}
	}
	var tests []map[string]json.RawMessage
	if err := json.Unmarshal(result["tests"], &tests); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"path", "rank", "confidence", "distance", "changed_symbols", "evidence"} {
		if len(tests[0][key]) == 0 {
			t.Errorf("missing tests[0].%s", key)
		}
	}
	if string(tests[0]["changed_symbols"]) != "[]" {
		t.Fatalf("file-only symbols must be []: %s", out)
	}
	var evidence []map[string]json.RawMessage
	if err := json.Unmarshal(tests[0]["evidence"], &evidence); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"kind", "seed", "distance", "steps", "confidence", "ambiguous"} {
		if len(evidence[0][key]) == 0 {
			t.Errorf("missing evidence.%s", key)
		}
	}
	if r.Tests[0].Confidence != "high" || r.Tests[0].Distance == nil || *r.Tests[0].Distance != 0 {
		t.Fatalf("changed test: %+v", r.Tests[0])
	}
}

func TestAffectedTestsQuietPathsAndAdvice(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, affectedBasicFiles())
	out, stderr, err := runAffectedCLI(t, repo, nil, "source.go", "--quiet")
	if err != nil {
		t.Fatal(err)
	}
	if out != "direct_test.go\ngamma_test.go\ntransitive_test.go\n" {
		t.Fatalf("quiet stdout = %q", out)
	}
	for _, want := range []string{"file_precision", "full suite", "safe to skip"} {
		requireOutputContains(t, stderr, want)
	}
}

func TestAffectedTestsSameNameDefinitionsAreNotCollapsed(t *testing.T) {
	before := "package p\ntype A struct{}\ntype B struct{}\n\nfunc (a A) Run() {\n println(1)\n}\n\nfunc (b B) Run() {\n println(2)\n println(3)\n}\n"
	repo, _ := newAffectedCLIRepo(t, map[string]string{"source.go": before, "direct_test.go": "package p\nfunc TestRun() { Run() }\n"})
	// A has a new-side changed seed. B has only an old-side changed line.
	// Both definitions must remain attributed, despite their equal name/kind.
	after := strings.Replace(before, "println(1)", "println(11)", 1)
	after = strings.Replace(after, " println(3)\n", "", 1)
	writeFile(t, repo, "source.go", after)
	out, _, err := runAffectedCLI(t, repo, nil, "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r, "direct_test.go")
	// Both receivers survive in the new blob, each with its own identity.
	seeds := r.Tests[0].ChangedSymbols
	if len(seeds) != 2 || seeds[0].StartLine != 5 || seeds[1].StartLine != 9 || seeds[1].EndLine != 11 {
		t.Fatalf("same-name definitions lost: %+v; result %+v", seeds, r)
	}
	for _, gap := range r.Limitations {
		if gap.Code == "missing_live_seed" {
			t.Fatalf("surviving definition reported missing: %+v", gap)
		}
	}
}

func TestAffectedTestsEmptyDiffAndAnalyzedSeedWithoutTests(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, map[string]string{"source.go": "package p\nfunc Alpha() int { return 1 }\n"})
	for _, changed := range []bool{false, true} {
		if changed {
			writeFile(t, repo, "source.go", "package p\nfunc Alpha() int { return 2 }\n")
		}
		out, _, err := runAffectedCLI(t, repo, nil, "--json")
		r := affectedCLIResult(t, out, err)
		affectedCLIPaths(t, r)
		if r.Status != "complete" || r.Fallback.Recommended || len(r.Limitations) != 0 {
			t.Fatalf("changed=%t: %+v", changed, r)
		}
		requireOutputContains(t, out, `"tests": []`)
		requireOutputContains(t, out, `"limitations": []`)
		requireOutputContains(t, out, "safe to skip")
	}
}

func TestAffectedTestsDeletedLinesInSurvivingFunction(t *testing.T) {
	repo, _ := newAffectedCLIRepo(t, map[string]string{
		"source.go":      "package p\nfunc Alpha() {\n println(1)\n println(2)\n}\n",
		"direct_test.go": "package p\nfunc TestAlpha() { Alpha() }\n",
	})
	writeFile(t, repo, "source.go", "package p\nfunc Alpha() {\n println(1)\n}\n")
	out, _, err := runAffectedCLI(t, repo, nil, "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIPaths(t, r, "direct_test.go")
	if r.Tests[0].Confidence != "high" || r.Tests[0].ChangedSymbols[0].EndLine != 4 {
		t.Fatalf("surviving current identity: %+v", r)
	}
	for _, gap := range r.Limitations {
		if gap.Code == "missing_live_seed" || gap.Code == "deleted_seed" {
			t.Fatalf("surviving function reported missing: %+v", gap)
		}
	}
}

func TestAffectedTestsDeletedSameNameMethodDoesNotBorrowSurvivor(t *testing.T) {
	before := "package p\ntype A struct{}\ntype B struct{}\nfunc (a A) Run() { println(1) }\nfunc (b B) Run() { println(2) }\n"
	repo, _ := newAffectedCLIRepo(t, map[string]string{
		"source.go":      before,
		"direct_test.go": "package p\nfunc TestRun() { Run() }\n",
	})
	writeFile(t, repo, "source.go", strings.Replace(before, "func (a A) Run() { println(1) }\n", "", 1))
	out, _, err := runAffectedCLI(t, repo, nil, "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIAdvice(t, r)
	affectedCLIGap(t, r, "ambiguous_blob_identity", "source.go")
	for _, test := range r.Tests {
		for _, s := range test.ChangedSymbols {
			if !s.Deleted && s.Name == "Run" && s.EndLine == 4 {
				t.Fatalf("deleted receiver borrowed surviving method: %+v", r)
			}
		}
	}
}

func TestAffectedTestsRenamedOutsideSymbolLinesKeepFileFallback(t *testing.T) {
	before := "import data from './config';\n\nexport function target() {\n return 1;\n}\n// padding to keep rename detection\n// unchanged line one\n// unchanged line two\n"
	repo, _ := newAffectedCLIRepo(t, map[string]string{
		"leaf.ts":           before,
		"barrel.ts":         "export * from './moved';\n",
		"tests/direct.ts":   "target();\n",
		"tests/consumer.ts": "import * as api from '../barrel';\n",
	})
	gitChanged(t, repo, "mv", "leaf.ts", "moved.ts")
	writeFile(t, repo, "moved.ts", strings.Replace(strings.Replace(before, "import data from './config';\n", "", 1), "return 1", "return 2", 1))
	gitChanged(t, repo, "add", ".")
	out, _, err := runAffectedCLI(t, repo, nil, "--staged", "--json")
	r := affectedCLIResult(t, out, err)
	affectedCLIGap(t, r, "renamed", "moved.ts")
	affectedCLIPaths(t, r, "tests/direct.ts", "tests/consumer.ts")
}
