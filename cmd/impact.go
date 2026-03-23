package cmd

import (
	"fmt"
	"os"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/spf13/cobra"
)

var impactCmd = &cobra.Command{
	Use:   "impact <symbol>",
	Short: "Transitive caller analysis — what is impacted if this symbol changes",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		dbPath := getDBPath(cmd)
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")

		results, err := index.FindImpact(dbPath, name, depth, limit)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			fmt.Fprintf(os.Stderr, "No callers found for '%s'.\n", name)
			os.Exit(1)
		}

		if jsonOut {
			return writeJSON(results)
		}

		for _, r := range results {
			fmt.Printf("[depth %d] %s \u2192 %s  (%s:%d)\n", r.Depth, r.Symbol, r.Caller, r.RelPath, r.Line)
		}
		return nil
	},
}

func init() {
	impactCmd.Flags().IntP("depth", "D", 2, "max call-chain depth (max 5)")
	impactCmd.Flags().IntP("limit", "n", 100, "max results")
	rootCmd.AddCommand(impactCmd)
}
