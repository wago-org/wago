//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64NativeCallConstant(typ wasm.ValType, cells []uint64) []byte {
	switch typ {
	case wasm.I32:
		return append([]byte{0x41}, wasmtest.SLEB32(int32(cells[0]))...)
	case wasm.I64:
		return append([]byte{0x42}, wasmtest.SLEB64(int64(cells[0]))...)
	case wasm.F32:
		return binary.LittleEndian.AppendUint32([]byte{0x43}, uint32(cells[0]))
	case wasm.F64:
		return binary.LittleEndian.AppendUint64([]byte{0x44}, cells[0])
	case wasm.V128:
		return binary.LittleEndian.AppendUint64(binary.LittleEndian.AppendUint64([]byte{0xfd, 12}, cells[0]), cells[1])
	default:
		panic("unsupported capacity fixture type")
	}
}

// Only the imported signature is wide. The sole defined signature is ()->i32;
// all native arguments are computed here and every returned cell is checked.
func rv64NarrowNativeCallModule(types []wasm.ValType, shared bool) []byte {
	_, cells := rv64NativeVectorInputs(types)
	imports := [][]byte{rv64VectorFunctionImport("host", 0)}
	if shared {
		imports = append([][]byte{rv64VectorMemoryImport()}, imports...)
	}
	body := []byte{}
	decls := wasmtest.ULEB(uint32(len(types)))
	slot := 0
	for _, typ := range types {
		decls = append(decls, 1, wasm.MustEncodeValType(typ))
		body = append(body, rv64NativeCallConstant(typ, cells[slot:])...)
		slot++
		if typ == wasm.V128 {
			slot++
		}
	}
	body = append(body, 0x10, 0)
	for index := len(types) - 1; index >= 0; index-- {
		body = append(body, 0x21)
		body = append(body, wasmtest.ULEB(uint32(index))...)
	}
	body = append(body, 0x41, 1)
	slot = 0
	for index, typ := range types {
		body = append(body, 0x20)
		body = append(body, wasmtest.ULEB(uint32(index))...)
		switch typ {
		case wasm.F32:
			body = append(body, 0xbc) // i32.reinterpret_f32
			body = append(body, rv64NativeCallConstant(wasm.I32, cells[slot:])...)
			body = append(body, 0x46)
		case wasm.F64:
			body = append(body, 0xbd) // i64.reinterpret_f64
			body = append(body, rv64NativeCallConstant(wasm.I64, cells[slot:])...)
			body = append(body, 0x51)
		case wasm.V128:
			body = append(body, rv64NativeCallConstant(typ, cells[slot:])...)
			body = append(body, 0xfd, 81, 0xfd, 83, 0x45) // xor, any_true, eqz
		default:
			body = append(body, rv64NativeCallConstant(typ, cells[slot:])...)
			if typ == wasm.I32 {
				body = append(body, 0x46)
			} else {
				body = append(body, 0x51)
			}
		}
		body = append(body, 0x71) // and every comparison
		slot++
		if typ == wasm.V128 {
			slot++
		}
	}
	body = append(body, 0x0b)
	fn := append(decls, body...)
	code := append(wasmtest.ULEB(uint32(len(fn))), fn...)
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(imports...)),
		wasmtest.Section(3, []byte{1, 1}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1))),
		wasmtest.Section(10, wasmtest.Vec(code)),
	)
}

func TestRV64PublicNarrowCallerWideNativeImportCapacity(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-shared%t-artifact%t", vector, shared, artifact), func(t *testing.T) {
					count := 64
					if vector {
						count = 128
					}
					types := make([]wasm.ValType, count)
					for index := range types {
						types[index] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[index%4]
						if vector {
							types[index] = wasm.V128
						}
					}
					var memory *Memory
					if shared {
						_, memory = importedMemoryProvider(t)
						defer memory.Close()
					}
					fail := NewGlobalI32(0, true)
					defer fail.Close()
					producerImports := NewImports().Global("env", "fail", fail)
					if shared {
						producerImports.Memory("env", "memory", memory)
					}
					producerCode := rv64PublicStartCompile(t, rv64NativeVectorProducer(types, shared), artifact)
					producer, err := Instantiate(producerCode, producerImports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					export, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					consumerCode := rv64PublicStartCompile(t, rv64NarrowNativeCallModule(types, shared), artifact)
					imports := testImports("env.host", export)
					if shared {
						imports.Memory("env", "memory", memory)
					}
					consumer, err := Instantiate(consumerCode, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					_, cells := rv64NativeVectorInputs(types)
					// Assert capacity before entering generated code, so the regression
					// fails safely even when the root arena is undersized.
					if len(consumer.serArgs) < len(cells)*8 || len(consumer.results) < len(cells)*8 {
						t.Fatalf("root call buffers %d/%d bytes, need %d each", len(consumer.serArgs), len(consumer.results), len(cells)*8)
					}
					if shared && (producer.jm != consumer.jm || producer.nativeContext == consumer.nativeContext) {
						t.Fatal("shared-home fixture")
					}
					for range 3 {
						if got, err := consumer.Invoke("call"); err != nil || !slices.Equal(got, []uint64{1}) {
							t.Fatal("wide imported cells", got, err)
						}
						if err := fail.Set(1); err != nil {
							t.Fatal(err)
						}
						_, err := consumer.Invoke("call")
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionName != "identity" {
							t.Fatal("wide imported producer trap", err)
						}
						if err := fail.Set(0); err != nil {
							t.Fatal(err)
						}
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					if err := producerCode.Close(); err != nil {
						t.Fatal(err)
					}
					if err := consumerCode.Close(); err != nil {
						t.Fatal(err)
					}
					runtime.GC()
					if got, err := consumer.Invoke("call"); err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("retired owners", got, err)
					}
					if err := consumer.Close(); err != nil {
						t.Fatal(err)
					}
					if producer.jm != nil || consumer.thunkMem != nil {
						t.Fatal("final owner cleanup")
					}
				})
			}
		}
	}
}

func TestRV64PublicNarrowCallerWideHostImportCapacity(t *testing.T) {
	types := make([]wasm.ValType, 64)
	sig := FuncSig{Params: make([]ValType, 64), Results: make([]ValType, 64)}
	for index := range types {
		types[index] = []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}[index%4]
		sig.Params[index] = []ValType{ValI32, ValI64, ValF32, ValF64}[index%4]
		sig.Results[index] = sig.Params[index]
	}
	_, want := rv64NativeVectorInputs(types)
	for _, artifact := range []bool{false, true} {
		for _, owned := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-owned%t", artifact, owned), func(t *testing.T) {
				calls := 0
				host := HostCallFunc(func(call HostCall) {
					if !slices.Equal(call.ParamSlots(), want) || len(call.ResultSlots()) != len(want) {
						panic("wide host transport")
					}
					copy(call.ResultSlots(), call.ParamSlots())
					calls++
					runtime.GC()
				})
				var binding any = host
				var rt *Runtime
				if owned {
					rt = NewRuntime()
					defer rt.Close()
					ref, err := rt.NewHostFuncRef(host, sig)
					if err != nil {
						t.Fatal(err)
					}
					defer ref.Close()
					binding = ref
				}
				c := rv64PublicStartCompile(t, rv64NarrowNativeCallModule(types, false), artifact)
				imports := testImports("env.host", binding)
				var in *Instance
				var err error
				if rt != nil {
					mod, moduleErr := rt.Module(c)
					if moduleErr != nil {
						t.Fatal(moduleErr)
					}
					defer mod.Close()
					in, err = rt.Instantiate(context.Background(), mod, WithImports(imports))
				} else {
					in, err = Instantiate(c, imports)
				}
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				if len(in.serArgs) < len(want)*8 || len(in.results) < len(want)*8 {
					t.Fatal("wide host root capacity")
				}
				fn, err := in.WasmFunc("call")
				if err != nil {
					t.Fatal(err)
				}
				for range 3 {
					if got, err := in.Invoke("call"); err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("wide host imported cells", got, err)
					}
					if got, err := fn.Invoke(); err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("prepared wide host imported cells", got, err)
					}
				}
				if calls != 6 {
					t.Fatal("host call count", calls)
				}
			})
		}
	}
}

func TestRV64PublicImportedCallCapacityDirectionsAndBounds(t *testing.T) {
	wide := make([]ValType, 128)
	for index := range wide {
		wide[index] = ValV128
	}
	for _, paramsWide := range []bool{false, true} {
		sig := FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI64}}
		wantParams, wantResults := 3, 256
		if paramsWide {
			sig.Params, wantParams, wantResults = wide, 256, 1
		} else {
			sig.Results = wide
		}
		c := Compiled{NumImports: 1, importFuncSigs: []FuncSig{sig}, Funcs: []FuncSig{{Params: []ValType{ValI32, ValI32, ValI32}, Results: []ValType{ValI32}}}}
		params, results, err := c.maxCallSlots()
		if err != nil || params != wantParams || results != wantResults {
			t.Fatal("independent imported capacities", params, results, err)
		}
	}
	tooWide := append(slices.Clone(wide), ValV128)
	for _, c := range []*Compiled{
		{NumImports: -1},
		{NumImports: 1},
		{importFuncSigs: []FuncSig{{}}},
		{NumImports: 1, importFuncSigs: []FuncSig{{Params: tooWide}}},
		{NumImports: 1, importFuncSigs: []FuncSig{{Results: tooWide}}},
	} {
		if _, _, err := c.maxCallSlots(); err == nil {
			t.Fatal("invalid imported capacity accepted")
		}
	}
}

func TestRV64PublicIndirectTypeCallCapacity(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, paramsWide := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-paramsWide%t-artifact%t", vector, paramsWide, artifact), func(t *testing.T) {
					wide := make([]wasm.ValType, 256)
					for i := range wide {
						wide[i] = wasm.I32
					}
					if vector {
						wide = wide[:128]
						for i := range wide {
							wide[i] = wasm.V128
						}
					}
					params, results := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32}, wide
					wantParams, wantResults := 3, 256
					if paramsWide {
						params, results, wantParams, wantResults = wide, []wasm.ValType{wasm.I64}, 256, 1
					}
					source := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(params, results))),
						wasmtest.Section(3, []byte{1, 0}),
						wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("table", 1, 0))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
					)
					c := rv64PublicStartCompile(t, source, artifact)
					in, err := Instantiate(c, nil)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					// Check the real arena before any possible foreign/table call. The wide
					// signature has no defined function and contributes only through Types.
					if len(in.serArgs) != wantParams*8 || len(in.results) != wantResults*8 {
						t.Fatal("indirect type transport capacity", len(in.serArgs), len(in.results), wantParams*8, wantResults*8)
					}
					if got, err := in.Invoke("call"); err != nil || !slices.Equal(got, []uint64{7}) {
						t.Fatal(got, err)
					}
				})
			}
		}
	}
}

func TestRV64PublicIndirectTypeCallCapacityBounds(t *testing.T) {
	wide := make([]ValueTypeDescriptor, 128)
	for i := range wide {
		wide[i] = ValueTypeDescriptor{Kind: ValueTypeV128}
	}
	for _, paramsWide := range []bool{false, true} {
		typ := DefinedTypeDescriptor{Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeI32}}, Results: wide}
		wantParams, wantResults := 3, 256
		if paramsWide {
			typ.Params, typ.Results, wantParams, wantResults = wide, []ValueTypeDescriptor{{Kind: ValueTypeI64}}, 256, 1
		}
		c := Compiled{HasTable: true, Types: []DefinedTypeDescriptor{typ}, Funcs: []FuncSig{{Params: []ValType{ValI32, ValI32, ValI32}, Results: []ValType{ValI32}}}}
		params, results, err := c.maxCallSlots()
		if err != nil || params != wantParams || results != wantResults {
			t.Fatal("indirect physical capacities", params, results, err)
		}
	}
	tooWide := append(slices.Clone(wide), ValueTypeDescriptor{Kind: ValueTypeV128})
	for _, typ := range []DefinedTypeDescriptor{
		{Kind: CompositeTypeFunction, Params: tooWide},
		{Kind: CompositeTypeFunction, Results: tooWide},
		{Kind: CompositeTypeStruct},
		{Kind: CompositeTypeFunction, Params: []ValueTypeDescriptor{{Kind: ValueTypeReference}}},
	} {
		c := Compiled{HasTable: true, Types: []DefinedTypeDescriptor{typ}}
		if _, _, err := c.maxCallSlots(); err == nil {
			t.Fatal("invalid unused indirect signature admitted", typ.Kind)
		}
	}
	for _, count := range []int{1024, 1025} {
		types := make([]DefinedTypeDescriptor, count)
		for i := range types {
			types[i].Kind = CompositeTypeFunction
		}
		c := Compiled{HasTable: true, Types: types}
		_, _, err := c.maxCallSlots()
		if (err != nil) != (count > 1024) {
			t.Fatal("indirect type directory limit", count, err)
		}
	}
	c := Compiled{Types: []DefinedTypeDescriptor{{Kind: CompositeTypeFunction, Params: tooWide}}, Funcs: []FuncSig{{Results: []ValType{ValI32}}}}
	if params, results, err := c.maxCallSlots(); err != nil || params != 0 || results != 1 {
		t.Fatal("direct-only capacity changed", params, results, err)
	}
}
