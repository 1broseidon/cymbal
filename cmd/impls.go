package cmd

import (
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var implsCmd = &cobra.Command{
	Use:   "impls <symbol>",
	Short: "Find types that implement / conform to / extend a symbol",
	Long: `Find local types that declare themselves as implementing, conforming to,
or extending the given name.

This covers Swift protocol conformance, Go interface embedding, Java/C#/Kotlin/
TypeScript implements clauses, Scala with-chains, Rust trait impls, Dart mixins/
interfaces, Python base classes, Ruby include/extend, PHP implements, and C++
base classes. Results are best-effort based on AST name matching — external
(framework) targets are returned with resolved=false.

The inverse direction is also supported: use --of to list what a specific type
itself implements.

Examples:
  cymbal impls Reader                   # who implements io.Reader?
  cymbal impls LiveActivityIntent       # works for external framework protocols
  cymbal impls Plugin --lang go         # only Go implementers
  cymbal impls --of TimerActivityIntent # what does this type implement?
  cymbal impls Foo --json               # structured output for agents`,
	Args: cobra.MaximumNArgs(1),
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

		name := inverse
		if name == "" {
			if len(args) == 0 {
				return fmt.Errorf("symbol name required (or use --of <type>)")
			}
			name = args[0]
		} else if len(args) > 0 {
			return fmt.Errorf("pass either a positional symbol or --of <type>, not both")
		}

		fetchLimit := widenPathFilterLimit(limit, len(includes) > 0 || len(excludes) > 0 || langFilter != "")

		var results []index.ImplementorResult
		var err error
		if inverse != "" {
			results, err = index.FindImplements(dbPath, name, fetchLimit)
		} else {
			results, err = index.FindImplementors(dbPath, name, fetchLimit)
		}
		if err != nil {
			return err
		}

		// Language filter.
		if langFilter != "" {
			filtered := results[:0]
			for _, r := range results {
				if r.Language == langFilter {
					filtered = append(filtered, r)
				}
			}
			results = filtered
		}

		// Path filter.
		results = filterByPath(results, func(r index.ImplementorResult) string { return r.RelPath }, includes, excludes)

		// Resolved/unresolved filter.
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

		if len(results) == 0 {
			if inverse != "" {
				fmt.Fprintf(os.Stderr, "No implements edges found for '%s'.\n", name)
			} else {
				fmt.Fprintf(os.Stderr, "No implementors found for '%s'.\n", name)
			}
			return nil
		}

		meta := []kv{{"symbol", name}}
		if inverse != "" {
			meta = append(meta, kv{"direction", "implements (outgoing)"})
			meta = append(meta, kv{"edges", fmt.Sprintf("%d", len(results))})
		} else {
			meta = append(meta, kv{"direction", "implementors (incoming)"})
			meta = append(meta, kv{"implementor_count", fmt.Sprintf("%d", len(results))})
		}

		return renderJSONOrFrontmatter(
			jsonOut,
			results,
			meta,
			formatImplementorResults(results, inverse != ""),
		)
	},
}

func init() {
	implsCmd.Flags().IntP("limit", "n", 50, "max results")
	implsCmd.Flags().StringP("lang", "l", "", "filter by language (swift, go, java, ...)")
	implsCmd.Flags().StringArray("path", nil, "include only results whose path matches this glob (repeatable)")
	implsCmd.Flags().StringArray("exclude", nil, "exclude results whose path matches this glob (repeatable)")
	implsCmd.Flags().String("of", "", "inverse direction: list what this type implements")
	implsCmd.Flags().Bool("resolved", false, "only show targets whose declaration is in the index")
	implsCmd.Flags().Bool("unresolved", false, "only show external / unresolved targets")
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
