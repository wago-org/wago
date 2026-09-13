package semver

import "testing"

func TestConstraintSuccessorOverflow(t *testing.T) {
	for _, input := range []string{
		"18446744073709551615", "1.18446744073709551615", "^18446744073709551615.1.2", "^0.18446744073709551615.2", "^0.0.18446744073709551615",
		"~18446744073709551615", "~1.18446744073709551615.2", ">18446744073709551615", ">1.18446744073709551615", "<=18446744073709551615", "<=1.18446744073709551615", "0 - 18446744073709551615", "0 - 1.18446744073709551615",
	} {
		t.Run(input, func(t *testing.T) {
			if _, err := ParseConstraint(input); err == nil {
				t.Fatal("unrepresentable successor accepted")
			}
		})
	}
}

func TestConstraintSuccessorRepresentable(t *testing.T) {
	for _, tc := range []struct{ constraint, version string }{
		{"18446744073709551614", "18446744073709551614.0.0"},
		{"1.18446744073709551614", "1.18446744073709551614.0"},
		{"^0.0.18446744073709551614", "0.0.18446744073709551614"},
		{"=18446744073709551615.18446744073709551615.18446744073709551615", "18446744073709551615.18446744073709551615.18446744073709551615"},
		{">=18446744073709551615", "18446744073709551615.0.0"},
		{"<18446744073709551615", "1.0.0"},
		{"0 - 18446744073709551615.0.0", "18446744073709551615.0.0"},
		{">1.18446744073709551615.0", "2.0.0"},
		{"~1.2.18446744073709551615", "1.2.18446744073709551615"},
	} {
		t.Run(tc.constraint, func(t *testing.T) {
			got, err := Satisfies(tc.version, tc.constraint)
			if err != nil || !got {
				t.Fatalf("got %v, %v", got, err)
			}
		})
	}
}

func BenchmarkConstraintSuccessor(b *testing.B) {
	for _, input := range []string{"^1.2.3", "~1.2.3", "1.2.x", "1.2.3 - 2.3", ">=1.2.3 <2.0.0"} {
		b.Run(input, func(b *testing.B) {
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if _, err := ParseConstraint(input); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
