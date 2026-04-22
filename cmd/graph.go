package cmd

import (
	"fmt"
	"os"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var graphCmd = &cobra.Command{
	Use:   "graph <symbol>",
	Short: "Render symbol relationships as a graph",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)

		store, err := index.OpenStore(dbPath)
		if err != nil {
			return err
		}
		defer store.Close()

		query := index.GraphQuery{Symbol: args[0]}
		if query.Direction, err = getGraphDirection(cmd); err != nil {
			return err
		}
		query.Depth, _ = cmd.Flags().GetInt("depth")
		query.Scope, _ = cmd.Flags().GetStringSlice("scope")
		query.Exclude, _ = cmd.Flags().GetStringSlice("exclude")
		query.IncludeUnresolved, _ = cmd.Flags().GetBool("include-unresolved")

		graph, err := store.BuildGraph(query)
		if err != nil {
			return err
		}
		return renderGraph(selectGraphFormat(cmd), graph)
	},
}

func init() {
	graphCmd.Flags().String("direction", string(index.GraphDirectionDown), "graph direction: down, up, or both")
	graphCmd.Flags().Int("depth", 2, "max traversal depth (max 5)")
	graphCmd.Flags().String("format", "", "output format: mermaid, dot, or json")
	graphCmd.Flags().StringSlice("scope", nil, "restrict root symbols by path glob (repeatable)")
	graphCmd.Flags().StringSlice("exclude", nil, "exclude symbols by path glob (repeatable)")
	graphCmd.Flags().Bool("include-unresolved", false, "show unresolved external calls as dashed ext:<fqn> nodes")
	rootCmd.AddCommand(graphCmd)
}

func getGraphDirection(cmd *cobra.Command) (index.GraphDirection, error) {
	raw, _ := cmd.Flags().GetString("direction")
	switch index.GraphDirection(raw) {
	case index.GraphDirectionDown, index.GraphDirectionUp, index.GraphDirectionBoth:
		return index.GraphDirection(raw), nil
	default:
		return "", fmt.Errorf("invalid --direction %q (want down, up, or both)", raw)
	}
}

func init() {
	rootCmd.AddCommand(graphCmd)
}

func selectGraphFormat(cmd *cobra.Command) index.GraphFormat {
	if getJSONFlag(cmd) {
		return index.GraphFormatJSON
	}
	if raw, _ := cmd.Flags().GetString("format"); raw != "" {
		return index.GraphFormat(raw)
	}
	if info, err := os.Stdout.Stat(); err == nil && (info.Mode()&os.ModeCharDevice) == 0 {
		return index.GraphFormatJSON
	}
	return index.GraphFormatMermaid
}
