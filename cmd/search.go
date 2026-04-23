package cmd

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

const rgSearchTimeout = 10 * time.Second

var searchCmd = &cobra.Command{
	Use:   "search <query> [path ...]",
	Short: "Search symbols or text across indexed repos",
	Long: `Search symbols by default, or use --text for full-text grep across file contents.
Results are ranked: exact match > prefix > fuzzy.

Trailing path operands are accepted as --path filters, so grep-shaped calls like
"cymbal search --text TODO cmd internal/foo.go" work as expected.`,
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
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
		query, pathOperands := splitSearchArgs(args)
		includes = append(includes, pathOperands...)
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
	rootCmd.AddCommand(searchCmd)
}

func splitSearchArgs(args []string) (string, []string) {
	if len(args) <= 1 {
		return strings.Join(args, " "), nil
	}
	firstPath := len(args)
	for firstPath > 1 && looksLikeSearchPathOperand(args[firstPath-1]) {
		firstPath--
	}
	if firstPath == len(args) {
		return strings.Join(args, " "), nil
	}
	paths := make([]string, 0, len(args)-firstPath)
	for _, arg := range args[firstPath:] {
		paths = append(paths, normalizeSearchPathOperand(arg))
	}
	return strings.Join(args[:firstPath], " "), paths
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
	fetchLimit := limit
	args = append(args, "--", query, ".")

	ctx, cancel := context.WithTimeout(context.Background(), rgSearchTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, rgPath, args...)
	cmd.Dir = repoRoot
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
	}
	var stderr strings.Builder
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
	}

	var (
		results  []index.TextResult
		limitHit bool
	)
	scanner := bufio.NewScanner(stdout)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		parts := strings.SplitN(line, ":", 3)
		if len(parts) < 3 {
			continue
		}
		lineNum, err := strconv.Atoi(parts[1])
		if err != nil {
			continue
		}
		relPath := normalizeRelPath(parts[0])
		if !allowPath(relPath, includes, excludes) {
			continue
		}
		results = append(results, index.TextResult{
			RelPath: relPath,
			Line:    lineNum,
			Snippet: strings.TrimSpace(parts[2]),
		})
		if fetchLimit > 0 && len(results) >= fetchLimit {
			limitHit = true
			cancel()
			break
		}
	}
	scanErr := scanner.Err()
	waitErr := cmd.Wait()
	if limitHit {
		scanErr = nil
		waitErr = nil
	}

	if ctx.Err() == context.DeadlineExceeded && len(results) == 0 {
		return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
	}
	if scanErr != nil {
		return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
	}
	if waitErr != nil && !limitHit {
		if exitErr, ok := waitErr.(*exec.ExitError); ok {
			if exitErr.ExitCode() == 1 {
				if len(results) == 0 {
					return fmt.Errorf("no results found for '%s'", query)
				}
			} else {
				return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
			}
		} else {
			return searchTextGo(dbPath, query, lang, limit, jsonOut, includes, excludes)
		}
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
