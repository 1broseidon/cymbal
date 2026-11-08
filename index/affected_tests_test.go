package index

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

type affectedFixture struct{ root, db string }

func newAffectedFixture(t *testing.T, files map[string]string) affectedFixture {
	t.Helper()
	root, db := t.TempDir(), filepath.Join(t.TempDir(), "index.db")
	for p, content := range files {
		writePhase3File(t, root, p, content)
	}
	if _, err := Index(root, db, Options{Workers: 1}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(CloseAll)
	return affectedFixture{root, db}
}
func (f affectedFixture) seed(t *testing.T, p, name string) AffectedTestSymbol {
	t.Helper()
	ss, err := FileOutline(f.db, filepath.Join(f.root, p))
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range ss {
		if s.Name == name {
			return AffectedTestSymbol{Path: p, Name: s.Name, Kind: s.Kind, Language: s.Language, StartLine: s.StartLine, EndLine: s.EndLine}
		}
	}
	t.Fatalf("missing fixture seed %s:%s: %+v", p, name, ss)
	return AffectedTestSymbol{}
}
func selectAffected(t *testing.T, f affectedFixture, in AffectedTestsInput, o AffectedTestsOptions) *AffectedTestsResult {
	t.Helper()
	r, e := FindAffectedTests(f.db, in, o)
	if e != nil {
		t.Fatal(e)
	}
	return r
}
func affectedPaths(r *AffectedTestsResult) []string {
	out := []string{}
	for _, x := range r.Tests {
		out = append(out, x.Path)
	}
	return out
}
func assertAffectedPaths(t *testing.T, r *AffectedTestsResult, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(affectedPaths(r), append([]string{}, want...)) {
		t.Fatalf("paths=%v want=%v; result=%+v", affectedPaths(r), want, r)
	}
}
func hasAffectedCode(r *AffectedTestsResult, code string) bool {
	for _, l := range r.Limitations {
		if l.Code == code {
			return true
		}
	}
	return false
}
func assertAffectedGap(t *testing.T, r *AffectedTestsResult, code string) {
	t.Helper()
	if !hasAffectedCode(r, code) || r.Status == "complete" || !r.Fallback.Recommended || !strings.Contains(r.Fallback.Reason, "full suite") {
		t.Fatalf("missing gap %q: %+v", code, r)
	}
}
func affectedEvidence(t *testing.T, x AffectedTest, kind string) AffectedTestsEvidence {
	t.Helper()
	for _, e := range x.Evidence {
		if e.Kind == kind {
			return e
		}
	}
	t.Fatalf("missing %s: %+v", kind, x)
	return AffectedTestsEvidence{}
}

func TestAffectedTestsDirectReference(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "tests/direct_test.go": "package p\nfunc TestDirect() {\n target()\n}\n", "tests/unrelated_test.go": "package p\nfunc TestUnrelated() {}\n"})
	seed := f.seed(t, "source.go", "target")
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{seed}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/direct_test.go")
	x := r.Tests[0]
	e := affectedEvidence(t, x, "direct_reference")
	if r.Status != "complete" || x.Rank != 1 || x.Confidence != "high" || *x.Distance != 1 || *e.Distance != 1 || len(e.Steps) != 2 || e.Steps[1].Path != x.Path || e.Steps[1].Line != 3 || e.Steps[1].ReferenceKind != "call" || !reflect.DeepEqual(x.ChangedSymbols, []AffectedTestSymbol{seed}) {
		t.Fatalf("incorrect direct evidence: %+v %+v", r, e)
	}
	if r.Fallback.Advisory == "" || r.Fallback.StaticAnalysis == "" || r.Fallback.Recommended {
		t.Fatalf("incorrect complete advisory: %+v", r.Fallback)
	}
}

func TestAffectedTestsTopLevelAndRealUseKinds(t *testing.T) {
	for _, tc := range []struct {
		kind, sourcePath, testPath, source, reference string
	}{
		{"call", "source.ts", "tests/top.test.ts", "export function target() {}\n", "target();\n"},
		{"use", "source.go", "tests/use_test.go", "package p\ntype target struct{}\n", "package p; var value = target{}\n"},
	} {
		t.Run(tc.kind, func(t *testing.T) {
			f := newAffectedFixture(t, map[string]string{tc.sourcePath: tc.source, tc.testPath: tc.reference})
			seed := f.seed(t, tc.sourcePath, "target")
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{seed}}, AffectedTestsOptions{})
			assertAffectedPaths(t, r, tc.testPath)
			e := affectedEvidence(t, r.Tests[0], "direct_reference")
			if len(e.Steps) != 2 || e.Steps[0].Path != seed.Path || e.Steps[1].Path != tc.testPath || e.Steps[1].Target != "target" || e.Steps[1].ReferenceKind != tc.kind || e.Steps[1].Line != 1 {
				t.Fatalf("lost real reference: %+v", e)
			}
		})
	}
}

func TestAffectedTestsTransitiveOrderedChainAndStops(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.go":           "package p\nfunc target() {}\n",
		"testutil/first.go":   "package p\nfunc first() { target() }\n",
		"prod/second.go":      "package p\nfunc second() { first() }\n",
		"tests/helpers.go":    "package p\nfunc helper() { second() }\n",
		"tests/downstream.go": "package p\nfunc downstream() { helper() }\n",
		"prod/unrelated.go":   "package p\nfunc unrelated() {}\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{Depth: 5})
	assertAffectedPaths(t, r, "tests/helpers.go")
	e := affectedEvidence(t, r.Tests[0], "transitive_reference")
	got := []string{}
	for _, s := range e.Steps {
		got = append(got, s.Path)
		if s.Line != 2 {
			t.Fatalf("incorrect location: %+v", s)
		}
	}
	if !reflect.DeepEqual(got, []string{"source.go", "testutil/first.go", "prod/second.go", "tests/helpers.go"}) || *e.Distance != 3 || r.Tests[0].Confidence != "medium" || r.Status != "complete" {
		t.Fatalf("incorrect chain: %+v %+v", r, e)
	}
}

func TestAffectedTestsSupportDedupAndRanking(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.go":       "package p\nfunc alpha() {}\nfunc beta() {}\n",
		"tests/a_test.go": "package p\nfunc TestA() { alpha() }\n",
		"tests/z_test.go": "package p\nfunc TestZ() {\n alpha()\n alpha()\n beta()\n}\n",
	})
	a, b := f.seed(t, "source.go", "alpha"), f.seed(t, "source.go", "beta")
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{a, b, a, b}}, AffectedTestsOptions{Limit: 1})
	assertAffectedPaths(t, r, "tests/z_test.go")
	x := r.Tests[0]
	if len(x.Evidence) != 3 || len(x.ChangedSymbols) != 2 || x.Rank != 1 {
		t.Fatalf("dedup/support: %+v", x)
	}
	assertAffectedGap(t, r, "result_limit")
}

func TestAffectedTestsDeterministicDiamondCycle(t *testing.T) {
	files := map[string]string{
		"source.go":       "package p\nfunc target() { cycle() }\nfunc other() {}\n",
		"a.go":            "package p\nfunc a() { target() }\n",
		"b.go":            "package p\nfunc b() { target() }\n",
		"merge.go":        "package p\nfunc merge() {\n b()\n a()\n}\nfunc cycle() { merge() }\n",
		"tests/result.go": "package p\nfunc check() { merge(); other() }\n",
	}
	f := newAffectedFixture(t, files)
	a, b := f.seed(t, "source.go", "target"), f.seed(t, "source.go", "other")
	run := func(seeds []AffectedTestSymbol) []byte {
		r := selectAffected(t, f, AffectedTestsInput{Symbols: seeds}, AffectedTestsOptions{Depth: 5})
		assertAffectedPaths(t, r, "tests/result.go")
		if hasAffectedCode(r, "depth_limit") {
			t.Fatalf("cycle falsely truncated: %+v", r)
		}
		for _, e := range r.Tests[0].Evidence {
			if e.Seed.Name == "target" && (len(e.Steps) != 4 || e.Steps[1].Path != "a.go") {
				t.Fatalf("noncanonical diamond: %+v", e)
			}
		}
		data, err := json.Marshal(r)
		if err != nil {
			t.Fatal(err)
		}
		return data
	}
	first := run([]AffectedTestSymbol{a, b, a})
	if got := run([]AffectedTestSymbol{b, a}); string(got) != string(first) {
		t.Fatalf("reversed input changes JSON:\n%s\n%s", first, got)
	}
	// Rebuild the real parser index in reverse physical insertion order. IDs must
	// not affect explanations or payloads.
	store, err := OpenStore(f.db)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = store.db.Exec("DELETE FROM files"); err != nil {
		t.Fatal(err)
	}
	store.Close()
	names := make([]string, 0, len(files))
	for p := range files {
		names = append(names, p)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(names)))
	for _, p := range names {
		if _, err := Index(f.root, f.db, Options{Workers: 1, Scope: filepath.Join(f.root, p)}); err != nil {
			t.Fatal(err)
		}
	}
	if got := run([]AffectedTestSymbol{b, a}); string(got) != string(first) {
		t.Fatalf("insertion IDs change JSON:\n%s\n%s", first, got)
	}
}

func TestAffectedTestsDiamondPrefersUnambiguousPath(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.go":       "package p\nfunc target() {}\n",
		"a.go":            "package p\nfunc left() { target() }\n",
		"collision.go":    "package p\nfunc left() {}\n",
		"z.go":            "package p\nfunc right() { target() }\n",
		"join.go":         "package p\nfunc join() {\n left()\n right()\n}\n",
		"tests/result.go": "package p\nfunc check() { join() }\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{Depth: 3})
	assertAffectedPaths(t, r, "tests/result.go")
	e := affectedEvidence(t, r.Tests[0], "transitive_reference")
	if len(e.Steps) != 4 || e.Steps[1].Path != "z.go" || e.Steps[1].Name != "right" || e.Steps[2].Name != "join" || e.Steps[3].Path != "tests/result.go" || *e.Distance != 3 || e.Ambiguous || e.Confidence != "medium" || r.Tests[0].Confidence != "medium" {
		t.Fatalf("unambiguous diamond path discarded: %+v", e)
	}
	assertAffectedGap(t, r, "ambiguous_definitions")
}

func TestAffectedTestsChangedTestStops(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"tests/helper.ts": "export function helper() {}\n", "tests/consumer.ts": "helper();\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"tests/helper.ts"}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/helper.ts")
	if *r.Tests[0].Distance != 0 || r.Tests[0].Confidence != "high" {
		t.Fatalf("changed test evidence: %+v", r.Tests[0])
	}
	affectedEvidence(t, r.Tests[0], "changed_test")
	assertAffectedGap(t, r, "file_precision")
}

func TestAffectedTestsWholeFileDiscoversAllDefinitions(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"pkg/source.go": "package p\nfunc alpha() {}\nfunc beta() {}\n", "tests/a.go": "package p\nfunc testA() { alpha() }\n", "tests/b.go": "package p\nfunc testB() { beta() }\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"pkg\\source.go", "pkg/source.go"}, Precision: "file"}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/a.go", "tests/b.go")
	for _, x := range r.Tests {
		if x.Confidence != "medium" || len(x.ChangedSymbols) != 1 {
			t.Fatalf("file precision: %+v", x)
		}
	}
	assertAffectedGap(t, r, "file_precision")
}

func TestAffectedTestsNoChangeAndNoCandidates(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc isolated() {}\n"})
	for _, in := range []AffectedTestsInput{{}, {Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "isolated")}}} {
		r := selectAffected(t, f, in, AffectedTestsOptions{})
		assertAffectedPaths(t, r)
		if r.Status != "complete" || r.SchemaVersion != 1 || r.Tests == nil || r.Limitations == nil || r.Fallback.Advisory == "" {
			t.Fatalf("complete empty result: %+v", r)
		}
		b, _ := json.Marshal(r)
		if strings.Contains(string(b), ":null") {
			t.Fatalf("empty collections must be arrays: %s", b)
		}
	}
}

func TestAffectedTestsMissingDeletedAndMismatchedDefinitions(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "other.go": "package p\nfunc other() {}\n", "tests/check.go": "package p\nfunc check() { target() }\n"})
	base := f.seed(t, "source.go", "target")
	for _, tc := range []struct {
		name, code string
		change     func(*AffectedTestSymbol)
		selected   bool
	}{
		{"wrong path", "missing_live_seed", func(s *AffectedTestSymbol) { s.Path = "other.go" }, false},
		{"missing path", "missing_live_seed", func(s *AffectedTestSymbol) { s.Path = "missing.go" }, false},
		{"wrong kind", "missing_live_seed", func(s *AffectedTestSymbol) { s.Kind = "method" }, false},
		{"wrong location", "missing_live_seed", func(s *AffectedTestSymbol) { s.StartLine++ }, false},
		{"deleted", "deleted_seed", func(s *AffectedTestSymbol) { s.Path = "old.go"; s.Deleted = true }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s := base
			tc.change(&s)
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{s}}, AffectedTestsOptions{})
			assertAffectedGap(t, r, tc.code)
			if tc.selected {
				assertAffectedPaths(t, r, "tests/check.go")
				if r.Tests[0].Confidence != "low" {
					t.Fatal("deleted seed must be uncertain")
				}
			} else {
				assertAffectedPaths(t, r)
			}
		})
	}
}

func TestAffectedTestsUpstreamLimitationsAndUnsupported(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n"})
	for _, code := range []string{"rename", "binary", "unsupported", "conflict", "unreadable", "parse_failure", "outside_symbols"} {
		t.Run(code, func(t *testing.T) {
			r := selectAffected(t, f, AffectedTestsInput{Files: []string{"asset.xyz"}, Limitations: []AffectedTestsLimitation{{Code: code, Message: "input diagnostic", Path: "asset.xyz"}}}, AffectedTestsOptions{})
			assertAffectedGap(t, r, code)
			if r.Status != "unavailable" {
				t.Fatalf("unsupported input status: %+v", r)
			}
		})
	}
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"source.go"}, Limitations: []AffectedTestsLimitation{{Code: "outside_symbols", Message: "changed package declaration", Path: "source.go"}}}, AffectedTestsOptions{})
	if r.Status != "partial" {
		t.Fatalf("analyzable supported file: %+v", r)
	}
}

func TestAffectedTestsCustomClassifierAndPaths(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"src/ü source.go": "package p\nfunc target() {}\n", "checks/# verify.go": "package p\nfunc verify() { target() }\n", "tests/beyond.go": "package p\nfunc beyond() { verify() }\n"})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "src/ü source.go", "target")}}, AffectedTestsOptions{TestPaths: []string{"checks/"}})
	assertAffectedPaths(t, r, "checks/# verify.go")
	for _, p := range []string{"../outside.go", "/absolute.go", "C:\\outside.go", ""} {
		if _, err := FindAffectedTests(f.db, AffectedTestsInput{Files: []string{p}}, AffectedTestsOptions{}); err == nil {
			t.Fatalf("accepted nonrelative path %q", p)
		}
	}
}

func TestAffectedTestsPropagatesQueryErrors(t *testing.T) {
	for _, table := range []string{"refs", "imports", "symbols", "files"} {
		t.Run(table, func(t *testing.T) {
			f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n"})
			seed := f.seed(t, "source.go", "target")
			s, err := OpenStore(f.db)
			if err != nil {
				t.Fatal(err)
			}
			// A malformed real table survives OpenStore's IF NOT EXISTS schema setup,
			// then fails the selector query. This is fault injection, not a query mock.
			if _, err := s.db.Exec(fmt.Sprintf("ALTER TABLE %s RENAME COLUMN %s TO broken", table, map[string]string{"refs": "line", "imports": "raw_path", "symbols": "end_line", "files": "rel_path"}[table])); err != nil {
				t.Fatal(err)
			}
			s.Close()
			if r, err := FindAffectedTests(f.db, AffectedTestsInput{Symbols: []AffectedTestSymbol{seed}}, AffectedTestsOptions{}); err == nil || r != nil {
				t.Fatalf("query failure became success: %+v %v", r, err)
			}
		})
	}
	p := filepath.Join(t.TempDir(), "corrupt.db")
	if err := os.WriteFile(p, []byte("not sqlite"), 0600); err != nil {
		t.Fatal(err)
	}
	if r, e := FindAffectedTests(p, AffectedTestsInput{}, AffectedTestsOptions{}); e == nil || r != nil {
		t.Fatalf("corrupt database accepted: %+v %v", r, e)
	}
}

func TestAffectedTestsHunkOutsideSymbolsAndConfigFallback(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.ts": "function target() {}\n", "tests/check.ts": "import data from '../config.json';\n"})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{{Path: "source.ts"}}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r)
	assertAffectedGap(t, r, "no_symbol_seed")
	r = selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{{Path: "config.json"}}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/check.ts")
	if r.Status != "partial" || len(r.Tests[0].ChangedSymbols) != 0 {
		t.Fatalf("config file evidence: %+v", r)
	}
	affectedEvidence(t, r.Tests[0], "direct_import")
}

func TestAffectedTestsCurrentDatabaseOnly(t *testing.T) {
	local := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n"})
	_ = newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "tests/sibling.go": "package p\nfunc check() { target() }\n"})
	r := selectAffected(t, local, AffectedTestsInput{Symbols: []AffectedTestSymbol{local.seed(t, "source.go", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r)
}

func TestAffectedTestsSupportedSymbolFreeWithoutEvidenceUnavailable(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"empty.go": "package p\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"empty.go"}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r)
	assertAffectedGap(t, r, "no_symbol_seed")
	if r.Status != "unavailable" {
		t.Fatalf("no usable seed or evidence: %+v", r)
	}
	r = selectAffected(t, f, AffectedTestsInput{Limitations: []AffectedTestsLimitation{{Code: "unreadable", Message: "Cannot read input", Path: "missing.go"}}}, AffectedTestsOptions{})
	if r.Status != "unavailable" {
		t.Fatalf("nonempty failed input: %+v", r)
	}
}

func TestAffectedTestsChangedTestHunkAttribution(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"tests/helper.ts": "function helper() {}\n", "tests/consumer.ts": "helper();\n"})
	seed := f.seed(t, "tests/helper.ts", "helper")
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{seed}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/helper.ts")
	if r.Status != "complete" || len(r.Tests[0].ChangedSymbols) != 1 || r.Tests[0].ChangedSymbols[0] != seed || *r.Tests[0].Distance != 0 {
		t.Fatalf("changed test hunk attribution: %+v", r)
	}
}

func TestAffectedTestsExplicitHunkFilesDoNotExpandUnchangedDefinitions(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc alpha() {}\nfunc beta() {}\n", "tests/a.go": "package p\nfunc a() { alpha() }\n", "tests/b.go": "package p\nfunc b() { beta() }\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"source.go"}, Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "alpha")}, Precision: "hunk"}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/a.go")
	if r.InputPrecision != "hunk" || r.Tests[0].Confidence != "high" || hasAffectedCode(r, "file_precision") {
		t.Fatalf("hunk file paths became whole-file seeds: %+v", r)
	}
}

func TestAffectedTestsSparseSeedUsesLiveIdentityWithoutDuplicateSupport(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.ts": "class Worker { run() {} }\n", "tests/check.ts": "worker.run();\n"})
	seed := f.seed(t, "source.ts", "run")
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{seed, {Path: "source.ts", Name: "run"}}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/check.ts")
	assertAffectedGap(t, r, "indirect_dispatch")
	if len(r.Tests[0].ChangedSymbols) != 1 || len(r.Tests[0].Evidence) != 1 || r.Tests[0].ChangedSymbols[0] != seed {
		t.Fatalf("sparse identity inflated support: %+v", r.Tests[0])
	}
}

func TestAffectedTestsUncontainedProductionCallIsPartial(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.ts":       "export function target() {}\n",
		"bootstrap.ts":    "target();\n",
		"tests/direct.ts": "target();\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.ts", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/direct.ts")
	assertAffectedGap(t, r, "uncontained_reference")
}

func TestAffectedTestsLocalVariableDoesNotHideCaller(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.go":     "package p\nfunc Target() int { return 1 }\n",
		"bridge.go":     "package p\nfunc Bridge() int {\n var result = Target()\n return result\n}\n",
		"check_test.go": "package p\nfunc TestBridge() { Bridge() }\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "Target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "check_test.go")
	e := affectedEvidence(t, r.Tests[0], "transitive_reference")
	if len(e.Steps) != 3 || e.Steps[1].Name != "Bridge" || e.Steps[1].Line != 3 {
		t.Fatalf("local variable hid containing function: %+v", e)
	}
}

func TestAffectedTestsOverlappingLineDefinitionsAreUncertain(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.js":  "function target() {}\n",
		"bridge.js":  "function alpha() {} function bridge() { target(); }\n",
		"tests/a.js": "alpha();\n",
		"tests/b.js": "bridge();\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.js", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/a.js")
	e := affectedEvidence(t, r.Tests[0], "transitive_reference")
	if len(e.Steps) != 3 || e.Steps[0].Path != "source.js" || e.Steps[0].Name != "target" || e.Steps[1].Path != "bridge.js" || e.Steps[1].Name != "alpha" || e.Steps[1].Target != "target" || e.Steps[1].ReferenceKind != "call" || !e.Steps[1].Ambiguous || e.Steps[2].Path != "tests/a.js" || e.Steps[2].Target != "alpha" || e.Steps[2].ReferenceKind != "call" || *e.Distance != 2 {
		t.Fatalf("lost canonical overlapping reference: %+v", e)
	}
	assertAffectedGap(t, r, "ambiguous_container")
	for _, test := range r.Tests {
		if test.Confidence != "low" || !test.Evidence[0].Ambiguous {
			t.Fatalf("same-line attribution presented as certain: %+v", test)
		}
	}
}

func TestAffectedTestsDistinctReferenceTargetsCountAsSupport(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"source.go":  "package p\nfunc target() {}\n",
		"bridges.go": "package p\nfunc left() { target() }\nfunc right() { target() }\n",
		"tests/a.go": "package p\nfunc checkA() { left() }\n",
		"tests/z.go": "package p\nfunc checkZ() { left(); right() }\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/z.go", "tests/a.go")
	if len(r.Tests[0].Evidence) != 2 {
		t.Fatalf("distinct same-line targets collapsed: %+v", r.Tests[0])
	}
}
