package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var indexCmd = &cobra.Command{
	Use:   "index [path]",
	Short: "Index a directory for symbol discovery",
	Long:  `Index a directory for symbol discovery.`,
	Args:  cobra.MaximumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		path := "."
		if len(args) > 0 {
			path = args[0]
		}

		absPath, err := filepath.Abs(path)
		if err != nil {
			return fmt.Errorf("resolving path: %w", err)
		}

		if _, err := os.Stat(absPath); err != nil {
			return fmt.Errorf("path not found: %s", absPath)
		}

		workers, _ := cmd.Flags().GetInt("workers")
		force, _ := cmd.Flags().GetBool("force")
		excludes, _ := cmd.Flags().GetStringArray("exclude")
		includeGenerated, _ := cmd.Flags().GetBool("include-generated")
		includeLargeFiles, _ := cmd.Flags().GetBool("include-large-files")

		// Use --db flag > CYMBAL_DB env > compute from target path.
		dbPath, _ := cmd.Flags().GetString("db")
		if dbPath == "" {
			if p := os.Getenv("CYMBAL_DB"); p != "" {
				dbPath = p
			} else {
				dbPath, err = index.RepoDBPath(absPath)
				if err != nil {
					return fmt.Errorf("computing db path: %w", err)
				}
			}
		}

		fmt.Fprintf(os.Stderr, "Indexing %s ...\n", absPath)
		start := time.Now()

		stats, err := index.Index(absPath, dbPath, index.Options{
			Workers:           workers,
			Force:             force,
			Exclude:           excludes,
			IncludeGenerated:  includeGenerated,
			IncludeLargeFiles: includeLargeFiles,
		})
		if err != nil {
			return fmt.Errorf("indexing failed: %w", err)
		}

		elapsed := time.Since(start)
		msg := fmt.Sprintf("Done in %s — %d indexed, %d symbols, %d unchanged",
			elapsed.Round(time.Millisecond), stats.FilesIndexed, stats.SymbolsFound, stats.FilesSkipped)
		if stats.StaleRemoved > 0 {
			msg += fmt.Sprintf(", %d stale removed", stats.StaleRemoved)
		}
		if stats.FilesExcluded > 0 {
			msg += fmt.Sprintf(", %d excluded", stats.FilesExcluded)
			if stats.BytesExcluded > 0 {
				msg += fmt.Sprintf(" (%s)", formatBytes(stats.BytesExcluded))
			}
		}
		if stats.ParseErrors > 0 {
			msg += fmt.Sprintf(", %d parse errors", stats.ParseErrors)
		}
		if stats.WriteErrors > 0 {
			msg += fmt.Sprintf(", %d write errors", stats.WriteErrors)
		}
		fmt.Fprintln(os.Stderr, msg)

		return nil
	},
}

func init() {
	indexCmd.Flags().IntP("workers", "w", 0, "number of parallel workers (0 = NumCPU)")
	indexCmd.Flags().BoolP("force", "f", false, "force re-index all files")
	indexCmd.Flags().StringArray("exclude", nil, "exclude files whose path matches this glob during indexing (repeatable)")
	indexCmd.Flags().Bool("include-generated", false, "index generated files that are skipped by default")
	indexCmd.Flags().Bool("include-large-files", false, "index large source files that are skipped by default")
	rootCmd.AddCommand(indexCmd)
}

func formatBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%dB", n)
	}
	div, exp := int64(unit), 0
	for m := n / unit; m >= unit; m /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f%ciB", float64(n)/float64(div), "KMGTPE"[exp])
}
