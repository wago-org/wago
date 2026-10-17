//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64TableInitializerModule(vector, imported bool, expr []byte, size, capacity uint32) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{typ, wasm.I32}, []wasm.ValType{typ}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))}
	funcs := []byte{3, 0, 1, 2}
	bodies := [][]byte{wasmtest.Code([]byte{0x20, 0, 0x0b})}
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 0))))
		funcs = []byte{2, 1, 2}
		bodies = nil
	}
	bodies = append(bodies, wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}), wasmtest.Code([]byte{0xd0, 0x70, 0x20, 0, 0xfc, 15, 0, 0x0b}))
	sections = append(sections, wasmtest.Section(3, funcs), wasmtest.Section(4, wasmtest.Vec(tableTestTableWithInit(size, capacity, expr))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 1), wasmtest.ExportEntry("grow", 0, 2), wasmtest.ExportEntry("table", 1, 0))), wasmtest.Section(10, wasmtest.Vec(bodies...)))
	return wasmtest.Module(sections...)
}

func TestRV64PublicTableInitializerContents(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, null := range []bool{false, true} {
			for _, size := range []uint32{0, 3, 1024} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("vector%t-null%t-size%d-artifact%t", vector, null, size, artifact), func(t *testing.T) {
						expr := tableTestRefFuncExpr(0)
						if null {
							expr = tableTestRefNullFuncExpr()
						}
						capacity := uint32(5)
						if size == 1024 {
							capacity = size
						}
						c := rv64TableStageCompile(t, fmt.Sprintf("table-init-vector%t-null%t-size%d", vector, null, size), rv64TableInitializerModule(vector, false, expr, size, capacity), artifact)
						if c.HasTableInitFunc == null || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) {
							t.Fatal("table initializer metadata")
						}
						in, err := Instantiate(c)
						if err != nil {
							t.Fatal(err)
						}
						defer in.Close()
						c.Close()
						descriptor := in.tableDescriptor(0)
						if !null {
							canonical := in.funcRefDescs[coreruntime.FuncRefDescBytes : coreruntime.FuncRefDescBytes+coreruntime.TableEntryBytes]
							for i := 0; i < int(size); i++ {
								row := descriptor[8+i*coreruntime.TableEntryBytes : 8+(i+1)*coreruntime.TableEntryBytes]
								if !bytes.Equal(row, canonical) {
									t.Fatal("incomplete canonical prefill", i)
								}
							}
						}
						firstZero := size
						if null {
							firstZero = 0
						}
						for _, b := range descriptor[8+int(firstZero)*coreruntime.TableEntryBytes:] {
							if b != 0 {
								t.Fatal("initializer wrote null/spare capacity")
							}
						}
						args, want := []uint64{0xfeedface00000007}, []uint64{7}
						if vector {
							args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
							want = args
						}
						for _, index := range []uint64{0, uint64(size) / 2, uint64(size)} {
							got, err := in.Invoke("call", append(append([]uint64(nil), args...), index)...)
							if index >= uint64(size) || null {
								tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
							} else if err != nil || len(got) != len(want) {
								t.Fatal("prefill call", got, err)
							} else {
								for i := range want {
									if got[i] != want[i] {
										t.Fatal("prefill value", got, want)
									}
								}
							}
						}
						if size < capacity {
							importedGlobalInvoke(t, in, "grow", []uint64{uint64(size)}, 1)
							_, err := in.Invoke("call", append(append([]uint64(nil), args...), uint64(size))...)
							tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
						}
						in.Close()
						if c.codeCache.mem != nil {
							t.Fatal("table initializer retained mapping")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicTableInitializerOverrides(t *testing.T) {
	for _, null := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("null%t-artifact%t", null, artifact), func(t *testing.T) {
				value := []byte{0xd2, 0, 0x0b}
				if null {
					value = []byte{0xd0, 0x70, 0x0b}
				}
				elem := append([]byte{4, 0x41, 1, 0x0b, 1}, value...)
				source := tableInitializerModule(tableTestRefFuncExpr(1), elem)
				c := rv64TableStageCompile(t, fmt.Sprintf("table-init-override-null%t", null), source, artifact)
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				importedGlobalInvoke(t, in, "callAt", []uint64{42}, 0)
				importedGlobalInvoke(t, in, "callAt", []uint64{42}, 2)
				if null {
					_, err := in.Invoke("callAt", 1)
					tableTestExpectTrap(t, err, TrapIndirectOutOfBounds)
				} else {
					importedGlobalInvoke(t, in, "callAt", []uint64{7}, 1)
				}
			})
		}
	}
}

func TestRV64PublicTableInitializerImports(t *testing.T) {
	for _, kind := range []string{"go", "native", "vector"} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s-artifact%t", kind, artifact), func(t *testing.T) {
				vector := kind == "vector"
				c := rv64TableStageCompile(t, fmt.Sprintf("table-init-import-vector%t", vector), rv64TableInitializerModule(vector, true, tableTestRefFuncExpr(0), 3, 5), artifact)
				im := NewImports()
				calls := 0
				var producer *Instance
				var pc *Compiled
				if kind == "go" {
					im.Function("env", "host", slotHostFunc(func(_ HostModule, p, r []uint64) { calls++; runtime.GC(); r[0] = p[0] }))
				} else {
					typ := wasm.I32
					if vector {
						typ = wasm.V128
					}
					pc = rv64TableStageCompile(t, fmt.Sprintf("table-init-leaf-vector%t", vector), rv64PublicNativeIdentityModule([]wasm.ValType{typ}), artifact)
					var err error
					producer, err = Instantiate(pc)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					ex, err := producer.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					im.Function("env", "host", ex)
				}
				in, err := Instantiate(c, im)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				c.Close()
				if producer != nil {
					producer.Close()
					pc.Close()
					if !producer.hasPhysicalResources() {
						t.Fatal("initializer target retired early")
					}
				}
				runtime.GC()
				want, args := []uint64{7}, []uint64{0xdeadbeef00000007}
				if vector {
					want = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					args = want
				}
				for i := uint64(0); i < 3; i++ {
					importedGlobalInvoke(t, in, "call", want, append(append([]uint64(nil), args...), i)...)
				}
				if kind == "go" && calls != 3 {
					t.Fatal("initializer host route", calls)
				}
				in.Close()
				if c.codeCache.mem != nil || producer != nil && (producer.hasPhysicalResources() || pc.codeCache.mem != nil) {
					t.Fatal("initializer target retained after consumer close")
				}
			})
		}
	}
}

func TestRV64PublicTableInitializerArtifactAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source metadata mutations")
	}
	c := rv64TableStageCompile(t, "table-init-metadata", tableInitializerModule(tableTestRefFuncExpr(1)), false)
	for index, mutate := range []func(*Compiled){
		func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
		func(v *Compiled) { v.TableInitFunc = uint32(v.NumImports + len(v.Funcs)) },
		func(v *Compiled) { v.TableSize = 0; v.TableInitFunc = ^uint32(0) },
		func(v *Compiled) { v.HasTable = false },
		func(v *Compiled) {
			v.tableImport = "env.table"
			v.tableImportMin = 3
			v.tableImportMax = 3
			v.tableImportHasMax = true
			v.TableSize = 0
			v.TableMax = 0
			v.TableHasMax = false
		},
	} {
		v := *c.executionView()
		mutate(&v)
		if err := railshotValidateCompiled(&v); err == nil {
			t.Fatal("invalid table initializer metadata admitted", index)
		}
		blob, err := marshalCompiled(&v)
		if err != nil {
			continue
		}
		loaded, err := LoadTrustedArtifact(blob)
		if index == 3 && err == nil {
			// The absent-table codec tag drops owned table fields. Descriptor-only
			// modules are now valid; a trusted artifact does not revalidate code
			// against caller-modified metadata.
			repaired := loaded.executionView()
			if repaired.HasTableInitFunc || repaired.tableCount() != 0 || repaired.TableSize != 0 || railshotValidateCompiled(repaired) != nil {
				loaded.Close()
				t.Fatal("invalid normalized table-free metadata")
			}
			loaded.Close()
			continue
		}
		if index == 4 && err == nil {
			// The imported-table codec tag has no initializer payload. It drops
			// the conflicting owned initializer, yielding a valid import.
			repaired := loaded.executionView()
			if repaired.HasTableInitFunc || repaired.tableImportCount() != 1 || railshotValidateCompiled(repaired) != nil {
				loaded.Close()
				t.Fatal("invalid normalized imported table")
			}
			loaded.Close()
			continue
		}
		if loaded != nil {
			loaded.Close()
		}
		if err == nil {
			t.Fatal("invalid table initializer artifact admitted", index)
		}
	}
	if c, err := Compile(NewRuntimeConfig().WithFeature(CoreFeatureReferenceTypes, false), tableInitializerModule(tableTestRefFuncExpr(1))); c != nil || err == nil {
		if c != nil {
			c.Close()
		}
		t.Fatal("disabled table initializer feature admitted", err)
	}
}

func TestRV64PublicTableInitializerWrongSignature(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "table-init-wrong-signature", tableInitializerModule(tableTestRefFuncExpr(2)), artifact)
			in, err := Instantiate(c)
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			_, err = in.Invoke("callAt", 0)
			tableTestExpectTrap(t, err, TrapIndirectWrongSig)
		})
	}
}
