package profile

import (
	"fmt"
	"strings"
	"testing"

	"github.com/wago-org/wago/internal/jitprofile"
)

func metadataControlImage(n int) *jitprofile.Image {
	im := &jitprofile.Image{ModuleID: "module", ArtifactID: "artifact", Size: uint64(n)}
	im.Regions = make([]jitprofile.Region, n)
	im.Functions = make([]jitprofile.Function, n)
	for i := range im.Regions {
		im.Regions[i] = jitprofile.Region{Offset: uint64(i), Size: 1, Kind: "guest-body", Function: i}
		im.Functions[i] = jitprofile.Function{Index: i, Spills: 11}
	}
	return im
}

func BenchmarkSamplyMetadataControls(b *testing.B) {
	for _, name := range []string{"dense31", "dense32", "dense33", "dense64", "excluded", "single", "shared", "repeated", "missing", "duplicates"} {
		b.Run(name, func(b *testing.B) {
			n := 4000
			switch name {
			case "dense31":
				n = 31
			case "dense32":
				n = 32
			case "dense33":
				n = 33
			case "dense64":
				n = 64
			}
			im := metadataControlImage(n)
			for i := range im.Regions {
				switch name {
				case "excluded", "single":
					im.Regions[i].Kind = "padding"
				case "shared", "duplicates":
					im.Regions[i].Function = 0
				case "missing":
					im.Regions[i].Function = n + i
				}
				if name == "duplicates" {
					im.Functions[i].Index = 0
				}
			}
			if name == "single" {
				im.Regions[0].Kind = "guest-body"
			}
			events := []jitprofile.Event{{Kind: "load", Image: im}}
			if name == "repeated" {
				events = append(events, events[0])
			}
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

func TestSamplyMetadataLookupControls(t *testing.T) {
	for _, n := range []int{1, 64} {
		for _, mode := range []string{"missing", "first", "repeated", "shared"} {
			t.Run(fmt.Sprintf("%s/%d", mode, n), func(t *testing.T) {
				im := metadataControlImage(n)
				last := n - 1
				switch mode {
				case "missing":
					im.Regions[last].Function = n + 1000
				case "first":
					im.Functions[0].Index = 1000
					im.Regions[last].Function = 1000
				case "shared":
					for i := range im.Regions {
						im.Regions[i].Function = 0
					}
				}
				events := []jitprofile.Event{{Kind: "load", Image: im}}
				observed := im
				if mode == "repeated" {
					next := *im
					next.ID = 1
					next.Functions = append([]jitprofile.Function(nil), im.Functions...)
					for i := range next.Functions {
						next.Functions[i].Spills = 99
					}
					events = append(events, jitprofile.Event{Kind: "load", ImageID: 1, Image: &next})
					observed = &next
				}
				symbol := Symbol(*observed, observed.Regions[last])
				input := fmt.Sprintf(`{"threads":[{"stringArray":[%q],"samples":{"stack":[0]},"stackTable":{"frame":[0]},"frameTable":{"func":[0]},"funcTable":{"name":[0]}}]}`, symbol)
				report, err := ReadSamply(strings.NewReader(input), events)
				if err != nil {
					t.Fatal(err)
				}
				if len(report.Rows) != 1 || report.Rows[0].Samples != 1 || report.Rows[0].Function != observed.Regions[last].Function {
					t.Fatalf("sampled row = %+v", report.Rows)
				}
				if mode == "missing" {
					if report.Rows[0].Static != nil {
						t.Fatal("missing function received static metadata")
					}
				} else if report.Rows[0].Static == nil || report.Rows[0].Static.Spills != 11 {
					t.Fatalf("first metadata = %+v", report.Rows[0].Static)
				}
			})
		}
	}
}
