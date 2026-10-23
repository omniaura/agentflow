package gonames

import (
	"go/token"
	"strings"
	"unicode"
)

// CleanPackageName turns a directory name into a Go package name: it keeps
// the letters and digits, lowercased, and prefixes "p" when the result would
// not be an identifier (it starts with a digit, is empty, or is a keyword).
func CleanPackageName(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	s := b.String()
	if !token.IsIdentifier(s) {
		s = "p" + s
	}
	return s
}
