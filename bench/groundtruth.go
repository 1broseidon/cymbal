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
		}
	}
	return checks
}

func summarizeGroundTruth(checks []GroundTruthCheck) GroundTruthSummary {
	var summary GroundTruthSummary
	var searchTP, searchFP, searchFN int
	var refsTP, refsFP, refsFN int
	var showPassed, showTotal int

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
		}
	}

	summary.SearchPrecision = ratioPct(searchTP, searchTP+searchFP)
	summary.SearchRecall = ratioPct(searchTP, searchTP+searchFN)
	summary.RefsPrecision = ratioPct(refsTP, refsTP+refsFP)
	summary.RefsRecall = ratioPct(refsTP, refsTP+refsFN)
	summary.ShowExactRate = ratioPct(showPassed, showTotal)
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
