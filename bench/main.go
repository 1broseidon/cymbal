// bench/main.go — Benchmark harness for cymbal.
//
// Measures: speed, accuracy, token efficiency, JIT freshness, agent workflow savings.
//
// Usage:
//
//	go run ./bench setup             — clone corpus repos into bench/.corpus/
//	go run ./bench run               — execute benchmarks, write RESULTS.md + results.json
//	go run ./bench update-baseline   — run benchmarks and save as baseline.json
//	go run ./bench check             — run benchmarks and compare against baseline.json
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

// ── Corpus config ──────────────────────────────────────────────────

type Corpus struct {
	Repos []Repo `yaml:"repos"`
}

type Repo struct {
	Name       string                 `yaml:"name"`
	URL        string                 `yaml:"url"`
	Ref        string                 `yaml:"ref"`
	Language   string                 `yaml:"language"`
	Tier       string                 `yaml:"tier"`
	Complexity string                 `yaml:"complexity"`
	Subset     string                 `yaml:"subset"`
	Tags       []string               `yaml:"tags"`
	Symbols    []Symbol               `yaml:"symbols"`
	Footguns   []FootgunCase          `yaml:"footguns"`
	MultiCases []GroundTruthMultiCase `yaml:"multi_cases"`
}



type Symbol struct {
	Name                string           `yaml:"name"`
	FileContains        string           `yaml:"file_contains"`
	Kind                string           `yaml:"kind"`
	ShowContains        string           `yaml:"show_contains"`
	RefsMin             int              `yaml:"refs_min"`
	GroundTruth         *GroundTruthSpec `yaml:"ground_truth"`
	SearchContains      []string         `yaml:"search_contains"`
	SearchExcludes      []string         `yaml:"search_excludes"`
	ShowContainsAll     []string         `yaml:"show_contains_all"`
	ShowExcludes        []string         `yaml:"show_excludes"`
	InvestigateContains []string         `yaml:"investigate_contains"`
	InvestigateExcludes []string         `yaml:"investigate_excludes"`
	RefsContains        []string         `yaml:"refs_contains"`
	RefsExcludes        []string         `yaml:"refs_excludes"`
}

type FootgunCase struct {
	Name                string   `yaml:"name"`
	Symbol              string   `yaml:"symbol"`
	Op                  Op       `yaml:"op"`
	Why                 string   `yaml:"why"`
	GrepNoiseMin        int      `yaml:"grep_noise_min"`
	CymbalContains      []string `yaml:"cymbal_contains"`
	CymbalExcludes      []string `yaml:"cymbal_excludes"`
	CymbalMaxMatches    int      `yaml:"cymbal_max_matches"`
	ExpectSmallerOutput bool     `yaml:"expect_smaller_output"`
}

// ── Tool abstraction ───────────────────────────────────────────────

type Op string

const (
	OpIndex       Op = "index"
	OpReindex     Op = "reindex"
	OpSearch      Op = "search"
	OpRefs        Op = "refs"
	OpShow        Op = "show"
	OpInvestigate Op = "investigate"
	OpImpls       Op = "impls"
	OpTrace       Op = "trace"
	OpGraph       Op = "graph"
)

type Tool struct {
	Name    string
	Binary  string
	Ops     map[Op]CmdFunc
	Cleanup func(repoDir string)
}

type CmdFunc func(repoDir, symbol string) *exec.Cmd

// ── Results ────────────────────────────────────────────────────────

type Result struct {
	Tool    string
	Repo    string
	Op      Op
	Symbol  string
	Timings []time.Duration
	Output  int    // bytes of output
	RawOut  string // captured output for accuracy checks
}

func (r Result) Median() time.Duration {
	s := make([]time.Duration, len(r.Timings))
	copy(s, r.Timings)
	sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
	return s[len(s)/2]
}

type AccuracyCheck struct {
	Repo    string
	Symbol  string
	Op      Op
	Passed  bool
	Details string
}

type FreshnessResult struct {
	Repo     string
	Scenario string
	Latency  time.Duration
	FilesHit int
}

type FootgunResult struct {
	Repo        string
	Name        string
	Symbol      string
	Op          Op
	Passed      bool
	GrepHits    int
	GrepBytes   int
	CymbalHits  int
	CymbalBytes int
	Details     string
}

type WorkflowResult struct {
	Repo          string
	Symbol        string
	CymbalCalls   int
	CymbalBytes   int
	BaselineCalls int
	BaselineBytes int
}

// ── JSON output / baseline ────────────────────────────────────────

type BenchReport struct {
	Timestamp   string              `json:"timestamp"`
	Platform    string              `json:"platform"`
	CPUs        int                 `json:"cpus"`
	Entries     map[string]OpTiming `json:"entries"`
	Accuracy    AccuracySummary     `json:"accuracy"`
	GroundTruth GroundTruthSummary  `json:"ground_truth"`
	Canonical   CanonicalSummary    `json:"canonical"`
	Footguns    AccuracySummary     `json:"footguns"`
}

type OpTiming struct {
	MedianMs    float64 `json:"median_ms"`
	OutputBytes int     `json:"output_bytes"`
}

type AccuracySummary struct {
	Passed int `json:"passed"`
	Total  int `json:"total"`
}

// Regression thresholds (ratio above baseline that triggers failure).
// Generous to avoid flaky results — 3x for indexing (I/O-heavy), 2x for queries.
const (
	indexThreshold = 3.0
	queryThreshold = 2.0
)

func entryKey(repo, tool string, op Op, symbol string) string {
	if symbol == "" {
		return fmt.Sprintf("%s/%s/%s", repo, tool, op)
	}
	return fmt.Sprintf("%s/%s/%s/%s", repo, tool, op, symbol)
}

func buildReport(results []Result, accuracy []AccuracyCheck, groundTruth GroundTruthSummary, canonical CanonicalSummary, footguns []FootgunResult) *BenchReport {
	report := &BenchReport{
		Timestamp: time.Now().UTC().Format(time.RFC3339),
		Platform:  fmt.Sprintf("%s/%s", runtime.GOOS, runtime.GOARCH),
		CPUs:      runtime.NumCPU(),
		Entries:   make(map[string]OpTiming),
	}
	for _, r := range results {
		key := entryKey(r.Repo, r.Tool, r.Op, r.Symbol)
		report.Entries[key] = OpTiming{
			MedianMs:    float64(r.Median().Microseconds()) / 1000.0,
			OutputBytes: r.Output,
		}
	}
	passed := 0
	for _, a := range accuracy {
		if a.Passed {
			passed++
		}
	}
	report.Accuracy = AccuracySummary{Passed: passed, Total: len(accuracy)}
	report.GroundTruth = groundTruth
	report.Canonical = canonical

	footgunPassed := 0
	for _, f := range footguns {
		if f.Passed {
			footgunPassed++
		}
	}
	report.Footguns = AccuracySummary{Passed: footgunPassed, Total: len(footguns)}
	return report
}

func writeJSON(report *BenchReport, path string) error {
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

func loadBaseline(path string) (*BenchReport, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading baseline: %w (run: go run ./bench update-baseline)", err)
	}
	var report BenchReport
	if err := json.Unmarshal(data, &report); err != nil {
		return nil, fmt.Errorf("parsing baseline: %w", err)
	}
	return &report, nil
}

func compareResults(current, baseline *BenchReport) (bool, string) {
	var b strings.Builder
	allPassed := true

	keys := make([]string, 0, len(baseline.Entries))
	for k := range baseline.Entries {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	fmt.Fprintf(&b, "  %-6s | %-45s | %10s | %10s | %s\n", "STATUS", "KEY", "BASELINE", "CURRENT", "RATIO")
	fmt.Fprintf(&b, "  %s\n", strings.Repeat("-", 90))

	for _, key := range keys {
		base := baseline.Entries[key]
		cur, ok := current.Entries[key]
		if !ok {
			continue // new repos not in baseline — skip
		}
		if base.MedianMs == 0 {
			continue
		}

		threshold := queryThreshold
		if strings.Contains(key, "/index") || strings.Contains(key, "/reindex") {
			threshold = indexThreshold
		}
		ratio := cur.MedianMs / base.MedianMs
		status := "PASS"
		if ratio > threshold {
			status = "FAIL"
			allPassed = false
		}
		fmt.Fprintf(&b, "  %-6s | %-45s | %8.1fms | %8.1fms | %.2fx\n",
			status, key, base.MedianMs, cur.MedianMs, ratio)
	}

	// Check accuracy regression
	if current.Accuracy.Passed < baseline.Accuracy.Passed {
		fmt.Fprintf(&b, "  FAIL   | accuracy regression: %d/%d -> %d/%d\n",
			baseline.Accuracy.Passed, baseline.Accuracy.Total,
			current.Accuracy.Passed, current.Accuracy.Total)
		allPassed = false
	}
	if baseline.GroundTruth.Total > 0 {
		if current.GroundTruth.Passed < baseline.GroundTruth.Passed {
			fmt.Fprintf(&b, "  FAIL   | ground-truth regression: %d/%d -> %d/%d\n",
				baseline.GroundTruth.Passed, baseline.GroundTruth.Total,
				current.GroundTruth.Passed, current.GroundTruth.Total)
			allPassed = false
		}
		if current.GroundTruth.SearchPrecision+0.001 < baseline.GroundTruth.SearchPrecision {
			fmt.Fprintf(&b, "  FAIL   | search precision regression: %.1f%% -> %.1f%%\n",
				baseline.GroundTruth.SearchPrecision, current.GroundTruth.SearchPrecision)
			allPassed = false
		}
		if current.GroundTruth.SearchRecall+0.001 < baseline.GroundTruth.SearchRecall {
			fmt.Fprintf(&b, "  FAIL   | search recall regression: %.1f%% -> %.1f%%\n",
				baseline.GroundTruth.SearchRecall, current.GroundTruth.SearchRecall)
			allPassed = false
		}
		if current.GroundTruth.RefsPrecision+0.001 < baseline.GroundTruth.RefsPrecision {
			fmt.Fprintf(&b, "  FAIL   | refs precision regression: %.1f%% -> %.1f%%\n",
				baseline.GroundTruth.RefsPrecision, current.GroundTruth.RefsPrecision)
			allPassed = false
		}
		if current.GroundTruth.RefsRecall+0.001 < baseline.GroundTruth.RefsRecall {
			fmt.Fprintf(&b, "  FAIL   | refs recall regression: %.1f%% -> %.1f%%\n",
				baseline.GroundTruth.RefsRecall, current.GroundTruth.RefsRecall)
			allPassed = false
		}
		if current.GroundTruth.ShowExactRate+0.001 < baseline.GroundTruth.ShowExactRate {
			fmt.Fprintf(&b, "  FAIL   | show exactness regression: %.1f%% -> %.1f%%\n",
				baseline.GroundTruth.ShowExactRate, current.GroundTruth.ShowExactRate)
			allPassed = false
		}
	}
	if baseline.Canonical.Total > 0 {
		if current.Canonical.Passed < baseline.Canonical.Passed {
			fmt.Fprintf(&b, "  FAIL   | canonical regression: %d/%d -> %d/%d\n",
				baseline.Canonical.Passed, baseline.Canonical.Total,
				current.Canonical.Passed, current.Canonical.Total)
			allPassed = false
		}
		if current.Canonical.SearchTop1Rate+0.001 < baseline.Canonical.SearchTop1Rate {
			fmt.Fprintf(&b, "  FAIL   | canonical search@1 regression: %.1f%% -> %.1f%%\n",
				baseline.Canonical.SearchTop1Rate, current.Canonical.SearchTop1Rate)
			allPassed = false
		}
		if current.Canonical.SearchMRR+0.001 < baseline.Canonical.SearchMRR {
			fmt.Fprintf(&b, "  FAIL   | canonical search MRR regression: %.2f -> %.2f\n",
				baseline.Canonical.SearchMRR, current.Canonical.SearchMRR)
			allPassed = false
		}
		if current.Canonical.ShowExactRate+0.001 < baseline.Canonical.ShowExactRate {
			fmt.Fprintf(&b, "  FAIL   | canonical show exact regression: %.1f%% -> %.1f%%\n",
				baseline.Canonical.ShowExactRate, current.Canonical.ShowExactRate)
			allPassed = false
		}
	}
	if current.Footguns.Passed < baseline.Footguns.Passed {
		fmt.Fprintf(&b, "  FAIL   | footgun regression: %d/%d -> %d/%d\n",
			baseline.Footguns.Passed, baseline.Footguns.Total,
			current.Footguns.Passed, current.Footguns.Total)
		allPassed = false
	}

	return allPassed, b.String()
}

// ── Tool definitions ───────────────────────────────────────────────

func cymbalDBPath(repoDir string) string {
	abs, _ := filepath.Abs(repoDir)
	h := sha256.Sum256([]byte(abs))
	home, _ := os.UserCacheDir()
	if home == "" {
		home, _ = os.UserHomeDir()
		if home != "" {
			home = filepath.Join(home, ".cymbal")
		}
	} else {
		home = filepath.Join(home, "cymbal")
	}
	return filepath.Join(home, "repos", hex.EncodeToString(h[:8]), "index.db")
}

func defineTools(cymbalBin string) []Tool {
	return []Tool{
		{
			Name:   "cymbal",
			Binary: cymbalBin,
			Ops: map[Op]CmdFunc{
				OpIndex: func(dir, _ string) *exec.Cmd {
					return exec.Command(cymbalBin, "index", ".")
				},
				OpReindex: func(dir, _ string) *exec.Cmd {
					return exec.Command(cymbalBin, "index", ".")
				},
				OpSearch: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "search", sym)
				},
				OpRefs: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "refs", sym)
				},
				OpShow: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "show", sym)
				},
				OpInvestigate: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "investigate", sym)
				},
				OpImpls: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "impls", sym)
				},
				OpTrace: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "trace", sym)
				},
				// OpGraph times the --graph render path on the trace verb.
				// Graph output is no longer a separate subcommand; it's a
				// render flag on edge-producing verbs.
				OpGraph: func(dir, sym string) *exec.Cmd {
					return exec.Command(cymbalBin, "trace", sym, "--graph", "--graph-format", "json")
				},
			},
			Cleanup: func(dir string) {
				os.Remove(cymbalDBPath(dir))
			},
		},
		{
			Name:   "ripgrep",
			Binary: "rg",
			Ops: map[Op]CmdFunc{
				OpSearch: func(dir, sym string) *exec.Cmd {
					return exec.Command("rg", "--no-heading", "-c", sym)
				},
				OpRefs: func(dir, sym string) *exec.Cmd {
					return exec.Command("rg", "--no-heading", "-n", sym)
				},
				OpShow: func(dir, sym string) *exec.Cmd {
					pattern := "(?:def |func |class |type |interface |struct |async def )" + sym
					return exec.Command("rg", "--no-heading", "-n", "-A", "30", pattern)
				},
				// rg parallel for impls: a regex that tries to catch `: Sym`, `extends Sym`,
				// `implements Sym`, `impl Sym for`, `: Sym,` — the classic grep-for-conformance
				// shotgun the Swift agent complained about. Deliberately broad so we can
				// measure how much noise real codebases surface.
				OpImpls: func(dir, sym string) *exec.Cmd {
					pattern := `(?::|extends|implements|impl)\s+` + regexp.QuoteMeta(sym) + `\b`
					return exec.Command("rg", "--no-heading", "-n", "-P", pattern)
				},
			},
		},
	}
}

// ── Core benchmark logic ───────────────────────────────────────────

const (
	indexIters = 3
	queryIters = 5
	warmup     = 1
)

func timeCmd(cmd *exec.Cmd) (time.Duration, []byte, error) {
	start := time.Now()
	out, err := cmd.CombinedOutput()
	return time.Since(start), out, err
}

type preRun func()

func runBench(tool Tool, op Op, repoDir, symbol string, iters int, before ...preRun) Result {
	r := Result{
		Tool:   tool.Name,
		Repo:   filepath.Base(repoDir),
		Op:     op,
		Symbol: symbol,
	}

	for i := 0; i < warmup; i++ {
		for _, fn := range before {
			fn()
		}
		cmd := tool.Ops[op](repoDir, symbol)
		cmd.Dir = repoDir
		cmd.Run()
	}

	for i := 0; i < iters; i++ {
		for _, fn := range before {
			fn()
		}
		cmd := tool.Ops[op](repoDir, symbol)
		cmd.Dir = repoDir
		d, out, err := timeCmd(cmd)
		// rg exits 1 on "no matches", which is a valid result for ripgrep's
		// imitation of impls and for OpSearch/OpRefs. Don't warn on those.
		if err != nil && op != OpSearch && op != OpRefs && !(tool.Name == "ripgrep" && op == OpImpls) {
			fmt.Fprintf(os.Stderr, "  WARN: %s %s %s %s: %v\n", tool.Name, op, r.Repo, symbol, err)
		}
		r.Timings = append(r.Timings, d)
		r.Output = len(out)
		r.RawOut = string(out) // keep last run for accuracy
	}
	return r
}

// ── Accuracy checks ────────────────────────────────────────────────

func containsAll(out string, needles []string) bool {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if !strings.Contains(out, needle) {
			return false
		}
	}
	return true
}

func firstMissing(out string, needles []string) string {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if !strings.Contains(out, needle) {
			return needle
		}
	}
	return ""
}

func firstUnexpected(out string, needles []string) string {
	for _, needle := range needles {
		if needle == "" {
			continue
		}
		if strings.Contains(out, needle) {
			return needle
		}
	}
	return ""
}

func countStructuredMatches(out string) int {
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if !strings.Contains(line, "_count:") {
			continue
		}
		parts := strings.SplitN(line, ":", 2)
		if len(parts) != 2 {
			continue
		}
		n, err := strconv.Atoi(strings.TrimSpace(parts[1]))
		if err == nil {
			return n
		}
	}
	return 0
}

func countIndicatorLines(out string) int {
	lines := strings.Split(strings.TrimSpace(out), "\n")
	count := 0
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || line == "---" || strings.HasPrefix(line, "query:") || strings.HasPrefix(line, "symbol:") || strings.HasPrefix(line, "file:") || strings.HasPrefix(line, "kind:") {
			continue
		}
		if strings.Contains(line, ">") || strings.Contains(line, ":") {
			count++
		}
	}
	return count
}

func accuracyPass(out string, contains []string, excludes []string) (bool, string) {
	if missing := firstMissing(out, contains); missing != "" {
		return false, fmt.Sprintf("missing %q", missing)
	}
	if unexpected := firstUnexpected(out, excludes); unexpected != "" {
		return false, fmt.Sprintf("unexpected %q", unexpected)
	}
	return true, ""
}

func checkAccuracy(results []Result, repos []Repo) []AccuracyCheck {
	var checks []AccuracyCheck

	for _, repo := range repos {
		for _, sym := range repo.Symbols {
			// Check search
			if r := findResult2(results, "cymbal", OpSearch, repo.Name, sym.Name); r != nil {
				contains := append([]string{}, sym.SearchContains...)
				if len(contains) == 0 {
					contains = []string{sym.FileContains, sym.Kind}
				}
				passed, detail := accuracyPass(r.RawOut, contains, sym.SearchExcludes)
				checks = append(checks, AccuracyCheck{
					Repo: repo.Name, Symbol: sym.Name, Op: OpSearch,
					Passed: passed, Details: detail,
				})
			}

			// Check show
			if r := findResult2(results, "cymbal", OpShow, repo.Name, sym.Name); r != nil {
				contains := append([]string{}, sym.ShowContainsAll...)
				if sym.ShowContains != "" {
					contains = append(contains, sym.ShowContains)
				}
				passed, detail := accuracyPass(r.RawOut, contains, sym.ShowExcludes)
				checks = append(checks, AccuracyCheck{
					Repo: repo.Name, Symbol: sym.Name, Op: OpShow,
					Passed: passed, Details: detail,
				})
			}

			// Check refs
			if sym.RefsMin > 0 {
				if r := findResult2(results, "cymbal", OpRefs, repo.Name, sym.Name); r != nil {
					refLines := countStructuredMatches(r.RawOut)
					if refLines == 0 {
						refLines = countIndicatorLines(r.RawOut)
					}
					passed, detail := accuracyPass(r.RawOut, sym.RefsContains, sym.RefsExcludes)
					if passed && refLines < sym.RefsMin {
						passed = false
						detail = fmt.Sprintf("expected >=%d refs, got %d", sym.RefsMin, refLines)
					}
					checks = append(checks, AccuracyCheck{
						Repo: repo.Name, Symbol: sym.Name, Op: OpRefs,
						Passed: passed, Details: detail,
					})
				}
			}

			// Check investigate
			if r := findResult2(results, "cymbal", OpInvestigate, repo.Name, sym.Name); r != nil {
				contains := append([]string{}, sym.InvestigateContains...)
				if len(contains) == 0 && sym.ShowContains != "" {
					contains = []string{sym.ShowContains}
				}
				passed, detail := accuracyPass(r.RawOut, contains, sym.InvestigateExcludes)
				checks = append(checks, AccuracyCheck{
					Repo: repo.Name, Symbol: sym.Name, Op: OpInvestigate,
					Passed: passed, Details: detail,
				})
			}
		}
	}
	return checks
}

func grepCmd(op Op, symbol string) *exec.Cmd {
	switch op {
	case OpRefs, OpSearch:
		return exec.Command("rg", "--no-heading", "-n", symbol)
	case OpShow:
		pattern := "(?:def |func |class |type |interface |struct |async def )" + symbol
		return exec.Command("rg", "--no-heading", "-n", "-A", "30", pattern)
	case OpInvestigate:
		return exec.Command("rg", "--no-heading", "-n", symbol)
	default:
		return exec.Command("rg", "--no-heading", "-n", symbol)
	}
}

func cymbalCmd(cymbalBin string, op Op, symbol string) *exec.Cmd {
	switch op {
	case OpSearch:
		return exec.Command(cymbalBin, "search", symbol)
	case OpRefs:
		return exec.Command(cymbalBin, "refs", symbol)
	case OpShow:
		return exec.Command(cymbalBin, "show", symbol)
	case OpInvestigate:
		return exec.Command(cymbalBin, "investigate", symbol)
	default:
		return exec.Command(cymbalBin, "search", symbol)
	}
}

func outputMatchCount(op Op, out string) int {
	if count := countStructuredMatches(out); count > 0 {
		return count
	}
	if op == OpShow {
		if strings.TrimSpace(out) == "" {
			return 0
		}
		return 1
	}
	return countIndicatorLines(out)
}

func benchFootguns(cymbalBin string, repos []Repo, corpusDir string) []FootgunResult {
	var results []FootgunResult

	for _, repo := range repos {
		dir := filepath.Join(corpusDir, repo.Name)
		for _, fc := range repo.Footguns {
			op := fc.Op
			if op == "" {
				op = OpSearch
			}

			rgCmd := grepCmd(op, fc.Symbol)
			rgCmd.Dir = dir
			rgOut, _ := rgCmd.CombinedOutput()

			cyCmd := cymbalCmd(cymbalBin, op, fc.Symbol)
			cyCmd.Dir = dir
			cyOut, cyErr := cyCmd.CombinedOutput()

			grepHits := countIndicatorLines(string(rgOut))
			cymbalHits := outputMatchCount(op, string(cyOut))

			passed := true
			detail := ""
			if cyErr != nil {
				passed = false
				detail = cyErr.Error()
			}
			if passed && fc.GrepNoiseMin > 0 && grepHits < fc.GrepNoiseMin {
				passed = false
				detail = fmt.Sprintf("grep noise too low: expected >=%d hits, got %d", fc.GrepNoiseMin, grepHits)
			}
			if passed {
				passed, detail = accuracyPass(string(cyOut), fc.CymbalContains, fc.CymbalExcludes)
			}
			if passed && fc.CymbalMaxMatches > 0 && cymbalHits > fc.CymbalMaxMatches {
				passed = false
				detail = fmt.Sprintf("expected <=%d cymbal matches, got %d", fc.CymbalMaxMatches, cymbalHits)
			}
			if passed && fc.ExpectSmallerOutput && len(cyOut) >= len(rgOut) {
				passed = false
				detail = fmt.Sprintf("expected cymbal output smaller than grep (%dB >= %dB)", len(cyOut), len(rgOut))
			}

			results = append(results, FootgunResult{
				Repo:        repo.Name,
				Name:        fc.Name,
				Symbol:      fc.Symbol,
				Op:          op,
				Passed:      passed,
				GrepHits:    grepHits,
				GrepBytes:   len(rgOut),
				CymbalHits:  cymbalHits,
				CymbalBytes: len(cyOut),
				Details:     detail,
			})
		}
	}

	return results
}

// ── JIT Freshness benchmark ────────────────────────────────────────

func benchFreshness(cymbalBin string, repos []Repo, corpusDir string) []FreshnessResult {
	var results []FreshnessResult

	for _, repo := range repos {
		dir := filepath.Join(corpusDir, repo.Name)
		sym := repo.Symbols[0].Name

		// Ensure indexed first
		cmd := exec.Command(cymbalBin, "index", ".", "--force")
		cmd.Dir = dir
		cmd.Run()

		// Scenario 1: hot (nothing changed)
		for i := 0; i < warmup; i++ {
			c := exec.Command(cymbalBin, "search", sym)
			c.Dir = dir
			c.Run()
		}
		d := medianOf(func() time.Duration {
			c := exec.Command(cymbalBin, "search", sym)
			c.Dir = dir
			start := time.Now()
			c.Run()
			return time.Since(start)
		}, 5)
		results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "hot (no changes)", Latency: d, FilesHit: 0})

		// Scenario 2: touch 1 file
		files := findSourceFiles(dir, 5)
		if len(files) >= 1 {
			touch(files[0])
			d = singleTimed(func() {
				c := exec.Command(cymbalBin, "search", sym)
				c.Dir = dir
				c.Run()
			})
			results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "1 file touched", Latency: d, FilesHit: 1})
		}

		// Scenario 3: touch 5 files
		if len(files) >= 5 {
			for _, f := range files[:5] {
				touch(f)
			}
			d = singleTimed(func() {
				c := exec.Command(cymbalBin, "search", sym)
				c.Dir = dir
				c.Run()
			})
			results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "5 files touched", Latency: d, FilesHit: 5})
		}

		// Scenario 4: delete + query (prune)
		if len(files) >= 1 {
			// Create a temp file, index it, then delete
			tmpFile := filepath.Join(dir, "_bench_tmp_delete_test.go")
			os.WriteFile(tmpFile, []byte("package main\nfunc BenchDeleteTest() {}\n"), 0644)
			c := exec.Command(cymbalBin, "index", ".")
			c.Dir = dir
			c.Run()
			os.Remove(tmpFile)
			d = singleTimed(func() {
				c := exec.Command(cymbalBin, "search", sym)
				c.Dir = dir
				c.Run()
			})
			results = append(results, FreshnessResult{Repo: repo.Name, Scenario: "1 file deleted (prune)", Latency: d, FilesHit: 1})
		}
	}
	return results
}

func findSourceFiles(dir string, n int) []string {
	var files []string
	filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		ext := filepath.Ext(path)
		switch ext {
		case ".go", ".py", ".js", ".ts", ".rs", ".java", ".rb", ".c", ".h":
			files = append(files, path)
		}
		return nil
	})
	if len(files) > n {
		return files[:n]
	}
	return files
}

func touch(path string) {
	now := time.Now()
	os.Chtimes(path, now, now)
}

func medianOf(fn func() time.Duration, n int) time.Duration {
	var times []time.Duration
	for i := 0; i < n; i++ {
		times = append(times, fn())
	}
	sort.Slice(times, func(i, j int) bool { return times[i] < times[j] })
	return times[len(times)/2]
}

func singleTimed(fn func()) time.Duration {
	start := time.Now()
	fn()
	return time.Since(start)
}

// ── Agent workflow comparison ──────────────────────────────────────

func benchWorkflow(cymbalBin string, repos []Repo, corpusDir string) []WorkflowResult {
	var results []WorkflowResult

	for _, repo := range repos {
		dir := filepath.Join(corpusDir, repo.Name)

		for _, sym := range repo.Symbols {
			// cymbal: 1 call = investigate
			cmd := exec.Command(cymbalBin, "investigate", sym.Name)
			cmd.Dir = dir
			cymOut, _ := cmd.CombinedOutput()

			// baseline: 3 calls = rg search + rg show + rg refs
			var baselineBytes int
			baselineCalls := 0

			cmd = exec.Command("rg", "--no-heading", "-c", sym.Name)
			cmd.Dir = dir
			out, _ := cmd.CombinedOutput()
			baselineBytes += len(out)
			baselineCalls++

			pattern := "(?:def |func |class |type |interface |struct |async def )" + sym.Name
			cmd = exec.Command("rg", "--no-heading", "-n", "-A", "30", pattern)
			cmd.Dir = dir
			out, _ = cmd.CombinedOutput()
			baselineBytes += len(out)
			baselineCalls++

			cmd = exec.Command("rg", "--no-heading", "-n", sym.Name)
			cmd.Dir = dir
			out, _ = cmd.CombinedOutput()
			baselineBytes += len(out)
			baselineCalls++

			results = append(results, WorkflowResult{
				Repo:          repo.Name,
				Symbol:        sym.Name,
				CymbalCalls:   1,
				CymbalBytes:   len(cymOut),
				BaselineCalls: baselineCalls,
				BaselineBytes: baselineBytes,
			})
		}
	}
	return results
}

// ── Setup command ──────────────────────────────────────────────────

func cmdSetup(corpus Corpus, corpusDir string) error {
	if err := os.MkdirAll(corpusDir, 0o755); err != nil {
		return err
	}

	for _, repo := range corpus.Repos {
		dest := filepath.Join(corpusDir, repo.Name)
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("  %s: already cloned\n", repo.Name)
			continue
		}
		fmt.Printf("  %s: cloning %s @ %s ...\n", repo.Name, repo.URL, repo.Ref)
		cmd := exec.Command("git", "clone", "--depth=1", "--branch", repo.Ref, repo.URL, dest)
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("cloning %s: %w", repo.Name, err)
		}
	}

	fmt.Println("\nCorpus ready.")
	return nil
}

// ── Run command ────────────────────────────────────────────────────

type benchOutput struct {
	results            []Result
	available          []Tool
	accuracy           []AccuracyCheck
	groundTruth        []GroundTruthCheck
	groundTruthSummary GroundTruthSummary
	canonical          []CanonicalCaseResult
	canonicalSummary   CanonicalSummary
	footguns           []FootgunResult
	freshness          []FreshnessResult
	workflows          []WorkflowResult
	report             *BenchReport
}

func executeBench(corpus Corpus, corpusDir, cymbalBin string) (*benchOutput, error) {
	available, err := selectAvailableTools(cymbalBin)
	if err != nil {
		return nil, err
	}

	results, err := runPhase1Speed(corpus, corpusDir, available)
	if err != nil {
		return nil, err
	}

	accuracy := runPhase2Accuracy(results, corpus.Repos)
	groundTruth, groundTruthSummary := runPhase3GroundTruth(cymbalBin, corpus.Repos, corpusDir)
	canonical, canonicalSummary := runPhase4Canonical(cymbalBin, corpus.Repos, corpusDir)
	footguns := runPhase5Footguns(cymbalBin, corpus.Repos, corpusDir)
	freshness := runPhase6Freshness(cymbalBin, corpus.Repos, corpusDir)
	workflows := runPhase7Workflow(cymbalBin, corpus.Repos, corpusDir)

	return &benchOutput{
		results:            results,
		available:          available,
		accuracy:           accuracy,
		groundTruth:        groundTruth,
		groundTruthSummary: groundTruthSummary,
		canonical:          canonical,
		canonicalSummary:   canonicalSummary,
		footguns:           footguns,
		freshness:          freshness,
		workflows:          workflows,
		report:             buildReport(results, accuracy, groundTruthSummary, canonicalSummary, footguns),
	}, nil
}

// selectAvailableTools probes PATH for each tool's binary and returns the
// subset that's installed. Tools skip with a SKIP message rather than
// failing — a partial bench is still useful.
func selectAvailableTools(cymbalBin string) ([]Tool, error) {
	tools := defineTools(cymbalBin)
	var available []Tool
	for _, t := range tools {
		if _, err := exec.LookPath(t.Binary); err != nil {
			fmt.Fprintf(os.Stderr, "  SKIP: %s not found (%s)\n", t.Name, t.Binary)
			continue
		}
		available = append(available, t)
	}
	if len(available) == 0 {
		return nil, fmt.Errorf("no tools available")
	}
	return available, nil
}

// runPhase1Speed times index/reindex and per-symbol query ops across every
// (repo, tool) pair. The returned []Result feeds both the accuracy phase and
// the token-efficiency tables in the final report.
func runPhase1Speed(corpus Corpus, corpusDir string, available []Tool) ([]Result, error) {
	fmt.Println("\n=== Phase 1: Speed + Token Efficiency ===")
	var results []Result
	for _, repo := range corpus.Repos {
		dir := filepath.Join(corpusDir, repo.Name)
		if _, err := os.Stat(dir); err != nil {
			return nil, fmt.Errorf("corpus repo %s not found — run: go run ./bench setup", repo.Name)
		}
		fmt.Printf("\n== %s (%s) ==\n", repo.Name, repo.Language)
		for _, tool := range available {
			fmt.Printf("  %s:\n", tool.Name)
			results = append(results, runSpeedForTool(tool, repo, dir)...)
		}
	}
	return results, nil
}

// runSpeedForTool benchmarks a single (repo, tool) pair: cold index, warm
// reindex, then each supported op per symbol.
func runSpeedForTool(tool Tool, repo Repo, dir string) []Result {
	var out []Result
	if _, ok := tool.Ops[OpIndex]; ok {
		var before []preRun
		if tool.Cleanup != nil {
			before = append(before, func() { tool.Cleanup(dir) })
		}
		fmt.Printf("    index (cold) ...")
		r := runBench(tool, OpIndex, dir, "", indexIters, before...)
		fmt.Printf(" %v\n", r.Median())
		out = append(out, r)
	}
	if _, ok := tool.Ops[OpReindex]; ok {
		fmt.Printf("    reindex ...")
		r := runBench(tool, OpReindex, dir, "", indexIters)
		fmt.Printf(" %v\n", r.Median())
		out = append(out, r)
	}
	for _, sym := range repo.Symbols {
		for _, op := range []Op{OpSearch, OpRefs, OpShow, OpInvestigate, OpImpls, OpTrace, OpGraph} {
			if _, ok := tool.Ops[op]; !ok {
				continue
			}
			fmt.Printf("    %s(%s) ...", op, sym.Name)
			r := runBench(tool, op, dir, sym.Name, queryIters)
			fmt.Printf(" %v\n", r.Median())
			out = append(out, r)
		}
	}
	return out
}

func runPhase2Accuracy(results []Result, repos []Repo) []AccuracyCheck {
	fmt.Println("\n=== Phase 2: Accuracy ===")
	accuracy := checkAccuracy(results, repos)
	passed := 0
	for _, a := range accuracy {
		if a.Passed {
			passed++
			fmt.Printf("  ✓ %s/%s/%s\n", a.Repo, a.Symbol, a.Op)
		} else {
			fmt.Printf("  ✗ %s/%s/%s: %s\n", a.Repo, a.Symbol, a.Op, a.Details)
		}
	}
	fmt.Printf("\n  Accuracy: %d/%d (%.0f%%)\n", passed, len(accuracy), pct(passed, len(accuracy)))
	return accuracy
}

func runPhase3GroundTruth(cymbalBin string, repos []Repo, corpusDir string) ([]GroundTruthCheck, GroundTruthSummary) {
	fmt.Println("\n=== Phase 3: Ground Truth Precision / Recall ===")
	groundTruth := benchGroundTruth(cymbalBin, repos, corpusDir)
	summary := summarizeGroundTruth(groundTruth)
	for _, gt := range groundTruth {
		printGroundTruthRow(gt)
	}
	if len(groundTruth) > 0 {
		fmt.Printf("\n  Ground truth: %d/%d (%.0f%%) | search P/R %.0f%%/%.0f%% | refs P/R %.0f%%/%.0f%% | show exact %.0f%%\n",
			summary.Passed, summary.Total, pct(summary.Passed, summary.Total),
			summary.SearchPrecision, summary.SearchRecall,
			summary.RefsPrecision, summary.RefsRecall,
			summary.ShowExactRate,
		)
	}
	return groundTruth, summary
}

func printGroundTruthRow(gt GroundTruthCheck) {
	if gt.Op == OpShow {
		if gt.Passed {
			fmt.Printf("  ✓ %s/%s/%s | exact\n", gt.Repo, gt.Symbol, gt.Op)
		} else {
			fmt.Printf("  ✗ %s/%s/%s: %s\n", gt.Repo, gt.Symbol, gt.Op, gt.Details)
		}
		return
	}
	if gt.Passed {
		fmt.Printf("  ✓ %s/%s/%s | P=%.0f%% R=%.0f%% (%d/%d)\n", gt.Repo, gt.Symbol, gt.Op, gt.Precision, gt.Recall, gt.TruePositives, gt.Expected)
	} else {
		fmt.Printf("  ✗ %s/%s/%s: %s\n", gt.Repo, gt.Symbol, gt.Op, gt.Details)
	}
}

func runPhase4Canonical(cymbalBin string, repos []Repo, corpusDir string) ([]CanonicalCaseResult, CanonicalSummary) {
	fmt.Println("\n=== Phase 4: Canonical Ranking Hard Mode ===")
	canonical := benchCanonicalCases(cymbalBin, repos, corpusDir)
	summary := summarizeCanonicalCases(canonical)
	for _, c := range canonical {
		status := "✓"
		if !c.Passed {
			status = "✗"
		}
		fmt.Printf("  %s %s/%s | cymbal rank=%d show=%v | grep rank=%d\n",
			status, c.Repo, c.Symbol, c.SearchRank, c.ShowExact, c.GrepRank)
		if !c.Passed && c.Details != "" {
			fmt.Printf("    %s\n", c.Details)
		}
	}
	if len(canonical) > 0 {
		fmt.Printf("\n  Canonical: %d/%d (%.0f%%) | cymbal @1 %.0f%% | cymbal MRR %.2f | show exact %.0f%% | tuned grep @1 %.0f%% | tuned grep MRR %.2f\n",
			summary.Passed, summary.Total, pct(summary.Passed, summary.Total),
			summary.SearchTop1Rate, summary.SearchMRR, summary.ShowExactRate,
			summary.GrepTop1Rate, summary.GrepMRR,
		)
	}
	return canonical, summary
}

func runPhase5Footguns(cymbalBin string, repos []Repo, corpusDir string) []FootgunResult {
	fmt.Println("\n=== Phase 5: Grep Footguns ===")
	footguns := benchFootguns(cymbalBin, repos, corpusDir)
	passed := 0
	for _, f := range footguns {
		if f.Passed {
			passed++
			fmt.Printf("  ✓ %s/%s/%s | grep=%d hits, cymbal=%d hits\n", f.Repo, f.Name, f.Op, f.GrepHits, f.CymbalHits)
		} else {
			fmt.Printf("  ✗ %s/%s/%s: %s\n", f.Repo, f.Name, f.Op, f.Details)
		}
	}
	if len(footguns) > 0 {
		fmt.Printf("\n  Footgun avoidance: %d/%d (%.0f%%)\n", passed, len(footguns), pct(passed, len(footguns)))
	}
	return footguns
}

func runPhase6Freshness(cymbalBin string, repos []Repo, corpusDir string) []FreshnessResult {
	fmt.Println("\n=== Phase 6: JIT Freshness ===")
	freshness := benchFreshness(cymbalBin, repos, corpusDir)
	for _, f := range freshness {
		fmt.Printf("  %s | %-25s | %v\n", f.Repo, f.Scenario, f.Latency.Round(time.Millisecond))
	}
	return freshness
}

func runPhase7Workflow(cymbalBin string, repos []Repo, corpusDir string) []WorkflowResult {
	fmt.Println("\n=== Phase 7: Agent Workflow ===")
	workflows := benchWorkflow(cymbalBin, repos, corpusDir)
	for _, w := range workflows {
		savings := 0
		if w.BaselineBytes > 0 {
			savings = 100 - (w.CymbalBytes*100)/w.BaselineBytes
		}
		fmt.Printf("  %s/%s: cymbal=%d calls/%dB, baseline=%d calls/%dB, savings=%d%%\n",
			w.Repo, w.Symbol, w.CymbalCalls, w.CymbalBytes, w.BaselineCalls, w.BaselineBytes, savings)
	}
	return workflows
}

func cmdRun(corpus Corpus, corpusDir, cymbalBin string) error {
	out, err := executeBench(corpus, corpusDir, cymbalBin)
	if err != nil {
		return err
	}

	// Write markdown report
	md := generateReport(out.results, out.available, out.accuracy, out.groundTruth, out.groundTruthSummary, out.canonical, out.canonicalSummary, out.footguns, out.freshness, out.workflows)
	mdPath := filepath.Join("bench", "RESULTS.md")
	if err := os.WriteFile(mdPath, []byte(md), 0o644); err != nil {
		return err
	}
	fmt.Printf("\nResults written to %s\n", mdPath)

	// Write JSON report
	jsonPath := filepath.Join("bench", "results.json")
	if err := writeJSON(out.report, jsonPath); err != nil {
		return err
	}
	fmt.Printf("JSON written to %s\n", jsonPath)

	return nil
}

// ── Report generation ──────────────────────────────────────────────

func generateReport(results []Result, tools []Tool, accuracy []AccuracyCheck, groundTruth []GroundTruthCheck, groundTruthSummary GroundTruthSummary, canonical []CanonicalCaseResult, canonicalSummary CanonicalSummary, footguns []FootgunResult, freshness []FreshnessResult, workflows []WorkflowResult) string {
	var b strings.Builder

	writeReportHeader(&b)

	toolNames := make([]string, len(tools))
	for i, t := range tools {
		toolNames[i] = t.Name
	}
	byRepo := map[string][]Result{}
	for _, r := range results {
		byRepo[r.Repo] = append(byRepo[r.Repo], r)
	}
	for _, repo := range sortedKeys(byRepo) {
		writeRepoSection(&b, repo, byRepo[repo], results, toolNames)
	}

	writeAccuracySection(&b, accuracy)
	writeGroundTruthSection(&b, groundTruth, groundTruthSummary)
	writeCanonicalSection(&b, canonical, canonicalSummary)
	writeFootgunSection(&b, footguns)
	writeFreshnessSection(&b, freshness)
	writeWorkflowSection(&b, workflows)

	return b.String()
}

func writeReportHeader(b *strings.Builder) {
	b.WriteString("# Cymbal Benchmark Results\n\n")
	fmt.Fprintf(b, "**Date:** %s  \n", time.Now().Format("2006-01-02 15:04 MST"))
	fmt.Fprintf(b, "**Platform:** %s/%s  \n", runtime.GOOS, runtime.GOARCH)
	fmt.Fprintf(b, "**CPU:** %d cores  \n\n", runtime.NumCPU())
}

// writeRepoSection emits per-repo indexing, query-speed, and token-efficiency
// tables. Kept as one function so the three sub-tables stay visually grouped
// under the repo heading in the output.
func writeRepoSection(b *strings.Builder, repo string, repoResults []Result, allResults []Result, toolNames []string) {
	fmt.Fprintf(b, "## %s\n\n", repo)
	writeIndexingTable(b, repo, allResults, toolNames)
	pairs := collectPairs(repoResults)
	writeQuerySpeedTable(b, repo, allResults, toolNames, pairs)
	writeTokenEfficiencyTable(b, repo, allResults, pairs)
}

func writeIndexingTable(b *strings.Builder, repo string, results []Result, toolNames []string) {
	b.WriteString("### Indexing\n\n")
	writeTableHeader(b, []string{"Operation"}, toolNames)
	for _, op := range []Op{OpIndex, OpReindex} {
		fmt.Fprintf(b, "| %s |", op)
		for _, tn := range toolNames {
			r := findResult2(results, tn, op, repo, "")
			if r == nil {
				b.WriteString(" — |")
			} else {
				fmt.Fprintf(b, " %s |", fmtDuration(r.Median()))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeQuerySpeedTable(b *strings.Builder, repo string, results []Result, toolNames []string, pairs []symOp) {
	b.WriteString("### Query Speed\n\n")
	writeTableHeader(b, []string{"Symbol", "Op"}, toolNames)
	for _, p := range pairs {
		fmt.Fprintf(b, "| `%s` | %s |", p.sym, p.op)
		for _, tn := range toolNames {
			r := findResult2(results, tn, Op(p.op), repo, p.sym)
			if r == nil {
				b.WriteString(" — |")
			} else {
				fmt.Fprintf(b, " %s |", fmtDuration(r.Median()))
			}
		}
		b.WriteString("\n")
	}
	b.WriteString("\n")
}

func writeTokenEfficiencyTable(b *strings.Builder, repo string, results []Result, pairs []symOp) {
	b.WriteString("### Token Efficiency\n\n")
	b.WriteString("| Symbol | Op | cymbal | ripgrep | savings |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, p := range pairs {
		cymR := findResult2(results, "cymbal", Op(p.op), repo, p.sym)
		rgR := findResult2(results, "ripgrep", Op(p.op), repo, p.sym)
		cymTok, rgTok, savingsStr := "—", "—", "—"
		if cymR != nil {
			cymTok = fmt.Sprintf("%s (~%d tok)", fmtBytes(cymR.Output), cymR.Output/4)
		}
		if rgR != nil {
			rgTok = fmt.Sprintf("%s (~%d tok)", fmtBytes(rgR.Output), rgR.Output/4)
		}
		if cymR != nil && rgR != nil && rgR.Output > 0 {
			savingsStr = formatSavings(100 - (cymR.Output*100)/rgR.Output)
		}
		fmt.Fprintf(b, "| `%s` | %s | %s | %s | %s |\n", p.sym, p.op, cymTok, rgTok, savingsStr)
	}
	b.WriteString("\n")
}

func formatSavings(savings int) string {
	if savings < 0 {
		return fmt.Sprintf("-%d%%", -savings)
	}
	return fmt.Sprintf("**%d%%**", savings)
}

// writeTableHeader writes a markdown table header row and its `---` separator.
// The fixed leading columns (e.g. "Symbol", "Op") come first; trailing
// dynamic columns are one per tool name.
func writeTableHeader(b *strings.Builder, fixedCols, toolNames []string) {
	for _, c := range fixedCols {
		fmt.Fprintf(b, "| %s ", c)
	}
	b.WriteString("|")
	for _, tn := range toolNames {
		fmt.Fprintf(b, " %s |", tn)
	}
	b.WriteString("\n")
	for range fixedCols {
		b.WriteString("|---")
	}
	b.WriteString("|")
	for range toolNames {
		b.WriteString("---|")
	}
	b.WriteString("\n")
}

func writeAccuracySection(b *strings.Builder, accuracy []AccuracyCheck) {
	b.WriteString("## Accuracy\n\n")
	b.WriteString("| Repo | Symbol | Op | Result |\n")
	b.WriteString("|---|---|---|---|\n")
	passed := 0
	for _, a := range accuracy {
		mark := "✓"
		if !a.Passed {
			mark = "✗ " + a.Details
		} else {
			passed++
		}
		fmt.Fprintf(b, "| %s | `%s` | %s | %s |\n", a.Repo, a.Symbol, a.Op, mark)
	}
	fmt.Fprintf(b, "\n**Overall: %d/%d (%.0f%%)**\n\n", passed, len(accuracy), pct(passed, len(accuracy)))
}

func writeGroundTruthSection(b *strings.Builder, groundTruth []GroundTruthCheck, summary GroundTruthSummary) {
	b.WriteString("## Ground Truth Precision / Recall\n\n")
	b.WriteString("| Repo | Symbol | Op | Precision | Recall | Result |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	if len(groundTruth) == 0 {
		b.WriteString("| — | — | — | — | — | No ground truth configured |\n")
		return
	}
	for _, gt := range groundTruth {
		writeGroundTruthRow(b, gt)
	}
	fmt.Fprintf(b, "\n**Overall: %d/%d (%.0f%%)**  \n", summary.Passed, summary.Total, pct(summary.Passed, summary.Total))
	fmt.Fprintf(b, "**Search precision / recall:** %.0f%% / %.0f%%  \n", summary.SearchPrecision, summary.SearchRecall)
	fmt.Fprintf(b, "**Refs precision / recall:** %.0f%% / %.0f%%  \n", summary.RefsPrecision, summary.RefsRecall)
	fmt.Fprintf(b, "**Show exactness:** %.0f%%\n\n", summary.ShowExactRate)
}

func writeGroundTruthRow(b *strings.Builder, gt GroundTruthCheck) {
	precision, recall := "—", "—"
	if gt.Op != OpShow {
		precision = fmt.Sprintf("%.0f%%", gt.Precision)
		recall = fmt.Sprintf("%.0f%%", gt.Recall)
	}
	mark := "✓"
	if !gt.Passed {
		mark = "✗ " + gt.Details
	} else if gt.Op == OpShow {
		mark = "✓ exact"
	}
	fmt.Fprintf(b, "| %s | `%s` | %s | %s | %s | %s |\n", gt.Repo, gt.Symbol, gt.Op, precision, recall, mark)
}

func writeCanonicalSection(b *strings.Builder, canonical []CanonicalCaseResult, summary CanonicalSummary) {
	b.WriteString("## Canonical Ranking Hard Mode\n\n")
	b.WriteString("| Repo | Symbol | Expected | cymbal rank | show exact | tuned grep rank | Result |\n")
	b.WriteString("|---|---|---|---|---|---|---|\n")
	if len(canonical) == 0 {
		b.WriteString("| — | — | — | — | — | — | No canonical cases configured |\n")
		return
	}
	for _, c := range canonical {
		mark := "✓"
		if !c.Passed {
			mark = "✗ " + c.Details
		}
		show := "no"
		if c.ShowExact {
			show = "yes"
		}
		fmt.Fprintf(b, "| %s | `%s` | `%s` | %d | %s | %d | %s |\n", c.Repo, c.Symbol, c.Expected, c.SearchRank, show, c.GrepRank, mark)
	}
	fmt.Fprintf(b, "\n**Overall: %d/%d (%.0f%%)**  \n", summary.Passed, summary.Total, pct(summary.Passed, summary.Total))
	fmt.Fprintf(b, "**cymbal search@1:** %.0f%%  \n", summary.SearchTop1Rate)
	fmt.Fprintf(b, "**cymbal search MRR:** %.2f  \n", summary.SearchMRR)
	fmt.Fprintf(b, "**cymbal show exactness:** %.0f%%  \n", summary.ShowExactRate)
	fmt.Fprintf(b, "**tuned grep @1 / MRR:** %.0f%% / %.2f\n\n", summary.GrepTop1Rate, summary.GrepMRR)
}

func writeFootgunSection(b *strings.Builder, footguns []FootgunResult) {
	b.WriteString("## Grep Footguns\n\n")
	b.WriteString("| Repo | Case | Op | grep hits | cymbal hits | Result |\n")
	b.WriteString("|---|---|---|---|---|---|\n")
	passed := 0
	for _, f := range footguns {
		mark := "✓"
		if !f.Passed {
			mark = "✗ " + f.Details
		} else {
			passed++
		}
		fmt.Fprintf(b, "| %s | `%s` | %s | %d | %d | %s |\n", f.Repo, f.Name, f.Op, f.GrepHits, f.CymbalHits, mark)
	}
	if len(footguns) > 0 {
		fmt.Fprintf(b, "\n**Overall: %d/%d (%.0f%%)**\n\n", passed, len(footguns), pct(passed, len(footguns)))
	} else {
		b.WriteString("\n_No footgun scenarios configured._\n\n")
	}
}

func writeFreshnessSection(b *strings.Builder, freshness []FreshnessResult) {
	b.WriteString("## JIT Freshness\n\n")
	b.WriteString("| Repo | Scenario | Latency |\n")
	b.WriteString("|---|---|---|\n")
	for _, f := range freshness {
		fmt.Fprintf(b, "| %s | %s | %s |\n", f.Repo, f.Scenario, fmtDuration(f.Latency))
	}
	b.WriteString("\n")
}

func writeWorkflowSection(b *strings.Builder, workflows []WorkflowResult) {
	b.WriteString("## Agent Workflow: cymbal investigate vs ripgrep\n\n")
	b.WriteString("| Repo | Symbol | cymbal | baseline (rg×3) | savings |\n")
	b.WriteString("|---|---|---|---|---|\n")
	for _, w := range workflows {
		savings := 0
		if w.BaselineBytes > 0 {
			savings = 100 - (w.CymbalBytes*100)/w.BaselineBytes
		}
		savingsStr := fmt.Sprintf("%d%%", savings)
		if savings > 0 {
			savingsStr = fmt.Sprintf("**%d%%**", savings)
		}
		fmt.Fprintf(b, "| %s | `%s` | %d call, %s (~%d tok) | %d calls, %s (~%d tok) | %s |\n",
			w.Repo, w.Symbol,
			w.CymbalCalls, fmtBytes(w.CymbalBytes), w.CymbalBytes/4,
			w.BaselineCalls, fmtBytes(w.BaselineBytes), w.BaselineBytes/4,
			savingsStr)
	}
	b.WriteString("\n")
}

// ── Helpers ────────────────────────────────────────────────────────

func findResult2(results []Result, tool string, op Op, repo, symbol string) *Result {
	for i := range results {
		r := &results[i]
		if r.Tool == tool && r.Op == op && r.Repo == repo && r.Symbol == symbol {
			return r
		}
	}
	return nil
}

type symOp struct{ sym, op string }

func collectPairs(results []Result) []symOp {
	seen := map[symOp]bool{}
	var pairs []symOp
	for _, r := range results {
		if r.Op == OpIndex || r.Op == OpReindex {
			continue
		}
		so := symOp{r.Symbol, string(r.Op)}
		if !seen[so] {
			seen[so] = true
			pairs = append(pairs, so)
		}
	}
	return pairs
}

func sortedKeys(m map[string][]Result) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func fmtDuration(d time.Duration) string {
	if d < time.Millisecond {
		return fmt.Sprintf("%.1fµs", float64(d.Microseconds()))
	}
	if d < time.Second {
		return fmt.Sprintf("%.1fms", float64(d.Microseconds())/1000)
	}
	return fmt.Sprintf("%.2fs", d.Seconds())
}

func fmtBytes(n int) string {
	if n < 1024 {
		return fmt.Sprintf("%dB", n)
	}
	if n < 1024*1024 {
		return fmt.Sprintf("%.1fKB", float64(n)/1024)
	}
	return fmt.Sprintf("%.1fMB", float64(n)/(1024*1024))
}

func pct(passed, total int) float64 {
	if total == 0 {
		return 100
	}
	return float64(passed) / float64(total) * 100
}

// ── Entrypoint ─────────────────────────────────────────────────────

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: go run ./bench [setup|run|check|update-baseline]\n")
		os.Exit(1)
	}

	corpusFile := filepath.Join("bench", "corpus.yaml")
	data, err := os.ReadFile(corpusFile)
	if err != nil {
		fmt.Fprintf(os.Stderr, "reading %s: %v\n", corpusFile, err)
		os.Exit(1)
	}

	var corpus Corpus
	if err := yaml.Unmarshal(data, &corpus); err != nil {
		fmt.Fprintf(os.Stderr, "parsing %s: %v\n", corpusFile, err)
		os.Exit(1)
	}

	corpusDir := filepath.Join("bench", ".corpus")
	baselinePath := filepath.Join("bench", "baseline.json")

	cymbalBin := "cymbal"
	if bin, err := exec.LookPath("./cymbal"); err == nil {
		cymbalBin, _ = filepath.Abs(bin)
	} else if bin, err := exec.LookPath("cymbal"); err == nil {
		cymbalBin = bin
	}

	switch os.Args[1] {
	case "setup":
		fmt.Println("Setting up benchmark corpus...")
		if err := cmdSetup(corpus, corpusDir); err != nil {
			fmt.Fprintf(os.Stderr, "setup: %v\n", err)
			os.Exit(1)
		}
	case "run":
		fmt.Println("Running benchmarks...")
		fmt.Printf("Using cymbal: %s\n", cymbalBin)
		if err := cmdRun(corpus, corpusDir, cymbalBin); err != nil {
			fmt.Fprintf(os.Stderr, "run: %v\n", err)
			os.Exit(1)
		}
	case "update-baseline":
		fmt.Println("Running benchmarks to generate baseline...")
		fmt.Printf("Using cymbal: %s\n", cymbalBin)
		out, err := executeBench(corpus, corpusDir, cymbalBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bench: %v\n", err)
			os.Exit(1)
		}
		if err := writeJSON(out.report, baselinePath); err != nil {
			fmt.Fprintf(os.Stderr, "writing baseline: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\nBaseline written to %s (%d entries)\n", baselinePath, len(out.report.Entries))
	case "check":
		baseline, err := loadBaseline(baselinePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%v\n", err)
			os.Exit(1)
		}
		fmt.Println("Running benchmarks for regression check...")
		fmt.Printf("Using cymbal: %s\n", cymbalBin)
		out, err := executeBench(corpus, corpusDir, cymbalBin)
		if err != nil {
			fmt.Fprintf(os.Stderr, "bench: %v\n", err)
			os.Exit(1)
		}
		passed, summary := compareResults(out.report, baseline)
		fmt.Printf("\n=== Regression Check ===\n")
		fmt.Printf("  Baseline: %s (%s, %d CPUs)\n", baseline.Timestamp, baseline.Platform, baseline.CPUs)
		fmt.Printf("  Current:  %s (%s, %d CPUs)\n", out.report.Timestamp, out.report.Platform, out.report.CPUs)
		fmt.Printf("  Thresholds: index=%.0fx, query=%.0fx\n\n", indexThreshold, queryThreshold)
		fmt.Print(summary)
		if passed {
			fmt.Println("\n  Result: PASS — no regressions detected")
		} else {
			fmt.Println("\n  Result: FAIL — regressions detected")
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\nUsage: go run ./bench [setup|run|check|update-baseline]\n", os.Args[1])
		os.Exit(1)
	}
}
