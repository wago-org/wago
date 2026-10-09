package profile

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func resolveIndexFixture(count int, shape string) ([]jitprofile.Event, []Sample) {
	var events []jitprofile.Event
	var samples []Sample
	for i := 0; i < count; i++ {
		id := uint64(i + 1)
		base := id << 12
		events = append(events, jitprofile.Event{Timestamp: id, Sequence: id, Kind: "load", ImageID: id,
			Image: &jitprofile.Image{ID: id, ModuleID: "m", ArtifactID: "a", Base: base, Size: 1,
				Regions: []jitprofile.Region{{Size: 1, Kind: "guest-body", Function: 0}}}})
		if shape == "interleaved" {
			samples = append(samples, Sample{Timestamp: id, PC: base, Period: 1})
		}
	}
	switch shape {
	case "batch":
		samples = []Sample{{Timestamp: uint64(count), PC: uint64(count) << 12, Period: 1}}
	case "future":
		samples = []Sample{{Timestamp: 0, PC: 0x1000, Period: 1}}
	case "retired":
		for i := 0; i < count; i++ {
			events = append(events, jitprofile.Event{Timestamp: uint64(count + i + 1), Kind: "retire", ImageID: uint64(i + 1)})
		}
		samples = []Sample{{Timestamp: uint64(2 * count), PC: 0x1000, Period: 1}}
	case "retires-only":
		for i := range events {
			events[i].Kind = "retire"
			events[i].Image = nil
		}
		samples = []Sample{{Timestamp: uint64(count), PC: 0x1000, Period: 1}}
	}
	return events, samples
}

func TestResolveNoSamplesKeepsInputBudgetChecks(t *testing.T) {
	events, _ := resolveIndexFixture(2, "none")
	limits := DefaultLimits()
	limits.Images = 1
	if _, err := ResolveWithLimits(events, nil, "observations", limits); err == nil {
		t.Fatal("accepted events beyond image budget")
	}
	report, err := Resolve(events, nil, "observations")
	if err != nil || report.Unit != "observations" || report.Samples != 0 || len(report.Rows) != 0 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
}

func TestResolveFutureEventsStayOutsideSampleWindow(t *testing.T) {
	events, _ := resolveIndexFixture(2, "none")
	report, err := Resolve(events, []Sample{{Timestamp: 1, PC: 0x1000, Period: 1}}, "observations")
	if err != nil || report.Samples != 1 || report.UnknownSamples != 0 || len(report.Rows) != 1 {
		t.Fatalf("report=%+v, err=%v", report, err)
	}
	limits := DefaultLimits()
	limits.Images = 1
	if _, err := ResolveWithLimits(events, []Sample{{Timestamp: 1, PC: 0x1000, Period: 1}}, "observations", limits); err == nil {
		t.Fatal("future event escaped input budget")
	}
}

func BenchmarkResolveIndexControls(b *testing.B) {
	for _, tc := range []struct {
		count int
		shape string
	}{
		{1, "interleaved"}, {8, "interleaved"}, {64, "interleaved"},
		{1024, "none"}, {1024, "future"}, {1024, "batch"},
		{1024, "retired"}, {1024, "retires-only"},
	} {
		b.Run(fmt.Sprintf("%s/%d", tc.shape, tc.count), func(b *testing.B) {
			events, samples := resolveIndexFixture(tc.count, tc.shape)
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				report, err := Resolve(events, samples, "observations")
				if err != nil || report.Samples != uint64(len(samples)) {
					b.Fatalf("report=%+v, err=%v", report, err)
				}
			}
		})
	}
}
