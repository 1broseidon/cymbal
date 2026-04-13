package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var deadCmd = &cobra.Command{
	Use:   "dead",
	Short: "Find potentially dead symbols — defined but never referenced",
	Long: `Find symbols that are defined in the codebase but have zero references.
Each result includes a confidence level indicating how likely it is to be
truly dead code:

  high   — private/unexported symbol with no references (very likely dead)
  medium — public or unknown-visibility symbol with no references
  low    — method or symbol in a dynamic-dispatch language (may be false positive)

Dead code detection is AST-level and name-based. Symbols may be falsely
reported if they are:
  - Called via reflection, decorators, or dynamic dispatch
  - Part of a public API consumed by external packages
  - Interface/protocol implementations invoked via polymorphism
  - Used as callbacks, signal handlers, or framework hooks

By default, test functions and entry points (main, init) are excluded.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)

		kind, _ := cmd.Flags().GetString("kind")
		lang, _ := cmd.Flags().GetString("lang")
		limit, _ := cmd.Flags().GetInt("limit")
		minConf, _ := cmd.Flags().GetString("min-confidence")
		includeTests, _ := cmd.Flags().GetBool("include-tests")

		// Validate min-confidence flag.
		switch minConf {
		case "", "low", "medium", "high":
			// valid
		default:
			return fmt.Errorf("invalid --min-confidence value %q: must be high, medium, or low", minConf)
		}

		q := index.DeadSymbolQuery{
			Kind:          kind,
			Language:      lang,
			MinConfidence: minConf,
			IncludeTests:  includeTests,
			Limit:         limit,
		}

		results, err := index.FindDeadSymbols(dbPath, q)
		if err != nil {
			return err
		}

		if len(results) == 0 {
			if jsonOut {
				return writeJSON([]index.DeadSymbol{})
			}
			fmt.Println("No potentially dead symbols found.")
			return nil
		}

		if jsonOut {
			return writeJSON(results)
		}

		// Count by confidence.
		counts := map[string]int{}
		for _, r := range results {
			counts[r.Confidence]++
		}

		var content strings.Builder
		for _, r := range results {
			fmt.Fprintf(&content, "  [%s]  %-10s  %-30s  %s:%d\n",
				confTag(r.Confidence), r.Kind, r.Name, r.RelPath, r.StartLine)
		}

		meta := []kv{
			{"total", fmt.Sprintf("%d", len(results))},
		}
		if counts["high"] > 0 {
			meta = append(meta, kv{"high_confidence", fmt.Sprintf("%d", counts["high"])})
		}
		if counts["medium"] > 0 {
			meta = append(meta, kv{"medium_confidence", fmt.Sprintf("%d", counts["medium"])})
		}
		if counts["low"] > 0 {
			meta = append(meta, kv{"low_confidence", fmt.Sprintf("%d", counts["low"])})
		}
		if q.MinConfidence != "" && q.MinConfidence != "low" {
			meta = append(meta, kv{"min_confidence", q.MinConfidence})
		}
		meta = append(meta, kv{"note", "AST-level detection — verify before deleting"})

		frontmatter(meta, content.String())
		return nil
	},
}

func init() {
	deadCmd.Flags().StringP("kind", "k", "", "filter by symbol kind (function, struct, method, ...)")
	deadCmd.Flags().StringP("lang", "l", "", "filter by language (go, python, ...)")
	deadCmd.Flags().IntP("limit", "n", 50, "max results")
	deadCmd.Flags().String("min-confidence", "", "minimum confidence level: high, medium, low (default: show all)")
	deadCmd.Flags().Bool("include-tests", false, "include test functions in results")
	rootCmd.AddCommand(deadCmd)
}

// confTag returns a fixed-width confidence tag for aligned output.
func confTag(confidence string) string {
	switch confidence {
	case "high":
		return "HIGH  "
	case "medium":
		return "MEDIUM"
	case "low":
		return "LOW   "
	default:
		return "      "
	}
}
