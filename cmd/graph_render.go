package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

// addGraphFlags wires the shared --graph / --graph-format / --graph-depth /
// --include-unresolved flags onto a command that produces edges (trace,
// impact, etc.). Direction is fixed per-verb by the caller.
func addGraphFlags(cmd *cobra.Command) {
	cmd.Flags().Bool("graph", false, "render output as a graph (mermaid on TTY, json when piped)")
	cmd.Flags().String("graph-format", "", "graph output format: mermaid, dot, or json (implies --graph)")
	cmd.Flags().Bool("include-unresolved", false, "include unresolved external calls as dashed ext:<fqn> nodes")
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

// renderAsGraph builds a graph for the given symbols + direction and renders
// it in the format selected by the verb's flags. It merges per-symbol graphs
// by de-duplicating nodes/edges via their stable IDs.
func renderAsGraph(cmd *cobra.Command, dbPath string, symbols []string, direction index.GraphDirection) error {
	if len(symbols) == 0 {
		return renderGraph(selectGraphFormatFromVerb(cmd), &index.GraphResult{
			Nodes:      []index.GraphNode{},
			Edges:      []index.GraphEdge{},
			Unresolved: []index.GraphUnresolved{},
		})
	}
	depth, _ := cmd.Flags().GetInt("depth")
	includeUnresolved, _ := cmd.Flags().GetBool("include-unresolved")

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
	return renderGraph(selectGraphFormatFromVerb(cmd), merged)
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
