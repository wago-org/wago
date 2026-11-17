//go:build amd64

package amd64

import (
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"testing"
)

func TestAllocationCellsLoopParity(t *testing.T) {
	bodies := [][]byte{{0x0b}, {0x21, 1, 0x0b}, {0x03, 0x40, 0x21, 1, 0x0b, 0x0b}, {0x10, 0, 0x0b}, {0x43, 0, 0, 0x80, 0x3f, 0x43, 0, 0, 0, 0x40, 0x0b}, {0x44, 0, 0, 0, 0, 0, 0, 0xf0, 0x3f, 0x0b}, {0x41, 0x80}, {0xfc, 0x0b, 0, 0x0b}, {0x02, 0x40, 0x43, 0, 0, 0, 0, 0x0b, 0x0b}}
	// Exercise candidate saturation, step cutoff, ties and float-unsafe helpers.
	var floats []byte
	for i := 0; i < 300; i++ {
		floats = append(floats, 0x43, byte(i%20), 0, 0, 0)
	}
	bodies = append(bodies, append(floats, 0x0b), []byte{0x43, 0, 0, 0, 0, 0x26, 0, 0x0b})
	var seed uint32 = 17
	for i := 0; i < 300; i++ {
		b := make([]byte, i%129)
		for j := range b {
			seed = seed*1664525 + 1013904223
			b[j] = byte(seed >> 24)
		}
		bodies = append(bodies, b)
	}
	for bi, body := range bodies {
		for _, wantFloat := range []bool{false, true} {
			for _, initial := range []int{0, 1, maxCallFreeLoopLookaheadBytes - 2, maxCallFreeLoopLookaheadBytes - 1, maxCallFreeLoopLookaheadBytes} {
				ra, rb := wasm.NewReader(body), wasm.NewReader(body)
				ba, bb := initial, initial
				a, w, st, f := scanLoopCallFreeDetails(ra, wasm.ModuleInstructionClassifier{}, false, []int{0, 1, 65}, wantFloat, &ba)
				oa, ow, os, of := oldAllocationScanLoopCallFreeDetails(rb, wasm.ModuleInstructionClassifier{}, false, []int{0, 1, 65}, wantFloat, &bb)
				if a != oa || w != ow || st != os || f != of || ba != bb || ra.Offset() != rb.Offset() {
					t.Fatalf("body=%d float=%v budget=%d parity differs", bi, wantFloat, initial)
				}
			}
		}
	}
}

func TestAllocationCellsAddressBudgetParity(t *testing.T) {
	f := &fn{}
	for _, typ := range []machineType{mtI32, mtI64} {
		nodes := []*elem{nil, {st: storage{kind: stConst, typ: typ, cval: -1}}, {st: storage{kind: stConst, typ: typ, cval: 7}}, {st: storage{kind: stReg, typ: typ}}}
		for i := 0; i < 40; i++ {
			e := &elem{st: storage{typ: typ}, arg0: nodes[len(nodes)-1], arg1: nodes[2]}
			e.setElemKind(ekDeferred)
			e.setDeferredOp([]wOp{opAdd, opAnd, opOr, opXor, opMul, opShl, opShrU}[i%7])
			nodes = append(nodes, e)
		}
		for _, e := range nodes {
			for _, wide := range []bool{false, true} {
				for budget := -1; budget <= 35; budget++ {
					oldBudget := budget
					want := f.oldAllocationUnsignedAddressMaximum(e, wide, &oldBudget)
					got, left := f.unsignedAddressMaximum(e, wide, budget)
					if got != want || left != oldBudget {
						t.Fatalf("wide=%v budget=%d got=%d/%d want=%d/%d", wide, budget, got, left, want, oldBudget)
					}
				}
			}
		}
	}
}
