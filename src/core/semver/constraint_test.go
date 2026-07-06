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
