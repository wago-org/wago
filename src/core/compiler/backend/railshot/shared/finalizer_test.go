package shared

import (
	"bytes"
	"testing"
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
