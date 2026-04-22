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
)

const (
	GraphEdgeKindCall GraphEdgeKind = "call"
)

const (
	GraphUnresolvedExternal GraphUnresolvedReason = "external"
)

type GraphQuery struct {
	Symbol            string
	Direction         GraphDirection
	Depth             int
	Scope             []string
	Exclude           []string
	IncludeUnresolved bool
	// Limit caps the graph at the top-N nodes by degree (edges touching
	// the node). When >0 and the graph exceeds Limit, nodes are sorted
	// by degree desc (stable tie-break: node ID asc), truncated, and a
	// single sentinel node is appended to make truncation visible.
	// The root symbol is always kept. Zero means no cap.
	Limit int
}

type GraphNode struct {
	ID       string        `json:"id"`
	Kind     GraphNodeKind `json:"kind"`
	Label    string        `json:"label"`
	Symbol   string        `json:"symbol,omitempty"`
	Path     string        `json:"path,omitempty"`
	Language string        `json:"language,omitempty"`
}

type GraphEdge struct {
	From     string        `json:"from"`
	To       string        `json:"to"`
	Kind     GraphEdgeKind `json:"kind"`
	Resolved bool          `json:"resolved"`
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
}

type graphSymbolMeta struct {
	path     string
	language string
}

func (s *Store) BuildGraph(q GraphQuery) (*GraphResult, error) {
	if q.Depth <= 0 {
		q.Depth = 2
	}
	if q.Depth > 5 {
		q.Depth = 5
	}
	if q.Direction == "" {
		q.Direction = GraphDirectionDown
	}

	result := &GraphResult{Unresolved: []GraphUnresolved{}}
	if strings.TrimSpace(q.Symbol) == "" {
		result.Nodes = []GraphNode{}
		result.Edges = []GraphEdge{}
		return result, nil
	}

	metas, err := s.symbolMetas()
	if err != nil {
		return nil, err
	}

	nodes := map[string]GraphNode{}
	edges := map[string]GraphEdge{}
	unresolved := map[string]GraphUnresolved{}

	addNode := func(symbol string, meta graphSymbolMeta, kind GraphNodeKind) string {
		id := graphNodeID(symbol)
		if _, ok := nodes[id]; ok {
			return id
		}
		nodes[id] = GraphNode{
			ID:       id,
			Kind:     kind,
			Label:    symbol,
			Symbol:   symbol,
			Path:     meta.path,
			Language: meta.language,
		}
		return id
	}
	addExternal := func(symbol string) string {
		id := graphNodeID(symbol)
		if _, ok := nodes[id]; ok {
			return id
		}
		nodes[id] = GraphNode{
			ID:     id,
			Kind:   GraphNodeKindExternal,
			Label:  symbol,
			Symbol: symbol,
		}
		return id
	}
	includeSymbol := func(symbol string, meta graphSymbolMeta) bool {
		if meta.path == "" {
			return true
		}
		if matchesAnyGlob(meta.path, q.Exclude) {
			return false
		}
		if len(q.Scope) == 0 {
			return true
		}
		return matchesAnyGlob(meta.path, q.Scope)
	}
	allowExpansion := func(symbol string, meta graphSymbolMeta) bool {
		if meta.path == "" {
			return true
		}
		if matchesAnyGlob(meta.path, q.Exclude) {
			return false
		}
		if len(q.Scope) == 0 {
			return true
		}
		if matchesAnyGlob(meta.path, q.Scope) {
			return true
		}
		return symbol == q.Symbol
	}
	addResolvedEdge := func(from, to string) {
		key := from + "|" + to
		if _, ok := edges[key]; ok {
			return
		}
		edges[key] = GraphEdge{From: from, To: to, Kind: GraphEdgeKindCall, Resolved: true}
	}
	addUnresolvedEdge := func(fromID, key, resolvedAs string) {
		ukey := fromID + "|" + resolvedAs
		if _, ok := unresolved[ukey]; ok {
			return
		}
		u := GraphUnresolved{From: fromID, Key: key, Reason: GraphUnresolvedExternal, ResolvedAs: resolvedAs}
		if q.IncludeUnresolved {
			u.To = addExternal(resolvedAs)
			edges[ukey] = GraphEdge{From: fromID, To: u.To, Kind: GraphEdgeKindCall, Resolved: false}
		}
		unresolved[ukey] = u
	}

	if q.Direction == GraphDirectionDown || q.Direction == GraphDirectionBoth {
		rows, err := s.FindTrace(q.Symbol, q.Depth, 1000)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			fromMeta := metas[row.Caller]
			toMeta, ok := metas[row.Callee]
			if !includeSymbol(row.Caller, fromMeta) {
				continue
			}
			fromID := addNode(row.Caller, fromMeta, GraphNodeKindSymbol)
			if ok {
				if includeSymbol(row.Callee, toMeta) {
					toID := addNode(row.Callee, toMeta, GraphNodeKindSymbol)
					addResolvedEdge(fromID, toID)
				}
				continue
			}
			addUnresolvedEdge(fromID, row.Callee, "ext:"+row.Callee)
		}
	}

	if q.Direction == GraphDirectionUp || q.Direction == GraphDirectionBoth {
		rows, err := s.FindImpact(q.Symbol, q.Depth, 1000)
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			fromMeta, ok := metas[row.Caller]
			toMeta := metas[row.Symbol]
			if !ok || !includeSymbol(row.Caller, fromMeta) || !includeSymbol(row.Symbol, toMeta) {
				continue
			}
			fromID := addNode(row.Caller, fromMeta, GraphNodeKindSymbol)
			toID := addNode(row.Symbol, toMeta, GraphNodeKindSymbol)
			addResolvedEdge(fromID, toID)
		}
	}

	if rootMeta, ok := metas[q.Symbol]; ok && includeSymbol(q.Symbol, rootMeta) {
		addNode(q.Symbol, rootMeta, GraphNodeKindSymbol)
	}

	for sym, meta := range metas {
		if !allowExpansion(sym, meta) {
			continue
		}
		if _, ok := nodes[graphNodeID(sym)]; ok {
			continue
		}
	}

	result.Nodes = mapValuesSorted(nodes, func(a, b GraphNode) bool { return a.ID < b.ID })
	result.Edges = mapValuesSorted(edges, func(a, b GraphEdge) bool {
		if a.From != b.From {
			return a.From < b.From
		}
		if a.To != b.To {
			return a.To < b.To
		}
		return a.Resolved && !b.Resolved
	})
	result.Unresolved = mapValuesSorted(unresolved, func(a, b GraphUnresolved) bool {
		if a.From != b.From {
			return a.From < b.From
		}
		if a.ResolvedAs != b.ResolvedAs {
			return a.ResolvedAs < b.ResolvedAs
		}
		return a.Key < b.Key
	})

	if q.Limit > 0 && len(result.Nodes) > q.Limit {
		result = truncateByDegree(result, q.Limit, graphNodeID(q.Symbol))
	}
	return result, nil
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

func (s *Store) symbolMetas() (map[string]graphSymbolMeta, error) {
	rows, err := s.db.Query(`
		SELECT s.name, f.rel_path, s.language
		FROM symbols s
		JOIN files f ON s.file_id = f.id
		WHERE s.depth = 0
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := map[string]graphSymbolMeta{}
	for rows.Next() {
		var name, relPath, language string
		if err := rows.Scan(&name, &relPath, &language); err != nil {
			continue
		}
		if _, ok := out[name]; ok {
			continue
		}
		out[name] = graphSymbolMeta{path: filepath.ToSlash(relPath), language: language}
	}
	return out, rows.Err()
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
