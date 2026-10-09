package profile

import (
	"io"
	"strconv"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func perfMapLoad(base, size uint64) jitprofile.Event {
	im := &jitprofile.Image{Base: base, Size: size}
	if size != 0 {
		im.Regions = []jitprofile.Region{{Size: size, Kind: "padding"}}
	}
	return jitprofile.Event{Kind: "load", Image: im}
}

func TestPerfMapAdjacentAndZeroSizeLoads(t *testing.T) {
	for _, tc := range []struct {
		name  string
		loads []jitprofile.Event
		bad   jitprofile.Event
	}{
		{
			name: "address order and adjacency",
			loads: []jitprofile.Event{
				perfMapLoad(0x120, 0x10),
				perfMapLoad(0x100, 0x10),
				perfMapLoad(0x110, 0x10),
			},
			bad: perfMapLoad(0x10f, 2),
		},
		{
			name: "overlap with next address span",
			loads: []jitprofile.Event{
				perfMapLoad(0x100, 0x10),
				perfMapLoad(0x120, 0x10),
			},
			bad: perfMapLoad(0x11f, 2),
		},
		{
			name: "zero size inside prior load",
			loads: []jitprofile.Event{
				perfMapLoad(0x100, 0x20),
				perfMapLoad(0x110, 0),
			},
			bad: perfMapLoad(0x11f, 2),
		},
		{
			name: "prior zero size inside later load",
			loads: []jitprofile.Event{
				perfMapLoad(0x110, 0),
				perfMapLoad(0x100, 0x20),
			},
			bad: perfMapLoad(0x11f, 2),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := NewPerfMap(io.Discard)
			if err := p.Write(tc.loads); err != nil {
				t.Fatalf("disjoint loads: %v", err)
			}
			if err := p.Write([]jitprofile.Event{tc.bad}); err == nil {
				t.Fatal("accepted overlapping load")
			}
		})
	}
}

func BenchmarkPerfMapDisjointLoads(b *testing.B) {
	for _, order := range []struct {
		name    string
		reverse bool
	}{{"ascending", false}, {"descending", true}} {
		b.Run(order.name, func(b *testing.B) {
			for _, n := range []int{1000, 2000, 4000} {
				b.Run(strconv.Itoa(n), func(b *testing.B) {
					events := make([]jitprofile.Event, n)
					for i := range events {
						address := i
						if order.reverse {
							address = n - 1 - i
						}
						events[i] = perfMapLoad(uint64(address)*16, 1)
					}
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						p := NewPerfMap(io.Discard)
						if err := p.Write(events); err != nil {
							b.Fatal(err)
						}
					}
				})
			}
		})
	}
}
