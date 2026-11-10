package jitprofile

import "testing"

func TestSourceMetadataCopiedAndValidated(t *testing.T) {
	s := New(Options{SourceMaps: true})
	defer s.Close()
	im := Image{Size: 16, Regions: []Region{{Offset: 0, Size: 16, Kind: "guest-body"}}, Sources: []SourceRange{{Offset: 4, Size: 4, Function: 2, WasmOffset: 3}}}
	if s.Register(im, nil) == 0 {
		t.Fatal(s.Status())
	}
	im.Sources[0].Function = 99
	events, _ := s.Read(0)
	if events[0].Image.Sources[0].Function != 2 {
		t.Fatal("caller can mutate source directory")
	}
	events[0].Image.Sources[0].Function = 98
	again, _ := s.Read(0)
	if again[0].Image.Sources[0].Function != 2 {
		t.Fatal("reader can mutate source directory")
	}
	im.Sources = []SourceRange{{Offset: 15, Size: 2}}
	if s.Register(im, nil) != 0 || s.Status().Dropped != 1 {
		t.Fatal("invalid source directory accepted")
	}
}

func TestInlineSourcesValidationOwnershipAndBudget(t *testing.T) {
	frames := []InlineFrame{{Function: 7, WasmOffset: 4}, {Function: 5, WasmOffset: 8, Parent: 1}}
	sources := []SourceRange{{Offset: 0, Size: 4, Function: 3, WasmOffset: 6, InlineParent: 2}}
	im := Image{Size: 4, Regions: []Region{{Offset: 0, Size: 4, Kind: "guest-body"}}, Sources: sources, InlineFrames: frames}
	s := New(Options{SourceMaps: true})
	defer s.Close()
	if s.Register(im, nil) == 0 {
		t.Fatal(s.Status())
	}
	frames[0].Function = 99
	first, _ := s.Read(0)
	chain := InlineCallers(first[0].Image.InlineFrames, 2)
	if len(chain) != 2 || chain[0].Function != 5 || chain[1].Function != 7 {
		t.Fatal(chain)
	}
	first[0].Image.InlineFrames[0].Function = 98
	again, _ := s.Read(0)
	if again[0].Image.InlineFrames[0].Function != 7 {
		t.Fatal("snapshot aliases inline table")
	}
	for _, bad := range [][]InlineFrame{{{Parent: 1}}, {{}, {Parent: 2}}, {{Parent: 2}, {Parent: 1}}} {
		if ValidateInlineSources(nil, bad) == nil {
			t.Fatal("cyclic/forward frame reference accepted", bad)
		}
	}
	if ValidateInlineSources(sources, frames[:1]) == nil {
		t.Fatal("dangling source parent accepted")
	}
	// Inline frames participate in the shared metadata-byte limit.
	im.InlineFrames = nil
	im.Sources = nil
	budget := imageBytes(&im)
	im.InlineFrames = frames
	bounded := New(Options{SourceMaps: true, MaxBytes: budget})
	defer bounded.Close()
	if bounded.Register(im, nil) != 0 || bounded.Status().Dropped != 1 {
		t.Fatal("inline bytes escaped budget", bounded.Status())
	}
}
