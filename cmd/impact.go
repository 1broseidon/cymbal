package cmd

import (
	"errors"
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
		plan := resolveDBs(cmd)
		if err := ensureFresh(plan.Primary); err != nil {
			return err
		}
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		ctx, _ := cmd.Flags().GetInt("context")
		noTests, _ := cmd.Flags().GetBool("no-tests")
		testPaths, _ := cmd.Flags().GetStringArray("test-path")
		classifier := index.NewClassifier(testPaths)
		scope, err := resolveScopeOrError(cmd)
		if err != nil {
			return err
		}

		names, err := collectSymbols(cmd, args)
		if err != nil {
			return err
		}
		dbForName := func(name string) string {
			entry, _ := findSymbolEntry(plan, name)
			return entry.Path
		}
		// A name with no callers still gets a full, zero-count answer; only a
		// name the index has never seen fails.
		names, failures := keepIndexed(names, dbForName)
		if len(names) == 0 {
			return errors.Join(failures...)
		}

		if graphRequested(cmd) {
			// Graph rendering uses a single DB — the seed locator picks
			// whichever federated DB owns the first requested name. Mixed
			// graphs across worktrees would be visually confusing and would
			// violate non-goal #1 (no cross-worktree graph traversal).
			entry, _ := findSymbolEntry(plan, names[0])
			err := renderAsGraph(cmd, entry.Path, names, index.GraphDirectionUp, 1)
			return errors.Join(append(failures, err)...)
		}

		// Per-symbol seed-only federation: each name routes to whichever
		// DB owns it, callers stay within that DB.
		opts := index.ImpactOptions{Scope: scope, Depth: depth, Limit: limit, NoTests: noTests, TestPaths: testPaths}
		merged, sourceMap, labelMap, totalRaw, truncated, err := mergeImpactPlan(plan, names, opts)
		if err != nil {
			return err
		}

		ambig := ambiguousSymbolLanguages(plan, names)
		prodN, testN, unknownN := classifyImpact(classifier, merged)
		refs, refErr := aggregateReferences(names, scope, classifier, dbForName)
		defsByName, defCount, ambiguous, defErr := collectDefinitions(names, dbForName)
		effDepth, effLimit := index.ClampImpactBounds(depth, limit)

		enriched := enrichImpact(merged, ctx)
		if jsonOut {
			// One object shape for any symbol count. Each result carries
			// hit_symbols attribution (which requested symbols brought the
			// caller in); for a single symbol that's just that symbol.
			out := make([]map[string]any, 0, len(enriched))
			for i, row := range enriched {
				out = append(out, map[string]any{
					"row":         row,
					"hit_symbols": sourceMap[impactKey(merged[i])],
				})
			}
			payload := map[string]any{
				"symbols":            names,
				"total_callers":      len(merged),
				"production_callers": prodN,
				"test_callers":       testN,
				"raw_rows":           totalRaw,
				"truncated":          truncated,
				"depth":              effDepth,
				"limit":              effLimit,
				"metrics":            refs, // #4: exact, name-scoped reference metrics (no risk label)
				"definition_count":   defCount,
				"resolve_scope":      string(scope),
				"results":            out,
			}
			if unknownN > 0 {
				payload["unknown_callers"] = unknownN
			}
			if refErr {
				payload["references_error"] = true // counts may be incomplete; not authoritative
			}
			if defErr {
				payload["definition_count_error"] = true
			}
			if ambiguous { // at least one seed has multiple definitions
				payload["definitions"] = defsByName
			}
			if len(ambig) > 0 {
				payload["symbol_languages"] = ambig
			}
			return errors.Join(append(failures, writeJSON(payload))...)
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
			for _, r := range enriched {
				if r.Depth != d {
					continue
				}
				ref := r.sourceSnippet.refLine(r.RelPath, r.Line)
				if len(names) > 1 {
					if hits := sourceMap[impactKey(r.ImpactResult)]; len(hits) > 0 {
						ref.text = fmt.Sprintf("%s  [%s]", ref.text, strings.Join(hits, ","))
					}
				}
				refs = append(refs, ref)
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
		meta = append(meta, kv{"depth", fmt.Sprintf("%d", effDepth)})
		if totalGroups < len(merged) {
			meta = append(meta, kv{"groups", fmt.Sprintf("%d", totalGroups)})
		}
		meta = append(meta, kv{"total_callers", formatCallerCounts(len(merged), prodN, testN, unknownN)})
		if truncated {
			meta = append(meta, kv{"truncated", "true"})
		}
		// Labeled "metrics" to match the JSON key (the spec's #4 block). Value is
		// reference-row count split by class, "in" distinct-file count split.
		if refErr {
			meta = append(meta, kv{"metrics", "unavailable (lookup error)"})
		} else {
			meta = append(meta, kv{"metrics", fmt.Sprintf("%s refs in %s files",
				formatCallerCounts(refs.Rows, refs.ProductionRows, refs.TestRows, refs.UnknownRows),
				formatCallerCounts(refs.Files, refs.ProductionFiles, refs.TestFiles, refs.UnknownFiles))})
		}
		// A seed with multiple definitions is ambiguous: impact and reference
		// counts are name-scoped and may span those definitions.
		if ambiguous {
			meta = append(meta, kv{"definition_count", fmt.Sprintf("%d", defCount)})
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
		return errors.Join(failures...)
	},
}

func init() {
	impactCmd.Flags().IntP("depth", "D", 2, "max call-chain depth (max 5)")
	impactCmd.Flags().IntP("limit", "n", 50, "max results per symbol")
	impactCmd.Flags().IntP("context", "C", 1, "lines of context around each call site (0 for single-line)")
	impactCmd.Flags().Bool("no-tests", false, "exclude callers in test files (keeps production + unknown)")
	impactCmd.Flags().StringArray("test-path", nil,
		"classify paths matching this pattern as test code, in addition to the built-in conventions (substring, or glob with **; repeatable)")
	addStdinFlag(impactCmd)
	addGraphFlags(impactCmd)
	addResolveScopeFlag(impactCmd)
	rootCmd.AddCommand(impactCmd)
}

// impactKey identifies a caller row uniquely enough to deduplicate across
// multiple input symbols. Two rows collide when they point at the same call
// site (file + line + caller identity), which is what we want: the union
// view should surface each real caller exactly once.
func impactKey(r index.ImpactResult) string {
	return fmt.Sprintf("%s:%d|%s", r.File, r.Line, r.Caller)
}

// mergeImpact runs FindImpact for each requested symbol against a single DB.
// Retained for back-compat with existing single-DB callers (tests).
func mergeImpact(dbPath string, names []string, depth, limit int) ([]index.ImpactResult, map[string][]string, int, error) {
	opts := index.ImpactOptions{Scope: index.ResolveScopeFamily, Depth: depth, Limit: limit}
	merged, source, _, raw, _, err := runMergeImpact(names, opts, func(string) string { return dbPath })
	return merged, source, raw, err
}

// mergeImpactPlan is the federation-aware variant. Each requested name routes
// to whichever DB in plan.Federated owns the seed; downstream callers stay
// within that DB (non-goal #1). labelMap maps each name to its worktree label.
// The returned bool is true if any seed's per-symbol limit truncated its callers.
func mergeImpactPlan(plan DBPlan, names []string, opts index.ImpactOptions) ([]index.ImpactResult, map[string][]string, map[string]string, int, bool, error) {
	resolve := func(name string) string {
		entry, _ := findSymbolEntry(plan, name)
		return entry.Path
	}
	labelMap := make(map[string]string, len(names))
	for _, name := range names {
		entry, _ := findSymbolEntry(plan, name)
		labelMap[name] = entry.Label()
	}
	merged, source, _, raw, truncated, err := runMergeImpact(names, opts, resolve)
	return merged, source, labelMap, raw, truncated, err
}

// runMergeImpact factors the shared dedup logic so the single-DB and
// federated paths don't drift apart over time. The returned bool is true if any
// seed symbol hit its per-symbol limit (the merged set may be incomplete).
func runMergeImpact(names []string, opts index.ImpactOptions, dbForName func(string) string) ([]index.ImpactResult, map[string][]string, map[string]int, int, bool, error) {
	var merged []index.ImpactResult
	sourceMap := map[string][]string{}
	seen := map[string]int{} // key -> index in merged
	totalRaw := 0
	truncated := false
	for _, name := range names {
		dbPath := dbForName(name)
		rows, tr, err := index.FindImpactWithOptions(dbPath, name, opts)
		if err != nil {
			return nil, nil, nil, 0, false, err
		}
		if tr {
			truncated = true
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
	return merged, sourceMap, seen, totalRaw, truncated, nil
}

// aggregateReferences sums exact, name-scoped reference counts across the seed
// symbols and returns the total definition count (a name with several
// definitions makes the name-scoped counts span them — surfaced as an ambiguity
// signal). dbForName routes each name to whichever federated DB owns it.
func aggregateReferences(names []string, scope index.ResolveScope, cl *index.Classifier, dbForName func(string) string) (counts index.ReferenceCounts, refErr bool) {
	// Merge per-file row counts across seeds before folding, so a file that
	// references more than one requested symbol is counted once in the distinct
	// referencing-file totals (summing per-symbol counts would double-count it).
	merged := map[string]int{}
	for _, name := range names {
		if perFile, err := index.ReferenceFileCountsWithScope(dbForName(name), name, scope); err == nil {
			for rel, c := range perFile {
				merged[rel] += c
			}
		} else {
			refErr = true
		}
	}
	return index.FoldReferenceCountsWith(cl, merged), refErr
}

// defLoc is one indexed definition of a name (for ambiguity reporting).
type defLoc struct {
	Path      string `json:"path"`
	Language  string `json:"language"`
	StartLine int    `json:"start_line"`
}

// symbolDefinitions returns a name's indexed definition locations; ok is false
// if the lookup itself failed (distinct from a name with zero definitions).
func symbolDefinitions(dbPath, name string) (locs []defLoc, ok bool) {
	syms, err := index.SymbolsByName(dbPath, name)
	if err != nil {
		return nil, false
	}
	for _, s := range syms {
		locs = append(locs, defLoc{Path: s.RelPath, Language: s.Language, StartLine: s.StartLine})
	}
	return locs, true
}

// collectDefinitions gathers per-seed definition locations across names.
// anyAmbiguous is true if any single seed has more than one definition (the
// signal that name-scoped metrics may conflate definitions) — tracked per seed,
// not via total > len(names), so an undefined seed can't mask a genuinely
// ambiguous one. defErr is true if any lookup failed (so a zero count isn't
// mistaken for "no defs").
func collectDefinitions(names []string, dbForName func(string) string) (byName map[string][]defLoc, total int, anyAmbiguous, defErr bool) {
	byName = map[string][]defLoc{}
	for _, name := range names {
		locs, ok := symbolDefinitions(dbForName(name), name)
		if !ok {
			defErr = true
			continue
		}
		if len(locs) > 0 {
			byName[name] = locs
			total += len(locs)
			if len(locs) > 1 {
				anyAmbiguous = true
			}
		}
	}
	return byName, total, anyAmbiguous, defErr
}

// classifyImpact splits a caller set into production / test / unknown counts by
// each caller's file path. Used for the header/JSON triage split.
func classifyImpact(cl *index.Classifier, rows []index.ImpactResult) (prod, test, unknown int) {
	for _, r := range rows {
		switch cl.Classify(r.RelPath) {
		case index.PathClassTest:
			test++
		case index.PathClassUnknown:
			unknown++
		default:
			prod++
		}
	}
	return prod, test, unknown
}

// formatCallerCounts renders "N" or "N (P production, T test, U unknown)",
// omitting zero buckets so well-classified output stays terse.
func formatCallerCounts(total, prod, test, unknown int) string {
	if test == 0 && unknown == 0 {
		return fmt.Sprintf("%d", total)
	}
	parts := []string{fmt.Sprintf("%d production", prod)}
	if test > 0 {
		parts = append(parts, fmt.Sprintf("%d test", test))
	}
	if unknown > 0 {
		parts = append(parts, fmt.Sprintf("%d unknown", unknown))
	}
	return fmt.Sprintf("%d (%s)", total, strings.Join(parts, ", "))
}

// summarizeWorktreeLabels collapses per-name labels into a single meta
// value: empty when no name came from a non-current worktree, the bare
// label when all non-empty labels agree, or a comma-separated set when
// names came from different worktrees.
func summarizeWorktreeLabels(names []string, labels map[string]string) string {
	seen := make(map[string]struct{}, len(names))
	var ordered []string
	for _, n := range names {
		l := labels[n]
		if l == "" {
			continue
		}
		if _, ok := seen[l]; ok {
			continue
		}
		seen[l] = struct{}{}
		ordered = append(ordered, l)
	}
	return strings.Join(ordered, ",")
}
