package parser

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"

	"github.com/1broseidon/cymbal/symbols"
)

// bodyHashBytes is how much of the SHA-256 digest a body hash keeps: 64 bits
// is ample for telling whether a symbol changed, and keeps JSON output and the
// index small.
const bodyHashBytes = 8

// setBodyHashes sets BodyHash on each symbol from the source lines it spans.
func setBodyHashes(src []byte, syms []symbols.Symbol) {
	if len(syms) == 0 {
		return
	}
	lines := bytes.Split(src, []byte("\n"))
	for i := range syms {
		syms[i].BodyHash = bodyHash(lines, syms[i].StartLine, syms[i].EndLine)
	}
}

// bodyHash hashes lines start..end (1-based, inclusive) with line endings and
// trailing whitespace removed, so converting CRLF to LF or stripping trailing
// spaces leaves the hash unchanged. Whole lines are hashed, including any
// modifiers before the start column, so changing `public` to `private`
// changes the hash.
func bodyHash(lines [][]byte, start, end int) string {
	start = max(start, 1)
	end = min(end, len(lines))
	h := sha256.New()
	for n := start; n <= end; n++ {
		if n > start {
			h.Write([]byte{'\n'})
		}
		h.Write(bytes.TrimRight(lines[n-1], " \t\r\f\v"))
	}
	return hex.EncodeToString(h.Sum(nil)[:bodyHashBytes])
}
