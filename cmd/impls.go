package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var implsCmd = &cobra.Command{
	Use:   "impls <symbol> [symbol2 ...]",
	Short: "Find types that implement / conform to / extend a symbol",
	Long: `Find local types that declare themselves as implementing, conforming to,
or extending the given name.

This covers Swift protocol conformance, Go interface embedding, Java/C#/Kotlin/
TypeScript implements clauses, Scala with-chains, Rust trait impls, Dart mixins/
interfaces, Python base classes, Ruby include/extend, PHP implements, and C++
base classes. Results are best-effort based on AST name matching — external
(framework) targets are returned with resolved=false.

Multi-symbol: pass more than one name (or pipe via --stdin) to get the answer
for all of them in one turn. JSON mode returns a map keyed by the requested
name.

The inverse direction is also supported: use --of to list what a specific type
itself implements. --of is always single-symbol.

Examples:
  cymbal impls Reader                       # who implements io.Reader?
  cymbal impls Reader Writer Closer         # three interfaces at once
  cymbal impls LiveActivityIntent           # works for external framework protocols
  cymbal impls Plugin --lang go             # only Go implementers
  cymbal impls --of TimerActivityIntent     # what does this type implement?
  cymbal impls Foo --json                   # structured output for agents`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		limit, _ := cmd.Flags().GetInt("limit")
		langFilter, _ := cmd.Flags().GetString("lang")
		includes, _ := cmd.Flags().GetStringArray("path")
		excludes, _ := cmd.Flags().GetStringArray("exclude")
		inverse, _ := cmd.Flags().GetString("of")
		resolvedOnly, _ := cmd.Flags().GetBool("resolved")
		unresolvedOnly, _ := cmd.Flags().GetBool("unresolved")

		// --of is inherently singular; positional args are disallowed with it.
		if inverse != "" {
			if len(args) > 0 {
				return fmt.Errorf("pass either positional symbols or --of <type>, not both")
			}
			return runImplsOne(dbPath, inverse, inverse, jsonOut, limit, langFilter, includes, excludes, resolvedOnly, unresolvedOnly)
		}

		names, err := collectSymbols(cmd, args)
		if err != nil {
			return err
		}

		// JSON multi-mode: one map keyed by requested name.
		if jsonOut && len(names) > 1 {
			out := make(map[string]any, len(names))
			for _, n := range names {
				rows, ferr := fetchImpls(dbPath, n, "", limit, langFilter, includes, excludes, resolvedOnly, unresolvedOnly)
				if ferr != nil {
					out[n] = map[string]any{"error": ferr.Error()}
					continue
				}
				out[n] = map[string]any{
					"symbol":            n,
					"direction":         "implementors (incoming)",
					"implementor_count": len(rows),
					"results":           rows,
				}
			}
			return writeJSON(out)
		}

		multi := len(names) > 1
		for i, n := range names {
			if multi {
				multiSymbolBanner(n, i == 0)
				multiSymbolHeader(n)
				// Keep "no implementors" on stdout so it stays in order with
				// the header banners instead of interleaving with stderr.
				rows, ferr := fetchImpls(dbPath, n, "", limit, langFilter, includes, excludes, resolvedOnly, unresolvedOnly)
				if ferr != nil {
					fmt.Printf("error: %v\n", ferr)
					continue
				}
				if len(rows) == 0 {
					fmt.Printf("No implementors found for '%s'.\n", n)
					continue
				}
				_ = renderJSONOrFrontmatter(false, rows,
					[]kv{
						{"symbol", n},
						{"direction", "implementors (incoming)"},
						{"implementor_count", fmt.Sprintf("%d", len(rows))},
					},
					formatImplementorResults(rows, false),
				)
				continue
			}
			if err := runImplsOne(dbPath, n, "", jsonOut, limit, langFilter, includes, excludes, resolvedOnly, unresolvedOnly); err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", n, err)
				continue
			}
		}
		return nil
	},
}

// fetchImpls runs an implementors-or-implements query with the shared filters.
// When inverse != "", --of <inverse> is used; otherwise `name` is the incoming
// target.
func fetchImpls(dbPath, name, inverse string, limit int, langFilter string, includes, excludes []string, resolvedOnly, unresolvedOnly bool) ([]index.ImplementorResult, error) {
	fetchLimit := widenPathFilterLimit(limit, len(includes) > 0 || len(excludes) > 0 || langFilter != "")
	var results []index.ImplementorResult
	var err error
	if inverse != "" {
		results, err = index.FindImplements(dbPath, inverse, fetchLimit)
	} else {
		results, err = index.FindImplementors(dbPath, name, fetchLimit)
	}
	if err != nil {
		return nil, err
	}
	if langFilter != "" {
		filtered := results[:0]
		for _, r := range results {
			if r.Language == langFilter {
				filtered = append(filtered, r)
			}
		}
		results = filtered
	}
	results = filterByPath(results, func(r index.ImplementorResult) string { return r.RelPath }, includes, excludes)
	if resolvedOnly || unresolvedOnly {
		filtered := results[:0]
		for _, r := range results {
			if resolvedOnly && !r.Resolved {
				continue
			}
			if unresolvedOnly && r.Resolved {
				continue
			}
			filtered = append(filtered, r)
		}
		results = filtered
	}
	if limit > 0 && len(results) > limit {
		results = results[:limit]
	}
	return results, nil
}

// runImplsOne renders a single-symbol impls result (either incoming or --of).
func runImplsOne(dbPath, name, inverse string, jsonOut bool, limit int, langFilter string, includes, excludes []string, resolvedOnly, unresolvedOnly bool) error {
	results, err := fetchImpls(dbPath, name, inverse, limit, langFilter, includes, excludes, resolvedOnly, unresolvedOnly)
	if err != nil {
		return err
	}
	if len(results) == 0 {
		if inverse != "" {
			fmt.Fprintf(os.Stderr, "No implements edges found for '%s'.\n", inverse)
		} else {
			fmt.Fprintf(os.Stderr, "No implementors found for '%s'.\n", name)
		}
		return nil
	}

	var meta []kv
	if inverse != "" {
		meta = []kv{{"symbol", inverse}, {"direction", "implements (outgoing)"}, {"edges", fmt.Sprintf("%d", len(results))}}
	} else {
		meta = []kv{{"symbol", name}, {"direction", "implementors (incoming)"}, {"implementor_count", fmt.Sprintf("%d", len(results))}}
	}
	return renderJSONOrFrontmatter(
		jsonOut,
		results,
		meta,
		formatImplementorResults(results, inverse != ""),
	)
}

func init() {
	implsCmd.Flags().IntP("limit", "n", 50, "max results per symbol")
	implsCmd.Flags().StringP("lang", "l", "", "filter by language (swift, go, java, ...)")
	implsCmd.Flags().StringArray("path", nil, "include only results whose path matches this glob (repeatable)")
	implsCmd.Flags().StringArray("exclude", nil, "exclude results whose path matches this glob (repeatable)")
	implsCmd.Flags().String("of", "", "inverse direction: list what this type implements (single symbol)")
	implsCmd.Flags().Bool("resolved", false, "only show targets whose declaration is in the index")
	implsCmd.Flags().Bool("unresolved", false, "only show external / unresolved targets")
	addStdinFlag(implsCmd)
	rootCmd.AddCommand(implsCmd)
}

// formatImplementorResults renders a human-readable listing. When inverse is
// true (the --of direction), the target column is the interesting column; the
// implementer is the fixed input type.
func formatImplementorResults(results []index.ImplementorResult, inverse bool) string {
	if len(results) == 0 {
		return ""
	}
	// Column width for primary name.
	nameWidth := 0
	for _, r := range results {
		primary := r.Implementer
		if inverse {
			primary = r.Target
		}
		if primary == "" {
			primary = "(anonymous)"
		}
		if n := len(primary); n > nameWidth {
			nameWidth = n
		}
	}
	if nameWidth > 48 {
		nameWidth = 48
	}

	var b strings.Builder
	for _, r := range results {
		primary := r.Implementer
		if inverse {
			primary = r.Target
		}
		if primary == "" {
			primary = "(anonymous)"
		}
		tag := ""
		if !r.Resolved {
			tag = "  (external)"
		}
		loc := fmt.Sprintf("%s:%d", r.RelPath, r.Line)
		if inverse {
			fmt.Fprintf(&b, "  %-*s  %s%s\n", nameWidth, primary, loc, tag)
		} else {
			fmt.Fprintf(&b, "  %-*s  %s%s\n", nameWidth, primary, loc, tag)
		}
	}
	return b.String()
}
