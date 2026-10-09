package profile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func BenchmarkSamplyFunctionMetadata(b *testing.B) {
	for _, n := range []int{1000, 2000, 4000} {
		b.Run(fmt.Sprint(n), func(b *testing.B) {
			im := &jitprofile.Image{ModuleID: "module", ArtifactID: "artifact", Size: uint64(n)}
			im.Regions = make([]jitprofile.Region, n)
			im.Functions = make([]jitprofile.Function, n)
			for i := 0; i < n; i++ {
				im.Regions[i] = jitprofile.Region{Offset: uint64(i), Size: 1, Kind: "guest-body", Function: i}
				im.Functions[i] = jitprofile.Function{Index: i, Spills: i + 1}
			}
			events := []jitprofile.Event{{Kind: "load", Image: im}}
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := ReadSamply(strings.NewReader(`{"threads":[]}`), events); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func TestSamplyDuplicateFunctionIndexUsesFirstMetadata(t *testing.T) {
	for _, n := range []int{1, 32} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			im := &jitprofile.Image{ModuleID: "module", ArtifactID: "artifact", Size: uint64(n)}
			for i := 0; i < n; i++ {
				im.Regions = append(im.Regions, jitprofile.Region{Offset: uint64(i), Size: 1, Kind: "guest-body", Function: i})
				im.Functions = append(im.Functions, jitprofile.Function{Index: i, Spills: 11})
			}
			im.Functions = append(im.Functions, jitprofile.Function{Index: n - 1, Spills: 99})
			symbol := Symbol(*im, im.Regions[n-1])
			input := fmt.Sprintf(`{"threads":[{"stringArray":[%q],"samples":{"stack":[0]},"stackTable":{"frame":[0]},"frameTable":{"func":[0]},"funcTable":{"name":[0]}}]}`, symbol)
			report, err := ReadSamply(strings.NewReader(input), []jitprofile.Event{{Kind: "load", Image: im}})
			if err != nil {
				t.Fatal(err)
			}
			if len(report.Rows) != 1 || report.Rows[0].Static == nil || report.Rows[0].Static.Spills != 11 {
				t.Fatalf("first function metadata was not retained: %+v", report.Rows)
			}
		})
	}
}
