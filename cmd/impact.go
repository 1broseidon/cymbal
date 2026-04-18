package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var impactCmd = &cobra.Command{
	Use:   "impact <symbol> [symbol2 ...]",
	Short: "Transitive caller analysis — what is impacted if this symbol changes",
	Long: `Find who calls this symbol, recursively, up to --depth.

Multi-symbol: pass more than one name (or pipe via --stdin) to get the union
of callers across all requested symbols. Each caller appears at most once;
a hit_symbols list records which of the requested symbols brought it in.
JSON mode returns a flat list with hit_symbols attribution.

Examples:
  cymbal impact handleRegister                    # single symbol
  cymbal impact Save Load Delete                  # union of callers
  cymbal impact Save Load -D 3                    # deeper chain
  cymbal outline store.go -s --names | cymbal impact --stdin`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		ctx, _ := cmd.Flags().GetInt("context")

		names, err := collectSymbols(cmd, args)
		if err != nil {
			return err
		}

		// Fetch once per symbol, then merge into a single deduplicated view.
		merged, sourceMap, totalRaw, err := mergeImpact(dbPath, names, depth, limit)
		if err != nil {
			return err
		}
		if len(merged) == 0 {
			if len(names) == 1 {
				return fmt.Errorf("no callers found for '%s'", names[0])
			}
			return fmt.Errorf("no callers found for any of: %s", strings.Join(names, ", "))
		}

		if jsonOut {
			enriched := enrichImpact(merged, ctx)
			// Attach hit_symbols attribution by returning a wrapper shape when
			// multi-symbol. Single-symbol JSON is unchanged.
			if len(names) > 1 {
				out := make([]map[string]any, 0, len(enriched))
				for i, row := range enriched {
					key := impactKey(merged[i])
					// enrichImpact returns values we rewrap; preserve its shape.
					m := map[string]any{"row": row, "hit_symbols": sourceMap[key]}
					out = append(out, m)
				}
				return writeJSON(map[string]any{
					"symbols":       names,
					"total_callers": len(merged),
					"raw_rows":      totalRaw,
					"results":       out,
				})
			}
			return writeJSON(enriched)
		}

		// Group by depth.
		maxDepth := 0
		for _, r := range merged {
			if r.Depth > maxDepth {
				maxDepth = r.Depth
			}
		}
		totalGroups := 0
		var content strings.Builder
		for d := 1; d <= maxDepth; d++ {
			var refs []refLine
			for _, r := range merged {
				if r.Depth != d {
					continue
				}
				ctxLines, ctxStart := readSourceContext(r.File, r.Line, ctx)
				label := strings.TrimSpace(readSourceLine(r.File, r.Line))
				if len(names) > 1 {
					if hits := sourceMap[impactKey(r)]; len(hits) > 0 {
						label = fmt.Sprintf("%s  [%s]", label, strings.Join(hits, ","))
					}
				}
				refs = append(refs, refLine{
					relPath:      r.RelPath,
					line:         r.Line,
					text:         label,
					contextLines: ctxLines,
					contextStart: ctxStart,
				})
			}
			if len(refs) == 0 {
				continue
			}
			lines, groups := dedupRefLines(refs)
			totalGroups += groups
			fmt.Fprintf(&content, "# depth %d\n", d)
			for _, l := range lines {
				content.WriteString(l)
				content.WriteByte('\n')
			}
		}

		meta := []kv{}
		if len(names) == 1 {
			meta = append(meta, kv{"symbol", names[0]})
		} else {
			meta = append(meta, kv{"symbols", strings.Join(names, ",")})
		}
		meta = append(meta, kv{"depth", fmt.Sprintf("%d", depth)})
		if totalGroups < len(merged) {
			meta = append(meta, kv{"groups", fmt.Sprintf("%d", totalGroups)})
		}
		meta = append(meta, kv{"total_callers", fmt.Sprintf("%d", len(merged))})
		if len(names) > 1 && totalRaw > len(merged) {
			meta = append(meta, kv{"deduped_from", fmt.Sprintf("%d", totalRaw)})
		}
		frontmatter(meta, content.String())
		return nil
	},
}

func init() {
	impactCmd.Flags().IntP("depth", "D", 2, "max call-chain depth (max 5)")
	impactCmd.Flags().IntP("limit", "n", 50, "max results per symbol")
	impactCmd.Flags().IntP("context", "C", 1, "lines of context around each call site (0 for single-line)")
	addStdinFlag(impactCmd)
	rootCmd.AddCommand(impactCmd)
}

// impactKey identifies a caller row uniquely enough to deduplicate across
// multiple input symbols. Two rows collide when they point at the same call
// site (file + line + caller identity), which is what we want: the union
// view should surface each real caller exactly once.
func impactKey(r index.ImpactResult) string {
	return fmt.Sprintf("%s:%d|%s", r.File, r.Line, r.Caller)
}

// mergeImpact runs FindImpact for each requested symbol, then dedupes callers
// by impactKey while preserving first-seen order. sourceMap records which of
// the requested symbols contributed each row so output can show attribution.
// totalRaw is the pre-dedup count for reporting ("dedupe savings").
func mergeImpact(dbPath string, names []string, depth, limit int) ([]index.ImpactResult, map[string][]string, int, error) {
	var merged []index.ImpactResult
	sourceMap := map[string][]string{}
	seen := map[string]int{} // key -> index in merged
	totalRaw := 0
	for _, name := range names {
		rows, err := index.FindImpact(dbPath, name, depth, limit)
		if err != nil {
			return nil, nil, 0, err
		}
		totalRaw += len(rows)
		for _, r := range rows {
			k := impactKey(r)
			if _, ok := seen[k]; !ok {
				seen[k] = len(merged)
				merged = append(merged, r)
			} else {
				// Keep shallowest depth; an indirect contributor shouldn't
				// make a direct caller look deeper than it is.
				idx := seen[k]
				if r.Depth < merged[idx].Depth {
					merged[idx] = r
				}
			}
			// Record attribution without duplicates.
			existing := sourceMap[k]
			dup := false
			for _, e := range existing {
				if e == name {
					dup = true
					break
				}
			}
			if !dup {
				sourceMap[k] = append(existing, name)
			}
		}
	}
	return merged, sourceMap, totalRaw, nil
}
