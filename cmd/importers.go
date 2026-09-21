package cmd

import (
	"fmt"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var importersCmd = &cobra.Command{
	Use:   "importers <file|package>",
	Short: "Find files that import a given file or package",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		target := args[0]
		dbPath := getDBPath(cmd)
		if err := ensureFresh(dbPath); err != nil {
			return err
		}
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")

		var results []index.ImporterResult
		var err error
		if graphRequested(cmd) {
			results, err = index.FindImportersByPath(dbPath, target, depth, 999999) // graph needs full connectivity, rely on user/auto cap later
		} else {
			results, err = index.FindImportersByPath(dbPath, target, depth, limit)
		}
		if err != nil {
			return err
		}

		if len(results) == 0 {
			if graphRequested(cmd) {
				return renderAsGraph(cmd, dbPath, nil, index.GraphDirectionUp, 1) // empty
			}
			return fmt.Errorf("no importers found for '%s'", target)
		}

		if graphRequested(cmd) {
			format := selectGraphFormatFromVerb(cmd)
			graph := buildImportersGraph(target, results)
			userLimit, _ := cmd.Flags().GetInt("graph-limit")
			graph = applyGraphLimit(graph, userLimit, format, graphRootIDSet("file\x1f"+target))
			return renderGraph(format, graph)
		}

		return renderJSONOrFrontmatter(
			jsonOut,
			results,
			[]kv{
				{"target", target},
				{"importer_count", fmt.Sprintf("%d", len(results))},
			},
			formatImporterResults(results),
		)
	},
}

func init() {
	importersCmd.Flags().IntP("depth", "D", 1, "import chain depth (max 3)")
	importersCmd.Flags().IntP("limit", "n", 50, "max results")
	addGraphFlags(importersCmd)
	rootCmd.AddCommand(importersCmd)
}
