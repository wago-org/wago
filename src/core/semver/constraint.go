package semver

import (
	"fmt"
	"strings"
)

// comparator is one primitive bound: op is one of ">", ">=", "<", "<=", "=".
type comparator struct {
	op string
	v  Version
}

// Constraint is a version range: a disjunction (||) of conjunctions (AND) of
// comparators, matching npm/node-semver semantics.
type Constraint struct {
	or  [][]comparator
	raw string
}

// Satisfies reports whether version meets constraint. It is the common entry
// point; both strings are parsed and an error is returned if either is malformed.
func Satisfies(version, constraint string) (bool, error) {
	v, err := Parse(version)
	if err != nil {
		return false, err
	}
	c, err := ParseConstraint(constraint)
	if err != nil {
		return false, err
	}
	return c.Check(v), nil
}

// ParseConstraint parses a range string. Supported forms (composable with spaces
// for AND and "||" for OR):
//
//	exact/x-range   1.2.3 · 1.2 · 1.x · 1 · * · "" (any)
//	comparators     >=1.2.3 · >1.2 · <=2 · <2.0.0 · =1.2.3
//	caret           ^1.2.3  (>=1.2.3 <2.0.0; 0.x-aware)
//	tilde           ~1.2.3  (>=1.2.3 <1.3.0)
//	hyphen          1.2.3 - 2.3.4  (>=1.2.3 <=2.3.4)
func ParseConstraint(s string) (Constraint, error) {
	c := Constraint{raw: strings.TrimSpace(s)}
	for _, part := range strings.Split(s, "||") {
		group, err := parseRange(strings.TrimSpace(part))
		if err != nil {
			return Constraint{}, err
		}
		c.or = append(c.or, group)
	}
	return c, nil
}

// String returns the original range text.
func (c Constraint) String() string { return c.raw }

// Check reports whether v satisfies the constraint.
func (c Constraint) Check(v Version) bool {
	for _, group := range c.or {
		if checkGroup(group, v) {
			return true
		}
	}
	return false
}

// checkGroup reports whether v satisfies every comparator in an AND-group. A
// pre-release version additionally must share [major,minor,patch] with at least
// one comparator that itself carries a pre-release — so "*" or ">=1.0.0" do not
// silently admit "2.0.0-beta" (the node-semver pre-release rule).
func checkGroup(group []comparator, v Version) bool {
	for _, cmp := range group {
		if !cmp.test(v) {
			return false
		}
	}
	if len(v.Pre) > 0 {
		allowed := false
		for _, cmp := range group {
			if len(cmp.v.Pre) > 0 && cmp.v.Major == v.Major && cmp.v.Minor == v.Minor && cmp.v.Patch == v.Patch {
				allowed = true
				break
			}
		}
		if !allowed {
			return false
		}
	}
	return true
}

func (cmp comparator) test(v Version) bool {
	c := v.Compare(cmp.v)
	switch cmp.op {
	case ">":
		return c > 0
	case ">=":
		return c >= 0
	case "<":
		return c < 0
	case "<=":
		return c <= 0
	default: // "="
		return c == 0
	}
}

// parseRange turns one AND-group string (no "||") into comparators.
func parseRange(s string) ([]comparator, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" || s == "x" || s == "X" {
		return anyRange(), nil
	}
	toks := tokenizeRange(s)
	if len(toks) == 3 && toks[1] == "-" {
		return hyphenRange(toks[0], toks[2])
	}
	var out []comparator
	for _, t := range toks {
		if t == "-" || t == "" {
			return nil, fmt.Errorf("semver: malformed range %q", s)
		}
		cs, err := expandToken(t)
		if err != nil {
			return nil, err
		}
		out = append(out, cs...)
	}
	return out, nil
}

// tokenizeRange splits a group into tokens, joining an operator to the version
// that follows it (even across spaces: ">= 1.2.3") and emitting a lone "-" (the
// hyphen-range separator) as its own token.
func tokenizeRange(s string) []string {
	var toks []string
	for i := 0; i < len(s); {
		for i < len(s) && s[i] == ' ' {
			i++
		}
		if i >= len(s) {
			break
		}
		if s[i] == '-' {
			toks = append(toks, "-")
			i++
			continue
		}
		opStart := i
		for i < len(s) && strings.IndexByte("<>=~^", s[i]) >= 0 {
			i++
		}
		op := s[opStart:i]
		for i < len(s) && s[i] == ' ' {
			i++
		}
		vStart := i
		for i < len(s) && s[i] != ' ' {
			i++
		}
		toks = append(toks, op+s[vStart:i])
	}
	return toks
}

// expandToken turns one token (with its optional operator) into comparators.
func expandToken(t string) ([]comparator, error) {
	op := ""
	switch {
	case strings.HasPrefix(t, ">="):
		op, t = ">=", t[2:]
	case strings.HasPrefix(t, "<="):
		op, t = "<=", t[2:]
	case strings.HasPrefix(t, ">"):
		op, t = ">", t[1:]
	case strings.HasPrefix(t, "<"):
		op, t = "<", t[1:]
	case strings.HasPrefix(t, "="):
		op, t = "=", t[1:]
	case strings.HasPrefix(t, "^"):
		op, t = "^", t[1:]
	case strings.HasPrefix(t, "~"):
		op, t = "~", t[1:]
	}
	if op != "" && strings.TrimSpace(t) == "" {
		return nil, fmt.Errorf("semver: operator %q without a version", op)
	}
	p, err := parsePartial(t)
	if err != nil {
		return nil, err
	}
	switch op {
	case "^":
		return caretRange(p), nil
	case "~":
		return tildeRange(p), nil
	case ">", ">=", "<", "<=":
		return compRange(op, p), nil
	default: // "" or "="
		return eqRange(p), nil
	}
}

// partial is a possibly-incomplete version: n is how many of major.minor.patch
// were given concretely (0 = wildcard/any, 3 = full).
type partial struct {
	major, minor, patch uint64
	pre                 []string
	n                   int
}

func (p partial) version() Version {
	return Version{Major: p.major, Minor: p.minor, Patch: p.patch, Pre: p.pre}
}

func parsePartial(s string) (partial, error) {
	s = strings.TrimSpace(s)
	if s == "" || s == "*" || s == "x" || s == "X" {
		return partial{n: 0}, nil
	}
	main := s
	if b := strings.IndexByte(main, '+'); b >= 0 { // build metadata is ignored in ranges
		main = main[:b]
	}
	var pre []string
	if d := strings.IndexByte(main, '-'); d >= 0 {
		var err error
		if pre, err = parseIdentifiers(main[d+1:], true); err != nil {
			return partial{}, err
		}
		main = main[:d]
	}
	parts := strings.Split(main, ".")
	if len(parts) > 3 {
		return partial{}, fmt.Errorf("semver: too many components in %q", s)
	}
	var out partial
	for idx, part := range parts {
		if part == "" {
			return partial{}, fmt.Errorf("semver: empty component in %q", s)
		}
		if part == "*" || part == "x" || part == "X" {
			break // wildcard truncates; n stays at idx
		}
		n, err := parseNumeric(part)
		if err != nil {
			return partial{}, err
		}
		switch idx {
		case 0:
			out.major = n
		case 1:
			out.minor = n
		case 2:
			out.patch = n
		}
		out.n = idx + 1
	}
	if len(pre) > 0 {
		if out.n != 3 {
			return partial{}, fmt.Errorf("semver: pre-release requires a full version in %q", s)
		}
		out.pre = pre
	}
	return out, nil
}

func anyRange() []comparator { return []comparator{{">=", Version{}}} }

func ver(maj, min, pat uint64) Version { return Version{Major: maj, Minor: min, Patch: pat} }

// eqRange handles a bare version or "=": full is exact; a partial is an x-range.
func eqRange(p partial) []comparator {
	switch p.n {
	case 0:
		return anyRange()
	case 1:
		return []comparator{{">=", ver(p.major, 0, 0)}, {"<", ver(p.major+1, 0, 0)}}
	case 2:
		return []comparator{{">=", ver(p.major, p.minor, 0)}, {"<", ver(p.major, p.minor+1, 0)}}
	default:
		return []comparator{{"=", p.version()}}
	}
}

// caretRange: compatible with the left-most non-zero component.
func caretRange(p partial) []comparator {
	if p.n == 0 {
		return anyRange()
	}
	var upper Version
	switch {
	case p.major != 0:
		upper = ver(p.major+1, 0, 0)
	case p.n == 1: // ^0
		upper = ver(1, 0, 0)
	case p.minor != 0:
		upper = ver(0, p.minor+1, 0)
	case p.n == 2: // ^0.0
		upper = ver(0, 1, 0)
	default: // ^0.0.x
		upper = ver(0, 0, p.patch+1)
	}
	return []comparator{{">=", p.version()}, {"<", upper}}
}

// tildeRange: patch-level changes if minor is given, else minor-level.
func tildeRange(p partial) []comparator {
	if p.n == 0 {
		return anyRange()
	}
	var upper Version
	if p.n >= 2 {
		upper = ver(p.major, p.minor+1, 0)
	} else {
		upper = ver(p.major+1, 0, 0)
	}
	return []comparator{{">=", p.version()}, {"<", upper}}
}

// compRange completes a partial version for an explicit comparator per
// node-semver's rules (e.g. ">1.2" -> ">=1.3.0", "<=1.2" -> "<1.3.0").
func compRange(op string, p partial) []comparator {
	switch op {
	case ">=":
		return []comparator{{">=", p.version()}}
	case "<":
		return []comparator{{"<", p.version()}}
	case ">":
		switch p.n {
		case 0:
			return []comparator{{"<", ver(0, 0, 0)}} // >* : matches nothing
		case 3:
			return []comparator{{">", p.version()}}
		case 2:
			return []comparator{{">=", ver(p.major, p.minor+1, 0)}}
		default:
			return []comparator{{">=", ver(p.major+1, 0, 0)}}
		}
	case "<=":
		switch p.n {
		case 0:
			return anyRange()
		case 3:
			return []comparator{{"<=", p.version()}}
		case 2:
			return []comparator{{"<", ver(p.major, p.minor+1, 0)}}
		default:
			return []comparator{{"<", ver(p.major+1, 0, 0)}}
		}
	}
	return nil
}

// hyphenRange builds ">=A <=B" with partial-version completion of both ends.
func hyphenRange(aTok, bTok string) ([]comparator, error) {
	a, err := parsePartial(aTok)
	if err != nil {
		return nil, err
	}
	b, err := parsePartial(bTok)
	if err != nil {
		return nil, err
	}
	var out []comparator
	if a.n == 0 {
		out = append(out, comparator{">=", Version{}})
	} else {
		out = append(out, comparator{">=", a.version()})
	}
	switch b.n {
	case 0: // no upper bound
	case 3:
		out = append(out, comparator{"<=", b.version()})
	case 2:
		out = append(out, comparator{"<", ver(b.major, b.minor+1, 0)})
	default: // n == 1
		out = append(out, comparator{"<", ver(b.major+1, 0, 0)})
	}
	return out, nil
}
