package cmd

import (
	"fmt"
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

		switch strings.ToLower(format) {
		case "json":
			return writeJSON(g)
		case "mermaid":
			printDependsMermaid(g)
		default:
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

// sanitizeDOTID escapes a rel_path for use as a DOT node identifier.
// Dots and slashes are replaced with underscores and the result is quoted.
func sanitizeDOTID(relPath string) string {
	id := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		".", "_",
		"-", "_",
	).Replace(relPath)
	return `"` + id + `"`
}

// printDependsDOT writes the graph in Graphviz DOT format.
func printDependsDOT(g *index.DependsGraph) {
	fmt.Println("digraph depends {")
	fmt.Println(`  rankdir=LR;`)
	fmt.Println(`  node [shape=box fontname="Helvetica"];`)
	fmt.Println()

	for _, n := range g.Nodes {
		label := n.ID
		if n.Language != "" {
			label = fmt.Sprintf("%s\\n[%s]", n.ID, n.Language)
		}
		fmt.Printf("  %s [label=%q];\n", sanitizeDOTID(n.ID), label)
	}

	if len(g.Nodes) > 0 && len(g.Edges) > 0 {
		fmt.Println()
	}

	for _, e := range g.Edges {
		fmt.Printf("  %s -> %s;\n", sanitizeDOTID(e.From), sanitizeDOTID(e.To))
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

// sanitizeMermaidID returns a safe Mermaid node ID (alphanumeric + underscores).
func sanitizeMermaidID(relPath string) string {
	return strings.NewReplacer(
		"/", "_",
		"\\", "_",
		".", "_",
		"-", "_",
	).Replace(relPath)
}

// printDependsMermaid writes the graph in Mermaid flowchart format.
func printDependsMermaid(g *index.DependsGraph) {
	fmt.Println("flowchart LR")

	for _, n := range g.Nodes {
		id := sanitizeMermaidID(n.ID)
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
		fmt.Printf("  %s --> %s\n", sanitizeMermaidID(e.From), sanitizeMermaidID(e.To))
	}

	if len(g.Cycles) > 0 {
		fmt.Println()
		for _, c := range g.Cycles {
			fmt.Printf("  %% cycle: %s\n", strings.Join(c, " -> "))
		}
	}
}
