package sqlcgo

import (
	"bytes"
	"regexp"
)

// QueryDecl is a query declared in a sqlc query file.
type QueryDecl struct {
	Name, Kind string
	// Offset is the byte offset of the line that names the query.
	Offset int
}

// queryNameRE matches the comment that starts a sqlc query, in either of
// the forms sqlc reads: -- name: ListNotes :many, or /* name: ... */.
var queryNameRE = regexp.MustCompile(`^\s*(?:--|/\*)\s*name:\s*([A-Za-z_][A-Za-z0-9_]*)\s+:([a-z]+)`)

// QueryDecls returns the queries a sqlc query file declares, in order.
func QueryDecls(src []byte) []QueryDecl {
	var qs []QueryDecl
	for off := 0; off < len(src); {
		end := bytes.IndexByte(src[off:], '\n')
		if end < 0 {
			end = len(src) - off
		}
		if m := queryNameRE.FindSubmatch(src[off : off+end]); m != nil {
			qs = append(qs, QueryDecl{Name: string(m[1]), Kind: string(m[2]), Offset: off})
		}
		off += end + 1
	}
	return qs
}
