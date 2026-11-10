package profile

import (
	"github.com/wago-org/wago/internal/jitprofile"
	"testing"
)

func TestSparseSourcesRespectGapsAndMappingGenerations(t *testing.T) {
	first := jitprofile.Image{ID: 1, Base: 0x100, Size: 16, ModuleID: "module", ArtifactID: "first", Regions: []jitprofile.Region{{Offset: 0, Size: 16, Kind: "guest-body", Function: 0}}, Sources: []jitprofile.SourceRange{{Offset: 4, Size: 4, Function: 2, WasmOffset: 9}}}
	second := first
	second.ID = 2
	second.ArtifactID = "second"
	second.Sources = []jitprofile.SourceRange{{Offset: 4, Size: 4, Function: 5, WasmOffset: 11}}
	events := []jitprofile.Event{{Timestamp: 1, Sequence: 1, Kind: "load", ImageID: 1, Image: &first}, {Timestamp: 3, Sequence: 2, Kind: "retire", ImageID: 1}, {Timestamp: 5, Sequence: 3, Kind: "load", ImageID: 2, Image: &second}}
	samples := []Sample{{Timestamp: 2, PC: 0x103, Period: 1}, {Timestamp: 2, PC: 0x104, Period: 1}, {Timestamp: 2, PC: 0x107, Period: 1}, {Timestamp: 2, PC: 0x108, Period: 1}, {Timestamp: 4, PC: 0x104, Period: 1}, {Timestamp: 6, PC: 0x104, Period: 1}}
	report, err := Resolve(events, samples, "nanoseconds")
	if err != nil {
		t.Fatal(err)
	}
	if report.UnknownSamples != 1 {
		t.Fatal(report)
	}
	for _, row := range report.Rows {
		for _, pc := range row.PCs {
			if pc.Offset < 4 || pc.Offset >= 8 {
				if pc.Source != nil {
					t.Fatal("gap assigned a source", pc)
				}
				continue
			}
			want := uint32(2)
			if row.ArtifactID == "second" {
				want = 5
			}
			if pc.Source == nil || pc.Source.Function != want {
				t.Fatal("wrong generation source", pc)
			}
		}
	}
	first.Sources[0].Function = 99
	for _, row := range report.Rows {
		for _, pc := range row.PCs {
			if pc.Source != nil && pc.Source.Function == 99 {
				t.Fatal("report aliases input source metadata")
			}
		}
	}
}

func TestInlineCallerAttributionSurvivesTeardown(t *testing.T) {
	im := jitprofile.Image{ID: 1, Base: 0x100, Size: 16, ModuleID: "m", ArtifactID: "a", Regions: []jitprofile.Region{{Offset: 0, Size: 16, Kind: "guest-body", Function: 7}}, Sources: []jitprofile.SourceRange{{Offset: 4, Size: 4, Function: 3, WasmOffset: 9, InlineParent: 2}}, InlineFrames: []jitprofile.InlineFrame{{Function: 7, WasmOffset: 12}, {Function: 5, WasmOffset: 6, Parent: 1}}}
	im.Functions = []jitprofile.Function{{Index: 3, Name: "leaf"}, {Index: 5, Name: "middle"}, {Index: 7, Name: "outer"}}
	events := []jitprofile.Event{{Timestamp: 1, Sequence: 1, Kind: "load", ImageID: 1, Image: &im}, {Timestamp: 3, Sequence: 2, Kind: "retire", ImageID: 1}}
	report, err := Resolve(events, []Sample{{Timestamp: 2, PC: 0x104, Period: 1}}, "nanoseconds")
	if err != nil {
		t.Fatal(err)
	}
	pc := report.Rows[0].PCs[0]
	if pc.SourceName != "leaf" || len(pc.InlineCallers) != 2 || pc.InlineCallers[0].Name != "middle" || pc.InlineCallers[1].Name != "outer" {
		t.Fatal("inline function names not resolved", pc)
	}
	if report.Rows[0].Function != 7 || pc.Source.Function != 3 || len(pc.InlineCallers) != 2 || pc.InlineCallers[0].Function != 5 || pc.InlineCallers[1].Function != 7 {
		t.Fatal(report)
	}
	im.InlineFrames[0].Function = 99
	if pc.InlineCallers[1].Function != 7 {
		t.Fatal("resolved ancestry aliases image")
	}
	im.InlineFrames[0].Parent = 1
	if _, err := Resolve(events, []Sample{{Timestamp: 2, PC: 0x104, Period: 1}}, "nanoseconds"); err == nil {
		t.Fatal("malformed ancestry accepted")
	}
}

func TestSourceResolutionRejectsMismatchedImageIdentity(t *testing.T) {
	im := jitprofile.Image{ID: 2, Base: 0x100, Size: 4, Regions: []jitprofile.Region{{Offset: 0, Size: 4, Kind: "guest-body"}}}
	_, err := Resolve([]jitprofile.Event{{Timestamp: 1, Kind: "load", ImageID: 1, Image: &im}}, []Sample{{Timestamp: 2, PC: 0x100, Period: 1}}, "nanoseconds")
	if err == nil {
		t.Fatal("inconsistent mapping identity accepted")
	}
}
