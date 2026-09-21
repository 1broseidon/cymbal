package index

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
)

type GraphDirection string

type GraphFormat string

type GraphNodeKind string

type GraphEdgeKind string

type GraphUnresolvedReason string

const (
	GraphDirectionDown GraphDirection = "down"
	GraphDirectionUp   GraphDirection = "up"
	GraphDirectionBoth GraphDirection = "both"
)

const (
	GraphFormatMermaid GraphFormat = "mermaid"
	GraphFormatDot     GraphFormat = "dot"
	GraphFormatJSON    GraphFormat = "json"
)

const (
	GraphNodeKindSymbol   GraphNodeKind = "symbol"
	GraphNodeKindExternal GraphNodeKind = "external"
	GraphNodeKindSentinel GraphNodeKind = "sentinel"
	GraphNodeKindFile     GraphNodeKind = "file"
)

const (
	GraphEdgeKindCall       GraphEdgeKind = "call"
	GraphEdgeKindImport     GraphEdgeKind = "import"
	GraphEdgeKindImplements GraphEdgeKind = "implements"
)

const (
	// GraphUnresolvedExternal: the callee resolves to no indexed symbol in any
	// language (stdlib, third-party, or builtin).
	GraphUnresolvedExternal GraphUnresolvedReason = "external"
	// GraphUnresolvedScopeFiltered: the callee name exists in the index but
	// only in a language outside the active resolution scope. Re-run with
	// --resolve-scope all to traverse it.
	GraphUnresolvedScopeFiltered GraphUnresolvedReason = "scope_filtered"
)

type GraphQuery struct {
	Symbol            string
	Direction         GraphDirection
	Depth             int
	Scope             []string
	Exclude           []string
	IncludeUnresolved bool
	// ResolveScope controls cross-language callee resolution (down/trace
	// direction). Empty defaults to ResolveScopeFamily (see NormalizeScope).
	ResolveScope ResolveScope
	// Limit caps the graph at the top-N nodes by degree (edges touching
	// the node). When >0 and the graph exceeds Limit, nodes are sorted
	// by degree desc (stable tie-break: node ID asc), truncated, and a
	// single sentinel node is appended to make truncation visible.
	// The root symbol is always kept. Zero means no cap.
	Limit int
	// NoTests contracts test-classified symbol nodes out of the graph — the
	// graph analog of impact's hide-but-traverse: the test node is removed,
	// and callers reachable only through it stay connected to its targets via
	// synthesized indirect edges (see GraphEdge.Indirect). The root is exempt.
	NoTests bool
	// TestPaths are user-supplied --test-path patterns layered over the
	// built-in test conventions when classifying nodes for NoTests.
	TestPaths []string
}

type GraphNode struct {
	ID       string        `json:"id"`
	Kind     GraphNodeKind `json:"kind"`
	Label    string        `json:"label"`
	Symbol   string        `json:"symbol,omitempty"`
	Path     string        `json:"path,omitempty"`
	Language string        `json:"language,omitempty"`
	// DefinitionCount and Definitions annotate a node whose name is ambiguous
	// — i.e. the index holds more than one symbol with this name. Because
	// resolution is name-only, distinct same-named symbols collapse into this
	// single node; the annotation lets a consumer see the conflation and
	// investigate each definition. Both are omitted unless count > 1.
	DefinitionCount int               `json:"definition_count,omitempty"`
	Definitions     []GraphDefinition `json:"definitions,omitempty"`
}

// GraphDefinition locates one indexed definition of an ambiguous node's name.
type GraphDefinition struct {
	Path      string `json:"path,omitempty"`
	Language  string `json:"language,omitempty"`
	StartLine int    `json:"start_line,omitempty"`
}

type GraphEdge struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Kind     GraphEdgeKind `json:"kind"`
	Resolved bool          `json:"resolved"`
	// Indirect marks a contracted edge: From reaches To through one or more
	// hidden intermediate nodes (e.g. test callers removed by NoTests), not by
	// a direct call. Rendered dashed like unresolved edges.
	Indirect bool `json:"indirect,omitempty"`
}

type GraphUnresolved struct {
	From       string                `json:"from"`
	To         string                `json:"to,omitempty"`
	Key        string                `json:"key"`
	Reason     GraphUnresolvedReason `json:"reason"`
	ResolvedAs string                `json:"resolved_as,omitempty"`
}

type GraphResult struct {
	Nodes      []GraphNode       `json:"nodes"`
	Edges      []GraphEdge       `json:"edges"`
	Unresolved []GraphUnresolved `json:"unresolved"`
	// Truncated reports how many nodes were dropped by the Limit cap.
	// Zero when no truncation happened.
	Truncated int `json:"truncated,omitempty"`
	// EdgesTruncated reports that the underlying trace/impact row fetch hit
	// its internal cap, so the graph is incomplete even if no node-limit
	// truncation happened (Truncated == 0).
	EdgesTruncated bool `json:"edges_truncated,omitempty"`
	// ResolveScope is the active cross-language resolution scope (down/trace
	// direction). Empty when not applicable (e.g. importer/impls graphs).
	ResolveScope ResolveScope `json:"resolve_scope,omitempty"`
}

type graphSymbolMeta struct {
	path      string
	language  string
	startLine int
}

type graphBuilder struct {
	q          GraphQuery
	metas      map[string][]graphSymbolMeta
	nodes      map[string]GraphNode
	edges      map[string]GraphEdge
	unresolved map[string]GraphUnresolved
}

// metaVisible reports whether a definition's path passes the graph's
// --graph-scope / --exclude globs. A path-less meta (e.g. a name with no
// indexed definition) is treated as visible.
func (b *graphBuilder) metaVisible(meta graphSymbolMeta) bool {
	if meta.path == "" {
		return true
	}
	if matchesAnyGlob(meta.path, b.q.Exclude) {
		return false
	}
	if len(b.q.Scope) == 0 {
		return true
	}
	return matchesAnyGlob(meta.path, b.q.Scope)
}

// classifyMeta inspects every indexed definition of name (restricted to
// scopeLangs when non-empty) and reports three things, kept deliberately
// separate:
//
//   - resolved: at least one definition matches the scope languages. This
//     classifies cross-language collisions consistently with FindTrace (a name
//     that exists only outside the scope is unresolved, not a spurious edge).
//   - meta: the definition to display — a visible one when any matching
//     definition is visible, otherwise the first scope-matching definition.
//   - visible: at least one matching definition passes the scope/exclude globs.
//
// Resolution and visibility differ when a name resolves but every matching
// definition is filtered by --exclude/--graph-scope: that yields no edge
// (filtered out), not an external/scope_filtered diagnostic. Considering all
// definitions — not just the first — is what fixes same-name collisions where
// one definition is in scope and another is excluded.
//
// A name with no metadata is unresolved but path-less-visible, preserving the
// prior behavior for call sites that gate only on visibility.
func (b *graphBuilder) classifyMeta(name string, scopeLangs []string) (resolved bool, meta graphSymbolMeta, visible bool) {
	metas := b.metas[name]
	if len(metas) == 0 {
		return false, graphSymbolMeta{}, true
	}
	var firstScoped graphSymbolMeta
	haveScoped := false
	for _, m := range metas {
		if len(scopeLangs) > 0 && !inLangs(m.language, scopeLangs) {
			continue
		}
		if !haveScoped {
			firstScoped, haveScoped = m, true
		}
		if b.metaVisible(m) {
			return true, m, true
		}
	}
	if !haveScoped {
		return false, graphSymbolMeta{}, false
	}
	return true, firstScoped, false
}

func (s *Store) BuildGraph(q GraphQuery) (*GraphResult, error) {
	q = normalizeGraphQuery(q)
	if strings.TrimSpace(q.Symbol) == "" {
		return emptyGraphResult(), nil
	}

	var traceRows []TraceResult
	var impactRows []ImpactResult
	edgesTruncated := false
	if graphDirectionIncludesDown(q.Direction) {
		// Always collect unresolved callees so GraphResult.Unresolved
		// diagnostics are populated regardless of q.IncludeUnresolved;
		// addUnresolvedEdge gates whether ext: nodes/edges are actually
		// rendered. UnresolvedExemptFromLimit makes the 1000 cap bound only
		// resolved traversal breadth, so external/scope-filtered calls don't
		// crowd it out — the unresolved diagnostics themselves are not capped.
		rows, truncated, err := s.findTraceWithOptions(q.Symbol, q.Depth, graphEdgeRowCap, TraceOptions{
			IncludeUnresolved:         true,
			UnresolvedExemptFromLimit: true,
			Scope:                     q.ResolveScope,
		})
		if err != nil {
			return nil, err
		}
		edgesTruncated = edgesTruncated || truncated
		traceRows = rows
	}

	if graphDirectionIncludesUp(q.Direction) {
		// Scope the upward caller traversal by the seed's language family so
		// impact --graph matches the scoped impact text output.
		var langs []string
		if NormalizeScope(q.ResolveScope) != ResolveScopeAll {
			if seedLangs, err := s.SymbolLanguages(q.Symbol); err == nil {
				langs = scopeLanguagesUnion(seedLangs, q.ResolveScope)
			}
		}
		rows, truncated, err := s.findImpactInLangs(q.Symbol, langs, q.Depth, graphEdgeRowCap, false, nil)
		if err != nil {
			return nil, err
		}
		edgesTruncated = edgesTruncated || truncated
		impactRows = rows
	}
	metas, err := s.symbolMetas(graphSymbolNames(q.Symbol, traceRows, impactRows))
	if err != nil {
		return nil, err
	}
	builder := newGraphBuilder(q, metas)
	builder.addTraceRows(traceRows)
	builder.addImpactRows(impactRows)
	builder.addRoot()

	result := builder.result()
	if q.NoTests {
		result = contractTestNodes(result, graphNodeID(q.Symbol), NewClassifier(q.TestPaths))
	}
	if q.Limit > 0 && len(result.Nodes) > q.Limit {
		result = truncateByDegree(result, q.Limit, graphNodeID(q.Symbol))
	}
	// Set last so they survive builder.result, contraction, and truncation.
	result.ResolveScope = NormalizeScope(q.ResolveScope)
	result.EdgesTruncated = edgesTruncated
	return result, nil
}

// graphEdgeRowCap bounds the underlying trace/impact row fetch per direction.
// When the cap is hit, GraphResult.EdgesTruncated reports the graph as
// incomplete rather than truncating silently.
const graphEdgeRowCap = 1000

func normalizeGraphQuery(q GraphQuery) GraphQuery {
	if q.Depth <= 0 {
		q.Depth = 2
	}
	if q.Depth > 5 {
		q.Depth = 5
	}
	if q.Direction == "" {
		q.Direction = GraphDirectionDown
	}
	return q
}

func emptyGraphResult() *GraphResult {
	return &GraphResult{
		Nodes:      []GraphNode{},
		Edges:      []GraphEdge{},
		Unresolved: []GraphUnresolved{},
	}
}

func newGraphBuilder(q GraphQuery, metas map[string][]graphSymbolMeta) *graphBuilder {
	return &graphBuilder{
		q:          q,
		metas:      metas,
		nodes:      map[string]GraphNode{},
		edges:      map[string]GraphEdge{},
		unresolved: map[string]GraphUnresolved{},
	}
}

func (b *graphBuilder) result() *GraphResult {
	return &GraphResult{
		Nodes: mapValuesSorted(b.nodes, func(a, b GraphNode) bool { return a.ID < b.ID }),
		Edges: mapValuesSorted(b.edges, func(a, b GraphEdge) bool {
			if a.From != b.From {
				return a.From < b.From
			}
			if a.To != b.To {
				return a.To < b.To
			}
			return a.Resolved && !b.Resolved
		}),
		Unresolved: mapValuesSorted(b.unresolved, func(a, b GraphUnresolved) bool {
			if a.From != b.From {
				return a.From < b.From
			}
			if a.ResolvedAs != b.ResolvedAs {
				return a.ResolvedAs < b.ResolvedAs
			}
			return a.Key < b.Key
		}),
	}
}

func (b *graphBuilder) addTraceRows(rows []TraceResult) {
	for _, row := range rows {
		b.addTraceRow(row)
	}
}

func (b *graphBuilder) addTraceRow(row TraceResult) {
	// The caller is drawn whenever it's visible (it's already a resolved
	// trace symbol); an excluded/out-of-scope caller hard-cuts the row.
	_, fromMeta, fromVisible := b.classifyMeta(row.Caller, nil)
	if !fromVisible {
		return
	}
	fromID := b.addNode(row.Caller, fromMeta, GraphNodeKindSymbol)
	// Resolve the callee within the caller's resolution scope so a same-named
	// symbol outside the scope doesn't yield a spurious resolved edge.
	scopeLangs := scopeLanguages(row.Language, b.q.ResolveScope)
	resolved, toMeta, visible := b.classifyMeta(row.Callee, scopeLangs)
	if !resolved {
		// Distinguish an out-of-scope name collision (exists in another
		// language) from a genuinely external callee, so agents can tell why
		// no edge was drawn and re-run with --resolve-scope all if needed.
		reason := GraphUnresolvedExternal
		if len(b.metas[row.Callee]) > 0 {
			reason = GraphUnresolvedScopeFiltered
		}
		b.addUnresolvedEdge(fromID, row.Callee, "ext:"+row.Callee, reason)
		return
	}
	// Resolved but every matching definition is filtered by scope/exclude:
	// no edge (intentional filter), not an unresolved diagnostic.
	if visible {
		toID := b.addNode(row.Callee, toMeta, GraphNodeKindSymbol)
		b.addResolvedEdge(fromID, toID)
	}
}

func (b *graphBuilder) addImpactRows(rows []ImpactResult) {
	for _, row := range rows {
		b.addImpactRow(row)
	}
}

func (b *graphBuilder) addImpactRow(row ImpactResult) {
	// Caller must resolve and be visible; the target (already a resolved
	// impact symbol) only needs to be visible.
	fromResolved, fromMeta, fromVisible := b.classifyMeta(row.Caller, nil)
	_, toMeta, toVisible := b.classifyMeta(row.Symbol, nil)
	if !fromResolved || !fromVisible || !toVisible {
		return
	}
	fromID := b.addNode(row.Caller, fromMeta, GraphNodeKindSymbol)
	toID := b.addNode(row.Symbol, toMeta, GraphNodeKindSymbol)
	b.addResolvedEdge(fromID, toID)
}

func (b *graphBuilder) addRoot() {
	resolved, rootMeta, visible := b.classifyMeta(b.q.Symbol, nil)
	if resolved && visible {
		b.addNode(b.q.Symbol, rootMeta, GraphNodeKindSymbol)
	}
}

func (b *graphBuilder) addNode(symbol string, meta graphSymbolMeta, kind GraphNodeKind) string {
	id := graphNodeID(symbol)
	if _, ok := b.nodes[id]; ok {
		return id
	}
	node := GraphNode{
		ID:       id,
		Kind:     kind,
		Label:    symbol,
		Symbol:   symbol,
		Path:     meta.path,
		Language: meta.language,
	}
	// Annotate name collisions: a name-only node that maps to more than one
	// indexed definition is conflated (this single node stands for all of
	// them). Surface the count and locations so a consumer can disambiguate.
	if defs := b.metas[symbol]; len(defs) > 1 {
		node.DefinitionCount = len(defs)
		listed := defs
		if len(listed) > graphMaxDefinitionsListed {
			listed = listed[:graphMaxDefinitionsListed]
		}
		node.Definitions = make([]GraphDefinition, 0, len(listed))
		for _, d := range listed {
			node.Definitions = append(node.Definitions, GraphDefinition{
				Path:      d.path,
				Language:  d.language,
				StartLine: d.startLine,
			})
		}
	}
	b.nodes[id] = node
	return id
}

// graphMaxDefinitionsListed caps the per-node Definitions list for pathological
// names; DefinitionCount remains the authoritative total.
const graphMaxDefinitionsListed = 20

func (b *graphBuilder) addExternal(symbol string) string {
	id := graphNodeID(symbol)
	if _, ok := b.nodes[id]; ok {
		return id
	}
	b.nodes[id] = GraphNode{
		ID:     id,
		Kind:   GraphNodeKindExternal,
		Label:  symbol,
		Symbol: symbol,
	}
	return id
}

func (b *graphBuilder) addResolvedEdge(from, to string) {
	key := from + "|" + to
	if _, ok := b.edges[key]; ok {
		return
	}
	b.edges[key] = GraphEdge{From: from, To: to, Kind: GraphEdgeKindCall, Resolved: true}
}

func (b *graphBuilder) addUnresolvedEdge(fromID, key, resolvedAs string, reason GraphUnresolvedReason) {
	ukey := fromID + "|" + resolvedAs
	if _, ok := b.unresolved[ukey]; ok {
		return
	}
	u := GraphUnresolved{From: fromID, Key: key, Reason: reason, ResolvedAs: resolvedAs}
	if b.q.IncludeUnresolved {
		u.To = b.addExternal(resolvedAs)
		b.edges[ukey] = GraphEdge{From: fromID, To: u.To, Kind: GraphEdgeKindCall, Resolved: false}
	}
	b.unresolved[ukey] = u
}

func graphDirectionIncludesDown(direction GraphDirection) bool {
	return direction == GraphDirectionDown || direction == GraphDirectionBoth
}

func graphDirectionIncludesUp(direction GraphDirection) bool {
	return direction == GraphDirectionUp || direction == GraphDirectionBoth
}

// truncateByDegree keeps the top-N nodes ranked by edge degree (in + out),
// always preserving the root, and appends a single sentinel node so the
// truncation is visible in any renderer. Edges and unresolved entries whose
// endpoints were dropped are also removed.
func truncateByDegree(g *GraphResult, limit int, rootID string) *GraphResult {
	total := len(g.Nodes)
	if total <= limit {
		return g
	}

	degree := make(map[string]int, total)
	for _, e := range g.Edges {
		degree[e.From]++
		degree[e.To]++
	}

	type ranked struct {
		node   GraphNode
		deg    int
		isRoot bool
	}
	ranks := make([]ranked, 0, total)
	for _, n := range g.Nodes {
		ranks = append(ranks, ranked{node: n, deg: degree[n.ID], isRoot: n.ID == rootID})
	}
	sort.SliceStable(ranks, func(i, j int) bool {
		if ranks[i].isRoot != ranks[j].isRoot {
			return ranks[i].isRoot
		}
		if ranks[i].deg != ranks[j].deg {
			return ranks[i].deg > ranks[j].deg
		}
		return ranks[i].node.ID < ranks[j].node.ID
	})

	keep := make(map[string]bool, limit)
	kept := make([]GraphNode, 0, limit+1)
	for i := 0; i < limit && i < len(ranks); i++ {
		keep[ranks[i].node.ID] = true
		kept = append(kept, ranks[i].node)
	}

	dropped := total - len(kept)
	kept = append(kept, GraphNode{
		ID:    "_truncated",
		Kind:  GraphNodeKindSentinel,
		Label: fmt.Sprintf("… (%d more, truncated)", dropped),
	})

	filteredEdges := make([]GraphEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			filteredEdges = append(filteredEdges, e)
		}
	}
	filteredUnresolved := make([]GraphUnresolved, 0, len(g.Unresolved))
	for _, u := range g.Unresolved {
		if keep[u.From] {
			filteredUnresolved = append(filteredUnresolved, u)
		}
	}
	return &GraphResult{
		Nodes:      kept,
		Edges:      filteredEdges,
		Unresolved: filteredUnresolved,
		Truncated:  dropped,
	}
}

// contractTestNodes removes test-classified symbol nodes from a graph while
// preserving connectivity — the graph analog of impact's --no-tests
// hide-but-traverse semantics. Dropping a test node outright would orphan a
// production caller whose only path to the seed runs through a test helper, so
// each edge into a dropped node is rewired to that node's surviving targets as
// an Indirect edge. A node is contracted only when its definition file
// classifies confidently as test (unknown/ambiguous stay), and the root never
// is. A direct edge between two survivors always wins over a synthesized one.
func contractTestNodes(g *GraphResult, rootID string, cl *Classifier) *GraphResult {
	drop := map[string]bool{}
	for _, n := range g.Nodes {
		if n.ID == rootID || n.Kind != GraphNodeKindSymbol {
			continue
		}
		if n.Path != "" && cl.Classify(n.Path) == PathClassTest {
			drop[n.ID] = true
		}
	}
	if len(drop) == 0 {
		return g
	}

	out := map[string][]string{}
	for _, e := range g.Edges {
		out[e.From] = append(out[e.From], e.To)
	}
	// survivingTargets walks forward from a dropped node, through any chain of
	// dropped nodes (cycle-safe), to the surviving nodes it ultimately reaches.
	survivingTargets := func(start string) []string {
		var targets []string
		seen := map[string]bool{}
		visited := map[string]bool{start: true}
		queue := []string{start}
		for len(queue) > 0 {
			id := queue[0]
			queue = queue[1:]
			for _, to := range out[id] {
				switch {
				case drop[to]:
					if !visited[to] {
						visited[to] = true
						queue = append(queue, to)
					}
				case !seen[to]:
					seen[to] = true
					targets = append(targets, to)
				}
			}
		}
		return targets
	}

	edges := map[string]GraphEdge{}
	edgeKey := func(from, to string) string { return from + "|" + to }
	for _, e := range g.Edges {
		if !drop[e.From] && !drop[e.To] {
			edges[edgeKey(e.From, e.To)] = e
		}
	}
	for _, e := range g.Edges {
		if drop[e.From] || !drop[e.To] {
			continue
		}
		for _, t := range survivingTargets(e.To) {
			if t == e.From {
				continue // recursion through a hidden node: no self-edge
			}
			if k := edgeKey(e.From, t); edges[k].From == "" {
				edges[k] = GraphEdge{From: e.From, To: t, Kind: e.Kind, Resolved: true, Indirect: true}
			}
		}
	}

	kept := make([]GraphNode, 0, len(g.Nodes)-len(drop))
	for _, n := range g.Nodes {
		if !drop[n.ID] {
			kept = append(kept, n)
		}
	}
	unresolved := make([]GraphUnresolved, 0, len(g.Unresolved))
	for _, u := range g.Unresolved {
		if !drop[u.From] {
			unresolved = append(unresolved, u)
		}
	}

	return &GraphResult{
		Nodes: kept,
		Edges: mapValuesSorted(edges, func(a, b GraphEdge) bool {
			if a.From != b.From {
				return a.From < b.From
			}
			return a.To < b.To
		}),
		Unresolved:   unresolved,
		Truncated:    g.Truncated,
		ResolveScope: g.ResolveScope,
	}
}

func graphSymbolNames(root string, trace []TraceResult, impact []ImpactResult) []string {
	names := map[string]bool{root: true}
	for _, row := range trace {
		names[row.Caller] = true
		names[row.Callee] = true
	}
	for _, row := range impact {
		names[row.Caller] = true
		names[row.Symbol] = true
	}
	out := make([]string, 0, len(names))
	for name := range names {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

func (s *Store) symbolMetas(names []string) (map[string][]graphSymbolMeta, error) {
	out := map[string][]graphSymbolMeta{}
	// Stay below SQLite's variable limit even on builds configured for 999
	// parameters. A name belongs to one batch, preserving definition order.
	const batchSize = 500
	for start := 0; start < len(names); start += batchSize {
		batch := names[start:min(start+batchSize, len(names))]
		if err := s.loadSymbolMetas(batch, out); err != nil {
			return nil, err
		}
	}
	return out, nil
}

func (s *Store) loadSymbolMetas(names []string, out map[string][]graphSymbolMeta) error {
	// All depths, not just top-level: methods and other nested symbols are
	// legitimate call-graph nodes, and trace/impact resolve against every
	// depth — restricting metadata to depth 0 made the graph mislabel real
	// nested methods as external. Deterministic ordering (top-level first,
	// then language/path/line) makes classifyMeta's selection stable: the
	// first visible matching definition wins, and ambiguity annotations list
	// the definitions in this order.
	args := make([]any, len(names))
	for i, name := range names {
		args[i] = name
	}
	placeholders := strings.TrimSuffix(strings.Repeat("?,", len(names)), ",")
	rows, err := s.db.Query(`
		SELECT s.name, f.rel_path, s.language, s.start_line
		FROM symbols s
		JOIN files f ON s.file_id = f.id
		WHERE s.name IN (`+placeholders+`)
		ORDER BY s.depth, s.language, f.rel_path, s.start_line
	`, args...)
	if err != nil {
		return err
	}
	defer rows.Close()

	type definitionKey struct {
		name string
		graphSymbolMeta
	}
	seen := make(map[definitionKey]bool)
	for rows.Next() {
		var name, relPath, language string
		var startLine int
		if err := rows.Scan(&name, &relPath, &language, &startLine); err != nil {
			return err
		}
		meta := graphSymbolMeta{path: filepath.ToSlash(relPath), language: language, startLine: startLine}
		// Keep distinct definitions per name (used for ambiguity annotation)
		// so callee resolution can match by language; drop exact duplicates
		// such as the same symbol indexed across sibling worktrees.
		key := definitionKey{name, meta}
		if !seen[key] {
			seen[key] = true
			out[name] = append(out[name], meta)
		}
	}
	return rows.Err()
}

// GraphNodeIDFor returns the stable graph-node ID for a symbol name.
// Exported so the cmd layer can precompute root IDs when applying
// merged-graph truncation.
func GraphNodeIDFor(name string) string { return graphNodeID(name) }

func graphNodeID(name string) string {
	sum := sha256.Sum256([]byte(name))
	return "n" + hex.EncodeToString(sum[:8])
}

func matchesAnyGlob(path string, globs []string) bool {
	path = filepath.ToSlash(path)
	for _, pattern := range globs {
		pattern = filepath.ToSlash(strings.TrimSpace(pattern))
		if pattern == "" {
			continue
		}
		if ok, _ := filepath.Match(pattern, path); ok {
			return true
		}
		if ok, _ := filepath.Match(pattern, filepath.Base(path)); ok {
			return true
		}
	}
	return false
}

func mapValuesSorted[T any](m map[string]T, less func(a, b T) bool) []T {
	out := make([]T, 0, len(m))
	for _, v := range m {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return less(out[i], out[j]) })
	return out
}
