package cmd

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/1broseidon/cymbal/index"
	"github.com/spf13/cobra"
)

var outlineCmd = &cobra.Command{
	Use:   "outline <file>",
	Short: "Show symbols defined in a file",
	Args:  cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		filePath, err := filepath.Abs(args[0])
		if err != nil {
			return err
		}

		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		sigs, _ := cmd.Flags().GetBool("signatures")
		namesOnly, _ := cmd.Flags().GetBool("names")

		symbols, err := index.FileOutline(dbPath, filePath)
		if err != nil {
			return err
		}

		if len(symbols) == 0 {
			fmt.Fprintf(os.Stderr, "No symbols found. Is the file indexed? Run 'cymbal index %s'\n",
				filepath.Dir(filePath))
			return nil
		}

		relPath := args[0]

		// --names: one symbol name per line, pipe-friendly for --stdin consumers.
		if namesOnly {
			var out strings.Builder
			seen := make(map[string]struct{}, len(symbols))
			for _, s := range symbols {
				if s.Name == "" {
					continue
				}
				if _, ok := seen[s.Name]; ok {
					continue
				}
				seen[s.Name] = struct{}{}
				out.WriteString(s.Name)
				out.WriteByte('\n')
			}
			fmt.Print(out.String())
			return nil
		}

		var content strings.Builder
		for _, s := range symbols {
			indent := strings.Repeat("  ", s.Depth)
			line := fmt.Sprintf("%s%s %s", indent, s.Kind, s.Name)
			if sigs && s.Signature != "" {
				line += s.Signature
			}
			line += fmt.Sprintf(" (L%d-%d)", s.StartLine, s.EndLine)
			content.WriteString(line)
			content.WriteByte('\n')
		}

		return renderJSONOrFrontmatter(
			jsonOut,
			symbols,
			[]kv{
				{"file", relPath},
				{"symbol_count", fmt.Sprintf("%d", len(symbols))},
			},
			content.String(),
		)
	},
}

func init() {
	outlineCmd.Flags().BoolP("signatures", "s", false, "show full parameter signatures")
	outlineCmd.Flags().Bool("names", false, "emit one symbol name per line (pipe-friendly for --stdin)")
	rootCmd.AddCommand(outlineCmd)
}
