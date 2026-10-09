package main

import "testing"

func inlineFixture() Snapshot {
	s := rawFixture()
	s.Regions[0].ID = "f7.pc5"
	s.Regions[0].Function = 7
	s.Regions[0].InlineParent = 2
	s.InlineFrames = []InlineFrame{{Function: 0, WasmOffset: 10}, {Function: 5, WasmOffset: 20, Parent: 1}}
	s.RawRegions[0].RightAnchor = s.Regions[0].ID
	s.RawRegions[1].LeftAnchor = s.Regions[0].ID
	return s
}

func TestInlineOwnershipUsesRootCaller(t *testing.T) {
	a, b := inlineFixture(), inlineFixture()
	r, err := Compare(a, b)
	if err != nil || !r.Complete || !r.RawComplete || r.RawCompared != 2 || r.Known != 1 {
		t.Fatal(r, err)
	}
	b.Regions[0].Instructions[0].Hex = "31c8"
	r, err = Compare(a, b)
	if err != nil || !r.Complete || len(r.Changes) != 1 || r.Changes[0].Function != 7 || r.Changes[0].WasmOffset == nil || *r.Changes[0].WasmOffset != 5 || r.Changes[0].InlineParent != 2 || len(r.BeforeInlineFrames) != 2 || len(r.AfterInlineFrames) != 2 {
		t.Fatal("known change lost logical inline location", r, err)
	}
	b = inlineFixture()
	// Logical callee and inner caller differ from the physical owner. Never
	// substitute either of those functions into opaque ownership metadata.
	for _, wrong := range []int{5, 7} {
		s := inlineFixture()
		s.RawRegions[0].Function = wrong
		if _, err := Compare(s, s); err == nil {
			t.Fatal("logical function admitted as physical owner", wrong)
		}
	}
	b.InlineFrames[0].WasmOffset++
	r, err = Compare(a, b)
	if err != nil || r.Complete || r.RawComplete || r.RawCompared != 2 || len(r.BeforeInlineFrames) != 2 || len(r.AfterInlineFrames) != 2 {
		t.Fatal("changed caller context qualified", r, err)
	}
	b = inlineFixture()
	b.Regions[0].InlineParent = 1
	r, err = Compare(a, b)
	if err != nil || r.Complete || r.Compared != 0 {
		t.Fatal("changed ancestry qualified", r, err)
	}
	b = inlineFixture()
	b.Regions[0].Instructions[0].Hex = "0f0b"
	r, err = Compare(a, b)
	if err != nil || r.Complete || r.Unknown != 1 || len(r.UnknownSites) != 1 || r.UnknownSites[0].Function != 7 || r.UnknownSites[0].InlineParent != 2 {
		t.Fatal("unknown lost logical inline location", r, err)
	}
}

func TestInlineMetadataRejection(t *testing.T) {
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.InlineFrames[0].Parent = 1 },
		func(s *Snapshot) { s.InlineFrames[1].Parent = 2 },
		func(s *Snapshot) { s.InlineFrames[0].Parent = 2 },
		func(s *Snapshot) { s.Regions[0].InlineParent = 3 },
		func(s *Snapshot) { s.InlineFrames = nil },
		func(s *Snapshot) { s.InlineFrames = make([]InlineFrame, maxInlineFrames+1) },
	} {
		s := inlineFixture()
		mutate(&s)
		if _, err := Compare(s, s); err == nil {
			t.Fatal("invalid ancestry admitted")
		}
		// Ancestry must be valid even without raw coverage metadata.
		s.Capture = nil
		s.RawRegions = nil
		if _, err := Compare(s, s); err == nil {
			t.Fatal("legacy coverage bypassed ancestry rejection")
		}
	}
}
