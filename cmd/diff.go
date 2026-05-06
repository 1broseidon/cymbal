package cmd

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff <symbol> [base]",
	Short: "Show git diff scoped to a symbol's definition",
	Long: `Show the git diff for a symbol's line range.

Resolves the symbol to a file and line range, then runs git diff
filtered to only hunks that overlap the symbol's definition.

Examples:
  cymbal diff ParseFile           # diff vs HEAD
  cymbal diff ParseFile main      # diff vs main branch
  cymbal diff ParseFile abc123    # diff vs specific commit
  cymbal diff --stat ParseFile    # show diffstat only`,
	Args: cobra.RangeArgs(1, 2),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		base := "HEAD"
		if len(args) > 1 {
			base = args[1]
		}
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		stat, _ := cmd.Flags().GetBool("stat")

		return runDiff(dbPath, name, base, stat, jsonOut)
	},
}

func init() {
	diffCmd.Flags().Bool("stat", false, "show diffstat instead of full diff")
	rootCmd.AddCommand(diffCmd)
}

func runDiff(dbPath, name, base string, stat, jsonOut bool) error {
	if strings.HasPrefix(base, "-") {
		return fmt.Errorf("invalid diff base %q", base)
	}

	results, err := index.SymbolsByName(dbPath, name)
	if err != nil {
		return err
	}

	if len(results) == 0 {
		return fmt.Errorf("symbol not found: %s", name)
	}

	// Multiple matches — disambiguate.
	if len(results) > 1 {
		if jsonOut {
			return writeJSON(map[string]any{
				"ambiguous": true,
				"matches":   results,
			})
		}
		fmt.Fprintf(os.Stderr, "Multiple matches for '%s' — be more specific:\n", name)
		for _, r := range results {
			fmt.Fprintf(os.Stderr, "  %-12s %-40s %s:%d-%d\n", r.Kind, r.Name, r.RelPath, r.StartLine, r.EndLine)
		}
		return fmt.Errorf("ambiguous symbol '%s' (%d matches)", name, len(results))
	}

	sym := results[0]

	// Get repo root.
	dir := filepath.Dir(sym.File)
	repoRoot, err := gitRepoRoot(dir)
	if err != nil {
		return fmt.Errorf("not a git repository: %w", err)
	}

	// Compute relative path from repo root.
	repoRoot = canonicalExistingPath(repoRoot)
	symFile := canonicalExistingPath(sym.File)
	relPath, err := filepath.Rel(repoRoot, symFile)
	if err != nil {
		return fmt.Errorf("computing relative path: %w", err)
	}

	if stat {
		return runDiffStat(repoRoot, relPath, base, sym, jsonOut)
	}

	// Run git diff.
	out, err := exec.Command("git", "-C", repoRoot, "diff", "--no-ext-diff", base, "--", relPath).Output()
	if err != nil {
		// git diff exits 1 when there are differences; check for real errors.
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return fmt.Errorf("git diff: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
	}

	diffOutput := string(out)
	if diffOutput == "" {
		if jsonOut {
			return writeJSON(map[string]any{
				"symbol":     sym.Name,
				"file":       sym.RelPath,
				"start_line": sym.StartLine,
				"end_line":   sym.EndLine,
				"base":       base,
				"diff":       "",
			})
		}
		fmt.Fprintf(os.Stderr, "No diff for %s (%s:%d-%d) against %s\n", sym.Name, sym.RelPath, sym.StartLine, sym.EndLine, base)
		return nil
	}

	filtered := filterDiffHunks(diffOutput, sym.StartLine, sym.EndLine)

	if jsonOut {
		return writeJSON(map[string]any{
			"symbol":     sym.Name,
			"file":       sym.RelPath,
			"start_line": sym.StartLine,
			"end_line":   sym.EndLine,
			"base":       base,
			"diff":       filtered,
		})
	}

	frontmatter([]kv{
		{"symbol", sym.Name},
		{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
		{"base", base},
	}, filtered)
	return nil
}

func runDiffStat(repoRoot, relPath, base string, sym index.SymbolResult, jsonOut bool) error {
	out, err := exec.Command("git", "-C", repoRoot, "diff", "--no-ext-diff", "--stat", base, "--", relPath).Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok && len(exitErr.Stderr) > 0 {
			return fmt.Errorf("git diff --stat: %s", strings.TrimSpace(string(exitErr.Stderr)))
		}
	}

	statOutput := string(out)

	if jsonOut {
		return writeJSON(map[string]any{
			"symbol":     sym.Name,
			"file":       sym.RelPath,
			"start_line": sym.StartLine,
			"end_line":   sym.EndLine,
			"base":       base,
			"stat":       strings.TrimSpace(statOutput),
		})
	}

	if statOutput == "" {
		fmt.Fprintf(os.Stderr, "No diff for %s (%s:%d-%d) against %s\n", sym.Name, sym.RelPath, sym.StartLine, sym.EndLine, base)
		return nil
	}

	fmt.Print(statOutput)
	return nil
}

// gitRepoRoot returns the repository root for the given directory.
func gitRepoRoot(dir string) (string, error) {
	out, err := exec.Command("git", "-C", dir, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}

func canonicalExistingPath(path string) string {
	abs, err := filepath.Abs(path)
	if err == nil {
		path = abs
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err == nil {
		return resolved
	}
	return filepath.Clean(path)
}

// filterDiffHunks filters unified diff output to only hunks whose new-file
// range overlaps [startLine, endLine]. File headers (--- a/, +++ b/) are preserved.
func filterDiffHunks(diffOutput string, startLine, endLine int) string {
	lines := strings.SplitAfter(diffOutput, "\n")

	var result strings.Builder
	var fileHeaders []string
	var currentHunk []string
	hunkOverlaps := false
	wroteHeaders := false

	for _, line := range lines {
		trimmed := strings.TrimRight(line, "\n")

		// File-level headers.
		if strings.HasPrefix(trimmed, "diff --git ") ||
			strings.HasPrefix(trimmed, "index ") ||
			strings.HasPrefix(trimmed, "--- ") ||
			strings.HasPrefix(trimmed, "+++ ") {
			// Flush previous hunk if it overlaps.
			if hunkOverlaps && len(currentHunk) > 0 {
				if !wroteHeaders {
					for _, h := range fileHeaders {
						result.WriteString(h)
					}
					wroteHeaders = true
				}
				for _, h := range currentHunk {
					result.WriteString(h)
				}
			}
			currentHunk = nil
			hunkOverlaps = false
			fileHeaders = append(fileHeaders, line)
			continue
		}

		// Hunk header.
		if strings.HasPrefix(trimmed, "@@") {
			// Flush previous hunk if it overlaps.
			if hunkOverlaps && len(currentHunk) > 0 {
				if !wroteHeaders {
					for _, h := range fileHeaders {
						result.WriteString(h)
					}
					wroteHeaders = true
				}
				for _, h := range currentHunk {
					result.WriteString(h)
				}
			}

			currentHunk = []string{line}
			hunkOverlaps = false

			// Parse @@ -a,b +c,d @@ to get new-file range.
			newStart, newCount := parseHunkHeader(trimmed)
			if newStart > 0 {
				hunkEnd := newStart + newCount - 1
				if newCount == 0 {
					hunkEnd = newStart
				}
				// Check overlap: hunk [newStart, hunkEnd] vs symbol [startLine, endLine].
				if newStart <= endLine && hunkEnd >= startLine {
					hunkOverlaps = true
				}
			}
			continue
		}

		// Regular diff line — accumulate in current hunk.
		if len(currentHunk) > 0 {
			currentHunk = append(currentHunk, line)
		}
	}

	// Flush final hunk.
	if hunkOverlaps && len(currentHunk) > 0 {
		if !wroteHeaders {
			for _, h := range fileHeaders {
				result.WriteString(h)
			}
		}
		for _, h := range currentHunk {
			result.WriteString(h)
		}
	}

	return result.String()
}

// parseHunkHeader extracts new-file start and count from a unified diff hunk header.
// Format: @@ -oldStart[,oldCount] +newStart[,newCount] @@
func parseHunkHeader(header string) (newStart, newCount int) {
	// Find the +N,M or +N portion.
	plusIdx := strings.Index(header, "+")
	if plusIdx < 0 {
		return 0, 0
	}

	rest := header[plusIdx+1:]
	// Find end (next space or @@).
	endIdx := strings.Index(rest, " ")
	if endIdx < 0 {
		return 0, 0
	}
	rangeStr := rest[:endIdx]

	parts := strings.SplitN(rangeStr, ",", 2)
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return 0, 0
	}

	count := 1
	if len(parts) == 2 {
		if c, err := strconv.Atoi(parts[1]); err == nil {
			count = c
		}
	}

	return start, count
}
