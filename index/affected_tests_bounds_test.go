package index

import (
	"fmt"
	"testing"
)

func TestAffectedTestsSeedBounds(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			source := "package p\n"
			tests := "package p\nfunc check() {\n"
			for i := 0; i < count; i++ {
				source += fmt.Sprintf("func seed%d() {}\n", i)
				tests += fmt.Sprintf("seed%d()\n", i)
			}
			tests += "}\n"
			f := newAffectedFixture(t, map[string]string{"source.go": source, "tests/check.go": tests})
			r := selectAffected(t, f, AffectedTestsInput{Files: []string{"source.go"}}, AffectedTestsOptions{MaxSymbols: 2})
			assertAffectedPaths(t, r, "tests/check.go")
			if hasAffectedCode(r, "seed_limit") != (count > 2) {
				t.Fatalf("seed cutoff boundary: %+v", r)
			}
			want := count
			if want > 2 {
				want = 2
			}
			if len(r.Tests[0].ChangedSymbols) != want {
				t.Fatalf("seed bound did not bound work: %+v", r.Tests[0])
			}
			if count > 2 {
				assertAffectedGap(t, r, "seed_limit")
			}
		})
	}
}
func TestAffectedTestsEvidenceBounds(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			test := "package p\nfunc check() {\n"
			for i := 0; i < count; i++ {
				test += "target()\n"
			}
			test += "}\n"
			f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "tests/check.go": test})
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{MaxEvidence: 2})
			assertAffectedPaths(t, r, "tests/check.go")
			if hasAffectedCode(r, "evidence_limit") != (count > 2) {
				t.Fatalf("evidence cutoff boundary: %+v", r)
			}
			want := count
			if want > 2 {
				want = 2
			}
			if r.ExaminedEvidence != want || len(r.Tests[0].Evidence) != want {
				t.Fatalf("evidence not bounded: %+v", r)
			}
			if count > 2 {
				assertAffectedGap(t, r, "evidence_limit")
			} else if r.Status != "complete" {
				t.Fatalf("exact bound falsely incomplete: %+v", r)
			}
		})
	}
}
func TestAffectedTestsResultBounds(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			files := map[string]string{"source.go": "package p\nfunc target() {}\n"}
			for i := 0; i < count; i++ {
				files[fmt.Sprintf("tests/%d.go", i)] = "package p\nfunc check() { target() }\n"
			}
			f := newAffectedFixture(t, files)
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{Limit: 2})
			if hasAffectedCode(r, "result_limit") != (count > 2) {
				t.Fatalf("result cutoff boundary: %+v", r)
			}
			want := count
			if want > 2 {
				want = 2
			}
			if len(r.Tests) != want {
				t.Fatalf("result not bounded: %+v", r)
			}
			if count > 2 {
				assertAffectedGap(t, r, "result_limit")
			}
		})
	}
}
func TestAffectedTestsDepthBounds(t *testing.T) {
	for _, distance := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(distance), func(t *testing.T) {
			files := map[string]string{"source.go": "package p\nfunc target() {}\n"}
			prev := "target"
			for i := 1; i < distance; i++ {
				name := fmt.Sprintf("helper%d", i)
				files[name+".go"] = "package p\nfunc " + name + "() { " + prev + "() }\n"
				prev = name
			}
			files["tests/check.go"] = "package p\nfunc check() { " + prev + "() }\n"
			f := newAffectedFixture(t, files)
			r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{Depth: 2})
			if hasAffectedCode(r, "depth_limit") != (distance > 2) {
				t.Fatalf("depth cutoff boundary: %+v", r)
			}
			if distance <= 2 {
				assertAffectedPaths(t, r, "tests/check.go")
				if *r.Tests[0].Distance != distance || r.Status != "complete" {
					t.Fatalf("depth result: %+v", r)
				}
			} else {
				assertAffectedPaths(t, r)
				assertAffectedGap(t, r, "depth_limit")
			}
		})
	}
	// A production leaf exactly at the numeric boundary has no additional work.
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc target() {}\n", "leaf.go": "package p\nfunc leaf() { target() }\n"})
	r := selectAffected(t, f, AffectedTestsInput{Symbols: []AffectedTestSymbol{f.seed(t, "source.go", "target")}}, AffectedTestsOptions{Depth: 1})
	if r.Status != "complete" || hasAffectedCode(r, "depth_limit") {
		t.Fatalf("exhausted production frontier: %+v", r)
	}
}
func TestAffectedTestsImportDepthAndEvidenceBounds(t *testing.T) {
	for _, count := range []int{1, 2, 3} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			files := map[string]string{}
			prev := "config"
			for i := 1; i < count; i++ {
				name := fmt.Sprintf("barrel%d", i)
				files[name+".ts"] = "export * from './" + prev + "';\n"
				prev = name
			}
			files["tests/check.ts"] = "import * as x from '../" + prev + "';\n"
			f := newAffectedFixture(t, files)
			r := selectAffected(t, f, AffectedTestsInput{Files: []string{"config.json"}}, AffectedTestsOptions{Depth: 2, MaxEvidence: 2})
			if hasAffectedCode(r, "depth_limit") != (count > 2) {
				t.Fatalf("import frontier probe: %+v", r)
			}
			if count <= 2 {
				assertAffectedPaths(t, r, "tests/check.ts")
				if *r.Tests[0].Distance != count {
					t.Fatalf("import distance: %+v", r)
				}
			}
			if hasAffectedCode(r, "evidence_limit") {
				t.Fatalf("probe consumed evidence budget: %+v", r)
			}
		})
	}
	f := newAffectedFixture(t, map[string]string{"tests/a.ts": "import x from '../config';\n", "tests/b.ts": "import x from '../config';\n", "tests/c.ts": "import x from '../config';\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"config.json"}}, AffectedTestsOptions{MaxEvidence: 2})
	assertAffectedPaths(t, r, "tests/a.ts", "tests/b.ts")
	assertAffectedGap(t, r, "evidence_limit")
}
func TestAffectedTestsUnlimitedBudgetsAndDefaults(t *testing.T) {
	opts := DefaultAffectedTestsOptions()
	if opts.Depth != 2 || opts.Limit != 50 || opts.MaxSymbols != 40 || opts.MaxEvidence != 500 {
		t.Fatalf("defaults: %+v", opts)
	}
	source := "package p\n"
	test := "package p\nfunc check() {\n"
	for i := 0; i < 41; i++ {
		source += fmt.Sprintf("func symbol%d() {}\n", i)
		for j := 0; j < 13; j++ {
			test += fmt.Sprintf("symbol%d()\n", i)
		}
	}
	test += "}\n"
	f := newAffectedFixture(t, map[string]string{"source.go": source, "tests/check.go": test})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"source.go"}}, AffectedTestsOptions{Depth: 99})
	if r.Bounds.Depth != 5 || r.Bounds.Limit != 50 || len(r.Tests) != 1 || len(r.Tests[0].ChangedSymbols) != 41 || r.ExaminedEvidence != 533 || len(r.Tests[0].Evidence) != 533 || hasAffectedCode(r, "seed_limit") || hasAffectedCode(r, "evidence_limit") {
		t.Fatalf("zero must mean unlimited: tests=%d examined=%d limitations=%+v", len(r.Tests), r.ExaminedEvidence, r.Limitations)
	}
	for _, o := range []AffectedTestsOptions{{Depth: -1}, {Limit: -1}, {MaxSymbols: -1}, {MaxEvidence: -1}} {
		if _, err := FindAffectedTests(f.db, AffectedTestsInput{}, o); err == nil {
			t.Fatalf("accepted negative bound %+v", o)
		}
	}
}

func TestAffectedTestsGlobalEvidenceBudgetPrioritizesSymbols(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"z.ts": "function target() {}\n", "tests/a.ts": "import data from '../config';\n", "tests/z.ts": "target();\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"config.json", "z.ts"}}, AffectedTestsOptions{MaxEvidence: 1})
	assertAffectedPaths(t, r, "tests/z.ts")
	assertAffectedGap(t, r, "evidence_limit")
	if r.ExaminedEvidence != 1 {
		t.Fatalf("not a global budget: %+v", r)
	}
}

func TestAffectedTestsGlobalEvidenceBudgetAcrossSeeds(t *testing.T) {
	f := newAffectedFixture(t, map[string]string{"source.go": "package p\nfunc alpha() {}\nfunc beta() {}\n", "tests/check.go": "package p\nfunc check() {\nalpha()\nalpha()\nbeta()\nbeta()\n}\n"})
	r := selectAffected(t, f, AffectedTestsInput{Files: []string{"source.go"}}, AffectedTestsOptions{MaxEvidence: 3})
	assertAffectedPaths(t, r, "tests/check.go")
	assertAffectedGap(t, r, "evidence_limit")
	if r.ExaminedEvidence != 3 || len(r.Tests[0].Evidence) != 3 || len(r.Tests[0].ChangedSymbols) != 2 {
		t.Fatalf("per-seed rather than global evidence budget: %+v", r)
	}
}
