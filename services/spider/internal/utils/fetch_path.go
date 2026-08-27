package utils

import (
	"net/url"
	"strings"
	"unicode/utf8"
)

// IsUnambiguousFetchPath reports whether parsed has one stable path
// interpretation across URL policy matching and the outbound HTTP request.
// Encoded percent signs are rejected because another decoding pass could turn
// them into a second escape sequence at an origin or intermediary.
func IsUnambiguousFetchPath(parsed *url.URL) bool {
	if parsed == nil || !utf8.ValidString(parsed.Path) || strings.ContainsRune(parsed.Path, '\\') || strings.Contains(parsed.Path, "//") {
		return false
	}
	escaped := parsed.EscapedPath()
	if containsEncodedPercent(escaped) || escaped != (&url.URL{Path: parsed.Path}).EscapedPath() {
		return false
	}
	for _, segment := range strings.Split(parsed.Path, "/") {
		if segment == "." || segment == ".." {
			return false
		}
	}
	return true
}

func containsEncodedPercent(path string) bool {
	for index := 0; index+2 < len(path); index++ {
		if path[index] != '%' || path[index+1] != '2' {
			continue
		}
		if path[index+2] == '5' {
			return true
		}
	}
	return false
}
