//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
	"unsafe"
)

func TestRegionF64ProofPackedCount(t *testing.T) {
	if unsafe.Sizeof(funcHints{}) != 28 {
		t.Fatalf("hint header grew: %d", unsafe.Sizeof(funcHints{}))
	}
	for _, flags := range []uint16{0, fixedScratchLeaseMask, noF64StoreMask, immediateFreeFlagsMask} {
		h := funcHints{immediateFreeOps: flags}
		for n := 0; n <= defaultStackArenaCap; n++ {
			if h.immediateFreeOpCount() != uint16(n) || h.immediateFreeOps&immediateFreeFlagsMask != flags {
				t.Fatalf("flags/count corrupted: %x %d", flags, n)
			}
			if n < defaultStackArenaCap {
				h.immediateFreeOps = h.immediateFreeOps&immediateFreeFlagsMask | h.immediateFreeOpCount() + 1
			}
		}
		h.setNoF64StoreProof(true)
		if !h.provenNoF64Store() || h.hasFixedScratchLease() != (flags&fixedScratchLeaseMask != 0) || h.immediateFreeOpCount() != defaultStackArenaCap {
			t.Fatal("set changed other facts")
		}
		h.setNoF64StoreProof(false)
		if h.provenNoF64Store() || h.immediateFreeOpCount() != defaultStackArenaCap {
			t.Fatal("clear changed count")
		}
	}
}
func TestRegionF64ProofByteAndAST(t *testing.T) {
	for _, store := range []bool{false, true} {
		body := []byte{0x01}
		expr := wasm.Expr{Instrs: []wasm.Instruction{{Kind: wasm.InstrNop}}}
		if store {
			body = append(body, 0x39, 3, 0)
			expr.Instrs = append(expr.Instrs, wasm.Instruction{Kind: wasm.InstrF64Store})
		}
		body = append(body, 0x0b)
		h, err := scanBodyBytes(body, 0, 0, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast := scanBody(expr, 0, 0, 0)
		want := boundedVectorPolls && !store
		if h.provenNoF64Store() != want || ast.provenNoF64Store() != want {
			t.Fatalf("store=%v byte=%v AST=%v", store, h.provenNoF64Store(), ast.provenNoF64Store())
		}
	}
	h, err := scanBodyBytes([]byte{0x39}, 0, 0, 0)
	if err == nil || h.provenNoF64Store() {
		t.Fatal("malformed body gained proof")
	}
	if (funcHints{}).provenNoF64Store() {
		t.Fatal("unknown facts must fail open")
	}
}
func TestRegionF64PrefilterGate(t *testing.T) {
	for _, interruptible := range []bool{false, true} {
		for _, knownAbsent := range []bool{false, true} {
			for _, depth := range []int{0, 1, 2} {
				f := fn{interruptible: interruptible, regionNoF64Store: knownAbsent, inlineDepth: depth}
				want := boundedVectorPolls && interruptible && knownAbsent && depth == 0
				if f.skipRegionWithoutF64Store() != want {
					t.Fatalf("gate interruptible=%v absent=%v depth=%d", interruptible, knownAbsent, depth)
				}
			}
		}
	}
	if boundedVectorPolls {
		stats := &CodegenStats{}
		f := fn{interruptible: true, regionNoF64Store: true, stats: stats}
		r := wasm.ReaderFrom([]byte{0x40, 0x0b})
		ok, err := f.tryRegionLoop(&r)
		if ok || err != nil || r.Offset() != 0 {
			t.Fatal("skip touched input")
		}
		if diagnosticsEnabled && stats.Peephole["region-skip-no-f64-store"] != 1 {
			t.Fatal("missing skip counter")
		}
	}
}
