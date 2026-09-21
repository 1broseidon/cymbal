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

Callees that don't resolve to an indexed symbol (stdlib, third-party, or
builtins) are filtered out by default. Pass --include-unresolved to keep
them in the text/JSON output (and as dashed ext: nodes in --graph mode).

A callee resolves only within its caller's language family by default
(--resolve-scope family: JVM java/kotlin/scala, JS javascript/typescript/tsx,
C c/cpp). Use --resolve-scope same for exact-language only, or all to resolve
across every language. This affects which callees a name resolves to, not
which symbol the trace starts from: if the symbol you ask about exists in more
than one language, the trace still covers each. Add a file hint like
pkg/file.go:Name to target a single symbol.

Multi-symbol: pass more than one name (or pipe via --stdin) to get the
union of callees across all requested symbols. Shared callees are deduped
and a hit_symbols attribution list records which of the requested symbols
reached each callee.

Examples:
  cymbal trace handleRegister                     # 3-deep call chain
  cymbal trace handleRegister --depth 5           # deeper trace
  cymbal trace Save Load Delete                   # union of callees
  cymbal trace handleRegister --kinds call,use    # include identifier mentions
  cymbal trace handleRegister --include-unresolved # keep stdlib/external calls
  cymbal outline svc.go -s --names | cymbal trace --stdin`,
	Args: cobra.MinimumNArgs(0),
	RunE: func(cmd *cobra.Command, args []string) error {
		plan := resolveDBs(cmd)
		if err := ensureFresh(plan.Primary); err != nil {
			return err
		}
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		// Report the depth the BFS actually traverses, not the raw flag —
		// the store clamps to [1, 5] (and defaults non-positive to 3).
		depth = index.ClampTraceDepth(depth)
		limit, _ := cmd.Flags().GetInt("limit")
		kindsRaw, _ := cmd.Flags().GetString("kinds")
		kinds := parseKindsFlag(kindsRaw)
		includeUnresolved, _ := cmd.Flags().GetBool("include-unresolved")
		scope, err := resolveScopeOrError(cmd)
		if err != nil {
			return err
		}

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
			entry, _ := findSymbolEntry(plan, names[0])
			return renderAsGraph(cmd, entry.Path, names, index.GraphDirectionDown, 2)
		}

		opts := index.TraceOptions{IncludeUnresolved: includeUnresolved, Scope: scope}
		merged, sourceMap, labelMap, totalRaw, truncated, err := mergeTracePlan(plan, names, depth, limit, kinds, opts)
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

		ambig := ambiguousSymbolLanguages(plan, names)

		if jsonOut {
			// One object shape for any symbol count. Each result carries
			// hit_symbols attribution (which requested symbols reached the
			// callee); for a single symbol that's just that symbol.
			out := make([]map[string]any, 0, len(merged))
			for _, r := range merged {
				out = append(out, map[string]any{
					"row":         r,
					"hit_symbols": sourceMap[traceKey(r)],
				})
			}
			payload := map[string]any{
				"symbols":       names,
				"direction":     "downward (callees)",
				"depth":         depth,
				"edges":         len(merged),
				"raw_rows":      totalRaw,
				"truncated":     truncated,
				"resolve_scope": string(scope),
				"results":       out,
			}
			if len(ambig) > 0 {
				payload["symbol_languages"] = ambig
			}
			return writeJSON(payload)
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
		if truncated {
			meta = append(meta, kv{"truncated", "true"})
		}
		meta = append(meta, kv{"resolve_scope", string(scope)})
		if s := formatSymbolLanguages(ambig); s != "" {
			meta = append(meta, kv{"symbol_languages", s})
		}
		if len(names) > 1 && totalRaw > len(merged) {
			meta = append(meta, kv{"deduped_from", fmt.Sprintf("%d", totalRaw)})
		}
		if wt := summarizeWorktreeLabels(names, labelMap); wt != "" {
			meta = append(meta, kv{"worktree", wt})
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

// mergeTrace runs FindTrace against a single DB. Retained for back-compat
// with single-DB callers (tests).
func mergeTrace(dbPath string, names []string, depth, limit int, kinds []string) ([]index.TraceResult, map[string][]string, int, error) {
	merged, source, _, raw, _, err := runMergeTrace(names, depth, limit, kinds, index.TraceOptions{}, func(string) string { return dbPath })
	return merged, source, raw, err
}

// mergeTracePlan is the federation-aware variant: each name routes to
// whichever DB owns the seed; callees stay within that DB (non-goal #1).
// The returned bool is true if any seed's per-symbol limit truncated its callees.
func mergeTracePlan(plan DBPlan, names []string, depth, limit int, kinds []string, opts index.TraceOptions) ([]index.TraceResult, map[string][]string, map[string]string, int, bool, error) {
	resolve := func(name string) string {
		entry, _ := findSymbolEntry(plan, name)
		return entry.Path
	}
	labelMap := make(map[string]string, len(names))
	for _, name := range names {
		entry, _ := findSymbolEntry(plan, name)
		labelMap[name] = entry.Label()
	}
	merged, source, _, raw, truncated, err := runMergeTrace(names, depth, limit, kinds, opts, resolve)
	return merged, source, labelMap, raw, truncated, err
}

func runMergeTrace(names []string, depth, limit int, kinds []string, opts index.TraceOptions, dbForName func(string) string) ([]index.TraceResult, map[string][]string, map[string]int, int, bool, error) {
	var merged []index.TraceResult
	sourceMap := map[string][]string{}
	seen := map[string]int{}
	totalRaw := 0
	truncated := false
	for _, name := range names {
		dbPath := dbForName(name)
		rows, tr, err := index.FindTraceWithTruncation(dbPath, name, depth, limit, opts, kinds...)
		if err != nil {
			return nil, nil, nil, 0, false, err
		}
		if tr {
			truncated = true
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
	return merged, sourceMap, seen, totalRaw, truncated, nil
}

func init() {
	traceCmd.Flags().Int("depth", 3, "max traversal depth")
	traceCmd.Flags().IntP("limit", "n", 50, "max results per symbol")
	traceCmd.Flags().String("kinds", "call",
		"comma-separated ref kinds to follow: call, use, implements (default call)")
	addStdinFlag(traceCmd)
	addGraphFlags(traceCmd)
	addResolveScopeFlag(traceCmd)
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
