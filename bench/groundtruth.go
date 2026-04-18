package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Ground-truth precision/recall support lives in this file so the bench harness
// can evolve without making main.go even larger.

type GroundTruthSpec struct {
	Search *GroundTruthSearchSpec `yaml:"search"`
	Show   *GroundTruthLocation   `yaml:"show"`
	Refs   *GroundTruthRefsSpec   `yaml:"refs"`
	Impls  *GroundTruthImplsSpec  `yaml:"impls"`
	Trace  *GroundTruthTraceSpec  `yaml:"trace"`
}

// GroundTruthMultiCase exercises the multi-symbol invocation path of
// show / impls / impact / trace. It drives a single command with N symbols
// and verifies:
//
//   - ExpectHits: substrings that MUST appear in stdout (implementer names,
//     callees, source lines, etc.). Coarse but robust across cosmetic output
//     tweaks.
//   - ForbidHits: substrings that MUST NOT appear.
//   - ExpectAttribution: for impact/trace, every listed Name must appear
//     alongside an `[src1,src2,...]` attribution tag containing at least the
//     listed Sources (order-insensitive). This is what verifies that the
//     dedupe + union behavior actually works.
//
// Stored at the repo level so it's not tied to a specific Symbol entry.
type GroundTruthMultiCase struct {
	Name              string                `yaml:"name"`
	Op                string                `yaml:"op"` // show | impls | impact | trace
	Symbols           []string              `yaml:"symbols"`
	Flags             []string              `yaml:"flags"`
	ExpectHits        []string              `yaml:"expect_hits"`
	ForbidHits        []string              `yaml:"forbid_hits"`
	ExpectAttribution []GroundTruthMultiHit `yaml:"expect_attribution"`
}

type GroundTruthMultiHit struct {
	Name    string   `yaml:"name"`
	Sources []string `yaml:"sources"`
}

// GroundTruthImplsSpec pins the exact set of local types that should be
// returned as implementors/conformers of the symbol. Expected entries are
// matched by implementer-name + file path. ForbidNoise names MUST NOT appear
// in output (e.g. a Swift type that only mentions the protocol in a type
// annotation, not in an inheritance clause).
type GroundTruthImplsSpec struct {
	Limit       int               `yaml:"limit"`
	Expected    []GroundTruthImpl `yaml:"expected"`
	ForbidNoise []string          `yaml:"forbid_noise"`
	External    bool              `yaml:"external"` // target is a framework protocol (Resolved=false expected)
	Of          string            `yaml:"of"`       // if set, run --of <type> instead of incoming direction
}

type GroundTruthImpl struct {
	Implementer string `yaml:"implementer"`
	File        string `yaml:"file"`
	Line        int    `yaml:"line"`
}

// GroundTruthTraceSpec verifies that default trace is call-only. ExpectCallees
// MUST appear; ForbidNoise (type-mention noise: UUID, Date, Sendable, String,
// Int, etc.) MUST NOT. WideIncludes is what should reappear when --kinds
// call,use is passed.
type GroundTruthTraceSpec struct {
	Depth         int      `yaml:"depth"`
	Limit         int      `yaml:"limit"`
	ExpectCallees []string `yaml:"expect_callees"`
	ForbidNoise   []string `yaml:"forbid_noise"`
	WideIncludes  []string `yaml:"wide_includes"` // must appear when --kinds call,use
}

type GroundTruthSearchSpec struct {
	Exact       bool                `yaml:"exact"`
	Limit       int                 `yaml:"limit"`
	Expected    []GroundTruthSymbol `yaml:"expected"`
	Canonical   *GroundTruthSymbol  `yaml:"canonical"`
	PreferPaths []string            `yaml:"prefer_paths"`
	AvoidPaths  []string            `yaml:"avoid_paths"`
}

type GroundTruthRefsSpec struct {
	Limit    int              `yaml:"limit"`
	Expected []GroundTruthRef `yaml:"expected"`
}

type GroundTruthLocation struct {
	File string `yaml:"file"`
	Line int    `yaml:"line"`
	Kind string `yaml:"kind"`
}

type GroundTruthSymbol struct {
	File string `yaml:"file"`
	Line int    `yaml:"line"`
	Kind string `yaml:"kind"`
	Name string `yaml:"name"`
}

type GroundTruthRef struct {
	File string `yaml:"file"`
	Line int    `yaml:"line"`
}

type GroundTruthCheck struct {
	Repo           string  `json:"repo"`
	Symbol         string  `json:"symbol"`
	Op             Op      `json:"op"`
	Passed         bool    `json:"passed"`
	Precision      float64 `json:"precision,omitempty"`
	Recall         float64 `json:"recall,omitempty"`
	TruePositives  int     `json:"true_positives,omitempty"`
	FalsePositives int     `json:"false_positives,omitempty"`
	FalseNegatives int     `json:"false_negatives,omitempty"`
	Expected       int     `json:"expected,omitempty"`
	Actual         int     `json:"actual,omitempty"`
	Details        string  `json:"details,omitempty"`
}

type GroundTruthSummary struct {
	Passed          int     `json:"passed"`
	Total           int     `json:"total"`
	SearchPrecision float64 `json:"search_precision"`
	SearchRecall    float64 `json:"search_recall"`
	RefsPrecision   float64 `json:"refs_precision"`
	RefsRecall      float64 `json:"refs_recall"`
	ShowExactRate   float64 `json:"show_exact_rate"`
	ImplsPrecision  float64 `json:"impls_precision"`
	ImplsRecall     float64 `json:"impls_recall"`
	TracePassRate   float64 `json:"trace_pass_rate"`
}

type CanonicalCaseResult struct {
	Repo       string  `json:"repo"`
	Symbol     string  `json:"symbol"`
	Expected   string  `json:"expected"`
	SearchRank int     `json:"search_rank"`
	SearchTop1 bool    `json:"search_top_1"`
	SearchMRR  float64 `json:"search_mrr"`
	ShowExact  bool    `json:"show_exact"`
	ShowActual string  `json:"show_actual,omitempty"`
	GrepRank   int     `json:"grep_rank"`
	GrepTop1   bool    `json:"grep_top_1"`
	GrepMRR    float64 `json:"grep_mrr"`
	GrepActual string  `json:"grep_actual,omitempty"`
	Passed     bool    `json:"passed"`
	Details    string  `json:"details,omitempty"`
}

type CanonicalSummary struct {
	Passed         int     `json:"passed"`
	Total          int     `json:"total"`
	SearchTop1Rate float64 `json:"search_top_1_rate"`
	SearchMRR      float64 `json:"search_mrr"`
	ShowExactRate  float64 `json:"show_exact_rate"`
	GrepTop1Rate   float64 `json:"grep_top_1_rate"`
	GrepMRR        float64 `json:"grep_mrr"`
}

type groundTruthSearchResponse struct {
	Results []struct {
		Name      string `json:"name"`
		Kind      string `json:"kind"`
		RelPath   string `json:"rel_path"`
		StartLine int    `json:"start_line"`
	} `json:"results"`
}

type groundTruthShowResponse struct {
	Results struct {
		File  string `json:"file"`
		Lines []struct {
			Line int `json:"line"`
		} `json:"lines"`
	} `json:"results"`
}

type groundTruthRefsResponse struct {
	Results []struct {
		RelPath string `json:"rel_path"`
		Line    int    `json:"line"`
	} `json:"results"`
}

type gtLoc struct {
	File string
	Line int
	Kind string
}

type grepCandidate struct {
	Loc   gtLoc
	Score int
	Line  string
}

func benchGroundTruth(cymbalBin string, repos []Repo, corpusDir string) []GroundTruthCheck {
	var checks []GroundTruthCheck
	for _, repo := range repos {
		repoDir := filepath.Join(corpusDir, repo.Name)
		for _, mc := range repo.MultiCases {
			checks = append(checks, runGroundTruthMulti(cymbalBin, repo.Name, repoDir, mc))
		}
		for _, sym := range repo.Symbols {
			if sym.GroundTruth == nil {
				continue
			}
			if sym.GroundTruth.Search != nil {
				checks = append(checks, runGroundTruthSearch(cymbalBin, repo.Name, repoDir, sym))
			}
			if sym.GroundTruth.Show != nil {
				checks = append(checks, runGroundTruthShow(cymbalBin, repo.Name, repoDir, sym))
			}
			if sym.GroundTruth.Refs != nil {
				checks = append(checks, runGroundTruthRefs(cymbalBin, repo.Name, repoDir, sym))
			}
			if sym.GroundTruth.Impls != nil {
				checks = append(checks, runGroundTruthImpls(cymbalBin, repo.Name, repoDir, sym))
			}
			if sym.GroundTruth.Trace != nil {
				checks = append(checks, runGroundTruthTrace(cymbalBin, repo.Name, repoDir, sym))
			}
		}
	}
	return checks
}

func summarizeGroundTruth(checks []GroundTruthCheck) GroundTruthSummary {
	var summary GroundTruthSummary
	var searchTP, searchFP, searchFN int
	var refsTP, refsFP, refsFN int
	var showPassed, showTotal int

	var implsTP, implsFP, implsFN int
	var traceTotal, tracePassed int

	summary.Total = len(checks)
	for _, check := range checks {
		if check.Passed {
			summary.Passed++
		}
		switch check.Op {
		case OpSearch:
			searchTP += check.TruePositives
			searchFP += check.FalsePositives
			searchFN += check.FalseNegatives
		case OpRefs:
			refsTP += check.TruePositives
			refsFP += check.FalsePositives
			refsFN += check.FalseNegatives
		case OpShow:
			showTotal++
			if check.Passed {
				showPassed++
			}
		case OpImpls:
			implsTP += check.TruePositives
			implsFP += check.FalsePositives
			implsFN += check.FalseNegatives
		case OpTrace:
			traceTotal++
			if check.Passed {
				tracePassed++
			}
		}
	}

	summary.SearchPrecision = ratioPct(searchTP, searchTP+searchFP)
	summary.SearchRecall = ratioPct(searchTP, searchTP+searchFN)
	summary.RefsPrecision = ratioPct(refsTP, refsTP+refsFP)
	summary.RefsRecall = ratioPct(refsTP, refsTP+refsFN)
	summary.ShowExactRate = ratioPct(showPassed, showTotal)
	summary.ImplsPrecision = ratioPct(implsTP, implsTP+implsFP)
	summary.ImplsRecall = ratioPct(implsTP, implsTP+implsFN)
	summary.TracePassRate = ratioPct(tracePassed, traceTotal)
	return summary
}

func benchCanonicalCases(cymbalBin string, repos []Repo, corpusDir string) []CanonicalCaseResult {
	var cases []CanonicalCaseResult
	for _, repo := range repos {
		repoDir := filepath.Join(corpusDir, repo.Name)
		for _, sym := range repo.Symbols {
			if sym.GroundTruth == nil || sym.GroundTruth.Search == nil || sym.GroundTruth.Search.Canonical == nil {
				continue
			}
			cases = append(cases, runCanonicalCase(cymbalBin, repo.Name, repoDir, sym))
		}
	}
	return cases
}

func summarizeCanonicalCases(cases []CanonicalCaseResult) CanonicalSummary {
	var summary CanonicalSummary
	var searchMRR, grepMRR float64
	var searchTop1, showExact, grepTop1 int

	summary.Total = len(cases)
	for _, c := range cases {
		if c.Passed {
			summary.Passed++
		}
		searchMRR += c.SearchMRR
		grepMRR += c.GrepMRR
		if c.SearchTop1 {
			searchTop1++
		}
		if c.ShowExact {
			showExact++
		}
		if c.GrepTop1 {
			grepTop1++
		}
	}
	summary.SearchTop1Rate = ratioPct(searchTop1, summary.Total)
	summary.ShowExactRate = ratioPct(showExact, summary.Total)
	summary.GrepTop1Rate = ratioPct(grepTop1, summary.Total)
	if summary.Total > 0 {
		summary.SearchMRR = searchMRR / float64(summary.Total)
		summary.GrepMRR = grepMRR / float64(summary.Total)
	}
	return summary
}

func runCanonicalCase(cymbalBin, repoName, repoDir string, sym Symbol) CanonicalCaseResult {
	spec := sym.GroundTruth.Search
	canonical := gtLoc{
		File: normalizeGTPath(spec.Canonical.File),
		Line: spec.Canonical.Line,
		Kind: spec.Canonical.Kind,
	}
	result := CanonicalCaseResult{
		Repo:     repoName,
		Symbol:   sym.Name,
		Expected: formatGTLoc(canonical),
	}

	searchArgs := []string{"--json", "search"}
	if spec.Exact {
		searchArgs = append(searchArgs, "--exact")
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = 200
	}
	searchArgs = append(searchArgs, "--limit", fmt.Sprintf("%d", limit), sym.Name)
	searchOut, err := runGroundTruthCmd(repoDir, cymbalBin, searchArgs...)
	if err != nil {
		result.Details = err.Error()
		return result
	}
	var searchPayload groundTruthSearchResponse
	if err := json.Unmarshal(searchOut, &searchPayload); err != nil {
		result.Details = fmt.Sprintf("parse search json: %v", err)
		return result
	}
	var searchActual []gtLoc
	for _, item := range searchPayload.Results {
		searchActual = append(searchActual, gtLoc{File: normalizeGTPath(item.RelPath), Line: item.StartLine, Kind: item.Kind})
	}
	result.SearchRank = gtRank(searchActual, canonical, true)
	result.SearchTop1 = result.SearchRank == 1
	result.SearchMRR = reciprocalRank(result.SearchRank)

	showOut, err := runGroundTruthCmd(repoDir, cymbalBin, "--json", "show", sym.Name)
	if err == nil {
		var showPayload groundTruthShowResponse
		if err := json.Unmarshal(showOut, &showPayload); err == nil {
			showLoc := gtLoc{File: normalizeGTPath(relToRepo(repoDir, showPayload.Results.File))}
			if len(showPayload.Results.Lines) > 0 {
				showLoc.Line = showPayload.Results.Lines[0].Line
			}
			result.ShowActual = formatGTLoc(showLoc)
			result.ShowExact = sameGTLoc(showLoc, canonical, false)
		} else {
			result.Details = appendDetail(result.Details, fmt.Sprintf("parse show json: %v", err))
		}
	} else {
		result.Details = appendDetail(result.Details, err.Error())
	}

	grepCandidates := tunedGrepCandidates(repoDir, sym, spec)
	result.GrepRank = grepRank(grepCandidates, canonical)
	result.GrepTop1 = result.GrepRank == 1
	result.GrepMRR = reciprocalRank(result.GrepRank)
	if len(grepCandidates) > 0 {
		result.GrepActual = formatGTLoc(grepCandidates[0].Loc)
	}

	if result.SearchRank == 0 {
		result.Details = appendDetail(result.Details, "canonical result missing from cymbal search")
	} else if !result.SearchTop1 {
		result.Details = appendDetail(result.Details, fmt.Sprintf("canonical ranked #%d in cymbal search", result.SearchRank))
	}
	if !result.ShowExact {
		result.Details = appendDetail(result.Details, fmt.Sprintf("show resolved to %s", result.ShowActual))
	}
	if result.GrepRank == 0 {
		result.Details = appendDetail(result.Details, "tuned grep missed canonical definition")
	}
	result.Passed = result.SearchTop1 && result.ShowExact
	return result
}

func tunedGrepCandidates(repoDir string, sym Symbol, spec *GroundTruthSearchSpec) []grepCandidate {
	pattern := `\b` + regexp.QuoteMeta(sym.Name) + `\b`
	cmd := exec.Command("rg", "--no-heading", "-n", "-P", pattern)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil && len(out) == 0 {
		return nil
	}

	seen := map[string]bool{}
	var candidates []grepCandidate
	for _, raw := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if raw == "" {
			continue
		}
		parts := strings.SplitN(raw, ":", 3)
		if len(parts) < 3 {
			continue
		}
		line, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		candidate := grepCandidate{
			Loc:  gtLoc{File: normalizeGTPath(parts[0]), Line: line},
			Line: parts[2],
		}
		candidate.Score = tunedGrepScore(candidate, sym, spec)
		key := fmt.Sprintf("%s:%d", candidate.Loc.File, candidate.Loc.Line)
		if seen[key] {
			continue
		}
		seen[key] = true
		candidates = append(candidates, candidate)
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		if candidates[i].Score != candidates[j].Score {
			return candidates[i].Score > candidates[j].Score
		}
		if candidates[i].Loc.File != candidates[j].Loc.File {
			return candidates[i].Loc.File < candidates[j].Loc.File
		}
		return candidates[i].Loc.Line < candidates[j].Loc.Line
	})
	return candidates
}

func tunedGrepScore(candidate grepCandidate, sym Symbol, spec *GroundTruthSearchSpec) int {
	score := 0
	path := strings.ToLower(candidate.Loc.File)
	line := strings.ToLower(strings.TrimSpace(candidate.Line))
	name := strings.ToLower(sym.Name)
	kind := strings.ToLower(sym.Kind)
	if spec.Canonical != nil && spec.Canonical.Kind != "" {
		kind = strings.ToLower(spec.Canonical.Kind)
	}

	if strings.Contains(line, name) {
		score += 8
	}
	if strings.Contains(line, "func ") || strings.Contains(line, "def ") || strings.Contains(line, "class ") || strings.Contains(line, "type ") || strings.Contains(line, "interface ") || strings.Contains(line, "struct ") || strings.Contains(line, "impl ") {
		score += 40
	}
	if strings.Contains(line, "func "+name) || strings.Contains(line, "class "+name) || strings.Contains(line, "type "+name) || strings.Contains(line, "interface "+name) || strings.Contains(line, "struct "+name) || strings.Contains(line, "def "+name) || strings.Contains(line, "async def "+name) {
		score += 60
	}
	if kind != "" && strings.Contains(line, kind) {
		score += 20
	}
	if kind == "constructor" && strings.Contains(line, name+"(") {
		score += 30
	}

	for _, prefer := range spec.PreferPaths {
		if strings.Contains(candidate.Loc.File, prefer) {
			score += 90
		}
	}
	for _, avoid := range spec.AvoidPaths {
		if strings.Contains(candidate.Loc.File, avoid) {
			score -= 90
		}
	}

	for _, noisy := range []string{"/playground/", "/example/", "/examples/", "/demo/", "/demos/", "/docs/", "/docs_src/", "/vendor/", "/node_modules/"} {
		if strings.Contains(path, noisy) {
			score -= 35
		}
	}
	for _, testLike := range []string{"_test.go", "/test/", "/tests/", "test_", "_spec."} {
		if strings.Contains(path, testLike) {
			score -= 45
		}
	}
	for _, sourceLike := range []string{"/src/", "/pkg/", "/crates/", "/fastapi/", "/packages/"} {
		if strings.Contains(path, sourceLike) {
			score += 10
		}
	}

	return score
}

func runGroundTruthSearch(cymbalBin, repoName, repoDir string, sym Symbol) GroundTruthCheck {
	spec := sym.GroundTruth.Search
	limit := spec.Limit
	if limit <= 0 {
		limit = 200
	}

	args := []string{"--json", "search"}
	if spec.Exact {
		args = append(args, "--exact")
	}
	args = append(args, "--limit", fmt.Sprintf("%d", limit), sym.Name)
	out, err := runGroundTruthCmd(repoDir, cymbalBin, args...)
	if err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpSearch, Details: err.Error()}
	}

	var payload groundTruthSearchResponse
	if err := json.Unmarshal(out, &payload); err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpSearch, Details: fmt.Sprintf("parse search json: %v", err)}
	}

	actual := make([]gtLoc, 0, len(payload.Results))
	for _, r := range payload.Results {
		actual = append(actual, gtLoc{File: normalizeGTPath(r.RelPath), Line: r.StartLine, Kind: r.Kind})
	}
	expected := make([]gtLoc, 0, len(spec.Expected))
	for _, e := range spec.Expected {
		expected = append(expected, gtLoc{File: normalizeGTPath(e.File), Line: e.Line, Kind: e.Kind})
	}
	return compareGroundTruth(repoName, sym.Name, OpSearch, actual, expected)
}

func runGroundTruthShow(cymbalBin, repoName, repoDir string, sym Symbol) GroundTruthCheck {
	out, err := runGroundTruthCmd(repoDir, cymbalBin, "--json", "show", sym.Name)
	if err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpShow, Details: err.Error()}
	}

	var payload groundTruthShowResponse
	if err := json.Unmarshal(out, &payload); err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpShow, Details: fmt.Sprintf("parse show json: %v", err)}
	}

	actual := gtLoc{File: normalizeGTPath(relToRepo(repoDir, payload.Results.File))}
	if len(payload.Results.Lines) > 0 {
		actual.Line = payload.Results.Lines[0].Line
	}
	expected := gtLoc{File: normalizeGTPath(sym.GroundTruth.Show.File), Line: sym.GroundTruth.Show.Line}
	passed := actual.File == expected.File && actual.Line == expected.Line
	detail := ""
	if !passed {
		detail = fmt.Sprintf("expected %s:%d, got %s:%d", expected.File, expected.Line, actual.File, actual.Line)
	}
	return GroundTruthCheck{
		Repo:    repoName,
		Symbol:  sym.Name,
		Op:      OpShow,
		Passed:  passed,
		Details: detail,
	}
}

func runGroundTruthRefs(cymbalBin, repoName, repoDir string, sym Symbol) GroundTruthCheck {
	spec := sym.GroundTruth.Refs
	limit := spec.Limit
	if limit <= 0 {
		limit = 200
	}
	args := []string{"--json", "refs", "--limit", fmt.Sprintf("%d", limit), sym.Name}
	out, err := runGroundTruthCmd(repoDir, cymbalBin, args...)
	if err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpRefs, Details: err.Error()}
	}

	var payload groundTruthRefsResponse
	if trimmed := strings.TrimSpace(string(out)); trimmed != "" {
		if err := json.Unmarshal(out, &payload); err != nil {
			return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpRefs, Details: fmt.Sprintf("parse refs json: %v", err)}
		}
	}

	actual := make([]gtLoc, 0, len(payload.Results))
	for _, r := range payload.Results {
		actual = append(actual, gtLoc{File: normalizeGTPath(r.RelPath), Line: r.Line})
	}
	expected := make([]gtLoc, 0, len(spec.Expected))
	for _, e := range spec.Expected {
		expected = append(expected, gtLoc{File: normalizeGTPath(e.File), Line: e.Line})
	}
	return compareGroundTruth(repoName, sym.Name, OpRefs, actual, expected)
}

// ── impls ground truth ─────────────────────────────────────────────

type groundTruthImplsResponse struct {
	Results []struct {
		Implementer string `json:"implementer"`
		Target      string `json:"target"`
		RelPath     string `json:"rel_path"`
		Line        int    `json:"line"`
		Resolved    bool   `json:"resolved"`
		Language    string `json:"language"`
	} `json:"results"`
}

// implsRow is the flattened row shape we consume from `cymbal --json impls`.
type implsRow struct {
	Implementer string `json:"implementer"`
	Target      string `json:"target"`
	RelPath     string `json:"rel_path"`
	Line        int    `json:"line"`
	Resolved    bool   `json:"resolved"`
	Language    string `json:"language"`
}

// parseImplsJSON accepts both the object-wrapped {"results": [...]} shape and
// a bare array, matching however cymbal --json currently renders impls.
func parseImplsJSON(out []byte) ([]implsRow, error) {
	trimmed := strings.TrimSpace(string(out))
	if trimmed == "" || trimmed == "null" {
		return nil, nil
	}
	var obj struct {
		Results []implsRow `json:"results"`
	}
	if err := json.Unmarshal(out, &obj); err == nil && len(obj.Results) > 0 {
		return obj.Results, nil
	}
	var bare []implsRow
	if err := json.Unmarshal(out, &bare); err != nil {
		return nil, err
	}
	return bare, nil
}

// primaryName returns the name we compare against expected/forbidden sets.
// For --of queries that's the target; otherwise it's the implementer.
func (r implsRow) primaryName(ofMode bool) string {
	nm := r.Implementer
	if ofMode {
		nm = r.Target
	}
	if nm == "" {
		nm = "(anonymous)"
	}
	return nm
}

// evaluateImpls computes tp/fp/fn + forbidden + resolved-mismatch lists for a
// set of impls rows against a spec. Pure, testable, and keeps
// runGroundTruthImpls well below the cyclomatic threshold.
type implsEval struct {
	tp               int
	missing          []string
	unexpected       []string
	forbidden        []string
	resolvedMismatch []string
}

func evaluateImpls(rows []implsRow, spec *GroundTruthImplsSpec) implsEval {
	var e implsEval
	ofMode := spec.Of != ""

	actualKeys := map[string]bool{}
	actualNames := map[string]bool{}
	for _, r := range rows {
		nm := r.primaryName(ofMode)
		actualKeys[nm+"|"+normalizeGTPath(r.RelPath)] = true
		actualNames[nm] = true
	}

	expectedNames := map[string]bool{}
	for _, exp := range spec.Expected {
		expectedNames[exp.Implementer] = true
		key := exp.Implementer + "|" + normalizeGTPath(exp.File)
		if actualKeys[key] {
			e.tp++
			continue
		}
		e.missing = append(e.missing, fmt.Sprintf("%s@%s", exp.Implementer, normalizeGTPath(exp.File)))
	}
	for _, r := range rows {
		nm := r.primaryName(ofMode)
		if nm == "(anonymous)" || expectedNames[nm] {
			continue
		}
		e.unexpected = append(e.unexpected, fmt.Sprintf("%s@%s", nm, normalizeGTPath(r.RelPath)))
	}
	for _, n := range spec.ForbidNoise {
		if actualNames[n] {
			e.forbidden = append(e.forbidden, n)
		}
	}
	if spec.External {
		for _, r := range rows {
			if r.Resolved {
				e.resolvedMismatch = append(e.resolvedMismatch, r.Implementer)
			}
		}
	}
	sort.Strings(e.missing)
	sort.Strings(e.unexpected)
	sort.Strings(e.forbidden)
	sort.Strings(e.resolvedMismatch)
	return e
}

func formatImplsDetail(e implsEval) string {
	var parts []string
	if len(e.missing) > 0 {
		parts = append(parts, "missing "+truncateGTList(e.missing))
	}
	if len(e.unexpected) > 0 {
		parts = append(parts, "unexpected "+truncateGTList(e.unexpected))
	}
	if len(e.forbidden) > 0 {
		parts = append(parts, "forbid_noise leaked: "+truncateGTList(e.forbidden))
	}
	if len(e.resolvedMismatch) > 0 {
		parts = append(parts, "expected external (resolved=false), got resolved=true for: "+truncateGTList(e.resolvedMismatch))
	}
	return strings.Join(parts, "; ")
}

func runGroundTruthImpls(cymbalBin, repoName, repoDir string, sym Symbol) GroundTruthCheck {
	spec := sym.GroundTruth.Impls
	limit := spec.Limit
	if limit <= 0 {
		limit = 200
	}
	args := []string{"--json", "impls", "--limit", fmt.Sprintf("%d", limit)}
	if spec.Of != "" {
		args = append(args, "--of", spec.Of)
	} else {
		args = append(args, sym.Name)
	}
	out, err := runGroundTruthCmd(repoDir, cymbalBin, args...)
	if err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpImpls, Details: err.Error()}
	}
	rows, perr := parseImplsJSON(out)
	if perr != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpImpls,
			Details: fmt.Sprintf("parse impls json: %v", perr)}
	}

	eval := evaluateImpls(rows, spec)
	fn := len(eval.missing)
	fp := len(eval.unexpected)
	passed := fn == 0 && fp == 0 && len(eval.forbidden) == 0 && len(eval.resolvedMismatch) == 0

	return GroundTruthCheck{
		Repo:           repoName,
		Symbol:         sym.Name,
		Op:             OpImpls,
		Passed:         passed,
		Precision:      ratioPct(eval.tp, eval.tp+fp),
		Recall:         ratioPct(eval.tp, eval.tp+fn),
		TruePositives:  eval.tp,
		FalsePositives: fp,
		FalseNegatives: fn,
		Expected:       len(spec.Expected),
		Actual:         len(rows),
		Details:        formatImplsDetail(eval),
	}
}

// ── trace noise-reduction ground truth ─────────────────────────────

type groundTruthTraceResponse struct {
	Results []struct {
		Callee string `json:"callee"`
		Line   int    `json:"line"`
	} `json:"results"`
}

func runGroundTruthTrace(cymbalBin, repoName, repoDir string, sym Symbol) GroundTruthCheck {
	spec := sym.GroundTruth.Trace
	depth := spec.Depth
	if depth <= 0 {
		depth = 2
	}
	limit := spec.Limit
	if limit <= 0 {
		limit = 200
	}

	// Default trace (call-only): expect_callees MUST appear, forbid_noise MUST NOT.
	args := []string{"--json", "trace", "--depth", fmt.Sprintf("%d", depth), "-n", fmt.Sprintf("%d", limit), sym.Name}
	out, err := runGroundTruthCmd(repoDir, cymbalBin, args...)
	if err != nil {
		return GroundTruthCheck{Repo: repoName, Symbol: sym.Name, Op: OpTrace, Details: err.Error()}
	}
	callees := parseTraceCallees(out)

	var missing []string
	for _, want := range spec.ExpectCallees {
		if !callees[want] {
			missing = append(missing, want)
		}
	}
	var leaked []string
	for _, bad := range spec.ForbidNoise {
		if callees[bad] {
			leaked = append(leaked, bad)
		}
	}

	// Widened trace (call,use): wide_includes should reappear.
	var wideMissing []string
	if len(spec.WideIncludes) > 0 {
		wideArgs := []string{"--json", "trace", "--depth", fmt.Sprintf("%d", depth),
			"-n", fmt.Sprintf("%d", limit), "--kinds", "call,use", sym.Name}
		if wideOut, werr := runGroundTruthCmd(repoDir, cymbalBin, wideArgs...); werr == nil {
			wide := parseTraceCallees(wideOut)
			for _, want := range spec.WideIncludes {
				if !wide[want] {
					wideMissing = append(wideMissing, want)
				}
			}
		} else {
			wideMissing = append(wideMissing, fmt.Sprintf("(widened trace failed: %v)", werr))
		}
	}

	sort.Strings(missing)
	sort.Strings(leaked)
	sort.Strings(wideMissing)

	detail := ""
	parts := []string{}
	if len(missing) > 0 {
		parts = append(parts, "missing callees: "+truncateGTList(missing))
	}
	if len(leaked) > 0 {
		parts = append(parts, "forbid_noise leaked as callee: "+truncateGTList(leaked))
	}
	if len(wideMissing) > 0 {
		parts = append(parts, "widened trace missing: "+truncateGTList(wideMissing))
	}
	if len(parts) > 0 {
		detail = strings.Join(parts, "; ")
	}

	tp := len(spec.ExpectCallees) - len(missing)
	return GroundTruthCheck{
		Repo:           repoName,
		Symbol:         sym.Name,
		Op:             OpTrace,
		Passed:         len(missing) == 0 && len(leaked) == 0 && len(wideMissing) == 0,
		Precision:      ratioPct(tp, tp+len(leaked)),
		Recall:         ratioPct(tp, tp+len(missing)),
		TruePositives:  tp,
		FalsePositives: len(leaked),
		FalseNegatives: len(missing),
		Expected:       len(spec.ExpectCallees),
		Actual:         len(callees),
		Details:        detail,
	}
}

func parseTraceCallees(out []byte) map[string]bool {
	trimmed := strings.TrimSpace(string(out))
	set := map[string]bool{}
	if trimmed == "" || trimmed == "null" {
		return set
	}
	// cymbal --json trace emits {"results": [...]} or bare array. Handle both.
	var obj groundTruthTraceResponse
	if err := json.Unmarshal(out, &obj); err == nil && len(obj.Results) > 0 {
		for _, r := range obj.Results {
			set[r.Callee] = true
		}
		return set
	}
	var arr []struct {
		Callee string `json:"callee"`
	}
	if err := json.Unmarshal(out, &arr); err == nil {
		for _, r := range arr {
			set[r.Callee] = true
		}
	}
	return set
}

func compareGroundTruth(repoName, symbol string, op Op, actual, expected []gtLoc) GroundTruthCheck {
	actualSet := map[string]gtLoc{}
	for _, loc := range actual {
		actualSet[gtLocKey(loc)] = loc
	}
	expectedSet := map[string]gtLoc{}
	for _, loc := range expected {
		expectedSet[gtLocKey(loc)] = loc
	}

	var missing []string
	var unexpected []string
	tp := 0
	for key, loc := range expectedSet {
		if _, ok := actualSet[key]; ok {
			tp++
			continue
		}
		missing = append(missing, formatGTLoc(loc))
	}
	for key, loc := range actualSet {
		if _, ok := expectedSet[key]; ok {
			continue
		}
		unexpected = append(unexpected, formatGTLoc(loc))
	}
	sort.Strings(missing)
	sort.Strings(unexpected)
	fp := len(unexpected)
	fn := len(missing)

	detail := ""
	if fn > 0 || fp > 0 {
		parts := make([]string, 0, 2)
		if fn > 0 {
			parts = append(parts, fmt.Sprintf("missing %s", truncateGTList(missing)))
		}
		if fp > 0 {
			parts = append(parts, fmt.Sprintf("unexpected %s", truncateGTList(unexpected)))
		}
		detail = strings.Join(parts, "; ")
	}

	return GroundTruthCheck{
		Repo:           repoName,
		Symbol:         symbol,
		Op:             op,
		Passed:         fn == 0 && fp == 0,
		Precision:      ratioPct(tp, tp+fp),
		Recall:         ratioPct(tp, tp+fn),
		TruePositives:  tp,
		FalsePositives: fp,
		FalseNegatives: fn,
		Expected:       len(expectedSet),
		Actual:         len(actualSet),
		Details:        detail,
	}
}

func sameGTLoc(a, b gtLoc, matchKind bool) bool {
	if normalizeGTPath(a.File) != normalizeGTPath(b.File) || a.Line != b.Line {
		return false
	}
	if !matchKind || a.Kind == "" || b.Kind == "" {
		return true
	}
	return a.Kind == b.Kind
}

func gtRank(actual []gtLoc, target gtLoc, matchKind bool) int {
	for i, loc := range actual {
		if sameGTLoc(loc, target, matchKind) {
			return i + 1
		}
	}
	return 0
}

func grepRank(candidates []grepCandidate, target gtLoc) int {
	for i, candidate := range candidates {
		if sameGTLoc(candidate.Loc, target, false) {
			return i + 1
		}
	}
	return 0
}

func reciprocalRank(rank int) float64 {
	if rank <= 0 {
		return 0
	}
	return 1.0 / float64(rank)
}

func appendDetail(existing, detail string) string {
	if detail == "" {
		return existing
	}
	if existing == "" {
		return detail
	}
	return existing + "; " + detail
}

func runGroundTruthCmd(repoDir, cymbalBin string, args ...string) ([]byte, error) {
	cmd := exec.Command(cymbalBin, args...)
	cmd.Dir = repoDir
	out, err := cmd.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	return out, nil
}

func ratioPct(numerator, denominator int) float64 {
	if denominator <= 0 {
		return 0
	}
	return float64(numerator) / float64(denominator) * 100
}

func gtLocKey(loc gtLoc) string {
	if loc.Kind != "" {
		return fmt.Sprintf("%s:%d:%s", normalizeGTPath(loc.File), loc.Line, loc.Kind)
	}
	return fmt.Sprintf("%s:%d", normalizeGTPath(loc.File), loc.Line)
}

func formatGTLoc(loc gtLoc) string {
	if loc.Kind != "" {
		return fmt.Sprintf("%s:%d (%s)", normalizeGTPath(loc.File), loc.Line, loc.Kind)
	}
	return fmt.Sprintf("%s:%d", normalizeGTPath(loc.File), loc.Line)
}

func truncateGTList(items []string) string {
	if len(items) <= 3 {
		return strings.Join(items, ", ")
	}
	return strings.Join(items[:3], ", ") + fmt.Sprintf(" (+%d more)", len(items)-3)
}

func normalizeGTPath(path string) string {
	path = filepath.ToSlash(path)
	path = strings.TrimPrefix(path, "./")
	return path
}

func relToRepo(repoDir, file string) string {
	if file == "" {
		return ""
	}
	repoBase := repoDir
	if !filepath.IsAbs(repoBase) {
		if abs, err := filepath.Abs(repoBase); err == nil {
			repoBase = abs
		}
	}
	target := file
	if !filepath.IsAbs(target) {
		target = filepath.Join(repoBase, target)
	}
	rel, err := filepath.Rel(repoBase, target)
	if err != nil {
		return normalizeGTPath(file)
	}
	return rel
}

// ── multi-symbol ground truth ──────────────────────────────────────
//
// runGroundTruthMulti drives `cymbal <op> sym1 sym2 ...` against the real
// corpus and asserts on the rendered text output. We check:
//
//   - ExpectHits: every substring must appear in stdout.
//   - ForbidHits: no substring may appear.
//   - ExpectAttribution: each listed name must appear on a line that also
//     contains an `[src1,src2,...]` tag containing at least the listed
//     Sources. This verifies the dedupe + union attribution behavior.
//
// Text-level assertions are intentionally coarse (robust to cosmetic tweaks,
// brittle enough to catch real regressions).
func runGroundTruthMulti(cymbalBin, repoName, repoDir string, mc GroundTruthMultiCase) GroundTruthCheck {
	check := GroundTruthCheck{
		Repo:   repoName,
		Symbol: mc.Name,
		Op:     Op(mc.Op),
	}
	if len(mc.Symbols) == 0 {
		check.Details = "multi_case requires symbols"
		return check
	}
	args := []string{mc.Op}
	args = append(args, mc.Flags...)
	args = append(args, mc.Symbols...)
	out, err := runGroundTruthCmd(repoDir, cymbalBin, args...)
	if err != nil {
		check.Details = err.Error()
		return check
	}
	text := string(out)

	var missing []string
	for _, want := range mc.ExpectHits {
		if !strings.Contains(text, want) {
			missing = append(missing, want)
		}
	}
	var leaked []string
	for _, bad := range mc.ForbidHits {
		if strings.Contains(text, bad) {
			leaked = append(leaked, bad)
		}
	}
	var attrMiss []string
	for _, hit := range mc.ExpectAttribution {
		if !lineHasAttribution(text, hit.Name, hit.Sources) {
			attrMiss = append(attrMiss,
				fmt.Sprintf("%s=[%s]", hit.Name, strings.Join(hit.Sources, ",")))
		}
	}

	sort.Strings(missing)
	sort.Strings(leaked)
	sort.Strings(attrMiss)

	var parts []string
	if len(missing) > 0 {
		parts = append(parts, "missing "+truncateGTList(missing))
	}
	if len(leaked) > 0 {
		parts = append(parts, "forbidden "+truncateGTList(leaked))
	}
	if len(attrMiss) > 0 {
		parts = append(parts, "attribution missing "+truncateGTList(attrMiss))
	}
	passed := len(missing) == 0 && len(leaked) == 0 && len(attrMiss) == 0

	tp := len(mc.ExpectHits) - len(missing)
	check.Passed = passed
	check.TruePositives = tp
	check.FalsePositives = len(leaked)
	check.FalseNegatives = len(missing)
	check.Expected = len(mc.ExpectHits) + len(mc.ExpectAttribution)
	check.Actual = tp + len(mc.ExpectAttribution) - len(attrMiss)
	if len(parts) > 0 {
		check.Details = strings.Join(parts, "; ")
	}
	return check
}

// lineHasAttribution returns true if any line of text contains `name` and a
// bracketed `[...]` tag that includes every source in `want`. Multi-symbol
// trace output also carries a leading depth tag like `[1]`, so we scan all
// `[...]` groups on the line and match on any one whose contents look like
// source names (non-numeric).
func lineHasAttribution(text, name string, want []string) bool {
	tagRe := regexp.MustCompile(`\[([^\[\]]+)\]`)
	for _, line := range strings.Split(text, "\n") {
		if !strings.Contains(line, name) {
			continue
		}
		for _, m := range tagRe.FindAllStringSubmatch(line, -1) {
			inner := m[1]
			// Skip depth-style tags ("[1]", "[12]"). Attribution always
			// contains non-digit characters.
			if _, err := strconv.Atoi(strings.TrimSpace(inner)); err == nil {
				continue
			}
			got := map[string]bool{}
			for _, s := range strings.Split(inner, ",") {
				got[strings.TrimSpace(s)] = true
			}
			missing := false
			for _, w := range want {
				if !got[w] {
					missing = true
					break
				}
			}
			if !missing {
				return true
			}
		}
	}
	return false
}
