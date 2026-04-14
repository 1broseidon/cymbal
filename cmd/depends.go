package cmd

import (
	"fmt"
	"sort"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var dependsCmd = &cobra.Command{
	Use:   "depends",
	Short: "Export the file-level import dependency graph",
	Long: `Export the file-level import dependency graph for indexed files.

Formats:
  dot      Graphviz DOT language (default)
  mermaid  Mermaid flowchart syntax
  json     JSON with nodes, edges, and detected cycles

Examples:
  cymbal depends
  cymbal depends --format mermaid
  cymbal depends --format json
  cymbal depends --scope cmd/
  cymbal depends --scope internal/ --depth 2`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)

		format, _ := cmd.Flags().GetString("format")
		scope, _ := cmd.Flags().GetString("scope")
		depth, _ := cmd.Flags().GetInt("depth")

		format = strings.ToLower(strings.TrimSpace(format))
		switch format {
		case "dot", "mermaid", "json":
		default:
			return fmt.Errorf("invalid format %q (expected: dot|mermaid|json)", format)
		}

		g, err := index.BuildDependsGraph(dbPath, index.DependsQuery{
			Scope: scope,
			Depth: depth,
		})
		if err != nil {
			return err
		}

		if len(g.Nodes) == 0 {
			return fmt.Errorf("no dependency edges found")
		}

		switch format {
		case "json":
			return writeJSON(g)
		case "mermaid":
			printDependsMermaid(g)
		case "dot":
			printDependsDOT(g)
		}
		return nil
	},
}

func init() {
	dependsCmd.Flags().StringP("format", "f", "dot", "output format: dot, mermaid, json")
	dependsCmd.Flags().StringP("scope", "s", "", "restrict to files whose rel_path starts with this prefix")
	dependsCmd.Flags().IntP("depth", "d", 0, "max traversal depth from scope roots (0 = unlimited)")
	rootCmd.AddCommand(dependsCmd)
}

// dependsNodeIDs builds deterministic collision-free node IDs for graph outputs.
// IDs are based on sorted node paths and use the n<index> form.
func dependsNodeIDs(g *index.DependsGraph) map[string]string {
	ids := make(map[string]string)
	next := 0

	for _, n := range g.Nodes {
		if _, exists := ids[n.ID]; exists {
			continue
		}
		ids[n.ID] = fmt.Sprintf("n%d", next)
		next++
	}

	// Defensive fallback: include any edge endpoints missing from Nodes.
	missingSet := make(map[string]struct{})
	for _, e := range g.Edges {
		if _, ok := ids[e.From]; !ok {
			missingSet[e.From] = struct{}{}
		}
		if _, ok := ids[e.To]; !ok {
			missingSet[e.To] = struct{}{}
		}
	}
	if len(missingSet) > 0 {
		missing := make([]string, 0, len(missingSet))
		for m := range missingSet {
			missing = append(missing, m)
		}
		sort.Strings(missing)
		for _, m := range missing {
			ids[m] = fmt.Sprintf("n%d", next)
			next++
		}
	}

	return ids
}

// printDependsDOT writes the graph in Graphviz DOT format.
func printDependsDOT(g *index.DependsGraph) {
	ids := dependsNodeIDs(g)

	fmt.Println("digraph depends {")
	fmt.Println(`  rankdir=LR;`)
	fmt.Println(`  node [shape=box fontname="Helvetica"];`)
	fmt.Println()

	for _, n := range g.Nodes {
		label := n.ID
		if n.Language != "" {
			label = fmt.Sprintf("%s\\n[%s]", n.ID, n.Language)
		}
		fmt.Printf("  %s [label=%q];\n", ids[n.ID], label)
	}

	if len(g.Nodes) > 0 && len(g.Edges) > 0 {
		fmt.Println()
	}

	for _, e := range g.Edges {
		fmt.Printf("  %s -> %s;\n", ids[e.From], ids[e.To])
	}

	if len(g.Cycles) > 0 {
		fmt.Println()
		fmt.Printf("  // %d cycle(s) detected:\n", len(g.Cycles))
		for _, c := range g.Cycles {
			fmt.Printf("  // %s\n", strings.Join(c, " -> "))
		}
	}

	fmt.Println("}")
}

// printDependsMermaid writes the graph in Mermaid flowchart format.
func printDependsMermaid(g *index.DependsGraph) {
	ids := dependsNodeIDs(g)

	fmt.Println("flowchart LR")

	for _, n := range g.Nodes {
		id := ids[n.ID]
		label := n.ID
		if n.Language != "" {
			label = fmt.Sprintf("%s\n[%s]", n.ID, n.Language)
		}
		fmt.Printf("  %s[\"%s\"]\n", id, label)
	}

	if len(g.Nodes) > 0 && len(g.Edges) > 0 {
		fmt.Println()
	}

	for _, e := range g.Edges {
		fmt.Printf("  %s --> %s\n", ids[e.From], ids[e.To])
	}

	if len(g.Cycles) > 0 {
		fmt.Println()
		for _, c := range g.Cycles {
			fmt.Printf("  %% cycle: %s\n", strings.Join(c, " -> "))
		}
	}
}
