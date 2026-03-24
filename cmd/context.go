package cmd

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/1broseidon/cymbal/internal/index"
	"github.com/spf13/cobra"
)

var contextCmd = &cobra.Command{
	Use:   "context <symbol>",
	Short: "Bundled context: source, type references, callers, and imports",
	Long: `Show bundled context for a symbol: source code, referenced types,
callers, and imports of the defining file.

Examples:
  cymbal context OpenStore
  cymbal context ParseFile --callers 10`,
	Args: cobra.ExactArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		name := args[0]
		dbPath := getDBPath(cmd)
		jsonOut := getJSONFlag(cmd)
		callers, _ := cmd.Flags().GetInt("callers")

		result, err := index.SymbolContext(dbPath, name, callers)
		if err != nil {
			var ambig *index.AmbiguousError
			if errors.As(err, &ambig) {
				if jsonOut {
					return writeJSON(map[string]any{
						"ambiguous": true,
						"matches":   ambig.Matches,
					})
				}
				fmt.Fprintf(os.Stderr, "Multiple matches for '%s' — be more specific:\n", name)
				for _, r := range ambig.Matches {
					fmt.Printf("  %-12s %-40s %s:%d-%d\n", r.Kind, r.Name, r.RelPath, r.StartLine, r.EndLine)
				}
				os.Exit(1)
			}
			return err
		}

		if jsonOut {
			return writeJSON(result)
		}

		sym := result.Symbol

		var content strings.Builder

		// Source section.
		content.WriteString("# Source\n")
		src := strings.TrimRight(result.Source, "\n")
		content.WriteString(src)
		content.WriteByte('\n')

		// Callers section.
		if len(result.Callers) > 0 {
			var refs []refLine
			for _, r := range result.Callers {
				refs = append(refs, refLine{
					relPath: r.RelPath,
					line:    r.Line,
					text:    strings.TrimSpace(readSourceLine(r.File, r.Line)),
				})
			}
			lines, _ := dedupRefLines(refs)
			fmt.Fprintf(&content, "\n# Callers (%d)\n", len(lines))
			for _, l := range lines {
				content.WriteString(l)
				content.WriteByte('\n')
			}
		}

		// File imports section.
		if len(result.FileImports) > 0 {
			fmt.Fprintf(&content, "\n# Imports\n")
			for _, imp := range result.FileImports {
				content.WriteString(imp)
				content.WriteByte('\n')
			}
		}

		frontmatter([]kv{
			{"symbol", sym.Name},
			{"kind", sym.Kind},
			{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
		}, content.String())
		return nil
	},
}

func init() {
	contextCmd.Flags().IntP("callers", "n", 20, "max callers to show")
	rootCmd.AddCommand(contextCmd)
}
