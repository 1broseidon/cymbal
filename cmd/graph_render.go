package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

// mermaidNodeCeiling is the hard cap on nodes in a mermaid render. Above
// this, renderers (GitHub, mermaid-live, most in-terminal viewers) begin
// to choke. When the graph would exceed this, we auto-truncate to the
// most-connected nodes and emit a stderr warning. DOT and JSON have no
// ceiling.
const mermaidNodeCeiling = 500

// addGraphFlags wires the shared --graph family of flags onto a command
// that produces edges (trace, impact, etc.). Direction is fixed per-verb
// by the caller.
func addGraphFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("graph", false, "render output as a graph (mermaid on TTY, json when piped)")
	cmd.Flags().String("graph-format", "", "graph output format: mermaid, dot, or json (implies --graph)")
	cmd.Flags().Bool("include-unresolved", false, "include unresolved external calls as dashed ext:<fqn> nodes")
	cmd.Flags().Int("graph-limit", 0, "cap graph at top-N nodes by degree (0 = no cap)")
}

// graphRequested reports whether the user asked for graph output on a verb
// that supports --graph. Returns true if --graph was passed or --graph-format
// was set to a non-empty value.
func graphRequested(cmd *cobra.Command) bool {
	if enabled, _ := cmd.Flags().GetBool("graph"); enabled {
		return true
	}
	if raw, _ := cmd.Flags().GetString("graph-format"); strings.TrimSpace(raw) != "" {
		return true
	}
	return false
}

// selectGraphFormatFromVerb picks the concrete output format for a verb
// that supports --graph. Precedence: --graph-format > --json > TTY default
// (mermaid) / piped default (json).
func selectGraphFormatFromVerb(cmd *cobra.Command) index.GraphFormat {
	if raw, _ := cmd.Flags().GetString("graph-format"); strings.TrimSpace(raw) != "" {
		return index.GraphFormat(strings.TrimSpace(raw))
	}
	if getJSONFlag(cmd) {
		return index.GraphFormatJSON
	}
	if info, err := os.Stdout.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		return index.GraphFormatJSON
	}
	return index.GraphFormatMermaid
}

// graphDepthOrDefault returns the effective depth for a graph render on a
// verb that already owns a --depth flag. When the user didn't override
// --depth, we use a tighter default for graph output than the text output
// would use — hotspots like impact on frequently-called symbols blow up
// visually at depth 2 but read fine at depth 1.
func graphDepthOrDefault(cmd *cobra.Command, graphDefault int) int {
	if cmd.Flags().Changed("depth") {
		d, _ := cmd.Flags().GetInt("depth")
		return d
	}
	return graphDefault
}

// renderAsGraph builds a graph for the given symbols + direction and renders
// it in the format selected by the verb's flags. It merges per-symbol graphs
// by de-duplicating nodes/edges via their stable IDs and applies the
// top-N-by-degree limit (user-supplied via --graph-limit, or the auto mermaid
// ceiling, whichever is tighter).
func renderAsGraph(cmd *cobra.Command, dbPath string, symbols []string, direction index.GraphDirection, graphDefaultDepth int) error {
	format := selectGraphFormatFromVerb(cmd)
	if len(symbols) == 0 {
		return renderGraph(format, &index.GraphResult{
			Nodes:      []index.GraphNode{},
			Edges:      []index.GraphEdge{},
			Unresolved: []index.GraphUnresolved{},
		})
	}
	depth := graphDepthOrDefault(cmd, graphDefaultDepth)
	includeUnresolved, _ := cmd.Flags().GetBool("include-unresolved")
	userLimit, _ := cmd.Flags().GetInt("graph-limit")

	merged := &index.GraphResult{
		Nodes:      []index.GraphNode{},
		Edges:      []index.GraphEdge{},
		Unresolved: []index.GraphUnresolved{},
	}
	seenNodes := map[string]bool{}
	seenEdges := map[string]bool{}
	seenUnresolved := map[string]bool{}

	for _, sym := range symbols {
		q := index.GraphQuery{
			Symbol:            sym,
			Direction:         direction,
			Depth:             depth,
			IncludeUnresolved: includeUnresolved,
		}
		g, err := index.BuildGraph(dbPath, q)
		if err != nil {
			return fmt.Errorf("graph %q: %w", sym, err)
		}
		for _, n := range g.Nodes {
			if !seenNodes[n.ID] {
				seenNodes[n.ID] = true
				merged.Nodes = append(merged.Nodes, n)
			}
		}
		for _, e := range g.Edges {
			key := e.From + "->" + e.To
			if !seenEdges[key] {
				seenEdges[key] = true
				merged.Edges = append(merged.Edges, e)
			}
		}
		for _, u := range g.Unresolved {
			key := u.From + "|" + u.Key
			if !seenUnresolved[key] {
				seenUnresolved[key] = true
				merged.Unresolved = append(merged.Unresolved, u)
			}
		}
	}

	merged = applyGraphLimit(merged, userLimit, format, symbols)
	return renderGraph(format, merged)
}

// applyGraphLimit applies the tighter of the user's --graph-limit and the
// mermaid auto-ceiling (mermaid only). Roots are always kept. Emits a
// stderr warning when the mermaid ceiling bites (the user-set limit is
// self-inflicted, no warning).
func applyGraphLimit(g *index.GraphResult, userLimit int, format index.GraphFormat, rootSymbols []string) *index.GraphResult {
	effective := userLimit
	auto := format == index.GraphFormatMermaid && len(g.Nodes) > mermaidNodeCeiling
	if auto {
		if effective <= 0 || mermaidNodeCeiling < effective {
			effective = mermaidNodeCeiling
		}
	}
	if effective <= 0 || len(g.Nodes) <= effective {
		return g
	}
	rootIDs := make(map[string]bool, len(rootSymbols))
	for _, s := range rootSymbols {
		rootIDs[index.GraphNodeIDFor(s)] = true
	}
	truncated := truncateResultByDegree(g, effective, rootIDs)
	if auto && userLimit <= 0 {
		fmt.Fprintf(os.Stderr,
			"warning: mermaid output truncated to %d nodes of %d; pass --graph-format json or --graph-limit N for full graph\n",
			effective, len(g.Nodes))
	}
	return truncated
}

// truncateResultByDegree keeps the top-N nodes ranked by edge degree,
// always preserving any root node ID, and appends a sentinel so the
// truncation is visible in any renderer. This is the cmd-layer mirror of
// index.truncateByDegree — split out because cmd-layer truncation can
// involve multiple roots (merged multi-symbol graphs).
func truncateResultByDegree(g *index.GraphResult, limit int, rootIDs map[string]bool) *index.GraphResult {
	if len(g.Nodes) <= limit {
		return g
	}
	degree := make(map[string]int, len(g.Nodes))
	for _, e := range g.Edges {
		degree[e.From]++
		degree[e.To]++
	}
	type ranked struct {
		node   index.GraphNode
		deg    int
		isRoot bool
	}
	ranks := make([]ranked, 0, len(g.Nodes))
	for _, n := range g.Nodes {
		ranks = append(ranks, ranked{node: n, deg: degree[n.ID], isRoot: rootIDs[n.ID]})
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
	kept := make([]index.GraphNode, 0, limit+1)
	for i := 0; i < limit && i < len(ranks); i++ {
		keep[ranks[i].node.ID] = true
		kept = append(kept, ranks[i].node)
	}
	dropped := len(g.Nodes) - len(kept)
	kept = append(kept, index.GraphNode{
		ID:    "_truncated",
		Kind:  index.GraphNodeKindSentinel,
		Label: fmt.Sprintf("… (%d more, truncated)", dropped),
	})
	filteredEdges := make([]index.GraphEdge, 0, len(g.Edges))
	for _, e := range g.Edges {
		if keep[e.From] && keep[e.To] {
			filteredEdges = append(filteredEdges, e)
		}
	}
	filteredUnresolved := make([]index.GraphUnresolved, 0, len(g.Unresolved))
	for _, u := range g.Unresolved {
		if keep[u.From] {
			filteredUnresolved = append(filteredUnresolved, u)
		}
	}
	return &index.GraphResult{
		Nodes:      kept,
		Edges:      filteredEdges,
		Unresolved: filteredUnresolved,
		Truncated:  dropped,
	}
}

func renderGraph(format index.GraphFormat, graph *index.GraphResult) error {
	switch format {
	case index.GraphFormatDot:
		_, err := fmt.Fprint(os.Stdout, renderGraphDOT(graph))
		return err
	case index.GraphFormatMermaid:
		_, err := fmt.Fprint(os.Stdout, renderGraphMermaid(graph))
		return err
	default:
		return writeJSON(graph)
	}
}

func renderGraphMermaid(graph *index.GraphResult) string {
	if len(graph.Nodes) == 0 && len(graph.Edges) == 0 {
		return "flowchart LR\n%% no edges\n"
	}
	var b strings.Builder
	b.WriteString("flowchart LR\n")
	for _, node := range graph.Nodes {
		fmt.Fprintf(&b, "  %s[%q]\n", node.ID, node.Label)
	}
	for _, edge := range graph.Edges {
		arrow := "-->"
		if !edge.Resolved {
			arrow = "-.->"
		}
		fmt.Fprintf(&b, "  %s %s %s\n", edge.From, arrow, edge.To)
	}
	return b.String()
}

func renderGraphDOT(graph *index.GraphResult) string {
	if len(graph.Nodes) == 0 && len(graph.Edges) == 0 {
		return "digraph cymbal { /* no edges */ }\n"
	}
	var b strings.Builder
	b.WriteString("digraph cymbal {\n")
	for _, node := range graph.Nodes {
		fmt.Fprintf(&b, "  %s [label=%q];\n", node.ID, node.Label)
	}
	for _, edge := range graph.Edges {
		attrs := ""
		if !edge.Resolved {
			attrs = " [style=dashed]"
		}
		fmt.Fprintf(&b, "  %s -> %s%s;\n", edge.From, edge.To, attrs)
	}
	b.WriteString("}\n")
	return b.String()
}
