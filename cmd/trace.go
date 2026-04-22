package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var traceCmd = &cobra.Command{
	Use:   "trace <symbol> [symbol2 ...]",
	Short: "Downward call trace — what does this symbol call?",
	Long: `Follow the call graph downward from a symbol: what it calls,
what those call, etc. Complementary to impact (which traces upward).

  investigate = "tell me about X"
  trace       = "what does X depend on?"
  impact      = "what depends on X?"

By default trace only follows invocation edges (ref kind=call). Use
--kinds to include broader relationships (e.g. type mentions).

Multi-symbol: pass more than one name (or pipe via --stdin) to get the
union of callees across all requested symbols. Shared callees are deduped
and a hit_symbols attribution list records which of the requested symbols
reached each callee.

Examples:
  cymbal trace handleRegister                     # 3-deep call chain
  cymbal trace handleRegister --depth 5           # deeper trace
  cymbal trace Save Load Delete                   # union of callees
  cymbal trace handleRegister --kinds call,use    # include identifier mentions
  cymbal outline svc.go -s --names | cymbal trace --stdin`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		kindsRaw, _ := cmd.Flags().GetString("kinds")
		kinds := parseKindsFlag(kindsRaw)

		// Strip file-hint prefixes ("pkg/file.go:Sym" -> "Sym"); trace resolves
		// by name internally so the hint is informational.
		rawNames, err := collectSymbols(cmd, args)
		if err != nil {
			return err
		}
		names := make([]string, 0, len(rawNames))
		for _, n := range rawNames {
			_, sym := parseSymbolArg(n)
			names = append(names, sym)
		}

		if graphRequested(cmd) {
			return renderAsGraph(cmd, dbPath, names, index.GraphDirectionDown)
		}

		merged, sourceMap, totalRaw, err := mergeTrace(dbPath, names, depth, limit, kinds)
		if err != nil {
			return err
		}
		if len(merged) == 0 {
			if len(names) == 1 {
				fmt.Printf("No outgoing calls found for '%s'.\n", names[0])
			} else {
				fmt.Printf("No outgoing calls found for any of: %s\n", strings.Join(names, ", "))
			}
			return nil
		}

		if jsonOut {
			if len(names) > 1 {
				// Attach hit_symbols attribution.
				out := make([]map[string]any, 0, len(merged))
				for _, r := range merged {
					out = append(out, map[string]any{
						"row":         r,
						"hit_symbols": sourceMap[traceKey(r)],
					})
				}
				return writeJSON(map[string]any{
					"symbols":   names,
					"direction": "downward (callees)",
					"depth":     depth,
					"edges":     len(merged),
					"raw_rows":  totalRaw,
					"results":   out,
				})
			}
			return writeJSON(merged)
		}

		var content strings.Builder
		for _, tr := range merged {
			if len(names) > 1 {
				if hits := sourceMap[traceKey(tr)]; len(hits) > 0 {
					fmt.Fprintf(&content, "  [%d] %s → %s  %s:%d  [%s]\n",
						tr.Depth, tr.Caller, tr.Callee, tr.RelPath, tr.Line,
						strings.Join(hits, ","))
					continue
				}
			}
			fmt.Fprintf(&content, "  [%d] %s → %s  %s:%d\n",
				tr.Depth, tr.Caller, tr.Callee, tr.RelPath, tr.Line)
		}

		meta := []kv{}
		if len(names) == 1 {
			meta = append(meta, kv{"symbol", names[0]})
		} else {
			meta = append(meta, kv{"symbols", strings.Join(names, ",")})
		}
		meta = append(meta, kv{"direction", "downward (callees)"})
		meta = append(meta, kv{"depth", fmt.Sprintf("%d", depth)})
		meta = append(meta, kv{"edges", fmt.Sprintf("%d", len(merged))})
		if len(names) > 1 && totalRaw > len(merged) {
			meta = append(meta, kv{"deduped_from", fmt.Sprintf("%d", totalRaw)})
		}
		frontmatter(meta, content.String())
		return nil
	},
}

// traceKey deduplicates trace rows by destination call site. The same callee
// reached by two requested symbols collapses into a single row with attribution.
func traceKey(r index.TraceResult) string {
	return fmt.Sprintf("%s:%d|%s", r.File, r.Line, r.Callee)
}

// mergeTrace runs FindTrace for each requested symbol and dedupes by traceKey,
// preserving first-seen order. sourceMap records which of the requested
// symbols contributed each row; totalRaw is the pre-dedup count.
func mergeTrace(dbPath string, names []string, depth, limit int, kinds []string) ([]index.TraceResult, map[string][]string, int, error) {
	var merged []index.TraceResult
	sourceMap := map[string][]string{}
	seen := map[string]int{}
	totalRaw := 0
	for _, name := range names {
		rows, err := index.FindTrace(dbPath, name, depth, limit, kinds...)
		if err != nil {
			return nil, nil, 0, err
		}
		totalRaw += len(rows)
		for _, r := range rows {
			k := traceKey(r)
			if _, ok := seen[k]; !ok {
				seen[k] = len(merged)
				merged = append(merged, r)
			} else {
				idx := seen[k]
				if r.Depth < merged[idx].Depth {
					merged[idx] = r
				}
			}
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

func init() {
	traceCmd.Flags().Int("depth", 3, "max traversal depth")
	traceCmd.Flags().IntP("limit", "n", 50, "max results per symbol")
	traceCmd.Flags().String("kinds", "call",
		"comma-separated ref kinds to follow: call, use, implements (default call)")
	addStdinFlag(traceCmd)
	addGraphFlags(traceCmd)
	rootCmd.AddCommand(traceCmd)
}

// parseKindsFlag splits a comma-separated --kinds value, trimming whitespace
// and dropping empties. Returns nil when the input is empty, which callers
// treat as "use the default set".
func parseKindsFlag(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		out = append(out, p)
	}
	return out
}
