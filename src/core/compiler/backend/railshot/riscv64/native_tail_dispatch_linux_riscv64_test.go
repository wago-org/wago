//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"encoding/binary"
	"errors"
	"fmt"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"testing"
)

func TestTailInvocationInstanceFactoriesReuseOneRecord(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar%t-shared%t", swar, shared), func(t *testing.T) {
				payload := []wasm.ValType{wasm.I32, wasm.F32, wasm.I64, wasm.F64}
				if swar {
					payload = append(payload, wasm.V128)
				}
				params := append([]wasm.ValType{wasm.I64}, payload...)
				source := tailImportTestSource(params, payload, tailImportCountdownBody(len(payload)))
				a, err := compileTailImportSourceInvocation(source, 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				b, err := compileTailImportSourceInvocation(source, 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				f := tailRANativeFixtureNew(t, shared, swarSignatureSlots(params), swarSignatureSlots(payload))
				ar, err := coreruntime.NewArena(4096)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() {
					if err := ar.Close(); err != nil {
						t.Error(err)
					}
				})
				ca, cb := ar.Alloc(abi.RV64InstanceContextBytes), ar.Alloc(abi.RV64InstanceContextBytes)
				copy(ca, f.ca)
				copy(cb, f.cb)
				f.ca, f.cb = ca, cb
				binary.LittleEndian.PutUint64(f.da[32:], uint64(tailRAPointer(ca)))
				binary.LittleEndian.PutUint64(f.db[32:], uint64(tailRAPointer(cb)))
				f.bind(t, f.e.StackLimit())
				rootView := f.e.RV64InvocationViewPointer()
				f.a.SetRV64InvocationView(rootView)
				af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
				if err := af[0].code.Seal(); err != nil {
					t.Fatal(err)
				}
				if err := bf[0].code.Seal(); err != nil {
					t.Fatal(err)
				}
				trampoline, err := coreruntime.HostCallTrampolinePointer()
				if err != nil {
					t.Fatal(err)
				}
				bind := func(image *tailImportImage, caller, target tailRAFunction) *NativeTailDispatcher {
					var sites []NativeTailSite
					for _, p := range image.plans {
						sites = append(sites, NativeTailSite{CallerHome: caller.job.LinMemBase(), CallerContext: caller.context, TargetContext: target.context, Target: NativeImportTarget{Entry: target.code.Base() + uintptr(target.info.Entry), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), HostTrampoline: trampoline, InvocationContext: true, StackOwnedActivation: true}, FrameBytes: caller.info.FrameBytes, SourceBase: p.sourceBase, SourceStride: p.sourceStride, FunctionIndex: p.functionIndex, SourcePC: p.sourcePC, Parameters: target.signature.Params, Results: target.signature.Results, Continuations: []NativeTailCodeRange{{caller.code.Base(), uintptr(len(caller.snapshot))}}})
					}
					o, err := NewNativeTailDispatcher(sites, len(image.code))
					if o != nil {
						t.Cleanup(func() {
							if err := o.Close(); err != nil {
								t.Error(err)
							}
						})
					}
					if err != nil {
						t.Fatal(err)
					}
					return o
				}
				oa, ob := bind(a, af[0], bf[0]), bind(b, bf[0], af[0])
				if oa.code == ob.code || tailRAPointer(oa.capability) == tailRAPointer(ob.capability) {
					t.Fatal("factories not instance owned")
				}
				exit1, err := nativeTailExitPointer()
				if err != nil {
					t.Fatal(err)
				}
				exit2, err := nativeTailExitPointer()
				if err != nil || exit1 == 0 || exit1 != exit2 {
					t.Fatal("shared exit identity", err)
				}
				for _, hops := range []uint64{1, 2049, 2} {
					binary.LittleEndian.PutUint64(f.args, hops)
					cell := 1
					var want []uint64
					for n, typ := range payload {
						value := uint64(0xffffaaaa7f800001) ^ uint64(n)*0x100010001
						binary.LittleEndian.PutUint64(f.args[cell*8:], value)
						cell++
						if swarClassOf(typ).narrow() {
							want = append(want, uint64(uint32(value)))
						} else {
							want = append(want, value)
						}
						if wasm.EqualValType(typ, wasm.V128) {
							binary.LittleEndian.PutUint64(f.args[cell*8:], 0xa5a5deadbeeffeed)
							cell++
							want = append(want, 0xa5a5deadbeeffeed)
						}
					}
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(rootView)
					if err := f.e.CallPrepared(af[0].code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
						t.Fatal("public view tail chain", hops, err)
					}
					for n, v := range want {
						if got := binary.LittleEndian.Uint64(f.results[n*8:]); got != v {
							t.Fatalf("hops%d cell%d=%x want%x", hops, n, got, v)
						}
					}
					if f.a.RV64InvocationView() != rootView {
						t.Fatal("exit lost exact parent view")
					}
					// This stack has far less room than2049 distinct5568-byte records. A
					// per-instance exit identity would hit its fence rather than complete.
				}

				if allocations := testing.AllocsPerRun(10, func() {
					binary.LittleEndian.PutUint64(f.args, 1)
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(rootView)
					if err := f.e.CallPrepared(af[0].code.Base(), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil {
						panic(err)
					}
				}); allocations != 0 {
					t.Fatal("warm tail bridge allocations", allocations)
				}
			})
		}
	}
}

func bindTailInvocationImageForTest(t *testing.T, image *tailImportImage, functions, targets []tailRAFunction) *NativeTailDispatcher {
	t.Helper()
	trampoline, err := coreruntime.HostCallTrampolinePointer()
	if err != nil {
		t.Fatal(err)
	}
	var sites []NativeTailSite
	for _, p := range image.plans {
		caller, target := functions[int(p.functionIndex)-image.imports], targets[p.importIndex]
		sites = append(sites, NativeTailSite{CallerHome: caller.job.LinMemBase(), CallerContext: caller.context, TargetContext: target.context, Target: NativeImportTarget{Entry: target.code.Base() + uintptr(target.info.Entry), Home: target.job.LinMemBase(), Context: tailRAPointer(target.context), HostTrampoline: trampoline, InvocationContext: true, StackOwnedActivation: true}, FrameBytes: caller.info.FrameBytes, SourceBase: p.sourceBase, SourceStride: p.sourceStride, FunctionIndex: p.functionIndex, SourcePC: p.sourcePC, Parameters: target.signature.Params, Results: target.signature.Results, Continuations: []NativeTailCodeRange{{caller.code.Base(), uintptr(len(caller.snapshot))}}})
	}
	owner, err := NewNativeTailDispatcher(sites, len(image.code))
	if owner != nil {
		t.Cleanup(func() {
			if err := owner.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	if err != nil {
		t.Fatal(err)
	}
	return owner
}

func tailInvocationFixtureForTest(t *testing.T, shared bool, args, results int) (*tailRANativeFixture, uintptr) {
	t.Helper()
	f := tailRANativeFixtureNew(t, shared, args, results)
	ar, err := coreruntime.NewArena(4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := ar.Close(); err != nil {
			t.Error(err)
		}
	})
	ca, cb := ar.Alloc(abi.RV64InstanceContextBytes), ar.Alloc(abi.RV64InstanceContextBytes)
	copy(ca, f.ca)
	copy(cb, f.cb)
	f.ca, f.cb = ca, cb
	binary.LittleEndian.PutUint64(f.da[32:], uint64(tailRAPointer(ca)))
	binary.LittleEndian.PutUint64(f.db[32:], uint64(tailRAPointer(cb)))
	f.bind(t, f.e.StackLimit())
	view := f.e.RV64InvocationViewPointer()
	f.a.SetRV64InvocationView(view)
	return f, view
}

func TestTailInvocationOrdinaryCallerContinuesAfterForeignTail(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar%t-shared%t", swar, shared), func(t *testing.T) {
				values := []wasm.ValType{wasm.I64}
				// Function[1] tails to import[0]. Root function[2] calls it normally,
				// then adds7, proving that the retained ordinary continuation survives.
				source := tailImportTestSource(values, values, []byte{0x20, 0, 0x12, 0, 0x0b}, []byte{0x20, 0, 0x10, 1, 0x42, 7, 0x7c, 0x0b})
				a, err := compileTailImportSourceInvocation(source, 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				b, err := compileTailImportSourceInvocation(tailImportTestSource(values, values, []byte{0x20, 0, 0x0b}), 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				f, view := tailInvocationFixtureForTest(t, shared, 1, 1)
				af, bf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db)
				for _, fn := range []tailRAFunction{af[0], bf[0]} {
					if err := fn.code.Seal(); err != nil {
						t.Fatal(err)
					}
				}
				bindTailInvocationImageForTest(t, a, af, []tailRAFunction{bf[0]})
				for range 3 {
					binary.LittleEndian.PutUint64(f.args, 49)
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					if err := f.e.CallPrepared(af[1].code.Base()+uintptr(af[1].info.Entry), f.args, f.a.LinMemBase(), f.trap, f.results); err != nil || binary.LittleEndian.Uint64(f.results) != 56 {
						t.Fatal("ordinary continuation", err)
					}
					if f.a.RV64InvocationView() != view {
						t.Fatal("ordinary exit lost original view")
					}
				}
			})
		}
	}
}

func TestTailInvocationNilDirectionsColdCleanupAndReuse(t *testing.T) {
	for _, swar := range []bool{false, true} {
		for _, cold := range []bool{false, true} {
			t.Run(fmt.Sprintf("swar%t-cold%t", swar, cold), func(t *testing.T) {
				a, err := compileTailImportSourceInvocation(tailImportTestSource(nil, nil, []byte{0x12, 0, 0x0b}), 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				body := []byte{0x0b}
				if cold {
					body = []byte{0, 0x0b}
				}
				b, err := compileTailImportSourceInvocation(tailImportTestSource(nil, nil, body), 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				leaf, err := compileTailImportSourceInvocation(tailImportTestSource(nil, nil, []byte{0x0b}), 0, swar, true)
				if err != nil {
					t.Fatal(err)
				}
				f, view := tailInvocationFixtureForTest(t, false, 0, 0)
				af, bf, lf := tailImportNativeFunctions(t, a, f.a, f.ca, f.da), tailImportNativeFunctions(t, b, f.b, f.cb, f.db), tailImportNativeFunctions(t, leaf, f.a, f.ca, f.da)
				for _, fn := range []tailRAFunction{af[0], bf[0], lf[0]} {
					if err := fn.code.Seal(); err != nil {
						t.Fatal(err)
					}
				}
				bindTailInvocationImageForTest(t, a, af, []tailRAFunction{bf[0]})
				for range 2 {
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					err := f.e.CallPrepared(af[0].code.Base(), nil, f.a.LinMemBase(), f.trap, nil)
					if cold {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != abi.TrapUnreachable {
							t.Fatal("cold nil tail", err)
						}
					} else if err != nil || f.a.RV64InvocationView() != view {
						t.Fatal("nil tail return", err)
					}
					f.bind(t, f.e.StackLimit())
					f.a.SetRV64InvocationView(view)
					if err := f.e.CallPrepared(lf[0].code.Base(), nil, f.a.LinMemBase(), f.trap, nil); err != nil {
						t.Fatal("cold Engine reuse", err)
					}
				}
			})
		}
	}
}
