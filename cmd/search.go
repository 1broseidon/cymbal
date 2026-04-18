package cmd

import (
	"fmt"
	"os/exec"
	"path/filepath"
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
		ignoreCase, _ := cmd.Flags().GetBool("ignore-case")
		textMode, _ := cmd.Flags().GetBool("text")
		includes, _ := cmd.Flags().GetStringArray("path")
		excludes, _ := cmd.Flags().GetStringArray("exclude")
		hasFilters := len(includes) > 0 || len(excludes) > 0

		effectiveExact, err := normalizeSearchMode(exact, ignoreCase, textMode)
		if err != nil {
			return err
		}

		if textMode {
			return searchText(dbPath, query, lang, limit, jsonOut, includes, excludes)
		}

		results, err := index.SearchSymbols(dbPath, index.SearchQuery{
			Text:       query,
			Kind:       kind,
			Language:   lang,
			Exact:      effectiveExact,
			IgnoreCase: ignoreCase,
			Limit:      widenPathFilterLimit(limit, hasFilters),
		})
		if err != nil {
			return err
		}

		results = filterByPath(results, func(r index.SymbolResult) string { return r.RelPath }, includes, excludes)
		if limit > 0 && len(results) > limit {
			results = results[:limit]
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
	searchCmd.Flags().BoolP("ignore-case", "i", false, "case-insensitive exact match (implies --exact; not supported with --text)")
	searchCmd.Flags().BoolP("text", "t", false, "full-text grep across file contents")
	searchCmd.Flags().StringArray("path", nil, "include only results whose path matches this glob (repeatable)")
	searchCmd.Flags().StringArray("exclude", nil, "exclude results whose path matches this glob (repeatable)")
	rootCmd.AddCommand(searchCmd)
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

func searchText(dbPath, query, lang string, limit int, jsonOut bool, includes, excludes []string) error {
	if rgPath, err := exec.LookPath("rg"); err == nil {
		return searchTextRg(rgPath, dbPath, query, lang, limit, jsonOut, includes, excludes)
	}
	return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
}

// searchTextRg delegates text search to ripgrep for speed.
func searchTextRg(rgPath, dbPath, query, lang string, limit int, jsonOut bool, includes, excludes []string) error {
	repoRoot := index.RepoRootFromDB(dbPath)
	if repoRoot == "" {
		return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
	}

	args := []string{"--no-heading", "-n", "--color=never"}
	if lang != "" {
		if rgLang := langToRgType(lang); rgLang != "" {
			args = append(args, "--type="+rgLang)
		}
	}
	fetchLimit := widenPathFilterLimit(limit, len(includes) > 0 || len(excludes) > 0)
	if fetchLimit > 0 {
		args = append(args, "--max-count=1", fmt.Sprintf("-m%d", fetchLimit))
	}
	args = append(args, "--", query, ".")

	cmd := exec.Command(rgPath, args...)
	cmd.Dir = repoRoot
	out, err := cmd.Output()
	if err != nil {
		// rg exits 1 when no matches; that's not an error.
		if cmd.ProcessState != nil && cmd.ProcessState.ExitCode() == 1 {
			return fmt.Errorf("no results found for '%s'", query)
		}
		// rg unavailable or hard error — fall back.
		return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
	}

	var results []index.TextResult
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		lineNum := 0
		fmt.Sscanf(parts[1], "%d", &lineNum)
		relPath := filepath.ToSlash(parts[0])
		results = append(results, index.TextResult{
			RelPath: relPath,
			Line:    lineNum,
			Snippet: strings.TrimSpace(parts[2]),
		})
	}

	results = filterByPath(results, func(r index.TextResult) string { return r.RelPath }, includes, excludes)
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	if len(results) == 0 {
		return fmt.Errorf("no results found for '%s'", query)
	}
	return renderTextResults(query, results, jsonOut)
}

// searchTextGo is the pure-Go fallback using the indexed file list.
func searchTextGo(dbPath, query, lang string, limit int, jsonOut bool, includes, excludes []string) error {
	results, err := index.TextSearch(dbPath, query, lang, widenPathFilterLimit(limit, len(includes) > 0 || len(excludes) > 0))
	if err != nil {
		return err
	}
	results = filterByPath(results, func(r index.TextResult) string { return r.RelPath }, includes, excludes)
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	if len(results) == 0 {
		return fmt.Errorf("no results found for '%s'", query)
	}
	return renderTextResults(query, results, jsonOut)
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

// langToRgType maps cymbal language names to rg --type values.
func langToRgType(lang string) string {
	switch strings.ToLower(lang) {
	case "go":
		return "go"
	case "python":
		return "py"
	case "typescript", "tsx":
		return "ts"
	case "javascript", "jsx":
		return "js"
	case "rust":
		return "rust"
	case "java":
		return "java"
	case "c":
		return "c"
	case "cpp", "c++":
		return "cpp"
	default:
		return ""
	}
}
