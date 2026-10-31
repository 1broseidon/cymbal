package index

import (
	"fmt"
	"strings"
	"testing"
)

func TestAffectedTestsImportsBarrelsAndStrengthRanking(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"leaf.ts":         "export function target() {}\n",
		"barrel.ts":       "export { target } from './leaf';\n",
		"tests/a.test.ts": "import * as api from '../barrel';\n",
		"tests/b.test.ts": "import * as api from '../leaf';\n",
		"tests/z.test.ts": "target();\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "leaf.ts", "target")}}, AffectedTestsOptions{Depth: 3})
	// Re-exports themselves can also have symbol use records. The direct call
	// must still outrank any import-only candidate, regardless of lexical path.
	if len(r.Tests) < 2 || r.Tests[0].Path != "tests/z.test.ts" {
		t.Fatalf("imports outrank direct symbol: %+v", r.Tests)
	}
	for _, x := range r.Tests {
		if x.Path == "tests/b.test.ts" {
			e := affectedEvidence(t, x, "direct_import")
			if e.Confidence != "low" || e.Steps[1].Import != "../leaf" || e.Steps[1].Target != "leaf.ts" {
				t.Fatalf("import provenance: %+v", e)
			}
		}
	}
	assertAffectedGap(t, r, "heuristic_import")
	// A symbol-free file seed must reach the consumer through real parser barrel
	// imports, including the complete file explanation.
	r = selectAffected(t, f, AffectedTestsInput{Files: []string{"leaf.json"}}, AffectedTestsOptions{Depth: 3})
	found := false
	for _, x := range r.Tests {
		if x.Path == "tests/a.test.ts" {
			e := affectedEvidence(t, x, "reverse_import")
			if *e.Distance != 2 || len(e.Steps) != 3 || e.Steps[1].Path != "barrel.ts" || e.Steps[2].Import != "../barrel" {
				t.Fatalf("barrel path: %+v", e)
			}
			found = true
		}
	}
	if !found {
		t.Fatal("missing reverse barrel candidate")
	}
}

func TestAffectedTestsLiteralAndBasenamePackageImports(t *testing.T) {
	for _, stem := range []string{"a_b", "a%b"} {
		t.Run(stem, func(t *testing.T) {
			f := newAffectedFixture(t, map[string]string{stem + ".ts": "export const untouched = 1;\n", "tests/literal.ts": "import x from '../" + stem + "';\n", "tests/collision.ts": "import x from '../axb';\n", "tests/basename.ts": "import x from '../other/" + stem + "';\n"})
			// Missing file inputs still support file-only import matching.
			r := selectAffected(t, f, AffectedTestsInput{Files: []string{stem + ".json"}}, AffectedTestsOptions{})
			assertAffectedPaths(t, r, "tests/basename.ts", "tests/literal.ts")
			for _, x := range r.Tests {
				if x.Confidence != "low" || len(x.ChangedSymbols) != 0 {
					t.Fatalf("file-only imports: %+v", x)
				}
			}
		})
	}
	f := newAffectedFixture(t, map[string]string{"pkg/leaf.go": "package p\nfunc target() {}\n", "tests/import_test.go": "package p\nimport \"example.org/repo/pkg\"\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"pkg/leaf.go"}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/import_test.go")
	e := affectedEvidence(t, r.Tests[0], "direct_import")
	if e.Steps[1].Import != "example.org/repo/pkg" {
		t.Fatalf("package candidate: %+v", e)
	}
}

func TestAffectedTestsNamingUsesActualCounterparts(t *testing.T) {
	for _, tc := range []struct {
		source, content string
		tests           []string
	}{
		{"item.go", "package p\nfunc item() {}\n", []string{"item_test.go"}},
		{"item.ts", "export function item() {}\n", []string{"item.test.jsx", "item.spec.tsx", "item.test.mjs"}},
		{"item.py", "def item():\n    pass\n", []string{"test_item.py", "item_test.py"}},
	} {
		t.Run(tc.source, func(t *testing.T) {
			files := map[string]string{tc.source: tc.content}
			for _, p := range tc.tests {
				files[p] = "// no graph evidence\n"
				if strings.HasSuffix(p, ".go") {
					files[p] = "package p\n"
				}
				if strings.HasSuffix(p, ".py") {
					files[p] = "# no graph evidence\n"
				}
			}
			f := newAffectedFixture(t, files)
			r := selectAffected(t, f, AffectedTestsInput{Files: []string{tc.source}}, AffectedTestsOptions{})
			want := append([]string{}, tc.tests...) // final lexical tie order
			for i := range want {
				for j := i + 1; j < len(want); j++ {
					if want[j] < want[i] {
						want[i], want[j] = want[j], want[i]
					}
				}
			}
			assertAffectedPaths(t, r, want...)
			for _, x := range r.Tests {
				e := affectedEvidence(t, x, "colocated")
				if x.Distance != nil || e.Distance != nil || x.Confidence != "low" {
					t.Fatalf("naming fabricated distance: %+v", x)
				}
			}
			assertAffectedGap(t, r, "heuristic_naming")
		})
	}
}

func TestAffectedTestsScopedSeedAmbiguity(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.ts": "export function target() {}\n", "other.js": "function target() {}\n", "collision.py": "def target():\n    pass\n", "tests/one.ts": "target();\n", "tests/two.js": "target();\n", "tests/three.py": "target()\n"})
	seed := f.seed(t, "source.ts", "target")
	for _, tc := range []struct {
		scope       ResolveScope
		count       int
		confidence  string
		definitions int
	}{{ResolveScopeSame, 1, "high", 1}, {ResolveScopeFamily, 2, "low", 2}, {ResolveScopeAll, 3, "low", 3}} {
		t.Run(string(tc.scope), func(t *testing.T) {
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{seed}}, AffectedTestsOptions{ResolveScope: tc.scope})
			if len(r.Tests) != tc.count {
				t.Fatalf("scope result: %+v", r)
			}
			for _, x := range r.Tests {
				if x.Confidence != tc.confidence {
					t.Fatalf("scope confidence: %+v", x)
				}
			}
			if tc.definitions > 1 {
				assertAffectedGap(t, r, "ambiguous_definitions")
				found := false
				for _, l := range r.Limitations {
					if l.Code == "ambiguous_definitions" && strings.Contains(l.Message, fmt.Sprintf("%d scoped", tc.definitions)) {
						found = true
					}
				}
				if !found {
					t.Fatalf("uncapped scoped count: %+v", r.Limitations)
				}
			} else if r.Status != "complete" {
				t.Fatalf("same scope has false ambiguity: %+v", r)
			}
		})
	}
}

func TestAffectedTestsIntermediateAmbiguity(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "a.go": "package p\nfunc helper() { target() }\n", "b.go": "package p\nfunc helper() { target() }\n", "tests/check.go": "package p\nfunc check() { helper() }\n"})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/check.go")
	if r.Tests[0].Confidence != "low" || !r.Tests[0].Evidence[0].Ambiguous || len(r.Tests[0].Evidence) != 1 || r.Tests[0].Evidence[0].Steps[1].Path != "a.go" {
		t.Fatalf("intermediate ambiguity lost: %+v", r.Tests[0])
	}
	assertAffectedGap(t, r, "ambiguous_definitions")
}

func TestAffectedTestsAmbiguityCountsAreUncapped(t *testing.T) {
	files := map[string]string{"source.go": "package p\nfunc target() {}\n", "tests/check.go": "package p\nfunc check() { target() }\n"}
	for i := 0; i < 125; i++ {
		files[fmt.Sprintf("copy%d.go", i)] = "package p\nfunc target() {}\n"
	}
	f := newAffectedFixture(t, files)
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/check.go")
	assertAffectedGap(t, r, "ambiguous_definitions")
	found := false
	for _, l := range r.Limitations {
		if strings.Contains(l.Message, "126 scoped") {
			found = true
		}
	}
	if !found {
		t.Fatalf("hidden definition cap: %+v", r.Limitations)
	}
}

func TestAffectedTestsCallbackAndInterfaceUncertainty(t *testing.T) {
	t.Run("production non-call", func(t *testing.T) {
		f := newAffectedFixture(t, map[string]string{"source.go": "package p\ntype Target struct {}\n", "prod.go": "package p\nfunc register() { _ = Target{} }\n", "tests/check.go": "package p\nfunc check() { register() }\n"})
		r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "Target")}}, AffectedTestsOptions{})
		assertAffectedPaths(t, r, "tests/check.go")
		e := affectedEvidence(t, r.Tests[0], "transitive_reference")
		if e.Steps[1].ReferenceKind != "use" || r.Tests[0].Confidence != "low" {
			t.Fatalf("callback invented call: %+v", e)
		}
		assertAffectedGap(t, r, "indirect_dispatch")
	})
	t.Run("top level non-call", func(t *testing.T) {
		f := newAffectedFixture(t, map[string]string{"source.go": "package p\ntype Target struct {}\n", "prod.go": "package p\nvar value = Target{}\n"})
		r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "Target")}}, AffectedTestsOptions{})
		assertAffectedPaths(t, r)
		assertAffectedGap(t, r, "indirect_dispatch")
	})
	t.Run("interface implements", func(t *testing.T) {
		f := newAffectedFixture(t, map[string]string{"source.ts": "export interface Runner { run(): void; }\n", "prod.ts": "class Worker implements Runner { run() {} }\n", "tests/check.ts": "new Worker();\n"})
		r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.ts", "Runner")}}, AffectedTestsOptions{Depth: 3})
		assertAffectedGap(t, r, "indirect_dispatch")
		found := false
		for _, x := range r.Tests {
			for _, e := range x.Evidence {
				for _, s := range e.Steps {
					if s.ReferenceKind == "implements" {
						found = true
					}
				}
			}
		}
		if !found {
			t.Fatalf("missing actual implements evidence: %+v", r.Tests)
		}
	})
	t.Run("method seed", func(t *testing.T) {
		f := newAffectedFixture(t, map[string]string{"source.ts": "class Worker { run() {} }\n", "tests/check.ts": "worker.run();\n"})
		r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.ts", "run")}}, AffectedTestsOptions{})
		assertAffectedGap(t, r, "indirect_dispatch")
	})
}

func TestAffectedTestsUnmodelledCallbackDoesNotInventChain(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.ts": "export function target() {}\n", "prod.ts": "function register() { consume(target); }\n", "tests/check.ts": "register();\n"})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.ts", "target")}}, AffectedTestsOptions{})
	assertAffectedPaths(t, r)
	// Bare JS callback arguments are absent from the reference schema. A finished
	// supported scan is not a semantic guarantee; the warning must remain visible.
	if !strings.Contains(r.Fallback.StaticAnalysis, "callback") || r.Fallback.Advisory == "" {
		t.Fatalf("hidden callback blind spot: %+v", r)
	}
}

func TestAffectedTestsIntermediateLanguageScope(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.ts": "function target() {}\n", "prod.ts": "function helper() { target(); }\n", "other.js": "function helper() {}\n", "other.py": "def helper():\n    pass\n", "tests/check.ts": "helper();\n", "tests/check.js": "helper();\n", "tests/check.py": "helper()\n"})
	for _, tc := range []struct {
		scope      ResolveScope
		count      int
		confidence string
	}{{ResolveScopeSame, 1, "medium"}, {ResolveScopeFamily, 2, "low"}, {ResolveScopeAll, 3, "low"}} {
		t.Run(string(tc.scope), func(t *testing.T) {
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.ts", "target")}}, AffectedTestsOptions{ResolveScope: tc.scope})
			if len(r.Tests) != tc.count {
				t.Fatalf("intermediate scope: %+v", r)
			}
			for _, x := range r.Tests {
				if x.Confidence != tc.confidence {
					t.Fatalf("intermediate scope confidence: %+v", x)
				}
			}
			if tc.count > 1 {
				assertAffectedGap(t, r, "ambiguous_definitions")
			}
		})
	}
}

func TestAffectedTestsDeterministicImportDiamondCycle(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"leftedge.ts":    "export * from './config';\nexport * from './backedge';\n",
		"rightedge.ts":   "export * from './config';\n",
		"joiner.ts":      "export * from './rightedge';\nexport * from './leftedge';\n",
		"backedge.ts":    "export * from './joiner';\n",
		"tests/check.ts": "import x from '../joiner';\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"config.json"}}, AffectedTestsOptions{Depth: 5})
	assertAffectedPaths(t, r, "tests/check.ts")
	e := affectedEvidence(t, r.Tests[0], "reverse_import")
	if *e.Distance != 3 || len(e.Steps) != 4 || e.Steps[1].Path != "leftedge.ts" || e.Steps[2].Path != "joiner.ts" || len(r.Tests[0].Evidence) != 1 || hasAffectedCode(r, "depth_limit") {
		t.Fatalf("noncanonical import diamond/cycle: %+v %+v", r, e)
	}
}

func TestAffectedTestsRankingDistanceAndAmbiguity(t *testing.T) {
	t.Run("distance", func(t *testing.T) {
		f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "first.go": "package p\nfunc first() { target() }\n", "second.go": "package p\nfunc second() { first() }\n", "tests/a.go": "package p\nfunc a() { second() }\n", "tests/z.go": "package p\nfunc z() { first() }\n"})
		r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{Depth: 3})
		assertAffectedPaths(t, r, "tests/z.go", "tests/a.go")
	})
	t.Run("ambiguity", func(t *testing.T) {
		f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc unique() {}\nfunc ambiguous() {}\n", "copy.go": "package p\nfunc ambiguous() {}\n", "tests/a.go": "package p\nfunc a() { ambiguous() }\n", "tests/z.go": "package p\nfunc z() { unique() }\n"})
		r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "ambiguous"), f.seed(t, "source.go", "unique")}}, AffectedTestsOptions{})
		assertAffectedPaths(t, r, "tests/z.go", "tests/a.go")
		if r.Tests[0].Confidence != "high" || r.Tests[1].Confidence != "low" {
			t.Fatalf("ambiguity confidence: %+v", r)
		}
	})
}

func TestAffectedTestsOutsideSymbolsKeepsIndependentFileFallback(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{
		"leaf.ts":           "import data from './config';\nexport function target() {}\n",
		"barrel.ts":         "export * from './leaf';\n",
		"tests/direct.ts":   "target();\n",
		"tests/consumer.ts": "import * as api from '../barrel';\n",
	})
	r := selectAffected(t, f, AffectedTestsInput{
		Precision: "hunk", Files: []string{"leaf.ts"},
		Symbols:     []AffectedTestSymbol{f.seed(t, "leaf.ts", "target")},
		Limitations: []AffectedTestsLimitation{{Code: "outside_symbols", Path: "leaf.ts", Message: "import line changed"}},
	}, AffectedTestsOptions{})
	assertAffectedPaths(t, r, "tests/direct.ts", "tests/consumer.ts")
	affectedEvidence(t, r.Tests[1], "reverse_import")
}
