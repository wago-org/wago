//go:build amd64

package amd64

import (
	"testing"
	"unsafe"

	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestGCPureIfProofBounds(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []byte
		reg  Reg
		want bool
	}{
		{"void", []byte{0x40, 0x0b}, RBP, true},
		{"else", []byte{0x40, 0x41, 1, 0x1a, 0x05, 0x42, 2, 0x1a, 0x0b}, RBP, true},
		{"result-conflict", []byte{0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b}, RBP, false},
		{"result-other-register", []byte{0x7f, 0x41, 1, 0x05, 0x41, 2, 0x0b}, R12, false},
		{"nested", []byte{0x40, 0x02, 0x40, 0x0b, 0x0b}, RBP, false},
		{"call", []byte{0x40, 0x10, 0, 0x0b}, RBP, false},
		{"assign", []byte{0x40, 0x41, 1, 0x21, 0, 0x0b}, RBP, false},
		{"gc", []byte{0x40, 0xfb, 2, 0, 0, 0x0b}, RBP, false},
		{"truncated", []byte{0x40, 0x41, 0x80}, RBP, false},
		{"repeat-else", []byte{0x40, 0x05, 0x05, 0x0b}, RBP, false},
		{"no-end", []byte{0x40, 0x41, 1, 0x1a}, RBP, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := fn{tracePCBase: 10, wasmPC: 10, gcResolved: gcResolvedObject{valid: true, reg: tc.reg}}
			r := wasm.ReaderFrom(tc.code)
			f.prepareGCPureIf(&r, 0x04, 0)
			if got := f.gcPureIfEndPC != 0; got != tc.want {
				t.Fatalf("admitted%v want%v", got, tc.want)
			}
			if r.Offset() != 0 {
				t.Fatal("lookahead advanced source reader")
			}
			if tc.want {
				for _, op := range []byte{0x04, 0x05, 0x0b} {
					f.prepareGCResolvedObject(op)
					if !f.gcResolved.valid {
						t.Fatal("lost entry certificate")
					}
				}

				f.wasmPC = f.gcPureIfEndPC + 1
				f.prepareGCResolvedObject(0x41)
				if f.gcPureIfEndPC != 0 || !f.gcResolved.valid {
					t.Fatal("past-end proof did not expire while preserving straight-line certificate")
				}
				f.prepareGCResolvedObject(0x05)
				if f.gcResolved.valid {
					t.Fatal("outer control retained address")
				}
				f.invalidateGCResolvedObject()
				if f.gcPureIfEndPC != 0 {
					t.Fatal("invalidation retained span")
				}
			}
		})
	}
	for _, n := range []int{31, 32, 63, 64, 256} {
		code := []byte{0x40}
		for i := 0; i < n; i++ {
			code = append(code, 0x1a)
		}
		code = append(code, 0x0b)
		f := fn{tracePCBase: 10, gcResolved: gcResolvedObject{valid: true, reg: RBP}}
		r := wasm.ReaderFrom(code)
		f.prepareGCPureIf(&r, 4, 0)
		if (f.gcPureIfEndPC != 0) != (n < 32) {
			t.Fatalf("incorrect admission for %d operations", n)
		}
	}
	for _, mode := range []string{"budget", "inline", "custom", "unreachable", "eh"} {
		f := fn{tracePCBase: 10, gcResolved: gcResolvedObject{valid: true, reg: RBP}}
		minCtrl := 0
		switch mode {
		case "budget":
			f.gcPureIfSites = 16
		case "inline":
			minCtrl = 1
		case "custom":
			f.customInstructions = map[uint32]CustomInstruction{0: {}}
		case "unreachable":
			f.unreachable = true
		case "eh":
			f.moduleEH = true
		}
		r := wasm.ReaderFrom([]byte{0x40, 0x0b})
		f.prepareGCPureIf(&r, 4, minCtrl)
		if f.gcPureIfEndPC != 0 {
			t.Fatal(mode)
		}
	}

	code := []byte{0x40}
	for i := 0; i < 6; i++ {
		code = append(code, 0x42, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x80, 0x00, 0x1a)
	}
	code = append(code, 0x0b)
	bounded := fn{tracePCBase: 10, gcResolved: gcResolvedObject{valid: true, reg: RBP}}
	reader := wasm.ReaderFrom(code)
	bounded.prepareGCPureIf(&reader, 4, 0)
	if bounded.gcPureIfEndPC != 0 {
		t.Fatal("byte limit exceeded")
	}

	offsetReader := wasm.ReaderFrom([]byte{0, 0, 0, 0, 0, 0x40, 0x0b})
	if err := offsetReader.JumpTo(5); err != nil {
		t.Fatal(err)
	}
	offsetFn := fn{tracePCBase: 10, wasmPC: 14, gcResolved: gcResolvedObject{valid: true, reg: RBP}}
	offsetFn.prepareGCPureIf(&offsetReader, 4, 0)
	if offsetReader.Offset() != 5 || offsetFn.gcPureIfEndPC != 16 {
		t.Fatal("source PC offset lost")
	}
	var f fn
	if unsafe.Offsetof(f.gcHandleResolutions) != unsafe.Offsetof(f.gcDynamicCopySites)+6 {
		t.Fatal("pure-if fields exceeded existing padding")
	}
}
