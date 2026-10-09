//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The same three import calls without private reference carriers provide an
// allocation oracle for the existing shared/owned host callback adapters.
func rv64ReferenceImportNumericControl(vector bool) []byte {
	typ := wasm.F64
	if vector {
		typ = wasm.V128
	}
	var imports [][]byte
	for _, name := range []string{"native", "shared", "owned"} {
		im := append(wasmtest.Name("env"), wasmtest.Name(name)...)
		imports = append(imports, append(im, 0, 0))
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, typ, wasm.I32}, []wasm.ValType{typ, wasm.I32, wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 3))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 1, 0x20, 0, 0x10, 0, 0x10, 1, 0x10, 2, 0x41, 9, 0x6a, 0x20, 2, 0x0b}))))
}

func TestRV64PublicReferenceFunctionImportsSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, wide := range []bool{false, true} {
					key := fmt.Sprintf("reference-imports-v%t-t%t-w%t", vector, tails, wide)
					t.Run(fmt.Sprintf("artifact%t-%s", artifact, key), func(t *testing.T) {
						c := rv64NativeTailCompile(t, key, wasmtest.ClosedReferenceImports(vector, tails, wide), artifact)
						if c.NumImports != 3 || !c.dynamicImports || c.dynamicFuncrefEscape || !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || len(c.FuncTypeID) != 3+len(c.Funcs) {
							t.Fatal("numeric imported reference ABI")
						}
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						pc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
						producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						nested := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						export, err := producer.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						calls, reentry := 0, true
						shared := HostCallFunc(func(call HostCall) {
							if calls%2 != 0 || call.ParamCount() != 1 || call.ResultCount() != 1 || call.ParamSlots()[0] != 0xfffffffd {
								panic("shared callback identity/normalized input")
							}
							calls++
							if reentry {
								got, err := nested.Invoke("identity", 0xdeadbeef80000001)
								if err != nil || !slices.Equal(got, []uint64{0x80000001}) {
									panic("nested import callback")
								}
								runtime.GC()
							}
							call.ResultSlots()[0] = 0xdeadbeeffffffffe
						})
						owned := rv64DynamicHostBinding(t, rt, true, HostCallFunc(func(call HostCall) {
							if calls%2 != 1 || call.ParamSlots()[0] != 0xfffffffe {
								panic("owned callback identity/normalized input")
							}
							calls++
							call.ResultSlots()[0] = 0xcafebabe00000000
						}), c.importFuncSigs[2])
						imports := NewImports().Function("env", "native", export).Function("env", "shared", shared).Function("env", "owned", owned)
						in := rv64NativeTokenInstantiate(t, rt, c, imports)
						controlCode := rv64NativeTailCompile(t, fmt.Sprintf("reference-imports-control-v%t", vector), rv64ReferenceImportNumericControl(vector), artifact)
						control := rv64NativeTokenInstantiate(t, rt, controlCode, imports)
						for _, i := range []int{5, 6} {
							if !railshotNonCallableFunction(in.c, i) || !bytes.Equal(in.funcRefDescs[40*(i+1):40*(i+2)], make([]byte, 40)) {
								t.Fatal("shifted internal descriptor ordinal", i)
							}
						}
						args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
						payload := []uint64{args[1]}
						if vector {
							args = append(args, 0x80000000deadbeef)
							payload = append(payload, args[2])
						}
						args = append(args, 0)
						want := append(slices.Clone(payload), 9, 0)
						for run, choice := range []uint64{0, 1, 2, 3, 4, 1} {
							null := uint64(0)
							if choice == 1 {
								null = 1
							}
							args[len(args)-1] = 0xfeedcafe00000000 | choice
							want[len(want)-1] = null
							before := calls
							got, err := in.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) || calls != before+2 || readGlobalObject(in.globalCells[3], ValI32) != null {
								t.Fatal("references live across imports", run, got, calls, err)
							}
							if run == 0 {
								if err := producer.Close(); err != nil {
									t.Fatal(err)
								}
								if err := pc.Close(); err != nil {
									t.Fatal(err)
								}
								if producer.resourcesClosed {
									t.Fatal("retired imported producer lost physical root")
								}
							}
						}
						reentry = false
						fn, err := in.WasmFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						session, err := fn.OpenSession()
						if err != nil {
							t.Fatal(err)
						}
						if got, err := session.Invoke(args...); err != nil || !slices.Equal(got, want) {
							t.Fatal("prepared imported reference call", got, err)
						}
						session.Close()
						allocs := testing.AllocsPerRun(5, func() {
							got, err := in.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) {
								panic("warm imported reference call")
							}
						})
						controlAllocs := testing.AllocsPerRun(5, func() {
							got, err := control.Invoke("run", args...)
							if err != nil || !slices.Equal(got, want) {
								panic("numeric import allocation control")
							}
						})
						if allocs > controlAllocs || allocs > 2 {
							t.Fatal("reference import allocation exceeds numeric control/bound", allocs, controlAllocs)
						}
						t.Logf("reference/numeric warm allocations: %g/%g", allocs, controlAllocs)
						var encoded []byte
						for _, v := range payload {
							encoded = binary.LittleEndian.AppendUint64(encoded, v)
						}
						for _, memory := range in.memoryDir.memories {
							b := memory.UnsafeBytes()
							if len(b) != 3*65536 || !bytes.Equal(b[48:52], []byte{0x12, 0x34, 0x56, 0x78}) || !bytes.Equal(b[64:64+len(encoded)], encoded) {
								t.Fatal("numeric state after imported call")
							}
						}
						if err := in.Close(); err != nil {
							t.Fatal(err)
						}
						if err := control.Close(); err != nil {
							t.Fatal(err)
						}
						if !in.resourcesClosed || !producer.resourcesClosed {
							t.Fatal("imported physical lifetime not released")
						}
					})
				}
			}
		}
	}
}
