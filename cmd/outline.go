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
	Use:   "outline <file> [file2 ...]",
	Short: "Show symbols defined in a file",
	Args:  cobra.MinimumNArgs(1),
	RunE: func(cmd *cobra.Command, args []string) error {
		dbPath := getDBPath(cmd)
		ensureFresh(dbPath)
		jsonOut := getJSONFlag(cmd)
		sigs, _ := cmd.Flags().GetBool("signatures")
		namesOnly, _ := cmd.Flags().GetBool("names")

		if jsonOut && len(args) > 1 {
			return outlineMultiJSON(dbPath, args)
		}

		if namesOnly {
			return outlineNames(dbPath, args)
		}

		multi := len(args) > 1
		for i, target := range args {
			symbols, err := outlineSymbols(dbPath, target)
			if err != nil {
				fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
				continue
			}
			if len(symbols) == 0 {
				emitNoOutline(target)
				continue
			}
			if multi {
				multiSymbolBanner(target, i == 0)
				multiSymbolHeader(target)
			}
			content := outlineContent(symbols, sigs)
			if err := renderJSONOrFrontmatter(
				jsonOut,
				symbols,
				[]kv{
					{"file", target},
					{"symbol_count", fmt.Sprintf("%d", len(symbols))},
				},
				content,
			); err != nil {
				return err
			}
		}
		return nil
	},
}

func outlineSymbols(dbPath, target string) ([]index.SymbolResult, error) {
	filePath, err := filepath.Abs(target)
	if err != nil {
		return nil, err
	}
	return index.FileOutline(dbPath, filePath)
}

func outlineNames(dbPath string, targets []string) error {
	var out strings.Builder
	seen := make(map[string]struct{})
	for _, target := range targets {
		symbols, err := outlineSymbols(dbPath, target)
		if err != nil {
			fmt.Fprintf(os.Stderr, "%s: %v\n", target, err)
			continue
		}
		if len(symbols) == 0 {
			emitNoOutline(target)
			continue
		}
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
	}
	fmt.Print(out.String())
	return nil
}

func outlineMultiJSON(dbPath string, targets []string) error {
	out := make(map[string]any, len(targets))
	for _, target := range targets {
		symbols, err := outlineSymbols(dbPath, target)
		if err != nil {
			out[target] = map[string]any{"error": err.Error()}
			continue
		}
		out[target] = symbols
	}
	return writeJSON(out)
}

func outlineContent(symbols []index.SymbolResult, sigs bool) string {
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
	return content.String()
}

func emitNoOutline(target string) {
	fmt.Fprintf(os.Stderr, "No symbols found. Is the file indexed? Run 'cymbal index %s'\n",
		filepath.Dir(target))
}

func init() {
	outlineCmd.Flags().BoolP("signatures", "s", false, "show full parameter signatures")
	outlineCmd.Flags().Bool("names", false, "emit one symbol name per line (pipe-friendly for --stdin)")
	rootCmd.AddCommand(outlineCmd)
}
