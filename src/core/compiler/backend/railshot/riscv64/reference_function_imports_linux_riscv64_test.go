//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package riscv64

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The last private call receives both the lower reference operand and the saved
// reference local. Its argument arena exposes exact arriving bits to this
// internal execution oracle; no reference-valued public export is admitted.
func referenceImportCarrierSource(vector, tails bool) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	types := [][]byte{
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, wasm.I32}, []wasm.ValType{typ, wasm.I32, wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.FuncRef, wasm.I32, typ}, []wasm.ValType{typ, wasm.I32, wasm.FuncRef}),
		wasmtest.FuncType([]wasm.ValType{wasm.FuncRef, wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
		wasmtest.FuncType(nil, nil),
	}
	imports := [][]byte{}
	for _, name := range []string{"native", "host"} {
		im := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		imports = append(imports, append(im, 0, 0))
	}
	root := []byte{0x20, 2, 4, 0x70, 0xd0, 0x70, 5, 0xd2, 6, 0x0b, 0x20, 0, 0x20, 1, 0x10, 3, 0xd1, 0x0b}
	call := byte(0x10)
	if tails {
		call = 0x12
	}
	forward := []byte{0x20, 0, 0x20, 1, 0x20, 2, call, 4, 0x0b}
	leaf := []byte{3, 1, 0x70, 1, 0x7f, 1, wasm.MustEncodeValType(typ), 0x20, 0, 0x22, 3, 0x20, 2, 0x20, 1, 0x10, 0, 0x10, 1, 0x21, 4, 0x21, 5, 0x20, 3, 0x10, 5, 0x21, 3, 0x20, 5, 0x20, 4, 0x20, 3, 0x0b}
	leaf = append(wasmtest.ULEB(uint32(len(leaf))), leaf...)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(types...)), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{5, 1, 2, 2, 3, 4}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 2))), wasmtest.Section(9, []byte{1, 3, 0, 1, 6}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(root), wasmtest.Code(forward), leaf, wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x0b}))))
}

func TestReferenceFunctionImportsRV64ExactCarriers(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				t.Run(fmt.Sprintf("shared%t-vector%t-tails%t", shared, vector, tails), func(t *testing.T) {
					m, err := wasm.DecodeModule(referenceImportCarrierSource(vector, tails))
					if err != nil {
						t.Fatal(err)
					}
					opts := closedReferenceProductionOptions(tails, false)
					opts.ImportBindings = hostImportBindings(2)
					image, err := CompileModuleWith(m, opts)
					if err != nil {
						t.Fatal(err)
					}
					slots := 3
					if vector {
						slots++
					}
					f, view := tailInvocationFixtureForTest(t, shared, slots, slots)
					sig := &m.Types[0].SubTypes[0].Comp
					remote := tailRANativeFrameInvocation(t, sig, f.b, f.cb, f.db, 0, true, func(e *integerEmitter) { tailRANativeSerialize(e, sig.Results, 0) })
					code := tailRAProofBuffer(t, image.Code)
					locals := make([]tailRAFunction, len(image.Entry))
					for i, entry := range image.Entry {
						end := len(image.Code)
						if i+1 < len(image.Entry) {
							end = image.Entry[i+1]
						}
						frame, err := NativeFrameBytes(image.Code[:end], entry)
						if err != nil {
							t.Fatal(err)
						}
						typ, _ := m.LocalFuncType(i)
						locals[i] = tailRAFunction{code: code, snapshot: image.Code, signature: typ, job: f.a, context: f.ca, directory: f.da, info: FunctionInfo{Entry: entry, CodeBytes: end - entry, FrameBytes: frame, FrameHeaderBytes: callFrameHeader, Params: swarSignatureSlots(typ.Params), Results: swarSignatureSlots(typ.Results), mode: emissionMode{production: true, interruptible: true, invocationContext: true, functionIndex: uint32(2 + i)}}}
					}
					proxy, _ := tailDynamicProxyForTest(t, locals[0], remote, 0)
					hostCode := HostIndirectSyncThunk(1, 1, 1)
					host := tailRAFunction{code: tailRAProofBuffer(t, hostCode), snapshot: hostCode, signature: sig, job: f.a, context: f.ca, directory: f.da, info: FunctionInfo{CodeBytes: len(hostCode)}}
					o := tailDynamicOwnerForTest(t, append([]tailRAFunction{proxy, host}, locals...))
					f.da = o.descriptors
					for _, i := range []int{3, 4, 5} {
						clear(o.descriptors[40*(i+1) : 40*(i+2)])
					}
					arena, err := coreruntime.NewArena(2 * coreruntime.ImportDispatchEntryBytes)
					if err != nil {
						t.Fatal(err)
					}
					t.Cleanup(func() {
						f.a.SetImportDispatchPtr(0)
						if err := arena.Close(); err != nil {
							t.Error(err)
						}
					})
					dispatch := arena.Alloc(2 * coreruntime.ImportDispatchEntryBytes)
					binary.LittleEndian.PutUint64(f.ca[64:], uint64(tailRAPointer(dispatch)))
					for i, target := range []tailRAFunction{proxy, host} {
						if err := target.code.Seal(); err != nil {
							t.Fatal(err)
						}
						row := dispatch[i*coreruntime.ImportDispatchEntryBytes:]
						binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchCodePtrOffset:], uint64(target.codeBase()))
						binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchHomeLinMemOffset:], uint64(f.a.LinMemBase()))
						binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchTargetContextOffset:], uint64(tailRAPointer(f.ca)))
						binary.LittleEndian.PutUint64(row[coreruntime.ImportDispatchCallerContextOffset:], uint64(tailRAPointer(f.ca)))
					}
					if err := code.Seal(); err != nil {
						t.Fatal(err)
					}
					if err := remote.code.Seal(); err != nil {
						t.Fatal(err)
					}
					root := tailRANativeRoot(t, o.functions[2], f.witness)
					if err := root.Seal(); err != nil {
						t.Fatal(err)
					}
					for _, null := range []uint64{0, 1, 0} {
						clear(f.args)
						clear(f.results)
						clear(f.trap)
						binary.LittleEndian.PutUint64(f.args, 0xdeadbeeffffffffd)
						binary.LittleEndian.PutUint64(f.args[8:], 0x7ff0000000000001)
						if vector {
							binary.LittleEndian.PutUint64(f.args[16:], 0x80000000deadbeef)
						}
						binary.LittleEndian.PutUint64(f.args[(slots-1)*8:], 0xcafefeed00000000|null)
						f.bind(t, f.e.StackLimit())
						f.a.SetRV64InvocationView(view)
						f.a.SetImportDispatchPtr(tailRAPointer(dispatch))
						calls := 0
						err := f.e.CallWithHostBase(root.Base(), f.args, f.a.LinMemBase(), f.trap, f.results, f.ctrl, func(_ uintptr, index uint32, args, results []uint64) {
							calls++
							if index != 1 || len(args) != 1 || args[0] != 0xfffffffd {
								panic("native then parked host identity")
							}
							runtime.GC()
							results[0] = 0xaabbccddfffffffe
						})
						if err != nil || calls != 1 {
							t.Fatal("imported closed reference execution", calls, err)
						}
						want := uint64(tailRAPointer(o.descriptors[7*40:]))
						if want>>32 == 0 {
							t.Fatal("oracle descriptor lacks upper bits")
						}
						if null != 0 {
							want = 0
						}
						if binary.LittleEndian.Uint64(f.args) != want || binary.LittleEndian.Uint64(f.args[8:]) != want {
							t.Fatal("full reference bits lost across imported calls", want, binary.LittleEndian.Uint64(f.args), binary.LittleEndian.Uint64(f.args[8:]))
						}
						if binary.LittleEndian.Uint64(f.results) != 0x7ff0000000000001 || vector && binary.LittleEndian.Uint64(f.results[8:]) != 0x80000000deadbeef || binary.LittleEndian.Uint64(f.results[(slots-2)*8:]) != 0xfffffffe || binary.LittleEndian.Uint64(f.results[(slots-1)*8:]) != null {
							t.Fatal("scalar/SWAR payload or null result")
						}
						// Scalar epilogues use caller-saved A0 for their last result.
						// Check the reserved state and platform canaries independently.
						f.assertCanaries(t)
						if binary.LittleEndian.Uint64(f.witness[64:]) != 0x123456789abcdef || binary.LittleEndian.Uint64(f.witness[72:]) != uint64(f.a.LinMemBase()) || !bytes.Equal(f.witness[96:128], f.witness[192:224]) || !bytes.Equal(f.witness[128:160], f.witness[160:192]) {
							t.Fatal("reserved guest/platform register restoration")
						}
					}
				})
			}
		}
	}
}
