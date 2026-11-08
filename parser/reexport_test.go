package parser

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/1broseidon/cymbal/lang"
	"github.com/1broseidon/cymbal/symbols"
)

func TestReExportImports(t *testing.T) {
	for _, ext := range []string{"js", "jsx", "mjs", "cjs", "ts", "mts", "cts", "tsx"} {
		t.Run(ext, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "barrel."+ext)
			src := `import named from './existing';
import './side-effect';
export { item, other as alias } from './named';
export * from "./star";
export * as namespace from './namespace';
export { default as Widget } from './default';
const local = './not-an-import';
export { local };
export default './also-not-an-import';
export const value = './not-a-source';
const dynamic = import('./dynamic');
const commonjs = require('./commonjs');
`
			if err := os.WriteFile(path, []byte(src), 0o600); err != nil {
				t.Fatal(err)
			}
			result, err := ParseFile(path, lang.Default.ForFile(path).Name)
			if err != nil {
				t.Fatal(err)
			}
			language := "javascript"
			if ext == "ts" || ext == "mts" || ext == "cts" {
				language = "typescript"
			}
			if ext == "tsx" {
				language = "tsx"
			}
			want := []symbols.Import{}
			for _, raw := range []string{"./existing", "./side-effect", "./named", "./star", "./namespace", "./default"} {
				want = append(want, symbols.Import{RawPath: raw, Language: language})
			}
			if !reflect.DeepEqual(result.Imports, want) {
				t.Fatalf("imports = %+v, want %+v", result.Imports, want)
			}
		})
	}
}

func TestReExportTypeImports(t *testing.T) {
	for _, language := range []string{"typescript", "tsx"} {
		t.Run(language, func(t *testing.T) {
			result, err := ParseBytes([]byte(`export type { Shape as PublicShape } from './types';
export { type Other } from './other';
type Local = string;
export type { Local };
`), "types.ts", language)
			if err != nil {
				t.Fatal(err)
			}
			want := []symbols.Import{{RawPath: "./types", Language: language}, {RawPath: "./other", Language: language}}
			if !reflect.DeepEqual(result.Imports, want) {
				t.Fatalf("imports = %+v, want %+v", result.Imports, want)
			}
		})
	}
}
