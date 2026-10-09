package main

import (
	"strings"
	"testing"
)

func rawFixture() Snapshot {
	s := fixture("amd64", "31c0")
	s.Regions[0].Instructions[0].Offset = 2
	s.Capture = &CaptureMetadata{NativeSHA256: strings.Repeat("a", 64), NativeBytes: 5, MappedBytes: 2, UnmappedBytes: 3, RawCoverage: true}
	s.RawRegions = []RawRegion{
		{"guest-body.f0.0.gap0", "guest-body.f0.0", "guest-body", 0, "owner-start", "f0.pc5", 0, "5190"},
		{"guest-body.f0.0.gap1", "guest-body.f0.0", "guest-body", 0, "f0.pc5", "owner-end", 4, "c3"},
	}
	return s
}
func TestOpaqueRangesAreSeparateFromOperands(t *testing.T) {
	a, b := rawFixture(), rawFixture()
	r, err := Compare(a, b)
	if err != nil || !r.Complete || !r.RawComplete || r.RawCompared != 2 || r.Compared != 1 || r.Known != 1 || r.Unknown != 0 {
		t.Fatal(r, err)
	}
	b.RawRegions[0].Hex = "5990"
	r, err = Compare(a, b)
	if err != nil || !r.Complete || !r.RawComplete || len(r.RawChanges) != 1 || len(r.Changes) != 0 || r.RawChanges[0].Category != "raw-bytes" {
		t.Fatal(r, err)
	}
	// Raw prologue length may change, without invented source PC or alignment.
	b.RawRegions[0].Hex = "519090"
	b.Regions[0].Instructions[0].Offset = 3
	b.RawRegions[1].Offset = 5
	b.Capture.NativeBytes = 6
	b.Capture.UnmappedBytes = 4
	r, err = Compare(a, b)
	if err != nil || !r.RawComplete || len(r.RawChanges) != 1 || r.Compared != 1 {
		t.Fatal(r, err)
	}
	// Opaque module-owned data has no function; no nearest function is inferred.
	a, b = rawFixture(), rawFixture()
	for _, s := range []*Snapshot{&a, &b} {
		s.RawRegions[0].RightAnchor = "owner-end"
		s.RawRegions[0].Function = -1
		s.RawRegions[0].Kind = "literal-data"
		s.RawRegions[0].Owner = "literal-data.f-1.0"
	}
	r, err = Compare(a, b)
	if err != nil || !r.RawComplete || r.Known != 1 {
		t.Fatal(r, err)
	}
	b.RawRegions[0].Owner = "literal-data.f-1.1"
	r, err = Compare(a, b)
	if err != nil || r.Complete || r.RawComplete || r.RawCompared != 1 {
		t.Fatal(r, err)
	}
}
func TestRawPartitionControls(t *testing.T) {
	for _, mutate := range []func(*Snapshot){
		func(s *Snapshot) { s.RawRegions[1].ID = s.RawRegions[0].ID },
		func(s *Snapshot) { s.RawRegions[0].Offset = 1 },
		func(s *Snapshot) { s.RawRegions[0].Hex = "519031" },
		func(s *Snapshot) { s.RawRegions[0].Hex = "51z0" },
		func(s *Snapshot) { s.RawRegions[0].RightAnchor = "missing" },
		func(s *Snapshot) { s.RawRegions[1].LeftAnchor = "owner-end" },
		func(s *Snapshot) { s.RawRegions[0].Function = -2 },
		func(s *Snapshot) { s.RawRegions[0].Kind = strings.Repeat("x", 65) },
		func(s *Snapshot) { s.Capture.RawCoverage = false },
		func(s *Snapshot) { s.Capture.NativeBytes = maxNativeBytes + 1 },
		func(s *Snapshot) { s.RawRegions = append(s.RawRegions, s.RawRegions[0]) },
		func(s *Snapshot) { s.RawRegions[0], s.RawRegions[1] = s.RawRegions[1], s.RawRegions[0] },
		func(s *Snapshot) { s.RawRegions = make([]RawRegion, maxRawRegions+1) },
	} {
		s := rawFixture()
		mutate(&s)
		if _, err := Compare(s, s); err == nil {
			t.Fatal("bad opaque metadata admitted", s.RawRegions)
		}
	}
	// Legacy partial snapshots remain readable; coverage is explicitly incomplete.
	a := fixture("amd64", "31c0")
	r, err := Compare(a, a)
	if err != nil || !r.Complete || r.RawComplete {
		t.Fatal(r, err)
	}
}
func TestIdenticalFastPathIsConservative(t *testing.T) {
	for _, c := range []struct {
		a, b  string
		reloc string
		known bool
	}{
		{"31C0", "31c0", "", true}, {"c3", "C3", "", false},
		{"0F8E01000000", "0f8e01000000", "f0.pc9.i0", true},
		{"e901000000", "e901000000", "f0.pc9.i0", true},
	} {
		a, b := fixture("amd64", c.a), fixture("amd64", c.b)
		a.Regions[0].Instructions[0].Relocation = c.reloc
		b.Regions[0].Instructions[0].Relocation = c.reloc
		r, err := Compare(a, b)
		if err != nil || r.Complete != c.known || len(r.Changes) != 0 {
			t.Fatal(r, err)
		}
	}
	s := fixture("amd64", "31c0")
	s.Regions[0].Instructions[0].Relocation = "invalid"
	if _, err := Compare(s, s); err == nil {
		t.Fatal("fast path bypassed relocation rejection")
	}
	for condition := 0; condition < 16; condition++ {
		a, b := fixture("amd64", jccNear[condition]), fixture("amd64", jccNear[condition][:4]+"01000000")
		a.Regions[0].Instructions[0].Relocation = "t"
		b.Regions[0].Instructions[0].Relocation = "t"
		r, err := Compare(a, b)
		if err != nil || !r.Complete || len(r.Changes) != 0 {
			t.Fatal(r, err)
		}
		b.Regions[0].Instructions[0].Relocation = "other"
		r, err = Compare(a, b)
		if err != nil || len(r.Changes) != 1 {
			t.Fatal(r, err)
		}
		b.Regions[0].Instructions[0].Relocation = "t"
		b.Regions[0].Instructions[0].Hex = jccShort[condition]
		r, err = Compare(a, b)
		if err != nil || len(r.Changes) != 1 {
			t.Fatal("width normalized away", r, err)
		}
	}
}

func TestOpaqueObservationsDoNotWaiveQualification(t *testing.T) {
	a, b := rawFixture(), rawFixture()
	b.Provenance.CPUFeatures = "different"
	b.RawRegions[0].Hex = "5990"
	r, err := Compare(a, b)
	if err != nil || r.Complete || r.RawComplete || r.RawCompared != 2 || len(r.RawChanges) != 1 {
		t.Fatal(r, err)
	}
	b.Provenance = a.Provenance
	b.Regions[0].Instructions[0].Hex = "0f0b"
	r, err = Compare(a, b)
	if err != nil || r.Complete || !r.RawComplete || r.Unknown != 1 || len(r.RawChanges) != 1 {
		t.Fatal(r, err)
	}
}
