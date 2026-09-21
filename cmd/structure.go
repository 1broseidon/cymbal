package cmd

import (
	"fmt"
	"os"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var structureCmd = &cobra.Command{
	Use:   "structure",
	Short: "Structural overview — entry points, hotspots, central packages",
	Long: `Show the structural shape of the indexed codebase. All data is derived
from the existing index — no AI, no guessing, just facts.

Reports:
  - Entry points (main, init, exported handlers)
  - Most referenced symbols (highest call-site count)
  - Most imported files (highest fan-in)
  - Largest packages (most symbols)

Designed to answer "I've never seen this repo — where do I start?"`,
	Args: cobra.NoArgs,
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		if err := ensureFresh(dbPath); err != nil {
			return err
		}
		jsonOut := getJSONFlag(cmd)
		limit, _ := cmd.Flags().GetInt("limit")

		result, err := index.Structure(dbPath, limit)
		if err != nil {
			return err
		}

		if jsonOut {
			return writeJSON(result)
		}

		printStructure(result)
		return nil
	},
}

func init() {
	structureCmd.Flags().IntP("limit", "n", 10, "max items per section")
	rootCmd.AddCommand(structureCmd)
}

func printStructure(r *index.StructureResult) {
	fmt.Fprintf(os.Stderr, "--- %s (%d files, %d symbols) ---\n\n", r.RepoRoot, r.Files, r.Symbols)

	// Entry points
	if len(r.EntryPoints) > 0 {
		fmt.Println("Entry points:")
		for _, s := range r.EntryPoints {
			fmt.Printf("  %-30s %s:%d\n", fmtSym(s.Kind, s.Name), s.RelPath, s.StartLine)
		}
		fmt.Println()
	}

	// Top by refs
	if len(r.TopByRefs) > 0 {
		fmt.Println("Most referenced symbols:")
		for _, s := range r.TopByRefs {
			fmt.Printf("  %-30s (%d refs)  %s:%d\n", fmtSym(s.Kind, s.Name), s.Count, s.RelPath, s.StartLine)
		}
		fmt.Println()
	}

	// Top packages
	if len(r.TopPackages) > 0 {
		fmt.Println("Largest packages:")
		for _, p := range r.TopPackages {
			fmt.Printf("  %-30s %d symbols, %d files\n", p.Path, p.Symbols, p.Files)
		}
		fmt.Println()
	}

	// Top by import fan-in
	if len(r.TopByImportFan) > 0 {
		fmt.Println("Most imported files:")
		for _, f := range r.TopByImportFan {
			fmt.Printf("  %-40s imported by %d files\n", f.RelPath, f.Count)
		}
		fmt.Println()
	}

	// Suggested commands (deduplicated by name)
	if len(r.TopByRefs) > 0 {
		fmt.Println("Try:")
		seen := map[string]bool{}
		for _, s := range r.TopByRefs {
			if seen[s.Name] || len(seen) >= 3 {
				continue
			}
			seen[s.Name] = true
			fmt.Printf("  cymbal investigate %s\n", s.Name)
		}
		if len(r.EntryPoints) > 0 {
			fmt.Printf("  cymbal trace %s\n", r.EntryPoints[0].Name)
		}
	}
}

func fmtSym(kind, name string) string {
	return fmt.Sprintf("%s %s", kind, name)
}
