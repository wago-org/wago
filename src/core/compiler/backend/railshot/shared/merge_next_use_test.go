package shared

import (
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestScanForwardMergeDeadLocals(t *testing.T) {
	candidates := []MergeLocalCandidate{{Local: 2, Reg: 3}, {Local: 4, Reg: 5, FP: true}}
	for _, tc := range []struct {
		name         string
		body         []byte
		localBase    int
		wantGP       uint64
		wantFP       uint64
		wantComplete bool
	}{
		{name: "overwrites after immediate", body: []byte{0x41, 0x7f, 0x1a, 0x21, 0x02, 0x22, 0x04}, wantGP: 1 << 3, wantFP: 1 << 5, wantComplete: true},
		{name: "read", body: []byte{0x20, 0x02, 0x21, 0x04}, wantFP: 1 << 5, wantComplete: true},
		{name: "barrier", body: []byte{0x40, 0x00}},
		{name: "exception barrier", body: []byte{0x08, 0x00}},
		{name: "br_on_null barrier", body: []byte{0xd5, 0x00, 0x21, 0x02, 0x20, 0x04}},
		{name: "br_on_non_null barrier", body: []byte{0xd6, 0x00, 0x21, 0x02, 0x20, 0x04}},
		{name: "physical end", body: []byte{0x0b}, wantGP: 1 << 3, wantFP: 1 << 5, wantComplete: true},
		{name: "nested end", body: []byte{0x0b}, localBase: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gp, fp, complete := ScanForwardMergeDeadLocals(wasm.NewReader(tc.body), tc.localBase, candidates)
			if gp != tc.wantGP || fp != tc.wantFP || complete != tc.wantComplete {
				t.Fatalf("got gp=%#x fp=%#x complete=%v, want gp=%#x fp=%#x complete=%v", gp, fp, complete, tc.wantGP, tc.wantFP, tc.wantComplete)
			}
		})
	}
}

func TestScanForwardMergeDeadLocalsBounds(t *testing.T) {
	body := make([]byte, MergeNextUseFuel)
	for i := range body {
		body[i] = 0x6a // i32.add: no immediate and no local effect
	}
	if gp, fp, ok := ScanForwardMergeDeadLocals(wasm.NewReader(body), 0, []MergeLocalCandidate{{Local: 0, Reg: 1}}); gp != 0 || fp != 0 || ok {
		t.Fatalf("fuel fallback = %#x, %#x, %v", gp, fp, ok)
	}
	tooMany := make([]MergeLocalCandidate, 65)
	if gp, fp, ok := ScanForwardMergeDeadLocals(wasm.NewReader([]byte{0x0b}), 0, tooMany); gp != 0 || fp != 0 || ok {
		t.Fatalf("capacity fallback = %#x, %#x, %v", gp, fp, ok)
	}
	if gp, fp, ok := ScanForwardMergeDeadLocals(wasm.NewReader([]byte{0x21}), 0, []MergeLocalCandidate{{Local: 0, Reg: 1}}); gp != 0 || fp != 0 || ok {
		t.Fatalf("malformed fallback = %#x, %#x, %v", gp, fp, ok)
	}
}
