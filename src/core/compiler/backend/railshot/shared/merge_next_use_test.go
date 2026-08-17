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
		wantGP       uint64
		wantFP       uint64
		wantComplete bool
	}{
		{name: "overwrites", body: []byte{0x21, 0x02, 0x22, 0x04}, wantGP: 1 << 3, wantFP: 1 << 5, wantComplete: true},
		{name: "read", body: []byte{0x20, 0x02, 0x21, 0x04}, wantFP: 1 << 5, wantComplete: true},
		{name: "barrier", body: []byte{0x40, 0x00}},
		{name: "physical end", body: []byte{0x0b}, wantGP: 1 << 3, wantFP: 1 << 5, wantComplete: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			gp, fp, complete := ScanForwardMergeDeadLocals(wasm.NewReader(tc.body), 0, candidates)
			if gp != tc.wantGP || fp != tc.wantFP || complete != tc.wantComplete {
				t.Fatalf("got gp=%#x fp=%#x complete=%v, want gp=%#x fp=%#x complete=%v", gp, fp, complete, tc.wantGP, tc.wantFP, tc.wantComplete)
			}
		})
	}
}

func TestMergeRegionHints(t *testing.T) {
	var hints MergeRegionHints
	for i := 0; i < MaxMergeRegionHints; i++ {
		hints.Note(i * 7)
	}
	hints.Note(999) // fixed-cap overflow is ignored
	for i := 0; i < MaxMergeRegionHints; i++ {
		if !hints.Has(i * 7) {
			t.Fatalf("missing hint %d", i*7)
		}
	}
	if hints.Has(999) || hints.Has(MaxMergeRegionBody+1) {
		t.Fatal("overflow or out-of-range hint was retained")
	}
}

func TestScanForwardMergeDeadLocalsFuelFallback(t *testing.T) {
	body := make([]byte, MergeNextUseFuel)
	for i := range body {
		body[i] = 0x6a // i32.add: no immediate and no local effect
	}
	if gp, fp, ok := ScanForwardMergeDeadLocals(wasm.NewReader(body), 0, []MergeLocalCandidate{{Local: 0, Reg: 1}}); gp != 0 || fp != 0 || ok {
		t.Fatalf("fuel fallback = %#x, %#x, %v", gp, fp, ok)
	}
}

func TestStackFenceElisionValid(t *testing.T) {
	if !StackFenceElisionValid(true, 4096) || StackFenceElisionValid(true, 4097) || !StackFenceElisionValid(false, 1<<20) {
		t.Fatal("stack-fence validation boundary changed")
	}
	if !ShouldSkipStackFence(false, 16, 8, 16) || ShouldSkipStackFence(true, 16, 8, 16) || ShouldSkipStackFence(false, 16, 8, 1<<10) {
		t.Fatal("stack-fence estimate boundary changed")
	}
}

func TestCompileRetryState(t *testing.T) {
	state := NewCompileRetryState(true)
	if !state.Retry(true, true) || state.AllowFenceSkip || !state.PinLocals {
		t.Fatalf("fence retry state = %+v", state)
	}
	if !state.Retry(false, true) || state.PinLocals {
		t.Fatalf("register retry state = %+v", state)
	}
	if state.Retry(true, true) {
		t.Fatalf("exhausted retry state retried: %+v", state)
	}
}
