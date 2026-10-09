package profile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func TestResolveInterleavedImageLifecycle(t *testing.T) {
	load := func(time, id, base uint64) jitprofile.Event {
		return jitprofile.Event{Timestamp: time, Sequence: time, Kind: "load", ImageID: id,
			Image: &jitprofile.Image{ID: id, ModuleID: fmt.Sprint(id), ArtifactID: "a", Base: base, Size: 1,
				Regions: []jitprofile.Region{{Size: 1, Kind: "guest-body", Function: 0}}}}
	}
	events := []jitprofile.Event{
		load(1, 1, 0x1000),
		load(2, 2, 0x3000),
		{Timestamp: 3, Sequence: 3, Kind: "retire", ImageID: 1},
		load(4, 3, 0x1000),
		// A transient overlap is gone before the sample at time 5.
		load(5, 4, 0x1000),
		{Timestamp: 5, Sequence: 6, Kind: "retire", ImageID: 3},
	}
	samples := []Sample{{1, 0x1000, 1}, {2, 0x3000, 1}, {3, 0x1000, 1}, {4, 0x1000, 1}, {5, 0x1000, 1}}
	report, err := Resolve(events, samples, "observations")
	if err != nil || report.Samples != 5 || report.UnknownSamples != 1 || len(report.Rows) != 4 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	if _, err := Resolve([]jitprofile.Event{load(1, 1, 0x1000), load(2, 2, 0x1000)}, []Sample{{2, 0x1000, 1}}, "observations"); err == nil || !strings.Contains(err.Error(), "overlapping live images") {
		t.Fatalf("expected overlap error, got %v", err)
	}
}

func TestResolveZeroSizeImageAtLiveBase(t *testing.T) {
	events := []jitprofile.Event{
		{Timestamp: 1, Kind: "load", ImageID: 1, Image: &jitprofile.Image{ID: 1, Base: 0x1000}},
		{Timestamp: 2, Kind: "load", ImageID: 2, Image: &jitprofile.Image{ID: 2, Base: 0x1000, Size: 1,
			Regions: []jitprofile.Region{{Size: 1, Kind: "guest-body", Function: 0}}}},
	}
	report, err := Resolve(events, []Sample{{Timestamp: 2, PC: 0x1000, Period: 1}}, "observations")
	if err != nil || report.Samples != 1 || report.UnknownSamples != 0 || len(report.Rows) != 1 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	reversed := []jitprofile.Event{events[1], events[0]}
	reversed[0].Timestamp, reversed[1].Timestamp = 1, 2
	report, err = Resolve(reversed, []Sample{{Timestamp: 2, PC: 0x1000, Period: 1}}, "observations")
	if err != nil || report.UnknownSamples != 0 || len(report.Rows) != 1 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
}

// BenchmarkResolveInterleavedLoads measures the lifecycle join when each load
// has a sample before the next load. Address order varies while all three
// cases have the same report size.
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
						address = (i * 251) % n
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

func BenchmarkResolveStableImage(b *testing.B) {
	image := &jitprofile.Image{ID: 1, ModuleID: "module", ArtifactID: "artifact", Base: 0x1000, Size: 1,
		Regions: []jitprofile.Region{{Size: 1, Kind: "guest-body", Function: 0}}}
	events := []jitprofile.Event{{Timestamp: 1, Kind: "load", ImageID: 1, Image: image}}
	samples := make([]Sample, 10_000)
	for i := range samples {
		samples[i] = Sample{Timestamp: 2, PC: 0x1000, Period: 1}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		report, err := Resolve(events, samples, "observations")
		if err != nil || report.Samples != uint64(len(samples)) || report.UnknownSamples != 0 {
			b.Fatalf("report=%+v, err=%v", report, err)
		}
	}
}
