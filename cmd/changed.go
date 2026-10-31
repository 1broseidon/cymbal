package cmd

import (
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var changedCmd = &cobra.Command{
	Use:   "changed",
	Short: "Diff-scoped impact — what's affected by your current changes",
	Long: `Map a git diff to the symbols it touches, then report each changed
symbol's references and transitive impact in one call.

By default it analyses unstaged working-tree changes (git diff). Use --staged
for the staged changes, or --base <ref> to diff the working tree against another
single ref (e.g. your branch point):

  cymbal changed                 # unstaged changes (working tree vs index)
  cymbal changed --staged        # staged changes (index vs HEAD)
  cymbal changed --base main     # working tree vs main

Changed symbols are attributed by parsing the actual diffed blobs on both sides:
added/modified lines map to symbols in the new version, deleted lines to symbols
in the old version, so whole-symbol deletions are named (not mis-attributed to a
neighbour) and --staged attribution matches the staged content even with
unstaged edits present. References and impact are then queried from the
working-tree index (the "what's affected now" question), name-scoped (cymbal
resolves references by name): when a changed name has several definitions the
counts may span them, and that is reported as definition_count.

Limitations: arbitrary commit ranges whose new side is not the working tree are
not supported (the index reflects the working tree). Deleted symbols are listed
but have no impact (they no longer exist). Reference counts are exact
(un-truncated) but name-scoped; caller counts are capped by --limit and flagged
when truncated. Operates only on the current worktree.`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		if err := ensureFresh(dbPath); err != nil {
			return err
		}
		jsonOut := getJSONFlag(cmd)
		depth, _ := cmd.Flags().GetInt("depth")
		limit, _ := cmd.Flags().GetInt("limit")
		maxSymbols, _ := cmd.Flags().GetInt("max-symbols")
		aggLimit, _ := cmd.Flags().GetInt("max-impact")
		noTests, _ := cmd.Flags().GetBool("no-tests")
		testPaths, _ := cmd.Flags().GetStringArray("test-path")
		classifier := index.NewClassifier(testPaths)
		staged, _ := cmd.Flags().GetBool("staged")
		base, _ := cmd.Flags().GetString("base")
		scope, err := resolveScopeOrError(cmd)
		if err != nil {
			return err
		}

		cwd, err := os.Getwd()
		if err != nil {
			return err
		}
		repoRoot, err := gitRepoRoot(cwd)
		if err != nil {
			return fmt.Errorf("not a git repository: %w", err)
		}
		repoRoot = canonicalExistingPath(repoRoot)

		analysis, err := readChangedAnalysis(repoRoot, staged, base)
		if err != nil {
			return err
		}
		changed, changedOrder, deleted, deletedOrder := analysis.legacyProjection()
		baseLabel := analysis.Base
		skipped, conflicted := analysis.SkippedFiles, analysis.ConflictedFiles

		analyzed := changedOrder
		symbolsTruncated := false
		if maxSymbols > 0 && len(changedOrder) > maxSymbols {
			analyzed = changedOrder[:maxSymbols]
			symbolsTruncated = true
		}

		type changedResult struct {
			Symbol          string                `json:"symbol"`
			Files           []string              `json:"files"`
			DefinitionCount int                   `json:"definition_count"`
			Definitions     []defLoc              `json:"definitions,omitempty"`
			Ambiguous       bool                  `json:"ambiguous,omitempty"`
			DefinitionError bool                  `json:"definition_error,omitempty"`
			References      index.ReferenceCounts `json:"references"`
			ReferencesError bool                  `json:"references_error,omitempty"`
			Impact          *impactSummary        `json:"impact,omitempty"`
			ImpactStatus    string                `json:"impact_status,omitempty"` // "cap" | "error"
		}
		results := make([]changedResult, 0, len(analyzed))
		impactTruncated := false
		aggRows := 0
		for _, name := range analyzed {
			res := changedResult{Symbol: name, Files: sortedKeys(changed[name])}
			if locs, ok := symbolDefinitions(dbPath, name); ok {
				res.DefinitionCount = len(locs)
				res.Ambiguous = len(locs) > 1
				if res.Ambiguous {
					res.Definitions = locs
				}
			} else {
				res.DefinitionError = true
			}
			if perFile, rerr := index.ReferenceFileCountsWithScope(dbPath, name, scope); rerr == nil {
				res.References = index.FoldReferenceCountsWith(classifier, perFile)
			} else {
				res.ReferencesError = true
			}
			if aggLimit > 0 && aggRows >= aggLimit {
				res.ImpactStatus = "cap"
				impactTruncated = true
			} else if rows, tr, ierr := index.FindImpactWithOptions(dbPath, name, index.ImpactOptions{
				Scope: scope, Depth: depth, Limit: limit, NoTests: noTests, TestPaths: testPaths,
			}); ierr != nil {
				res.ImpactStatus = "error"
			} else {
				prod, test, unknown := classifyImpact(classifier, rows)
				res.Impact = &impactSummary{
					TotalCallers:      len(rows),
					ProductionCallers: prod,
					TestCallers:       test,
					UnknownCallers:    unknown,
					Truncated:         tr,
				}
				aggRows += len(rows)
				if tr {
					impactTruncated = true
				}
				// Note: reaching the aggregate cap here is NOT truncation by
				// itself — truncated is set only when a later symbol is actually
				// skipped (the "cap" branch above), so it never fires when the
				// cap is hit exactly on the final symbol with nothing omitted.
			}
			results = append(results, res)
		}

		truncated := symbolsTruncated || impactTruncated

		// deletedList pairs each removed symbol with the file(s) it left.
		type deletedSym struct {
			Symbol string   `json:"symbol"`
			Files  []string `json:"files"`
		}
		var deletedList []deletedSym
		for _, name := range deletedOrder {
			deletedList = append(deletedList, deletedSym{Symbol: name, Files: sortedKeys(deleted[name])})
		}

		if jsonOut {
			payload := map[string]any{
				"base":            baseLabel,
				"changed_symbols": len(changedOrder),
				"analyzed":        len(analyzed),
				"truncated":       truncated,
				"resolve_scope":   string(scope),
				"results":         results,
			}
			if len(deletedList) > 0 {
				payload["deleted_symbols"] = deletedList
			}
			if skipped > 0 {
				payload["skipped_files"] = skipped
			}
			if conflicted > 0 {
				payload["conflicted_files"] = conflicted
			}
			return writeJSON(payload)
		}

		renderWarnings := func(b *strings.Builder) {
			if len(deletedList) == 0 && skipped == 0 && conflicted == 0 {
				return
			}
			b.WriteString("\nwarnings:\n")
			for _, d := range deletedList {
				fmt.Fprintf(b, "  deleted: %s (%s)\n", d.Symbol, strings.Join(d.Files, ", "))
			}
			if skipped > 0 {
				fmt.Fprintf(b, "  %d changed file(s) had no parseable symbols (binary, unsupported, or non-code)\n", skipped)
			}
			if conflicted > 0 {
				fmt.Fprintf(b, "  %d conflicted file(s) (unmerged) not analyzed\n", conflicted)
			}
		}

		if len(changedOrder) == 0 {
			var b strings.Builder
			b.WriteString("No changed symbols found in the diff.\n")
			renderWarnings(&b)
			fmt.Fprint(os.Stderr, b.String())
			return nil
		}

		var content strings.Builder
		for _, res := range results {
			fmt.Fprintf(&content, "# %s  (%s)\n", res.Symbol, strings.Join(res.Files, ", "))
			if res.DefinitionCount > 1 {
				fmt.Fprintf(&content, "  definitions: %d (name-scoped metrics may span them)\n", res.DefinitionCount)
			}
			if res.ReferencesError {
				content.WriteString("  references: unavailable (lookup error)\n")
			} else {
				rc := res.References
				fmt.Fprintf(&content, "  references: %s in %s\n",
					formatCallerCounts(rc.Rows, rc.ProductionRows, rc.TestRows, rc.UnknownRows),
					formatCallerCounts(rc.Files, rc.ProductionFiles, rc.TestFiles, rc.UnknownFiles))
			}
			switch {
			case res.Impact != nil:
				im := res.Impact
				line := fmt.Sprintf("  impact: %s callers",
					formatCallerCounts(im.TotalCallers, im.ProductionCallers, im.TestCallers, im.UnknownCallers))
				if im.Truncated {
					line += "  [truncated]"
				}
				content.WriteString(line + "\n")
			case res.ImpactStatus == "cap":
				content.WriteString("  impact: not computed (max-impact cap reached)\n")
			case res.ImpactStatus == "error":
				content.WriteString("  impact: not computed (lookup error)\n")
			}
		}
		renderWarnings(&content)

		meta := []kv{
			{"changed_symbols", fmt.Sprintf("%d", len(changedOrder))},
			{"base", baseLabel},
		}
		if symbolsTruncated {
			meta = append(meta, kv{"analyzed", fmt.Sprintf("%d of %d", len(analyzed), len(changedOrder))})
		}
		if truncated {
			meta = append(meta, kv{"truncated", "true"})
		}
		meta = append(meta, kv{"resolve_scope", string(scope)})
		frontmatter(meta, content.String())
		return nil
	},
}

// impactSummary is the per-symbol caller rollup embedded in changed output.
type impactSummary struct {
	TotalCallers      int  `json:"total_callers"`
	ProductionCallers int  `json:"production_callers"`
	TestCallers       int  `json:"test_callers"`
	UnknownCallers    int  `json:"unknown_callers"`
	Truncated         bool `json:"truncated"`
}

func init() {
	changedCmd.Flags().Bool("staged", false, "diff staged changes (vs HEAD) instead of the working tree")
	changedCmd.Flags().String("base", "", "diff the working tree against this ref instead of HEAD (e.g. main)")
	changedCmd.Flags().IntP("depth", "D", 2, "max call-chain depth for impact (max 5)")
	changedCmd.Flags().IntP("limit", "n", 50, "max callers per changed symbol")
	changedCmd.Flags().Int("max-symbols", 40, "max changed symbols to analyze (0 = unlimited)")
	changedCmd.Flags().Int("max-impact", 500, "soft cap on total caller rows across symbols; once reached, remaining symbols skip impact (0 = unlimited)")
	changedCmd.Flags().Bool("no-tests", false, "exclude callers in test files from impact")
	changedCmd.Flags().StringArray("test-path", nil,
		"classify paths matching this pattern as test code, in addition to the built-in conventions (substring, or glob with **; repeatable)")
	addResolveScopeFlag(changedCmd)
	rootCmd.AddCommand(changedCmd)
}

// changedDiffArgs builds the git-diff argument list for the requested mode and a
// human label for the comparison base. The parser assumes plain uncolored
// unified-diff output with a/ b/ prefixes, so every user config that would
// change that contract is pinned: core.quotePath (path escaping),
// diff.mnemonicPrefix / diff.noprefix / diff.srcPrefix / diff.dstPrefix
// (header prefixes; the src/dst keys are ignored by git < 2.45), --no-color
// (color.diff=always colors piped output too), --no-textconv (line numbers
// must match the raw blob we parse, not a textconv rendering).
//
//	default:  git diff           — unstaged: working tree vs index
//	--staged: git diff --cached  — staged:   index vs HEAD
//	--base R: git diff R --      — working tree vs ref R
func changedDiffArgs(staged bool, base string) ([]string, string, error) {
	common := []string{
		"-c", "core.quotePath=false",
		"-c", "diff.mnemonicPrefix=false",
		"-c", "diff.noprefix=false",
		"-c", "diff.srcPrefix=a/",
		"-c", "diff.dstPrefix=b/",
		"diff", "--no-ext-diff", "--no-color", "--no-textconv", "--find-renames",
	}
	switch {
	case staged && base != "":
		return nil, "", fmt.Errorf("--staged and --base are mutually exclusive")
	case staged:
		return append(common, "--cached"), "staged", nil
	case base != "":
		if strings.HasPrefix(base, "-") {
			return nil, "", fmt.Errorf("invalid base ref %q", base)
		}
		if strings.Contains(base, "..") {
			// An arbitrary commit range's new side isn't the working tree, so
			// parsing the working tree as "new" would misattribute. Out of scope.
			return nil, "", fmt.Errorf("--base takes a single ref, not a range (%q)", base)
		}
		// Trailing "--" disambiguates the ref from a same-named file.
		return append(common, base, "--"), base, nil
	default:
		return common, "working tree", nil
	}
}

func sortedKeys(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
