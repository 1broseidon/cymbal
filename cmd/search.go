package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

var searchCmd = &cobra.Command{
	Use:   "search <query> [path ...]",
	Short: "Search symbols or text across indexed repos",
	Long: `Search symbols by default, or use --text for full-text grep across file contents.
Results are ranked: exact match > prefix > fuzzy.

Trailing path operands are accepted as --path filters, so grep-shaped calls like
"cymbal search --text TODO cmd internal/foo.go" work as expected.

In symbol mode, pass multiple queries to search them independently. In text mode,
multiple query words are joined into one Go regular expression. Text search is
line-oriented and uses the indexed file inventory, ordered by path and line.
Index exclusions and --lang/--path/--exclude apply before --limit.`,
	Args: cobra.ArbitraryArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		plan := resolveDBs(cmd)
		if err := ensureFresh(plan.Primary); err != nil {
			return err
		}
		jsonOut := getJSONFlag(cmd)
		kind, _ := cmd.Flags().GetString("kind")
		limit, _ := cmd.Flags().GetInt("limit")
		lang, _ := cmd.Flags().GetString("lang")
		exact, _ := cmd.Flags().GetBool("exact")
		ignoreCase, _ := cmd.Flags().GetBool("ignore-case")
		textMode, _ := cmd.Flags().GetBool("text")
		includes, _ := cmd.Flags().GetStringArray("path")
		excludes, _ := cmd.Flags().GetStringArray("exclude")
		queries, pathOperands := splitSearchArgs(args)
		includes = append(includes, pathOperands...)

		effectiveExact, err := normalizeSearchMode(exact, ignoreCase, textMode)
		if err != nil {
			return err
		}

		if textMode {
			if stdin, _ := cmd.Flags().GetBool("stdin"); stdin {
				return fmt.Errorf("--stdin is only supported for symbol search")
			}
			query := strings.Join(queries, " ")
			if strings.TrimSpace(query) == "" {
				return fmt.Errorf("no search query provided")
			}
			// Text mode stays single-DB: rg/grep already scans the current
			// working tree; federating across worktrees would scan files we
			// can't read from cwd. Worktree-text-search is a separate scope.
			return searchText(plan.Primary, query, lang, limit, jsonOut, includes, excludes)
		}

		queries, err = collectSymbols(cmd, queries)
		if err != nil {
			return err
		}
		var (
			results []index.SymbolResult
			missing []string
		)
		// Federate symbol lookup across all entries in the plan. Current cwd
		// queried first so its hits sort to the top. Per-DB missing lists
		// merge into one "missing across all worktrees" set.
		missingPerQuery := make(map[string]int)
		for _, query := range queries {
			missingPerQuery[query] = 0
		}
		for _, entry := range plan.Federated {
			entryResults, entryMissing, err := searchSymbolQueries(entry.Path, queries, kind, lang, effectiveExact, ignoreCase, limit, includes, excludes)
			if err != nil {
				// One DB failing (e.g. corrupt sibling) shouldn't sink the
				// whole query — log to stderr and move on.
				if entry.IsCurrent {
					return err
				}
				fmt.Fprintf(cmd.ErrOrStderr(), "cymbal: skipping worktree %s — query error: %v\n", entry.Label(), err)
				continue
			}
			label := entry.Label()
			if label != "" {
				for i := range entryResults {
					entryResults[i].Worktree = label
				}
			}
			results = append(results, entryResults...)
			for _, q := range entryMissing {
				missingPerQuery[q]++
			}
		}
		// A query is "missing" only when every federated DB missed it.
		totalDBs := len(plan.Federated)
		for _, query := range queries {
			if missingPerQuery[query] == totalDBs {
				missing = append(missing, query)
			}
		}
		for _, query := range missing {
			fmt.Fprintf(cmd.ErrOrStderr(), "%s: no results found\n", query)
		}
		query := strings.Join(queries, " ")
		if len(results) == 0 {
			return fmt.Errorf("no results found for '%s'", query)
		}

		// Ranking is already applied by the store layer:
		// exact queries use RankSymbols; FTS queries use rankWithinFTSTiers.
		// A second flat RankSymbols here would break FTS tier order.

		var content strings.Builder
		for _, r := range results {
			if r.Worktree != "" {
				fmt.Fprintf(&content, "[worktree:%s] %s %s %s:%d\n", r.Worktree, r.Kind, r.Name, r.RelPath, r.StartLine)
			} else {
				fmt.Fprintf(&content, "%s %s %s:%d\n", r.Kind, r.Name, r.RelPath, r.StartLine)
			}
		}

		meta := []kv{{"query", query}}
		if len(queries) > 1 {
			meta = append(meta, kv{"query_count", fmt.Sprintf("%d", len(queries))})
		}
		meta = append(meta, kv{"result_count", fmt.Sprintf("%d", len(results))})

		return renderJSONOrFrontmatter(jsonOut, results, meta, content.String())
	},
}

func init() {
	searchCmd.Flags().SetNormalizeFunc(func(_ *pflag.FlagSet, name string) pflag.NormalizedName {
		if name == "file" {
			return pflag.NormalizedName("path")
		}
		return pflag.NormalizedName(name)
	})
	searchCmd.Flags().StringP("kind", "k", "", "filter by symbol kind (function, class, method, etc.)")
	searchCmd.Flags().IntP("limit", "n", 20, "max results")
	searchCmd.Flags().StringP("lang", "l", "", "filter by language (go, python, typescript, etc.)")
	searchCmd.Flags().BoolP("exact", "e", false, "exact name match only")
	searchCmd.Flags().BoolP("ignore-case", "i", false, "case-insensitive exact match (implies --exact; not supported with --text)")
	searchCmd.Flags().BoolP("text", "t", false, "full-text grep across file contents")
	searchCmd.Flags().StringArray("path", nil, "include only results whose path matches this glob (repeatable)")
	searchCmd.Flags().StringArray("exclude", nil, "exclude results whose path matches this glob (repeatable)")
	addStdinFlag(searchCmd)
	rootCmd.AddCommand(searchCmd)
}

func splitSearchArgs(args []string) ([]string, []string) {
	if len(args) == 0 {
		return nil, nil
	}
	if len(args) == 1 {
		return cleanSearchQueries(args), nil
	}
	firstPath := len(args)
	for firstPath > 1 && looksLikeSearchPathOperand(args[firstPath-1]) {
		firstPath--
	}
	if firstPath == len(args) {
		return cleanSearchQueries(args), nil
	}
	paths := make([]string, 0, len(args)-firstPath)
	for _, arg := range args[firstPath:] {
		paths = append(paths, normalizeSearchPathOperand(arg))
	}
	return cleanSearchQueries(args[:firstPath]), paths
}

func cleanSearchQueries(args []string) []string {
	out := make([]string, 0, len(args))
	for _, arg := range args {
		arg = strings.TrimSpace(arg)
		if arg != "" {
			out = append(out, arg)
		}
	}
	return out
}

func looksLikeSearchPathOperand(arg string) bool {
	arg = strings.TrimSpace(arg)
	if arg == "" || strings.HasPrefix(arg, "-") {
		return false
	}
	if isFilePath(arg) {
		return true
	}
	if _, err := os.Stat(arg); err == nil {
		return true
	}
	return false
}

func normalizeSearchPathOperand(arg string) string {
	arg = strings.TrimSpace(arg)
	rel := normalizeRelPath(arg)
	info, err := os.Stat(arg)
	if err == nil && info.IsDir() {
		if rel == "" || rel == "." {
			return "**"
		}
		return strings.TrimSuffix(rel, "/") + "/**"
	}
	return rel
}

func searchSymbolQueries(dbPath string, queries []string, kind, lang string, exact, ignoreCase bool, limit int, includes, excludes []string) ([]index.SymbolResult, []string, error) {
	seen := make(map[string]struct{})
	results := make([]index.SymbolResult, 0, len(queries))
	var missing []string
	for _, query := range queries {
		queryResults, err := index.SearchSymbols(dbPath, index.SearchQuery{
			Text:       query,
			Kind:       kind,
			Language:   lang,
			Exact:      exact,
			IgnoreCase: ignoreCase,
			Limit:      limit,
			Paths:      index.PathFilter{Include: includes, Exclude: excludes},
		})
		if err != nil {
			return nil, nil, err
		}
		if limit > 0 && len(queryResults) > limit {
			queryResults = queryResults[:limit]
		}
		if len(queryResults) == 0 {
			missing = append(missing, query)
			continue
		}
		for _, result := range queryResults {
			id := result.SymbolID()
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			results = append(results, result)
		}
	}
	return results, missing, nil
}

func normalizeSearchMode(exact, ignoreCase, textMode bool) (bool, error) {
	if !ignoreCase {
		return exact, nil
	}
	if textMode {
		return exact, fmt.Errorf("--ignore-case is not supported with --text")
	}
	// FTS-backed non-exact search is already case-insensitive, so `-i`
	// upgrades symbol search to an exact case-insensitive match.
	if !exact {
		return true, nil
	}
	return exact, nil
}

func renderTextResults(query string, results []index.TextResult, jsonOut bool) error {
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
