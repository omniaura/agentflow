package gonames

import "strings"

var replacerPackageName = strings.NewReplacer(" ", "", "-", "", "_", "")

// CleanPackageName removes all spaces, hyphens, and underscores from a string and converts it to lowercase.
func CleanPackageName(name string) string {
	return strings.ToLower(replacerPackageName.Replace(name))
}
