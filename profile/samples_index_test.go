package profile

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

// BenchmarkResolveInterleavedLoads measures the lifecycle join when each load
// has a sample before the next load. Address order changes the cost of an
// ordered slice insertion, while all three cases have the same report size.
func BenchmarkResolveInterleavedLoads(b *testing.B) {
	for _, order := range []string{"ascending", "descending", "permuted"} {
		for _, n := range []int{256, 512, 1024} {
			b.Run(fmt.Sprintf("%s/%d", order, n), func(b *testing.B) {
				events := make([]jitprofile.Event, n)
				samples := make([]Sample, n)
				for i := range events {
					address := i
					switch order {
					case "descending":
						address = n - 1 - i
					case "permuted":
						address = (i * 257) % n
					}
					base := uint64(address+1) << 12
					id := uint64(i + 1)
					image := &jitprofile.Image{ID: id, ModuleID: "module", ArtifactID: "artifact", Base: base, Size: 1,
						Regions: []jitprofile.Region{{Size: 1, Kind: "guest-body", Function: 0}}}
					events[i] = jitprofile.Event{Timestamp: id, Sequence: id, Kind: "load", ImageID: id, Image: image}
					samples[i] = Sample{Timestamp: id, PC: base, Period: 1}
				}
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					report, err := Resolve(events, samples, "observations")
					if err != nil || report.Samples != uint64(n) || report.UnknownSamples != 0 {
						b.Fatalf("report=%+v, err=%v", report, err)
					}
				}
			})
		}
	}
}
