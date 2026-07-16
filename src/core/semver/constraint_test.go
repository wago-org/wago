package semver

import "testing"

func TestSatisfies(t *testing.T) {
	cases := []struct {
		version, constraint string
		want                bool
	}{
		// any
		{"1.2.3", "", true},
		{"1.2.3", "*", true},
		{"1.2.3", "x", true},
		// exact / comparators
		{"1.2.3", "1.2.3", true},
		{"1.2.4", "1.2.3", false},
		{"1.2.3", "=1.2.3", true},
		{"1.2.3", ">=1.2.3", true},
		{"1.2.2", ">=1.2.3", false},
		{"1.2.3", ">1.2.3", false},
		{"1.2.4", ">1.2.3", true},
		{"1.2.3", "<=1.2.3", true},
		{"1.2.3", "<1.2.3", false},
		// AND
		{"1.5.0", ">=1.2.3 <2.0.0", true},
		{"2.0.0", ">=1.2.3 <2.0.0", false},
		{"1.2.2", ">=1.2.3 <2.0.0", false},
		// operator with spaces
		{"1.5.0", ">= 1.2.3 < 2.0.0", true},
		// OR
		{"1.0.0", "1.0.0 || 2.0.0", true},
		{"2.0.0", "1.0.0 || 2.0.0", true},
		{"1.5.0", "1.0.0 || 2.0.0", false},
		// x-ranges
		{"1.2.9", "1.2.x", true},
		{"1.3.0", "1.2.x", false},
		{"1.9.9", "1.x", true},
		{"2.0.0", "1.x", false},
		{"1.2.0", "1", true},
		{"2.0.0", "1", false},
		// caret
		{"1.2.3", "^1.2.3", true},
		{"1.9.9", "^1.2.3", true},
		{"2.0.0", "^1.2.3", false},
		{"0.2.3", "^0.2.3", true},
		{"0.3.0", "^0.2.3", false},
		{"0.0.3", "^0.0.3", true},
		{"0.0.4", "^0.0.3", false},
		{"1.5.0", "^1", true},
		{"2.0.0", "^1", false},
		// tilde
		{"1.2.3", "~1.2.3", true},
		{"1.2.9", "~1.2.3", true},
		{"1.3.0", "~1.2.3", false},
		{"1.2.0", "~1.2", true},
		{"1.3.0", "~1.2", false},
		{"1.9.0", "~1", true},
		{"2.0.0", "~1", false},
		// hyphen ranges
		{"1.2.3", "1.2.3 - 2.3.4", true},
		{"2.3.4", "1.2.3 - 2.3.4", true},
		{"2.3.5", "1.2.3 - 2.3.4", false},
		{"1.2.2", "1.2.3 - 2.3.4", false},
		{"2.3.0", "1.2.3 - 2.3", true}, // partial upper: <=2.3.x
		{"2.4.0", "1.2.3 - 2.3", false},
		// >= / < partial completion
		{"1.3.0", ">1.2", true}, // >1.2 => >=1.3.0
		{"1.2.9", ">1.2", false},
		{"1.2.9", "<=1.2", true}, // <=1.2 => <1.3.0
		{"1.3.0", "<=1.2", false},
		// pre-release gating
		{"1.2.3-beta", ">=1.2.3", false}, // pre not admitted by a non-pre range
		{"2.0.0-beta", "*", false},       // wildcard excludes pre-releases
		{"1.2.3-beta.2", ">=1.2.3-beta.1 <1.3.0", true},
		{"1.2.4-beta", ">=1.2.3-alpha <1.3.0", false}, // different [M.m.p] than the pre bound
		{"1.2.3", ">=1.2.3-alpha", true},              // a release satisfies a pre-anchored range
	}
	for _, c := range cases {
		got, err := Satisfies(c.version, c.constraint)
		if err != nil {
			t.Errorf("Satisfies(%q, %q): %v", c.version, c.constraint, err)
			continue
		}
		if got != c.want {
			t.Errorf("Satisfies(%q, %q) = %v, want %v", c.version, c.constraint, got, c.want)
		}
	}
}

func TestParseConstraintInvalid(t *testing.T) {
	for _, in := range []string{"1.2.3.4", ">=", "^1.2.3.4", ">=abc", "1.2.3 - - 2.0.0"} {
		if _, err := ParseConstraint(in); err == nil {
			t.Errorf("ParseConstraint(%q) = nil error, want error", in)
		}
	}
}

func TestConstraintEdgeForms(t *testing.T) {
	for _, tc := range []struct {
		version, constraint string
		want                bool
	}{
		{"0.0.0", ">*", false},
		{"0.0.0", "<=*", true},
		{"1.2.3", ">=1", true},
		{"0.9.9", ">=1", false},
		{"1.2.3", "<2", true},
		{"2.0.0", "<2", false},
		{"0.9.9", "^0", true},
		{"1.0.0", "^0", false},
		{"0.0.9", "^0.0", true},
		{"0.1.0", "^0.0", false},
		{"0.0.3", "^0.0.3", true},
		{"0.0.4", "^0.0.3", false},
		{"1.2.3", "~*", true},
		{"99.0.0", "* - *", true},
		{"1.5.0", "* - 1", true},
		{"2.0.0", "* - 1", false},
	} {
		got, err := Satisfies(tc.version, tc.constraint)
		if err != nil || got != tc.want {
			t.Errorf("Satisfies(%q, %q) = %v, %v; want %v, nil", tc.version, tc.constraint, got, err, tc.want)
		}
	}
	if got := MustParse("1.2.3").String(); got != "1.2.3" {
		t.Fatalf("Constraint version String = %q", got)
	}
	if got, err := ParseConstraint("  >=1.0.0  "); err != nil || got.String() != ">=1.0.0" {
		t.Fatalf("Constraint String = %q, %v", got.String(), err)
	}
	if _, err := Satisfies("bad", "*"); err == nil {
		t.Fatal("invalid version accepted")
	}
	if _, err := Satisfies("1.0.0", "bad"); err == nil {
		t.Fatal("invalid constraint accepted")
	}
}

func TestConstraintPartialAndComparatorBoundaries(t *testing.T) {
	for _, tc := range []struct {
		version, constraint string
		want                bool
	}{
		{"1.2.3", "=1.2.3+build.7", true},
		{"1.2.3", ">=1.2.3", true},
		{"1.2.3", "<1.2.4", true},
		{"1.2.3", ">1.2.2", true},
		{"1.2.3", "<=1.2.3", true},
		{"1.2.3-alpha", "=1.2.3-alpha", true},
	} {
		got, err := Satisfies(tc.version, tc.constraint)
		if err != nil || got != tc.want {
			t.Fatalf("Satisfies(%q, %q) = %v, %v", tc.version, tc.constraint, got, err)
		}
	}
	for _, invalid := range []string{"1..2", "1.2.x-beta", "1.2.3.4", "1.2.3-"} {
		if _, err := ParseConstraint(invalid); err == nil {
			t.Fatalf("invalid partial %q accepted", invalid)
		}
	}
}

func TestRangeExpansionHelpers(t *testing.T) {
	for _, p := range []partial{{}, {major: 1, n: 1}, {major: 1, minor: 2, n: 2}, {major: 1, minor: 2, patch: 3, n: 3}} {
		if len(eqRange(p)) == 0 || len(caretRange(p)) == 0 || len(tildeRange(p)) == 0 {
			t.Fatalf("range expansion empty for %#v", p)
		}
		for _, op := range []string{">=", "<", ">", "<="} {
			if len(compRange(op, p)) == 0 {
				t.Fatalf("%s expansion empty for %#v", op, p)
			}
		}
	}
	if compRange("!", partial{}) != nil {
		t.Fatal("unknown comparator expanded")
	}
	if _, err := hyphenRange("bad", "1"); err == nil {
		t.Fatal("invalid hyphen lower endpoint accepted")
	}
	if _, err := hyphenRange("1", "bad"); err == nil {
		t.Fatal("invalid hyphen upper endpoint accepted")
	}
	if got := tokenizeRange("  >=   1.2.3  -  "); len(got) != 2 || got[0] != ">=1.2.3" || got[1] != "-" {
		t.Fatalf("tokenize hyphen range = %#v", got)
	}
	if got := tokenizeRange(">=   "); len(got) != 1 || got[0] != ">=" {
		t.Fatalf("tokenize trailing operator = %#v", got)
	}
}
