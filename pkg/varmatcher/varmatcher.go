package varmatcher

import (
	"log/slog"
	"regexp"
	"slices"
)

var Regex = regexp.MustCompile(`<!([a-zA-Z0-9_]+)>`)

func Vars(s string) []string {
	matches := Regex.FindAllStringSubmatch(s, -1)
	var vars []string
	for _, match := range matches {
		if !slices.Contains(vars, match[1]) {
			slog.Debug("Found variable", "name", match[1])
			vars = append(vars, match[1])
		}
	}
	return vars
}
