package index

import (
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strings"
)

// AffectedTestSymbol identifies a definition, not just a name. Empty Name denotes
// a changed-file seed. Paths are repository-relative, with slash separators.
type AffectedTestSymbol struct {
	Path      string `json:"path"`
	Name      string `json:"name,omitempty"`
	Kind      string `json:"kind,omitempty"`
	Language  string `json:"language,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
	EndLine   int    `json:"end_line,omitempty"`
	Deleted   bool   `json:"deleted,omitempty"`
}

type AffectedTestsInput struct {
	Files       []string                  `json:"files"`
	Symbols     []AffectedTestSymbol      `json:"symbols"`
	Precision   string                    `json:"precision"` // "hunk" (default) or "file"
	Limitations []AffectedTestsLimitation `json:"limitations"`
}

type AffectedTestsOptions struct {
	Depth        int          `json:"depth"`        // zero defaults to 2; maximum 5
	Limit        int          `json:"limit"`        // zero defaults to 50
	MaxSymbols   int          `json:"max_symbols"`  // zero means unlimited
	MaxEvidence  int          `json:"max_evidence"` // zero means unlimited
	ResolveScope ResolveScope `json:"resolve_scope"`
	TestPaths    []string     `json:"test_paths"`
}

// DefaultAffectedTestsOptions supplies the recommended CLI/library bounds.
// Start with these options when bounded defaults are wanted; zero budgets in
// AffectedTestsOptions deliberately mean unlimited.
func DefaultAffectedTestsOptions() AffectedTestsOptions {
	return AffectedTestsOptions{Depth: 2, Limit: 50, MaxSymbols: 40, MaxEvidence: 500, ResolveScope: ResolveScopeFamily, TestPaths: []string{}}
}

type AffectedTestsLimitation struct {
	Code    string              `json:"code"`
	Message string              `json:"message"`
	Path    string              `json:"path,omitempty"`
	Seed    *AffectedTestSymbol `json:"seed,omitempty"`
}

type AffectedTestsStep struct {
	Path          string `json:"path"`
	Name          string `json:"name,omitempty"`
	Kind          string `json:"kind,omitempty"`
	Language      string `json:"language,omitempty"`
	Line          int    `json:"line,omitempty"`
	EndLine       int    `json:"end_line,omitempty"`
	ReferenceKind string `json:"reference_kind,omitempty"`
	Import        string `json:"import,omitempty"`
	Target        string `json:"target,omitempty"`
	Ambiguous     bool   `json:"ambiguous"`
}

type AffectedTestsEvidence struct {
	Kind       string              `json:"kind"`
	Seed       AffectedTestSymbol  `json:"seed"`
	Distance   *int                `json:"distance"`
	Steps      []AffectedTestsStep `json:"steps"`
	Confidence string              `json:"confidence"`
	Ambiguous  bool                `json:"ambiguous"`
}

type AffectedTest struct {
	Path           string                  `json:"path"`
	Rank           int                     `json:"rank"`
	Confidence     string                  `json:"confidence"`
	Distance       *int                    `json:"distance"`
	ChangedSymbols []AffectedTestSymbol    `json:"changed_symbols"`
	Evidence       []AffectedTestsEvidence `json:"evidence"`
}

type AffectedTestsFallback struct {
	Recommended    bool   `json:"recommended"`
	Reason         string `json:"reason"`
	Advisory       string `json:"advisory"`
	StaticAnalysis string `json:"static_analysis"`
}

type AffectedTestsResult struct {
	SchemaVersion    int                       `json:"schema_version"`
	Status           string                    `json:"status"`
	InputPrecision   string                    `json:"input_precision"`
	Bounds           AffectedTestsOptions      `json:"bounds"`
	ExaminedEvidence int                       `json:"examined_evidence"`
	Tests            []AffectedTest            `json:"tests"`
	Limitations      []AffectedTestsLimitation `json:"limitations"`
	Fallback         AffectedTestsFallback     `json:"fallback"`
}

const affectedAdvisory = "Selection does not prove that unselected tests are safe to skip."
const affectedStatic = "Static name-based analysis cannot resolve every callback, alias, reflection, or interface dispatch. Classified test paths are not a runnable test inventory."

// FindAffectedTests selects and ranks tests in this database only. It does not
// refresh the index, run Git, print output, or guarantee test coverage. Files
// with file precision are expanded to every indexed definition inside the library.
// With hunk precision, Files only retains changed paths; Symbols supplies seeds.
// Empty precision selects file mode when Files is nonempty, otherwise hunk mode.
// Input diagnostics
// from diff attribution can be passed independently through Limitations.
func FindAffectedTests(dbPath string, input AffectedTestsInput, opts AffectedTestsOptions) (*AffectedTestsResult, error) {
	if opts.Depth < 0 || opts.Limit < 0 || opts.MaxSymbols < 0 || opts.MaxEvidence < 0 {
		return nil, fmt.Errorf("affected tests: bounds must not be negative")
	}
	if opts.Depth == 0 {
		opts.Depth = 2
	}
	if opts.Depth > 5 {
		opts.Depth = 5
	}
	if opts.Limit == 0 {
		opts.Limit = 50
	}
	opts.ResolveScope = NormalizeScope(opts.ResolveScope)
	opts.TestPaths = append([]string{}, opts.TestPaths...)
	sort.Strings(opts.TestPaths)
	precision := input.Precision
	if precision == "" {
		precision = "hunk"
		if len(input.Files) > 0 {
			precision = "file"
		}
	}
	if precision != "hunk" && precision != "file" {
		return nil, fmt.Errorf("affected tests: invalid precision %q", precision)
	}
	store, err := OpenStore(dbPath)
	if err != nil {
		return nil, err
	}
	defer store.Close()
	q, err := newAffectedQueries(store, opts.MaxEvidence)
	if err != nil {
		return nil, fmt.Errorf("affected tests: %w", err)
	}
	a := &affectedSelector{q: q, classifier: NewClassifier(opts.TestPaths), result: &AffectedTestsResult{SchemaVersion: 1, Status: "complete", InputPrecision: precision, Bounds: opts, Tests: []AffectedTest{}, Limitations: []AffectedTestsLimitation{}, Fallback: AffectedTestsFallback{Advisory: affectedAdvisory, StaticAnalysis: affectedStatic}}, limitations: map[string]bool{}, candidates: map[string]*AffectedTest{}, charged: map[string]bool{}}
	for _, l := range input.Limitations {
		a.limitation(l)
	}
	files := map[string]bool{}
	seeds := map[string]AffectedTestSymbol{}
	for _, f := range input.Files {
		p, e := affectedPath(f)
		if e != nil {
			return nil, e
		}
		files[p] = true
		if precision == "file" {
			for _, s := range q.byFile[p] {
				seeds[affectedKey(s)] = s
			}
		}
	}
	for _, s := range input.Symbols {
		p, e := affectedPath(s.Path)
		if e != nil {
			return nil, e
		}
		s.Path = p
		files[p] = true
		if s.Name != "" {
			s = q.completeSeed(s)
			seeds[affectedKey(s)] = s
		}
	}
	if precision == "file" && len(files) > 0 {
		a.gap("file_precision", "Whole-file input cannot identify which definitions changed.", "", nil)
	}
	for _, p := range sortedKeys(files) {
		if _, ok := q.files[p]; !ok {
			a.gap("missing_file", "Changed path is not in the live index.", p, nil)
		}
	}
	ordered := make([]AffectedTestSymbol, 0, len(seeds))
	for _, s := range seeds {
		ordered = append(ordered, s)
	}
	sort.Slice(ordered, func(i, j int) bool { return affectedKey(ordered[i]) < affectedKey(ordered[j]) })
	admitted := ordered
	if opts.MaxSymbols > 0 && len(admitted) > opts.MaxSymbols {
		for _, s := range admitted[opts.MaxSymbols:] {
			a.gap("seed_limit", "Seed bound skipped this definition.", s.Path, &s)
		}
		admitted = admitted[:opts.MaxSymbols]
	}
	fileSeeds := map[string][]AffectedTestSymbol{}
	for _, s := range admitted {
		fileSeeds[s.Path] = append(fileSeeds[s.Path], s)
	}
	analyzed := false
	productionPaths := []string{}
	fallbackPaths := map[string]bool{}
	// Process all changed tests and symbol relationships before spending the
	// shared dependency budget on weaker file heuristics.
	for _, p := range sortedKeys(files) {
		_, exists := q.files[p]
		if exists && a.classifier.Classify(p) == PathClassTest {
			changedSeeds := fileSeeds[p]
			if precision == "file" || len(changedSeeds) == 0 {
				changedSeeds = []AffectedTestSymbol{{Path: p}}
			}
			for _, s := range changedSeeds {
				a.add(p, AffectedTestsEvidence{Kind: "changed_test", Seed: s, Distance: intPointer(0), Steps: []AffectedTestsStep{symbolStep(s, false)}, Confidence: "high"})
			}
			analyzed = true
			continue
		}
		productionPaths = append(productionPaths, p)
		if len(fileSeeds[p]) == 0 {
			a.gap("no_symbol_seed", "Changed content has no usable indexed symbol; file evidence is heuristic.", p, nil)
			fallbackPaths[p] = true
		}
	}
	for _, s := range admitted {
		if a.classifier.Classify(s.Path) == PathClassTest {
			if _, exists := q.files[s.Path]; exists {
				continue
			}
		}
		live := q.live(s)
		if !live || s.Deleted {
			code := "missing_live_seed"
			if s.Deleted {
				code = "deleted_seed"
			}
			a.gap(code, "Changed definition is absent or deleted; name matches cannot establish a live target.", s.Path, &s)
		}
		if live && !s.Deleted {
			analyzed = true
		}
		symbolCandidate := false
		if live || s.Deleted {
			var e error
			symbolCandidate, e = a.symbolWalk(s)
			if e != nil {
				return nil, e
			}
		}
		fallbackPaths[s.Path] = fallbackPaths[s.Path] || !symbolCandidate
	}
	for _, gap := range input.Limitations {
		if gap.Code == "outside_symbols" {
			p, err := affectedPath(gap.Path)
			if err != nil {
				return nil, err
			}
			fallbackPaths[p] = true
		}
	}
	// File imports and naming cannot attribute a use to a definition. Their
	// seeds remain changed-file facts, not invented per-definition support.
	for _, p := range productionPaths {
		if e := a.importWalk(AffectedTestSymbol{Path: p}, false); e != nil {
			return nil, e
		}
	}
	for _, p := range productionPaths {
		a.naming(AffectedTestSymbol{Path: p})
	}
	for _, p := range productionPaths {
		if fallbackPaths[p] {
			if e := a.importWalk(AffectedTestSymbol{Path: p}, true); e != nil {
				return nil, e
			}
		}
	}
	if len(a.candidates) > 0 || a.result.ExaminedEvidence > 0 {
		analyzed = true
	}
	if (len(files) > 0 || len(input.Limitations) > 0) && !analyzed {
		a.result.Status = "unavailable"
	} else if len(a.result.Limitations) > 0 {
		a.result.Status = "partial"
	}
	a.finish()
	return a.result, nil
}

func affectedPath(p string) (string, error) {
	p = strings.ReplaceAll(p, "\\", "/")
	p = path.Clean(p)
	if p == "." || p == ".." || strings.HasPrefix(p, "../") || path.IsAbs(p) || (len(p) > 1 && p[1] == ':') {
		return "", fmt.Errorf("affected tests: expected repository-relative file path: %q", p)
	}
	return p, nil
}

// Keys contain only the selector's fixed, JSON-safe value types.
func affectedKey(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("affected tests: invalid internal key: %v", err))
	}
	return string(b)
}
func sortedKeys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
func intPointer(n int) *int { return &n }
func (a *affectedSelector) limitation(l AffectedTestsLimitation) {
	k := affectedKey(l)
	if !a.limitations[k] {
		a.limitations[k] = true
		a.result.Limitations = append(a.result.Limitations, l)
	}
}
func (a *affectedSelector) gap(code, msg, p string, s *AffectedTestSymbol) {
	a.limitation(AffectedTestsLimitation{Code: code, Message: msg, Path: p, Seed: s})
}
func evidenceStrength(k string) int {
	switch k {
	case "changed_test":
		return 0
	case "direct_reference":
		return 1
	case "transitive_reference":
		return 2
	case "direct_import":
		return 3
	case "colocated":
		return 4
	default:
		return 5
	}
}
func confidenceStrength(s string) int {
	switch s {
	case "high":
		return 0
	case "medium":
		return 1
	default:
		return 2
	}
}
func evidenceLess(x, y AffectedTestsEvidence) bool {
	if evidenceStrength(x.Kind) != evidenceStrength(y.Kind) {
		return evidenceStrength(x.Kind) < evidenceStrength(y.Kind)
	}
	dx, dy := 0, 0
	if x.Distance != nil {
		dx = *x.Distance
	}
	if y.Distance != nil {
		dy = *y.Distance
	}
	if dx != dy {
		return dx < dy
	}
	if x.Ambiguous != y.Ambiguous {
		return !x.Ambiguous
	}
	return affectedKey(x) < affectedKey(y)
}
func (a *affectedSelector) add(p string, e AffectedTestsEvidence) {
	t := a.candidates[p]
	if t == nil {
		t = &AffectedTest{Path: p, ChangedSymbols: []AffectedTestSymbol{}, Evidence: []AffectedTestsEvidence{}}
		a.candidates[p] = t
	}
	// Only terminal sites and seeds count as support, not alternate graph paths.
	key := func(v AffectedTestsEvidence) string {
		return affectedKey(v.Seed) + v.Kind + affectedKey(v.Steps[len(v.Steps)-1])
	}
	for i, old := range t.Evidence {
		if key(old) == key(e) {
			if e.Distance != nil && old.Distance != nil && (*e.Distance < *old.Distance || (*e.Distance == *old.Distance && affectedKey(e.Steps) < affectedKey(old.Steps))) {
				t.Evidence[i] = e
			}
			return
		}
	}
	t.Evidence = append(t.Evidence, e)
}
func (a *affectedSelector) finish() {
	for _, p := range sortedKeys(a.candidates) {
		t := a.candidates[p]
		sort.Slice(t.Evidence, func(i, j int) bool { return evidenceLess(t.Evidence[i], t.Evidence[j]) })
		t.Confidence = "low"
		syms := map[string]AffectedTestSymbol{}
		for _, e := range t.Evidence {
			if confidenceStrength(e.Confidence) < confidenceStrength(t.Confidence) {
				t.Confidence = e.Confidence
			}
			if e.Distance != nil && (t.Distance == nil || *e.Distance < *t.Distance) {
				t.Distance = intPointer(*e.Distance)
			}
			if e.Seed.Name != "" {
				syms[affectedKey(e.Seed)] = e.Seed
			}
		}
		for _, k := range sortedKeys(syms) {
			t.ChangedSymbols = append(t.ChangedSymbols, syms[k])
		}
		a.result.Tests = append(a.result.Tests, *t)
	}
	sort.Slice(a.result.Tests, func(i, j int) bool {
		x, y := a.result.Tests[i], a.result.Tests[j]
		ex, ey := x.Evidence[0], y.Evidence[0]
		if evidenceStrength(ex.Kind) != evidenceStrength(ey.Kind) {
			return evidenceStrength(ex.Kind) < evidenceStrength(ey.Kind)
		}
		dx, dy := 0, 0
		if ex.Distance != nil {
			dx = *ex.Distance
		}
		if ey.Distance != nil {
			dy = *ey.Distance
		}
		if dx != dy {
			return dx < dy
		}
		if ex.Ambiguous != ey.Ambiguous {
			return !ex.Ambiguous
		}
		support := func(t AffectedTest) int {
			n := 0
			for _, e := range t.Evidence {
				if e.Kind == t.Evidence[0].Kind {
					n++
				}
			}
			return n
		}
		if support(x) != support(y) {
			return support(x) > support(y)
		}
		return x.Path < y.Path
	})
	if len(a.result.Tests) > a.result.Bounds.Limit {
		a.gap("result_limit", "Ranked test bound omitted candidates.", "", nil)
		a.result.Tests = a.result.Tests[:a.result.Bounds.Limit]
		if a.result.Status == "complete" {
			a.result.Status = "partial"
		}
	}
	for i := range a.result.Tests {
		a.result.Tests[i].Rank = i + 1
	}
	sort.Slice(a.result.Limitations, func(i, j int) bool {
		return affectedKey(a.result.Limitations[i]) < affectedKey(a.result.Limitations[j])
	})
	if a.result.Status != "complete" {
		a.result.Fallback.Recommended = true
		a.result.Fallback.Reason = "Analysis has gaps. Run the full suite."
	}
}

type affectedSelector struct {
	q           *affectedQueries
	classifier  *Classifier
	result      *AffectedTestsResult
	limitations map[string]bool
	candidates  map[string]*AffectedTest
	charged     map[string]bool
}

func (a *affectedSelector) admit(key, p string, s AffectedTestSymbol) bool {
	if a.charged[key] {
		return true
	}
	if a.result.Bounds.MaxEvidence > 0 && a.result.ExaminedEvidence >= a.result.Bounds.MaxEvidence {
		a.gap("evidence_limit", "Dependency evidence bound skipped work.", p, &s)
		return false
	}
	a.charged[key] = true
	a.result.ExaminedEvidence++
	return true
}
func (a *affectedSelector) ambiguous(s AffectedTestSymbol) bool {
	n := a.q.definitionCount(s.Name, scopeLanguages(s.Language, a.result.Bounds.ResolveScope))
	if n > 1 {
		a.gap("ambiguous_definitions", fmt.Sprintf("Name matches %d scoped definitions.", n), s.Path, &s)
		return true
	}
	return false
}
func indirectKind(k string) bool {
	return k == "method" || k == "interface" || k == "protocol" || k == "trait"
}
func symbolStep(s AffectedTestSymbol, amb bool) AffectedTestsStep {
	return AffectedTestsStep{Path: s.Path, Name: s.Name, Kind: s.Kind, Language: s.Language, Line: s.StartLine, EndLine: s.EndLine, Ambiguous: amb}
}

type affectedSymbolNode struct {
	s        AffectedTestSymbol
	steps    []AffectedTestsStep
	amb      bool
	distance int
}

func (a *affectedSelector) symbolWalk(seed AffectedTestSymbol) (bool, error) {
	amb := a.ambiguous(seed) || seed.Deleted
	queue := []affectedSymbolNode{{s: seed, steps: []AffectedTestsStep{symbolStep(seed, amb)}, amb: amb}}
	seen := map[string]bool{}
	reached := map[string]bool{affectedKey(seed): true}
	found := false
	for len(queue) > 0 {
		sort.Slice(queue, func(i, j int) bool {
			if queue[i].distance != queue[j].distance {
				return queue[i].distance < queue[j].distance
			}
			if queue[i].amb != queue[j].amb {
				return !queue[i].amb
			}
			return affectedKey(queue[i].steps) < affectedKey(queue[j].steps)
		})
		n := queue[0]
		queue = queue[1:]
		nk := affectedKey(n.s)
		if seen[nk] {
			continue
		}
		seen[nk] = true
		if indirectKind(n.s.Kind) {
			a.gap("indirect_dispatch", "Method or interface dispatch is not semantically resolved.", n.s.Path, &seed)
		}
		refs, err := a.q.references(n.s.Name, scopeLanguages(n.s.Language, a.result.Bounds.ResolveScope), n.distance >= a.result.Bounds.Depth)
		if err != nil {
			return false, err
		}
		for _, r := range refs {
			caller, ok, containerAmbiguous := a.q.enclosing(r.Path, r.Line)
			terminal := a.classifier.Classify(r.Path) == PathClassTest
			if n.distance >= a.result.Bounds.Depth {
				if terminal || !ok || !reached[affectedKey(caller)] {
					a.gap("depth_limit", "Unvisited references remain beyond the depth bound.", r.Path, &seed)
				}
				continue
			}
			if !a.admit("ref:"+affectedKey(r), r.Path, seed) {
				break
			}
			if containerAmbiguous {
				a.gap("ambiguous_container", "Line-only reference overlaps equally narrow definitions; its enclosing symbol is uncertain.", r.Path, &seed)
			}
			uncertain := r.Kind != "call" && !terminal
			if uncertain || r.Kind == "implements" {
				a.gap("indirect_dispatch", "Observed non-call use or implements relationship does not resolve runtime dispatch.", r.Path, &seed)
			}
			if !terminal && !ok {
				a.gap("uncontained_reference", "Production reference has no enclosing symbol; further symbol traversal is unavailable.", r.Path, &seed)
				continue
			}
			if !terminal && seen[affectedKey(caller)] {
				continue
			}
			nextAmb := n.amb || uncertain || containerAmbiguous || r.Kind == "implements" || indirectKind(n.s.Kind)
			if ok && !terminal {
				nextAmb = a.ambiguous(caller) || nextAmb
			}
			st := AffectedTestsStep{Path: r.Path, Name: caller.Name, Kind: caller.Kind, Language: r.Language, Line: r.Line, EndLine: caller.EndLine, ReferenceKind: r.Kind, Target: r.Name, Ambiguous: nextAmb}
			steps := append(append([]AffectedTestsStep{}, n.steps...), st)
			if terminal {
				kind, confidence := "transitive_reference", "medium"
				if n.distance == 0 {
					kind = "direct_reference"
					if a.result.InputPrecision == "hunk" {
						confidence = "high"
					}
				}
				if nextAmb {
					confidence = "low"
				}
				a.add(r.Path, AffectedTestsEvidence{Kind: kind, Seed: seed, Distance: intPointer(n.distance + 1), Steps: steps, Confidence: confidence, Ambiguous: nextAmb})
				found = true
				continue
			}
			reached[affectedKey(caller)] = true
			queue = append(queue, affectedSymbolNode{s: caller, steps: steps, amb: nextAmb, distance: n.distance + 1})
		}
	}
	return found, nil
}

type affectedFileNode struct {
	path     string
	steps    []AffectedTestsStep
	distance int
}

func (a *affectedSelector) importWalk(seed AffectedTestSymbol, fallback bool) error {
	queue := []affectedFileNode{{path: seed.Path, steps: []AffectedTestsStep{symbolStep(seed, false)}}}
	seen := map[string]bool{}
	reached := map[string]bool{seed.Path: true}
	for len(queue) > 0 {
		sort.Slice(queue, func(i, j int) bool {
			if queue[i].distance != queue[j].distance {
				return queue[i].distance < queue[j].distance
			}
			return affectedKey(queue[i].steps) < affectedKey(queue[j].steps)
		})
		n := queue[0]
		queue = queue[1:]
		if seen[n.path] {
			continue
		}
		seen[n.path] = true
		rows, err := a.q.importers(n.path, n.distance >= a.result.Bounds.Depth)
		if err != nil {
			return err
		}
		for _, r := range rows {
			terminal := a.classifier.Classify(r.Path) == PathClassTest
			if n.distance >= a.result.Bounds.Depth {
				if reached[r.Path] {
					continue
				}
				if fallback {
					a.gap("depth_limit", "Unvisited import relationships remain beyond the depth bound.", r.Path, &seed)
				}
				continue
			}
			if !a.admit("import:"+n.path+":"+affectedKey(r), r.Path, seed) {
				break
			}
			if seen[r.Path] {
				continue
			}
			a.gap("heuristic_import", "Raw import suffix, basename, or package-directory matching is heuristic.", r.Path, &seed)
			steps := append(append([]AffectedTestsStep{}, n.steps...), AffectedTestsStep{Path: r.Path, Import: r.Raw, Target: n.path})
			if terminal {
				kind := "direct_import"
				if n.distance > 0 {
					kind = "reverse_import"
				}
				a.add(r.Path, AffectedTestsEvidence{Kind: kind, Seed: seed, Distance: intPointer(n.distance + 1), Steps: steps, Confidence: "low"})
				continue
			}
			if fallback {
				reached[r.Path] = true
				queue = append(queue, affectedFileNode{path: r.Path, steps: steps, distance: n.distance + 1})
			}
		}
	}
	return nil
}
func (a *affectedSelector) naming(seed AffectedTestSymbol) {
	dir, base := path.Dir(seed.Path), path.Base(seed.Path)
	ext := path.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	for _, p := range sortedKeys(a.q.files) {
		if path.Dir(p) != dir || p == seed.Path || a.classifier.Classify(p) != PathClassTest {
			continue
		}
		b := path.Base(p)
		match := false
		switch ext {
		case ".go":
			match = b == stem+"_test.go"
		case ".py":
			match = b == "test_"+stem+".py" || b == stem+"_test.py"
		default:
			if affectedJSExt(ext) && affectedJSExt(path.Ext(b)) {
				noExt := strings.TrimSuffix(b, path.Ext(b))
				match = noExt == stem+".test" || noExt == stem+".spec"
			}
		}
		if match {
			a.gap("heuristic_naming", "Colocated filename matching is not dependency evidence.", p, &seed)
			a.add(p, AffectedTestsEvidence{Kind: "colocated", Seed: seed, Steps: []AffectedTestsStep{symbolStep(seed, false), {Path: p}}, Confidence: "low"})
		}
	}
}
func affectedJSExt(s string) bool {
	switch s {
	case ".js", ".jsx", ".ts", ".tsx", ".mjs", ".cjs", ".mts", ".cts":
		return true
	}
	return false
}
