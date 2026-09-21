package cmd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "cymbal",
	Short: "Fast code indexer and symbol discovery tool",
	Long: `Cymbal is a blazing-fast code indexer, parser, and symbol discovery CLI.
It uses tree-sitter for multi-language AST parsing and SQLite for indexed storage,
designed to be called by AI agents and developer tools.`,
	// Don't dump Usage on RunE errors — keeps "no results found" exits clean.
	SilenceUsage: true,
	PersistentPreRunE: func(cmd *cobra.Command, args []string) error {
		return prepareUpdateNotice(cmd)
	},
	PersistentPostRun: func(cmd *cobra.Command, args []string) {
		emitUpdateNotice(cmd)
	},
}

// Execute runs the root command.
func Execute() error {
	return rootCmd.Execute()
}

func init() {
	rootCmd.PersistentFlags().StringP("db", "d", "", "path to cymbal database (default: auto per-repo)")
	rootCmd.PersistentFlags().Bool("json", false, "output as JSON")
	rootCmd.PersistentFlags().Bool("no-federate", false, "disable cross-worktree symbol federation (single DB only)")
}

// federationCap bounds fan-out for pathological setups with hundreds of
// worktrees. The reporter's case is 1, typical agent workflows are 1–5,
// 32 is a generous ceiling that still keeps query time bounded.
const federationCap = 32

// DBEntry is one database in a federation set.
type DBEntry struct {
	Path      string // absolute path to the SQLite index.db
	Root      string // canonical worktree path
	Branch    string // empty when detached
	IsCurrent bool   // matches the cwd/path-arg we routed from
}

// Label returns a human-readable worktree identifier:
//   - "" when this is the current entry (callers can omit labels)
//   - "<basename>" when basename matches branch (or branch is empty)
//   - "<basename> (<branch>)" when they differ
func (e DBEntry) Label() string {
	if e.IsCurrent {
		return ""
	}
	base := filepath.Base(e.Root)
	if e.Branch == "" || e.Branch == base {
		return base
	}
	return base + " (" + e.Branch + ")"
}

// DBPlan describes which databases a command should consult.
//
// Federated always has at least one entry (the primary). When NoFederate
// is true the slice has length 1, so commands can iterate uniformly.
type DBPlan struct {
	Primary    string
	Federated  []DBEntry
	NoFederate bool
}

// resolveDBs computes the database plan for a command invocation.
//
// Priority order:
//  1. --db flag or CYMBAL_DB env: single-DB mode, federation disabled.
//  2. --no-federate flag: single-DB mode keyed on cwd, federation disabled.
//  3. cwd's git common dir has linked worktrees: federation across all
//     indexed sibling worktrees, capped at federationCap.
//  4. Plain repo (no worktree relationships): single entry, no fan-out cost.
func resolveDBs(cmd *cobra.Command) DBPlan {
	primary := getDBPath(cmd)
	plan := DBPlan{
		Primary:   primary,
		Federated: []DBEntry{{Path: primary, IsCurrent: true}},
	}
	// Explicit overrides bypass federation entirely.
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		plan.NoFederate = true
		return plan
	}
	if os.Getenv("CYMBAL_DB") != "" {
		plan.NoFederate = true
		return plan
	}
	if nf, _ := cmd.Flags().GetBool("no-federate"); nf {
		plan.NoFederate = true
		return plan
	}
	cwd, err := os.Getwd()
	if err != nil {
		return plan
	}
	root, err := index.FindGitRoot(cwd)
	if err != nil {
		return plan
	}
	commonDir, err := index.RepoCommonDir(root)
	if err != nil || commonDir == "" {
		return plan
	}
	entries, err := index.EnumerateWorktrees(commonDir)
	if err != nil || len(entries) <= 1 {
		return plan
	}
	// Resolve current root canonically for IsCurrent comparison.
	currentRoot := root
	if abs, err := filepath.Abs(root); err == nil {
		currentRoot = abs
	}
	if resolved, err := filepath.EvalSymlinks(currentRoot); err == nil {
		currentRoot = resolved
	}
	federated := make([]DBEntry, 0, len(entries))
	skipped := 0
	for _, e := range entries {
		if e.IsBare {
			continue
		}
		dbPath, err := index.RepoDBPath(e.Path)
		if err != nil {
			continue
		}
		isCurrent := pathsEqual(e.Path, currentRoot)
		// Skip un-indexed siblings silently — they're not in our DB pool yet.
		// We do NOT auto-index them (hard regression guarantee #3).
		if !isCurrent {
			if _, statErr := os.Stat(dbPath); statErr != nil {
				skipped++
				continue
			}
		}
		federated = append(federated, DBEntry{
			Path:      dbPath,
			Root:      e.Path,
			Branch:    e.Branch,
			IsCurrent: isCurrent,
		})
		if len(federated) >= federationCap {
			fmt.Fprintf(os.Stderr, "cymbal: capping worktree federation at %d (%d worktrees found). Pass --no-federate to disable.\n", federationCap, len(entries))
			break
		}
	}
	if skipped > 0 {
		fmt.Fprintf(os.Stderr, "cymbal: skipped %d unindexed sibling worktree(s); run `cymbal index .` inside each to include them.\n", skipped)
	}
	// Put the current entry first so its hits sort to the top of result
	// listings. Add a defensive synthetic entry if porcelain didn't list it
	// (shouldn't happen for non-bare repos, but cheap to handle).
	ordered := make([]DBEntry, 0, len(federated))
	var hasCurrent bool
	for _, fe := range federated {
		if fe.IsCurrent {
			ordered = append(ordered, fe)
			hasCurrent = true
			break
		}
	}
	if !hasCurrent {
		ordered = append(ordered, DBEntry{Path: primary, Root: currentRoot, IsCurrent: true})
	}
	for _, fe := range federated {
		if !fe.IsCurrent {
			ordered = append(ordered, fe)
		}
	}
	plan.Federated = ordered
	return plan
}

// pathsEqual canonicalizes both paths before comparing. Used to mark the
// current entry in a worktree federation when paths may differ by symlinks
// or trailing slashes.
func pathsEqual(a, b string) bool {
	canon := func(p string) string {
		if abs, err := filepath.Abs(p); err == nil {
			p = abs
		}
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		return filepath.Clean(p)
	}
	return canon(a) == canon(b)
}

// getDBPath returns the database path. Priority: --db flag > CYMBAL_DB env > auto per-repo.
func getDBPath(cmd *cobra.Command) string {
	if p, _ := cmd.Flags().GetString("db"); p != "" {
		return p
	}
	if p := os.Getenv("CYMBAL_DB"); p != "" {
		return p
	}
	cwd, err := os.Getwd()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: cannot determine working directory: %v\n", err)
		return fallbackDBPath()
	}
	root, err := index.FindGitRoot(cwd)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: not inside a git repository — results may be empty.\n")
		fmt.Fprintf(os.Stderr, "  Run 'cymbal index <path>' to index a specific directory.\n")
		return fallbackDBPath()
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return fallbackDBPath()
	}
	dbPath, err := index.RepoDBPath(abs)
	if err != nil {
		return fallbackDBPath()
	}
	return dbPath
}

func fallbackDBPath() string {
	dbPath, err := index.RepoDBPath("_fallback")
	if err != nil {
		// Last resort: temp directory, never a relative path in the project.
		return filepath.Join(os.TempDir(), "cymbal", "cymbal.db")
	}
	return dbPath
}

func getJSONFlag(cmd *cobra.Command) bool {
	v, _ := cmd.Flags().GetBool("json")
	return v
}

// ensureFresh verifies the index against the current working tree before queries.
// A refresh failure is an operational error, not a successful unchanged index.
func ensureFresh(dbPath string) error {
	_, err := index.EnsureFreshWithError(dbPath)
	if err != nil {
		return fmt.Errorf("refreshing index: %w", err)
	}
	return nil
}
