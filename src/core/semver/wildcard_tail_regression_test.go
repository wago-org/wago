package semver

import "testing"

func TestReviewRejectInvalidWildcardTail(t *testing.T) {
	for _, input := range []string{"1.x.garbage", "*.garbage", "1.x."} {
		if c, err := ParseConstraint(input); err == nil {
			t.Errorf("invalid constraint %q accepted; matches 1.9.0 = %v", input, c.Check(MustParse("1.9.0")))
		}
	}
}

func TestValidWildcardTails(t *testing.T) {
	for _, input := range []string{"1.x.x", "*.X", "1.*.2"} {
		c, err := ParseConstraint(input)
		if err != nil {
			t.Fatal(err)
		}
		if !c.Check(MustParse("1.9.0")) {
			t.Errorf("%q does not match 1.9.0", input)
		}
	}
}
