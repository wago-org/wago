//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	enc "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
)

func sparseNativeOwner(t *testing.T, functions []tailRAFunction) (*NativeTailDefinedOwner, tailRAFunction) {
	t.Helper()
	var code []byte
	entries := make([]int, len(functions))
	for i, f := range functions {
		for len(code)&15 != 0 {
			code = append(code, 0x13, 0, 0, 0)
		}
		entries[i] = len(code)
		code = append(code, f.snapshot...)
	}
	buffer := tailRAProofBuffer(t, code)
	for i := range functions {
		functions[i].code, functions[i].snapshot, functions[i].info.Entry = buffer, code, entries[i]
		end := len(code)
		if i+1 < len(functions) {
			end = entries[i+1]
		}
		functions[i].info.CodeBytes = end - entries[i]
	}
	o := tailDynamicOwnerForTest(t, functions)
	for _, i := range []int{0, 2} {
		clear(o.descriptors[40*(i+1) : 40*(i+2)])
		clear(o.directory[32+32*i : 32+32*(i+1)])
	}
	fn, _ := tailPublishedFunctionForTest(t, o.functions[1])
	owner, err := NewNativeTailSparseDefinedOwner(fn.published, fn.job, o.context, o.descriptors, o.directory, entries, []*wasm.CompType{nil, fn.signature, nil})
	if err != nil {
		t.Fatal(err)
	}
	return owner, owner.owner.functions[1]
}

func TestNativeTailSparseRV64SelectionAndBinding(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-vector%t", shared, vector), func(t *testing.T) {
				payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				if vector {
					payload = append(payload, wasm.V128)
				}
				sig := &wasm.CompType{Kind: wasm.CompFunc, Params: payload, Results: payload}
				f, view := tailInvocationFixtureForTest(t, shared, swarSignatureSlots(payload), swarSignatureSlots(payload))
				makeOwner := func(caller bool) (*NativeTailDefinedOwner, tailRAFunction) {
					job, context, dir := f.b, f.cb, f.db
					if caller {
						job, context, dir = f.a, f.ca, f.da
					}
					functions := make([]tailRAFunction, 3)
					for i := range functions {
						functions[i] = tailRANativeFrameInvocationLocals(t, sig, job, context, dir, uint32(i), true, i*2, func(e *integerEmitter) {
							if i != 1 {
								e.a.Constant64(enc.X29, int64(tailRAPointer(f.witness)+240))
								e.a.Addi(enc.X5, enc.Zero, 1)
								e.a.Sd(enc.X5, enc.X29, 0)
							} else if caller {
								tailDynamicReferenceForTest(e, tailRAPointer(f.witness)+224)
								e.tailRAProofCall(0, sig, 0, func(i int) int32 { return int32(callFrameHeader + i*16) })
							}
							tailRANativeSerialize(e, payload, 0)
						})
					}
					return sparseNativeOwner(t, functions)
				}
				callerOwner, caller := makeOwner(true)
				targetOwner, target := makeOwner(false)
				f.da, f.db = callerOwner.owner.descriptors, targetOwner.owner.descriptors
				root := tailRANativeRoot(t, caller, f.witness)
				if err := root.Seal(); err != nil {
					t.Fatal(err)
				}
				site := tailDynamicSiteForTest(t, caller, nil, caller.info.FrameBytes-8)
				site.dynamic = nil
				site.Continuations = append(site.Continuations, NativeTailCodeRange{root.Base(), uintptr(len(root.Bytes()))})
				site, err := NativeTailDefinedSite(site, caller.info.FrameBytes-8, []*NativeTailDefinedOwner{targetOwner})
				if err != nil {
					t.Fatal(err)
				}
				dispatcher := tailDynamicBindForTest(t, site, len(caller.snapshot)+len(target.snapshot)+len(root.Bytes()))
				input := make([]byte, len(f.args))
				var want []uint64
				cell := 0
				for _, typ := range payload {
					v := uint64(0xffffaaaa80000001) + uint64(cell)*0x101010101
					binary.LittleEndian.PutUint64(input[8*cell:], v)
					cell++
					w := v
					if swarClassOf(typ).narrow() {
						w = uint64(uint32(v))
					}
					want = append(want, w)
					if wasm.EqualValType(typ, wasm.V128) {
						binary.LittleEndian.PutUint64(input[8*cell:], ^v)
						cell++
						want = append(want, ^v)
					}
				}
				invoke := func(index int) error {
					copy(f.args, input)
					for i := range f.results {
						f.results[i] = 0xa5
					}
					clear(f.trap)
					clear(f.witness[240:248])
					binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(f.db)+uintptr(40*(index+1))))
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					return f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
				}
				check := func() {
					t.Helper()
					for i, v := range want {
						if binary.LittleEndian.Uint64(f.results[8*i:]) != v {
							t.Fatal("callable target after hole lost payload", i)
						}
					}
					if binary.LittleEndian.Uint64(f.witness[240:]) != 0 {
						t.Fatal("entered noncallable physical function")
					}
					f.assertReturned(t)
				}
				if err := dispatcher.Detach(); err != nil {
					t.Fatal(err)
				}
				// Ordinal zero is deliberately noncallable, but owns the same exact
				// caller job/context and therefore still authenticates Bind.
				if err := dispatcher.Bind(callerOwner); err != nil {
					t.Fatal("sparse caller rebinding", err)
				}
				if err := invoke(1); err != nil {
					t.Fatal("callable target after hole", err)
				}
				check()
				if n := testing.AllocsPerRun(4, func() {
					if err := dispatcher.Detach(); err != nil {
						panic(err)
					}
					if err := dispatcher.Bind(callerOwner); err != nil {
						panic(err)
					}
					if err := invoke(1); err != nil {
						panic(err)
					}
				}); n != 0 {
					t.Fatal("warm sparse binding/dispatch allocated", n)
				}
				for _, index := range []int{0, 2} {
					for _, rewritten := range []bool{false, true} {
						desc := f.db[40*(index+1) : 40*(index+2)]
						row := targetOwner.owner.directory[32+32*index : 32+32*(index+1)]
						if rewritten {
							// Even a coordinated live row replacement cannot create a
							// genuine frozen target at a noncallable ordinal.
							copy(desc, f.db[80:120])
							copy(row, targetOwner.owner.directory[64:96])
							binary.LittleEndian.PutUint64(desc[24:], uint64(tailRAPointer(desc)))
						}
						err := invoke(index)
						clear(desc)
						clear(row)
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != abi.TrapCalledFnNotLinked || len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != 37 || !bytes.Equal(f.args, input) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) || binary.LittleEndian.Uint64(f.witness[240:]) != 0 {
							t.Fatal("noncallable target prewrite trap", index, rewritten, err)
						}
						f.assertCanaries(t)
						if err := invoke(1); err != nil {
							t.Fatal("sparse target trap reuse", err)
						}
						check()
					}
				}
			})
		}
	}
}
