//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64TableGlobalInitializerModule(vector bool, size, capacity uint32, override bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}),
			wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}),
			wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
		)),
		wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(size, capacity, []byte{0x23, 0}))),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("identity", 0, 0),
			wasmtest.ExportEntry("call", 0, 1),
			wasmtest.ExportEntry("grow", 0, 2),
			wasmtest.ExportEntry("table", 1, 0),
		)),
	}
	if override {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(tableTestActiveElemExpr(1, tableTestRefFuncExpr(0)))))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 0, 0x0b}),
		wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
		wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 15, 0, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicTableImportedGlobalInitializer(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			for _, size := range []uint32{0, 3, 1024} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-null%t-size%d-artifact%t", vector, null, size, artifact), func(t *testing.T) {
						rt := NewRuntime()
						defer rt.Close()
						imports := NewImports()
						var producer *Instance
						var producerCode *Compiled
						if null {
							global, err := rt.NewFuncRefGlobal(FuncRef{}, false)
							if err != nil {
								t.Fatal(err)
							}
							defer global.Close()
							imports.Global("env", "ref", global)
						} else {
							producerCode = rv64TableStageCompile(t, fmt.Sprintf("table-global-producer-vector%t", vector), rv64NativeTokenModule(vector, true, true, false), artifact)
							producer = rv64NativeTokenInstantiate(t, rt, producerCode, NewImports())
							global, err := producer.ExportedGlobalObject("reference")
							if err != nil {
								t.Fatal(err)
							}
							imports.Global("env", "ref", global)
						}

						capacity := size + 1
						if size == 1024 {
							capacity = size
						}
						override := size == 3
						key := fmt.Sprintf("table-global-vector%t-null%t-size%d", vector, null, size)
						compiled := rv64TableStageCompile(t, key, rv64TableGlobalInitializerModule(vector, size, capacity, override), artifact)
						wantElems := 1
						if override {
							wantElems++
						}
						if compiled.HasTableInitFunc || len(compiled.Elems) != wantElems {
							t.Fatal("table global initializer metadata", compiled.HasTableInitFunc, len(compiled.Elems))
						}
						in := rv64NativeTokenInstantiate(t, rt, compiled, imports)
						compiled.Close()
						if producer != nil {
							producer.Close()
							producerCode.Close()
							if !producer.hasPhysicalResources() {
								t.Fatal("table initializer lost imported global owner")
							}
						}

						args, want := rv64ElementGlobalArguments(vector)
						indices := []uint32{}
						if size != 0 {
							indices = []uint32{0, size / 2, size - 1}
						}
						for _, index := range indices {
							call := append(append([]uint64(nil), args...), uint64(index))
							if null && !(override && index == 1) {
								_, err := in.Invoke("call", call...)
								tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
							} else {
								importedGlobalInvoke(t, in, "call", want, call...)
							}
						}

						descriptor := in.tableDescriptor(0)
						if !null && size != 0 {
							bits := readGlobalObject(in.globalCells[0], ValFuncRef)
							canonical, valid := rv64PublishedDescriptor(producer, bits)
							if !valid {
								t.Fatal("imported table initializer descriptor has no owner")
							}
							for _, index := range indices {
								if override && index == 1 {
									continue
								}
								row := descriptor[8+int(index)*coreruntime.TableEntryBytes : 8+(int(index)+1)*coreruntime.TableEntryBytes]
								if !bytes.Equal(row, canonical[:coreruntime.TableEntryBytes]) {
									t.Fatal("table initializer changed canonical descriptor", index)
								}
							}
						}
						if size < capacity {
							importedGlobalInvoke(t, in, "grow", []uint64{uint64(size)}, 1)
							call := append(append([]uint64(nil), args...), uint64(size))
							_, err := in.Invoke("call", call...)
							tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
						}

						in.Close()
						if producer != nil && (producer.hasPhysicalResources() || producerCode.codeCache.mem != nil) || compiled.codeCache.mem != nil {
							t.Fatal("table initializer retained resources after consumer close")
						}
					})
				}
			}
		}
	}
}
