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

// Add a genuine native import ahead of the three physical local definitions.
// Internal frames stay at absolute ordinals 1 and 3; ordinal 2 is callable.
func sparseNativeImportedOwner(t *testing.T, local *NativeTailOwner, remote tailRAFunction) (*NativeTailOwner, int) {
	t.Helper()
	source := local.owner.functions[1]
	proxy, proof := tailDynamicProxyForTest(t, source, remote, 0)
	functions := []tailRAFunction{proxy}
	entries := make([]int, len(local.owner.functions))
	for i, f := range local.owner.functions {
		entries[i] = f.info.Entry
		f.signature = source.signature // Temporary canonical rows, cleared below.
		functions = append(functions, f)
	}
	o := tailDynamicOwnerForTest(t, functions)
	for n, v := range []uint64{uint64(remote.codeBase() + uintptr(remote.info.Entry)), uint64(remote.job.LinMemBase()), uint64(tailRAPointer(remote.context)), 0} {
		binary.LittleEndian.PutUint64(o.directory[32+8*n:], v)
	}
	for _, i := range []int{1, 3} {
		clear(o.descriptors[40*(i+1) : 40*(i+2)])
		clear(o.directory[32+32*i : 32+32*(i+1)])
	}
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	callee := NativeImportTarget{Entry: remote.codeBase() + uintptr(remote.info.Entry), Home: remote.job.LinMemBase(), Context: tailRAPointer(remote.context), Arguments: proof.arguments, Results: proof.results, ArgumentSlots: swarSignatureSlots(remote.signature.Params), ResultSlots: swarSignatureSlots(remote.signature.Results), StackOwnedActivation: true, InvocationContext: true, HostTrampoline: trampoline}
	bridge, certificate, err := NativeImportThunkWithCertificate(callee, source.job.LinMemBase(), tailRAPointer(o.context), 0)
	if err != nil || !bytes.Equal(bridge, proxy.snapshot) {
		t.Fatal("issued native bridge", err)
	}
	proxy, _ = tailPublishedFunctionForTest(t, o.functions[0])
	remote, _ = tailPublishedFunctionForTest(t, remote)
	genuine, err := NewNativeTailFunction(remote.published, remote.job, remote.context, remote.directory, remote.info.Entry, remote.info.Entry+remote.info.CodeBytes, remote.info.mode.functionIndex, *remote.signature)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := NewNativeTailSparseImportedOwner(source.published, source.job, o.context, o.descriptors, o.directory, entries, []*wasm.CompType{source.signature, nil, source.signature, nil}, []NativeTailImport{{Image: proxy.published, CodeBytes: len(bridge), Target: genuine, Arguments: proof.arguments, Results: proof.results, Certificate: certificate}}, trampoline)
	if err != nil {
		t.Fatal(err)
	}
	return owner, len(bridge) + len(remote.snapshot)
}

func TestNativeTailSparseRV64SelectionAndBinding(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, mode := range [][2]bool{{false, false}, {true, false}, {false, true}, {true, true}} {
			vector, imported := mode[0], mode[1]
			t.Run(fmt.Sprintf("shared%t-vector%t-imported%t", shared, vector, imported), func(t *testing.T) {
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
							} else {
								e.a.Constant64(enc.X29, int64(tailRAPointer(f.witness)+248))
								e.a.Addi(enc.X5, enc.Zero, 22)
								e.a.Sd(enc.X5, enc.X29, 0)
							}
							tailRANativeSerialize(e, payload, 0)
						})
					}
					return sparseNativeOwner(t, functions)
				}
				callerOwner, caller := makeOwner(true)
				targetOwner, target := makeOwner(false)
				f.da, f.db = callerOwner.owner.descriptors, targetOwner.owner.descriptors
				callable, holes, extraBytes := 1, []int{0, 2}, 0
				if imported {
					remote := tailRANativeFrameInvocation(t, sig, f.a, f.ca, f.da, 0, true, func(e *integerEmitter) {
						e.a.Constant64(enc.X29, int64(tailRAPointer(f.witness)+248))
						e.a.Addi(enc.X5, enc.Zero, 33)
						e.a.Sd(enc.X5, enc.X29, 0)
						tailRANativeSerialize(e, payload, 0)
					})
					targetOwner, extraBytes = sparseNativeImportedOwner(t, targetOwner, remote)
					f.db = targetOwner.owner.descriptors
					callable, holes = 2, []int{1, 3}
				}
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
				dispatcher := tailDynamicBindForTest(t, site, len(caller.snapshot)+len(target.snapshot)+len(root.Bytes())+extraBytes)
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
					clear(f.witness[240:256])
					binary.LittleEndian.PutUint64(f.witness[224:], uint64(tailRAPointer(f.db)+uintptr(40*(index+1))))
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					return f.e.CallPrepared(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results)
				}
				check := func(identity uint64) {
					t.Helper()
					for i, v := range want {
						if binary.LittleEndian.Uint64(f.results[8*i:]) != v {
							t.Fatal("callable target after hole lost payload", i)
						}
					}
					if binary.LittleEndian.Uint64(f.witness[240:]) != 0 {
						t.Fatal("entered noncallable physical function")
					}
					if got := binary.LittleEndian.Uint64(f.witness[248:]); got != identity {
						t.Fatal("selected wrong callable identity", got, identity)
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
				if err := invoke(callable); err != nil {
					t.Fatal("callable target after hole", err)
				}
				check(22)
				if imported {
					if err := invoke(0); err != nil {
						t.Fatal("certified native import selected", err)
					}
					check(33)
				}
				if n := testing.AllocsPerRun(4, func() {
					if err := dispatcher.Detach(); err != nil {
						panic(err)
					}
					if err := dispatcher.Bind(callerOwner); err != nil {
						panic(err)
					}
					if err := invoke(callable); err != nil {
						panic(err)
					}
					if binary.LittleEndian.Uint64(f.witness[248:]) != 22 {
						panic("warm local identity")
					}
					if imported {
						if err := invoke(0); err != nil {
							panic(err)
						}
						if binary.LittleEndian.Uint64(f.witness[248:]) != 33 {
							panic("warm imported identity")
						}
					}
				}); n != 0 {
					t.Fatal("warm sparse binding/dispatch allocated", n)
				}
				for _, index := range holes {
					for _, rewritten := range []bool{false, true} {
						desc := f.db[40*(index+1) : 40*(index+2)]
						row := targetOwner.owner.directory[32+32*index : 32+32*(index+1)]
						if rewritten {
							// Even a coordinated live row replacement cannot create a
							// genuine frozen target at a noncallable ordinal.
							copy(desc, f.db[40*(callable+1):40*(callable+2)])
							copy(row, targetOwner.owner.directory[32+32*callable:32+32*(callable+1)])
							binary.LittleEndian.PutUint64(desc[24:], uint64(tailRAPointer(desc)))
						}
						err := invoke(index)
						clear(desc)
						clear(row)
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != abi.TrapCalledFnNotLinked || len(trap.Frames) != 1 || trap.Frames[0].ProgramCounter != 37 || !bytes.Equal(f.args, input) || !bytes.Equal(f.results, bytes.Repeat([]byte{0xa5}, len(f.results))) || binary.LittleEndian.Uint64(f.witness[240:]) != 0 {
							t.Fatal("noncallable target prewrite trap", index, rewritten, err)
						}
						if binary.LittleEndian.Uint64(f.witness[248:]) != 0 {
							t.Fatal("hole trap entered callable body")
						}
						f.assertCanaries(t)
						if err := invoke(callable); err != nil {
							t.Fatal("sparse target trap reuse", err)
						}
						check(22)
						if imported {
							if err := invoke(0); err != nil {
								t.Fatal("imported target trap reuse", err)
							}
							check(33)
						}
					}
				}
			})
		}
	}
}
