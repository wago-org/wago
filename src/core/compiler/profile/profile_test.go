package profile

import (
	"bytes"
	"testing"
)

func TestCanonicalRoundTripAndHash(t *testing.T) {
	p := Module{
		ModuleHash: [32]byte{1, 2, 3}, Generation: 9,
		Source: SourceRailshot, Phase: PhaseSteady,
		FunctionCounts: []uint64{10, 20},
		EdgeCounts: []EdgeCount{
			{Site: Site{Function: 1, Offset: 8}, Target: 3, Count: 4},
			{Site: Site{Function: 0, Offset: 2}, Target: 1, Count: 7},
		},
		CallTargets: []TargetHistogram{{Site: Site{Function: 1, Offset: 12}, Targets: []TargetCount{{Function: 8, Count: 2}, {Function: 3, Count: 9}}}},
	}
	encoded, err := Marshal(p)
	if err != nil {
		t.Fatal(err)
	}
	got, err := Unmarshal(encoded)
	if err != nil {
		t.Fatal(err)
	}
	reencoded, err := Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(encoded, reencoded) {
		t.Fatalf("profile encoding is not canonical:\n%s\n%s", encoded, reencoded)
	}
	first, err := Hash(p)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Hash(got)
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatalf("canonical hashes differ: %x != %x", first, second)
	}
}

func TestStrictDecode(t *testing.T) {
	valid := `{"version":1,"module_hash":[0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0,0],"generation":0,"source":"static","phase":"startup"}`
	if _, err := Unmarshal([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	for _, malformed := range []string{
		valid[:len(valid)-1] + `,"unknown":1}`,
		valid + valid,
		`{"version":2,"source":"static","phase":"startup"}`,
		`{"version":1,"source":"other","phase":"startup"}`,
	} {
		if _, err := Unmarshal([]byte(malformed)); err == nil {
			t.Fatalf("malformed profile accepted: %s", malformed)
		}
	}
}

func TestRejectsAmbiguousHistograms(t *testing.T) {
	base := Module{Version: Version, Source: SourceStatic, Phase: PhaseStartup}
	tests := []Module{
		func() Module {
			p := base
			p.CallTargets = []TargetHistogram{{Site: Site{}, Targets: []TargetCount{{Function: 2}, {Function: 2}}}}
			return p
		}(),
		func() Module {
			p := base
			p.ValueRanges = []ValueHistogram{{Site: Site{}, Buckets: []ValueBucket{{Low: 5, High: 4}}}}
			return p
		}(),
		func() Module {
			p := base
			p.MemOpSizes = []ValueHistogram{{Site: Site{}, Buckets: []ValueBucket{{Low: 1, High: 4}, {Low: 4, High: 8}}}}
			return p
		}(),
	}
	for _, profile := range tests {
		if _, err := Marshal(profile); err == nil {
			t.Fatalf("ambiguous histogram accepted: %#v", profile)
		}
	}
}
