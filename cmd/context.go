package cmd

import (
	"fmt"
	"strings"

	"github.com/1broseidon/cymbal/index"
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
	Args: cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		callers, _ := cmd.Flags().GetInt("callers")

		if jsonOut && len(args) > 1 {
			out := make(map[string]any, len(args))
			for _, name := range args {
				result, err := index.SymbolContext(dbPath, name, callers)
				if err != nil {
					out[name] = map[string]any{"error": err.Error()}
					continue
				}
				out[name] = result
			}
			return writeJSON(out)
		}

		for i, name := range args {
			if i > 0 {
				fmt.Println()
			}

			result, err := index.SymbolContext(dbPath, name, callers)
			if err != nil {
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

			// Implementors / Implements sections (type-like symbols only).
			if len(result.Implementors) > 0 {
				fmt.Fprintf(&content, "\n# Implementors (%d)\n", len(result.Implementors))
				for _, imp := range result.Implementors {
					nm := imp.Implementer
					if nm == "" {
						nm = "(anonymous)"
					}
					tag := ""
					if !imp.Resolved {
						tag = "  (external)"
					}
					fmt.Fprintf(&content, "  %s  %s:%d%s\n", nm, imp.RelPath, imp.Line, tag)
				}
			}
			if len(result.Implements) > 0 {
				fmt.Fprintf(&content, "\n# Implements (%d)\n", len(result.Implements))
				for _, imp := range result.Implements {
					tag := ""
					if !imp.Resolved {
						tag = "  (external)"
					}
					fmt.Fprintf(&content, "  %s  %s:%d%s\n", imp.Target, imp.RelPath, imp.Line, tag)
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

			meta := []kv{
				{"symbol", sym.Name},
				{"kind", sym.Kind},
				{"file", fmt.Sprintf("%s:%d", sym.RelPath, sym.StartLine)},
			}
			if result.Ambiguous {
				also := make([]string, 0, max(0, len(result.Matches)-1))
				for _, r := range result.Matches[1:] {
					also = append(also, fmt.Sprintf("%s:%d", r.RelPath, r.StartLine))
				}
				meta = append(meta, kv{"matches", fmt.Sprintf("%d (also: %s)", result.MatchCount, strings.Join(also, ", "))})
			}
			frontmatter(meta, content.String())
		}
		return nil
	},
}

func init() {
	contextCmd.Flags().IntP("callers", "n", 20, "max callers to show")
	rootCmd.AddCommand(contextCmd)
}
