package main

import (
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"sort"
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
	Exact    bool                `yaml:"exact"`
	Limit    int                 `yaml:"limit"`
	Expected []GroundTruthSymbol `yaml:"expected"`
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
