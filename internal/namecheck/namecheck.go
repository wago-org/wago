// Package namecheck checks the ASCII name grammars shared by runtime plugin
// admission and project manifests. It does not normalize or repair input.
package namecheck

import "strings"

func alnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

func lowerAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= '0' && c <= '9'
}

func part(s string, path bool) bool {
	if len(s) == 0 || !alnum(s[0]) || !alnum(s[len(s)-1]) {
		return false
	}
	for i := 1; i < len(s)-1; i++ {
		c := s[i]
		if !alnum(c) && c != '-' && !(path && (c == '.' || c == '_' || c == '~')) {
			return false
		}
	}
	return true
}

// CanonicalPath requires at least two dotted host labels and at least one
// slash-separated path component. Labels and components start and end in an
// ASCII letter or digit. Callers retain their own total-length limits.
func CanonicalPath(s string) bool {
	slash := strings.IndexByte(s, '/')
	if slash <= 0 {
		return false
	}
	start, labels := 0, 0
	for i := 0; i <= slash; i++ {
		if i != slash && s[i] != '.' {
			continue
		}
		if !part(s[start:i], false) {
			return false
		}
		start = i + 1
		labels++
	}
	if labels < 2 {
		return false
	}
	for i := start; i <= len(s); i++ {
		if i != len(s) && s[i] != '/' {
			continue
		}
		if !part(s[start:i], true) {
			return false
		}
		start = i + 1
	}
	return true
}

// Slug accepts lowercase ASCII letters, digits, dots, underscores, and dashes;
// its first byte must be a letter or digit. Callers bound its total length.
func Slug(s string) bool {
	if len(s) == 0 || !lowerAlnum(s[0]) {
		return false
	}
	for i := 1; i < len(s); i++ {
		c := s[i]
		if !lowerAlnum(c) && c != '.' && c != '_' && c != '-' {
			return false
		}
	}
	return true
}

// Platform accepts two nonempty lowercase ASCII alphanumeric components.
func Platform(s string) bool {
	slash := strings.IndexByte(s, '/')
	if slash <= 0 || slash == len(s)-1 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if i != slash && !lowerAlnum(s[i]) {
			return false
		}
	}
	return true
}

// GitHubUser accepts one through 39 ASCII letters, digits, and interior dashes.
func GitHubUser(s string) bool { return len(s) <= 39 && part(s, false) }
