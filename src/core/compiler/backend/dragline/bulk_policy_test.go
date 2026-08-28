package dragline

import (
	"math"
	"testing"

	compilerprofile "github.com/wago-org/wago/src/core/compiler/profile"
)

func TestARM64ProfileSelectsMOPSWithBoundedEvidence(t *testing.T) {
	site := compilerprofile.Site{Function: 7, Offset: 19}
	profile := func(buckets ...compilerprofile.ValueBucket) *compilerprofile.Module {
		return &compilerprofile.Module{MemOpSizes: []compilerprofile.ValueHistogram{{Site: site, Buckets: buckets}}}
	}
	for _, test := range []struct {
		name string
		p    *compilerprofile.Module
		want bool
	}{
		{name: "unobserved", want: true},
		{name: "sparse", p: profile(compilerprofile.ValueBucket{Low: 1, High: 16, Count: 9}), want: true},
		{name: "tiny-dominated", p: profile(compilerprofile.ValueBucket{Low: 0, High: 64, Count: 91}, compilerprofile.ValueBucket{Low: 65, High: 4096, Count: 9}), want: false},
		{name: "large-threshold", p: profile(compilerprofile.ValueBucket{Low: 0, High: 64, Count: 10}, compilerprofile.ValueBucket{Low: 65, High: 4096, Count: 90}), want: true},
		{name: "crossing-is-large", p: profile(compilerprofile.ValueBucket{Low: 32, High: 128, Count: 10}), want: true},
		{name: "saturated", p: profile(compilerprofile.ValueBucket{Low: 0, High: 64, Count: 1}, compilerprofile.ValueBucket{Low: 65, High: 4096, Count: math.MaxUint64}), want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := arm64ProfileSelectsMOPS(test.p, site.Function, site.Offset); got != test.want {
				t.Fatalf("MOPS selection = %t, want %t", got, test.want)
			}
		})
	}
	if !arm64ProfileSelectsMOPS(profile(compilerprofile.ValueBucket{Low: 0, High: 64, Count: 100}), site.Function, site.Offset+1) {
		t.Fatal("profile from another site changed MOPS selection")
	}
}
