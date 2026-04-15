package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var searchCmd = &cobra.Command{
	Use:   "search <query>",
	Short: "Search symbols or text across indexed repos",
	Long: `Search symbols by default, or use --text for full-text grep across file contents.
Results are ranked: exact match > prefix > fuzzy.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		query := strings.Join(args, " ")
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		kind, _ := cmd.Flags().GetString("kind")
		limit, _ := cmd.Flags().GetInt("limit")
		lang, _ := cmd.Flags().GetString("lang")
		exact, _ := cmd.Flags().GetBool("exact")
		textMode, _ := cmd.Flags().GetBool("text")

		if textMode {
			return searchText(dbPath, query, lang, limit, jsonOut)
		}

		results, err := index.SearchSymbols(dbPath, index.SearchQuery{
			Text:     query,
			Kind:     kind,
			Language: lang,
			Exact:    exact,
			Limit:    limit,
		})
		if err != nil {
			return err
		}

		if len(results) == 0 {
			return fmt.Errorf("no results found for '%s'", query)
		}

		// Ranking is already applied by the store layer:
		// exact queries use RankSymbols; FTS queries use rankWithinFTSTiers.
		// A second flat RankSymbols here would break FTS tier order.

		var content strings.Builder
		for _, r := range results {
			fmt.Fprintf(&content, "%s %s %s:%d\n", r.Kind, r.Name, r.RelPath, r.StartLine)
		}

		return renderJSONOrFrontmatter(
			jsonOut,
			results,
			[]kv{
				{"query", query},
				{"result_count", fmt.Sprintf("%d", len(results))},
			},
			content.String(),
		)
	},
}

func init() {
	searchCmd.Flags().StringP("kind", "k", "", "filter by symbol kind (function, class, method, etc.)")
	searchCmd.Flags().IntP("limit", "n", 20, "max results")
	searchCmd.Flags().StringP("lang", "l", "", "filter by language (go, python, typescript, etc.)")
	searchCmd.Flags().BoolP("exact", "e", false, "exact name match only")
	searchCmd.Flags().BoolP("text", "t", false, "full-text grep across file contents")
	rootCmd.AddCommand(searchCmd)
}

func searchText(dbPath, query, lang string, limit int, jsonOut bool) error {
	results, err := index.TextSearch(dbPath, query, lang, limit)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		return fmt.Errorf("no results found for '%s'", query)
	}

	var content strings.Builder
	for _, r := range results {
		fmt.Fprintf(&content, "%s:%d: %s\n", r.RelPath, r.Line, r.Snippet)
	}

	return renderJSONOrFrontmatter(
		jsonOut,
		results,
		[]kv{
			{"query", query},
			{"result_count", fmt.Sprintf("%d", len(results))},
		},
		content.String(),
	)
}
