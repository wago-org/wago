//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func TestTailDynamicRV64NilColdAndOrdinaryContinuation(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, nilDirections := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-nil%t", shared, nilDirections), func(t *testing.T) {
				values := []wasm.ValType{wasm.I64}
				if nilDirections {
					values = nil
				}
				sig := &wasm.CompType{Kind: wasm.CompFunc, Params: values, Results: values}
				f, view := tailInvocationFixtureForTest(t, shared, len(values), len(values))
				a := tailRANativeFrameInvocation(t, sig, f.a, f.ca, f.da, 0, true, func(e *integerEmitter) {
					tailDynamicReferenceForTest(e, tailRAPointer(f.witness)+224)
					e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(48 + i*16) })
				})
				leaf := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 0, true, func(e *integerEmitter) { tailRANativeSerialize(e, values, 0) })
				cold := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 1, true, func(e *integerEmitter) { e.emitProductionTrap(abi.TrapUnreachable, 53) })
				oa := tailDynamicOwnerForTest(t, []tailRAFunction{a})
				ob := tailDynamicOwnerForTest(t, []tailRAFunction{leaf, cold})
				a = oa.functions[0]
				f.da, f.db = oa.descriptors, ob.descriptors
				// This genuine framed caller resumes after its linked call into the
				// tailing function. Result+7 distinguishes the saved continuation.
				outer := tailRANativeFrameInvocation(t, sig, f.a, f.ca, f.da, 2, true, func(e *integerEmitter) {
					e.restoreCallTransport()
					e.swarCallArguments(sig, 0, func(i int) int32 { return int32(48 + i*16) })
					e.a.Constant64(enc.X28, int64(a.code.Base()+uintptr(a.info.Entry)))
					e.a.Jalr(enc.RA, enc.X28, 0)
					e.restoreCallTransport()
					if !nilDirections {
						e.a.Ld(enc.X5, enc.X13, 0)
						e.a.Addi(enc.X5, enc.X5, 7)
						e.a.Sd(enc.X5, enc.X13, 0)
					}
				})
				root := tailRANativeRoot(t, outer, f.witness)
				for _, code := range []*coreruntime.CodeBuffer{a.code, leaf.code, cold.code, outer.code, root} {
					if err := code.Seal(); err != nil {
						t.Fatal(err)
					}
				}
				s := tailDynamicSiteForTest(t, a, []tailRADynamicOwner{ob}, a.info.FrameBytes-8)
				s.Continuations = append(s.Continuations, NativeTailCodeRange{outer.code.Base(), uintptr(len(outer.snapshot))}, NativeTailCodeRange{root.Base(), uintptr(len(root.Bytes()))})
				o := tailDynamicBindForTest(t, s, len(a.snapshot)+len(leaf.snapshot)+len(cold.snapshot)+len(outer.snapshot)+len(root.Bytes()))
				for _, target := range []int{1, 0, 1, 0} {
					binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(ob.descriptors)+uintptr(40*(target+1))))
					if !nilDirections {
						binary.LittleEndian.PutUint64(f.args, 49)
						binary.LittleEndian.PutUint64(f.results, 0xfeeddeadbeeffeed)
					}
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					err := f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
					if target == 1 {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != abi.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].ProgramCounter != 53 {
							t.Fatal("cold target attribution", err)
						}
						if !nilDirections && binary.LittleEndian.Uint64(f.results) != 0xfeeddeadbeeffeed {
							t.Fatal("cold hop published result")
						}
					} else {
						if err != nil {
							t.Fatal("cold reuse", err)
						}
						if !nilDirections && binary.LittleEndian.Uint64(f.results) != 56 {
							t.Fatal("ordinary continuation skipped")
						}
						f.assertReturned(t)
						if f.a.RV64InvocationView() != view {
							t.Fatal("original parent view lost")
						}
					}
					f.assertCanaries(t)
				}
				if err := o.Close(); err != nil {
					t.Fatal(err)
				}
				if binary.LittleEndian.Uint64(f.ca[120:]) != 0 || o.dynamic != nil || o.contexts != nil {
					t.Fatal("retired dynamic owner retained borrowed bindings")
				}
			})
		}
	}
}
