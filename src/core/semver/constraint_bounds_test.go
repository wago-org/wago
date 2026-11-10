package semver

import "testing"

func TestGeneratedUpperBoundsExcludePrereleases(t *testing.T) {
	for _, tc := range []struct{ constraint, version string }{
		{"^1.2.3 >=2.0.0-alpha", "2.0.0-alpha"},
		{"^1.2.3 >=2.0.0-0", "2.0.0-0"},
		{"^1.2.3 >=2.0.0-alpha || 3.x", "2.0.0-alpha"},
		{"^0.2.3 >=0.3.0-alpha", "0.3.0-alpha"},
		{"^0.0.3 >=0.0.4-alpha", "0.0.4-alpha"},
		{"~1.2.3 >=1.3.0-alpha", "1.3.0-alpha"},
		{"~1 >=2.0.0-alpha", "2.0.0-alpha"},
		{"1.x >=2.0.0-alpha", "2.0.0-alpha"},
		{"1.2.x >=1.3.0-alpha", "1.3.0-alpha"},
		{"<=1 >=2.0.0-alpha", "2.0.0-alpha"},
		{"<=1.2 >=1.3.0-alpha", "1.3.0-alpha"},
		{"<2 >=2.0.0-alpha", "2.0.0-alpha"},
		{">* >=0.0.0-alpha", "0.0.0-alpha"},
	} {
		t.Run(tc.constraint, func(t *testing.T) {
			got, err := Satisfies(tc.version, tc.constraint)
			if err != nil || got {
				t.Fatalf("Satisfies(%q, %q) = %v, %v; want false", tc.version, tc.constraint, got, err)
			}
		})
	}
}

func TestGeneratedUpperBoundPreservesExplicitComparators(t *testing.T) {
	for _, constraint := range []string{"<2.0.0 >=2.0.0-alpha", "<=2.0.0 >=2.0.0-alpha", "^2.0.0-alpha", "2.0.0-alpha - 2.0.0"} {
		got, err := Satisfies("2.0.0-alpha", constraint)
		if err != nil || !got {
			t.Fatalf("Satisfies(%q) = %v, %v; want true", constraint, got, err)
		}
	}
}

func TestHyphenPartialUpperBoundary(t *testing.T) {
	for _, input := range []string{"1 - 1", "1 - 1.2"} {
		c, err := ParseConstraint(input)
		if err != nil {
			t.Fatal(err)
		}
		upper := c.or[0][len(c.or[0])-1]
		v := upper.v
		v.Pre = []string{"alpha"}
		if upper.test(v) {
			t.Fatalf("%q upper bound admits its excluded prerelease %s", input, v)
		}
	}
}

func BenchmarkConstraintBounds(b *testing.B) {
	for _, input := range []string{"^1.2.3", "~1.2.3", "1.2.x", "1.2.3 - 2.3", ">=1.2.3 <2.0.0"} {
		b.Run(input+"/parse", func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := ParseConstraint(input); err != nil {
					b.Fatal(err)
				}
			}
		})
		b.Run(input+"/check", func(b *testing.B) {
			c, err := ParseConstraint(input)
			if err != nil {
				b.Fatal(err)
			}
			v, err := Parse("1.2.9")
			if err != nil {
				b.Fatal(err)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if !c.Check(v) {
					b.Fatal("version rejected")
				}
			}
		})
	}
}
