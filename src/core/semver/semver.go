// Package semver implements Semantic Versioning 2.0.0 (https://semver.org):
// version parsing, precedence comparison (including pre-release and build
// metadata rules), and npm-style range constraints (comparators, ^, ~, x-ranges,
// hyphen ranges, AND, and || OR). It has no external dependencies.
package semver

import (
	"fmt"
	"strconv"
	"strings"
)

// Version is a parsed semantic version. Build metadata is retained for String()
// but, per the spec, ignored in precedence comparisons.
type Version struct {
	Major, Minor, Patch uint64
	Pre                 []string // dot-separated pre-release identifiers; empty if none
	Build               []string // dot-separated build-metadata identifiers; empty if none
}

// Parse parses a full "major.minor.patch" version with optional "-prerelease" and
// "+build". A single leading "v"/"V" is tolerated. It rejects leading zeros in
// numeric identifiers and other malformed input, per the semver 2.0.0 grammar.
func Parse(s string) (Version, error) {
	orig := s
	if len(s) > 0 && (s[0] == 'v' || s[0] == 'V') {
		s = s[1:]
	}
	var build []string
	if b := strings.IndexByte(s, '+'); b >= 0 {
		var err error
		if build, err = parseIdentifiers(s[b+1:], false); err != nil {
			return Version{}, fmt.Errorf("semver %q: build: %w", orig, err)
		}
		s = s[:b]
	}
	var pre []string
	if p := strings.IndexByte(s, '-'); p >= 0 {
		var err error
		if pre, err = parseIdentifiers(s[p+1:], true); err != nil {
			return Version{}, fmt.Errorf("semver %q: pre-release: %w", orig, err)
		}
		s = s[:p]
	}
	nums := strings.Split(s, ".")
	if len(nums) != 3 {
		return Version{}, fmt.Errorf("semver %q: need major.minor.patch", orig)
	}
	maj, err := parseNumeric(nums[0])
	if err != nil {
		return Version{}, fmt.Errorf("semver %q: major: %w", orig, err)
	}
	min, err := parseNumeric(nums[1])
	if err != nil {
		return Version{}, fmt.Errorf("semver %q: minor: %w", orig, err)
	}
	pat, err := parseNumeric(nums[2])
	if err != nil {
		return Version{}, fmt.Errorf("semver %q: patch: %w", orig, err)
	}
	return Version{Major: maj, Minor: min, Patch: pat, Pre: pre, Build: build}, nil
}

// MustParse parses s and panics on error. For constants known to be valid.
func MustParse(s string) Version {
	v, err := Parse(s)
	if err != nil {
		panic(err)
	}
	return v
}

// parseNumeric parses a semver numeric identifier: digits only, no leading zero
// (except the single value "0").
func parseNumeric(s string) (uint64, error) {
	if s == "" {
		return 0, fmt.Errorf("empty numeric identifier")
	}
	if len(s) > 1 && s[0] == '0' {
		return 0, fmt.Errorf("leading zero in %q", s)
	}
	n, err := strconv.ParseUint(s, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid numeric identifier %q", s)
	}
	return n, nil
}

// parseIdentifiers splits and validates a dot-separated identifier list.
// Pre-release identifiers additionally forbid leading zeros in all-numeric parts.
func parseIdentifiers(s string, prerelease bool) ([]string, error) {
	if s == "" {
		return nil, fmt.Errorf("empty")
	}
	ids := strings.Split(s, ".")
	for _, id := range ids {
		if id == "" {
			return nil, fmt.Errorf("empty identifier")
		}
		numeric := true
		for i := 0; i < len(id); i++ {
			c := id[i]
			if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c == '-') {
				return nil, fmt.Errorf("invalid character in %q", id)
			}
			if !(c >= '0' && c <= '9') {
				numeric = false
			}
		}
		if prerelease && numeric && len(id) > 1 && id[0] == '0' {
			return nil, fmt.Errorf("leading zero in numeric pre-release %q", id)
		}
	}
	return ids, nil
}

// String renders the canonical string form.
func (v Version) String() string {
	s := strconv.FormatUint(v.Major, 10) + "." + strconv.FormatUint(v.Minor, 10) + "." + strconv.FormatUint(v.Patch, 10)
	if len(v.Pre) > 0 {
		s += "-" + strings.Join(v.Pre, ".")
	}
	if len(v.Build) > 0 {
		s += "+" + strings.Join(v.Build, ".")
	}
	return s
}

// Compare returns -1, 0, or 1 as v precedes, equals, or follows o, per semver
// 2.0.0 §11. Build metadata does not affect precedence.
func (v Version) Compare(o Version) int {
	if c := cmpUint(v.Major, o.Major); c != 0 {
		return c
	}
	if c := cmpUint(v.Minor, o.Minor); c != 0 {
		return c
	}
	if c := cmpUint(v.Patch, o.Patch); c != 0 {
		return c
	}
	return comparePre(v.Pre, o.Pre)
}

// comparePre applies the pre-release precedence rules: a version with a
// pre-release has lower precedence than one without; otherwise identifiers are
// compared left to right (numeric < alphanumeric; numeric numerically; else
// ASCII), and a larger set of identifiers wins if all preceding are equal.
func comparePre(a, b []string) int {
	switch {
	case len(a) == 0 && len(b) == 0:
		return 0
	case len(a) == 0:
		return 1 // a is a release, higher precedence
	case len(b) == 0:
		return -1
	}
	for i := 0; i < len(a) && i < len(b); i++ {
		ai, bi := a[i], b[i]
		an, aNum := numericID(ai)
		bn, bNum := numericID(bi)
		switch {
		case aNum && bNum:
			if c := cmpUint(an, bn); c != 0 {
				return c
			}
		case aNum && !bNum:
			return -1 // numeric identifiers have lower precedence
		case !aNum && bNum:
			return 1
		default:
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
		}
	}
	return cmpInt(len(a), len(b))
}

func numericID(s string) (uint64, bool) {
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return 0, false
		}
	}
	n, err := strconv.ParseUint(s, 10, 64)
	return n, err == nil
}

func cmpUint(a, b uint64) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}

func cmpInt(a, b int) int {
	switch {
	case a < b:
		return -1
	case a > b:
		return 1
	default:
		return 0
	}
}
