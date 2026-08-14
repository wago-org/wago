package shared

import (
	"bytes"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/optimization"
)

func TestFinalizeIdentityPreservesCodeAndMapsEveryOffset(t *testing.T) {
	code := []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08}
	want := append([]byte(nil), code...)
	sites := []RelaxSite{
		{Off: 0, Kind: RelaxFrameSub, LongLen: 4, ShortLen: 4},
		{Off: 4, Kind: RelaxDeadHole, LongLen: 4, ShortLen: 0},
	}
	labels := []CodeLabel{{Off: 0}, {Off: 4}, {Off: 8}}
	marks := []CodeMark{
		{Off: 0, Kind: MarkEntry},
		{Off: 4, Index: 3, Kind: MarkCallReloc},
		{Off: 8, Index: 1, Kind: MarkCallReturn},
	}

	result, err := FinalizeIdentity(code, sites, labels, marks)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(result.Code, want) {
		t.Fatalf("identity bytes = %x, want %x", result.Code, want)
	}
	if len(result.Code) > 0 && &result.Code[0] != &code[0] {
		t.Fatal("identity finalization copied the code buffer")
	}
	for off := 0; off <= len(code); off++ {
		mapped, ok := result.Offsets.Map(off)
		if !ok || mapped != off {
			t.Fatalf("map(%d) = %d, %v", off, mapped, ok)
		}
	}
	if _, ok := result.Offsets.Map(len(code) + 1); ok {
		t.Fatal("offset beyond function unexpectedly mapped")
	}
}

func TestFinalizeIdentityRejectsInvalidRecords(t *testing.T) {
	code := make([]byte, 8)
	tests := []struct {
		name   string
		sites  []RelaxSite
		labels []CodeLabel
		marks  []CodeMark
	}{
		{name: "site offset", sites: []RelaxSite{{Off: 6, LongLen: 4}}},
		{name: "short longer", sites: []RelaxSite{{Off: 0, LongLen: 4, ShortLen: 5}}},
		{name: "label offset", labels: []CodeLabel{{Off: 9}}},
		{name: "mark offset", marks: []CodeMark{{Off: 9}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := FinalizeIdentity(code, test.sites, test.labels, test.marks); err == nil {
				t.Fatal("invalid finalizer record was accepted")
			}
		})
	}
}

func TestOffsetMapAppliesSortedDeletions(t *testing.T) {
	deletions := []DeletedRange{{Off: 4, Len: 4}, {Off: 12, Len: 8}}
	offsets, err := NewOffsetMap(24, deletions)
	if err != nil {
		t.Fatal(err)
	}
	if offsets.FinalLen() != 12 {
		t.Fatalf("final length = %d, want 12", offsets.FinalLen())
	}
	for _, test := range []struct {
		off  int
		want int
		ok   bool
	}{
		{0, 0, true},
		{4, 4, true},
		{6, 0, false},
		{8, 4, true},
		{12, 8, true},
		{16, 0, false},
		{20, 8, true},
		{24, 12, true},
	} {
		got, ok := offsets.Map(test.off)
		if got != test.want || ok != test.ok {
			t.Errorf("map(%d) = %d, %v; want %d, %v", test.off, got, ok, test.want, test.ok)
		}
	}
}

func TestOffsetMapRejectsInvalidDeletions(t *testing.T) {
	for _, deletions := range [][]DeletedRange{
		{{Off: 4}},
		{{Off: 7, Len: 2}},
		{{Off: 4, Len: 4}, {Off: 6, Len: 2}},
		{{Off: 8, Len: 2}, {Off: 4, Len: 2}},
	} {
		if _, err := NewOffsetMap(8, deletions); err == nil {
			t.Fatalf("invalid deletions accepted: %#v", deletions)
		}
	}
}

func TestCodegenPolicyObjectiveOwnsAlignment(t *testing.T) {
	for _, test := range []struct {
		objective     OptimizationObjective
		wantAlign     uint8
		wantCompact   bool
		wantDeletions uint8
	}{
		{OptimizeSpeed, 4, false, 8},
		{OptimizeBalanced, 4, false, 8},
		{OptimizeSize, 0, true, MaxOffsetMapDeletions},
		{OptimizeEmbedded, 0, true, MaxOffsetMapDeletions},
	} {
		policy := CodegenPolicyForObjective(optimization.Selection{}, test.objective)
		if policy.Objective != test.objective || policy.FunctionAlignLog2 != test.wantAlign || policy.InternalAlignLog2 != test.wantAlign || policy.LoopAlignLog2 != test.wantAlign || policy.CompactNative != test.wantCompact || policy.MaxFinalizerDeletions != test.wantDeletions {
			t.Errorf("objective %d policy = %#v, want alignment log2 %d, compact %v, deletions %d", test.objective, policy, test.wantAlign, test.wantCompact, test.wantDeletions)
		}
	}
}
