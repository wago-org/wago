package semver

import "testing"

func TestParseValid(t *testing.T) {
	cases := []struct {
		in            string
		maj, min, pat uint64
		pre           string
		build         string
	}{
		{"0.1.0", 0, 1, 0, "", ""},
		{"v1.2.3", 1, 2, 3, "", ""},
		{"1.2.3-alpha.1", 1, 2, 3, "alpha.1", ""},
		{"1.0.0-0.3.7", 1, 0, 0, "0.3.7", ""},
		{"1.0.0+build.5", 1, 0, 0, "", "build.5"},
		{"1.0.0-beta+exp.sha.5114f85", 1, 0, 0, "beta", "exp.sha.5114f85"},
		{"10.20.30", 10, 20, 30, "", ""},
	}
	for _, c := range cases {
		v, err := Parse(c.in)
		if err != nil {
			t.Errorf("Parse(%q): %v", c.in, err)
			continue
		}
		if v.Major != c.maj || v.Minor != c.min || v.Patch != c.pat {
			t.Errorf("Parse(%q) = %d.%d.%d", c.in, v.Major, v.Minor, v.Patch)
		}
		if got := join(v.Pre); got != c.pre {
			t.Errorf("Parse(%q) pre = %q, want %q", c.in, got, c.pre)
		}
		if got := join(v.Build); got != c.build {
			t.Errorf("Parse(%q) build = %q, want %q", c.in, got, c.build)
		}
	}
}

func TestParseInvalid(t *testing.T) {
	for _, in := range []string{"", "1", "1.2", "1.2.3.4", "01.2.3", "1.02.3", "1.2.x", "1.2.3-", "1.2.3-01", "a.b.c", "1.2.-1"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) = nil error, want error", in)
		}
	}
}

func TestCompare(t *testing.T) {
	// Ordered ascending; each precedes the next (semver 2.0.0 §11 example).
	order := []string{
		"1.0.0-alpha", "1.0.0-alpha.1", "1.0.0-alpha.beta", "1.0.0-beta",
		"1.0.0-beta.2", "1.0.0-beta.11", "1.0.0-rc.1", "1.0.0",
		"1.0.1", "1.1.0", "2.0.0",
	}
	for i := 0; i < len(order); i++ {
		for j := 0; j < len(order); j++ {
			a, b := MustParse(order[i]), MustParse(order[j])
			want := sign(i - j)
			if got := a.Compare(b); got != want {
				t.Errorf("Compare(%q,%q) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	// Build metadata is ignored in precedence.
	if MustParse("1.0.0+a").Compare(MustParse("1.0.0+b")) != 0 {
		t.Error("build metadata affected precedence")
	}
}

func TestVersionFormattingAndParserEdges(t *testing.T) {
	if got := MustParse("v1.2.3-alpha+build.7").String(); got != "1.2.3-alpha+build.7" {
		t.Fatalf("String = %q", got)
	}
	if got := MustParse("V1.2.3").String(); got != "1.2.3" {
		t.Fatalf("uppercase v = %q", got)
	}
	for _, in := range []string{"1.2.3+", "1.2.3+a..b", "1.2.3+a_", "1.2.3-", "1.2.3-a..b", "1.2.3-a_"} {
		if _, err := Parse(in); err == nil {
			t.Errorf("Parse(%q) accepted malformed identifier", in)
		}
	}
	defer func() {
		if recover() == nil {
			t.Fatal("MustParse did not panic")
		}
	}()
	_ = MustParse("not-a-version")
}

func join(ids []string) string {
	out := ""
	for i, s := range ids {
		if i > 0 {
			out += "."
		}
		out += s
	}
	return out
}

func sign(n int) int {
	switch {
	case n < 0:
		return -1
	case n > 0:
		return 1
	default:
		return 0
	}
}
