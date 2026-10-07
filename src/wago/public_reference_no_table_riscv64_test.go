//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"os"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceNoTableModule(vector, imported, null bool) []byte {
	typ := wasm.I32
	if vector {
		typ = wasm.V128
	}
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))}
	count, start := 256, 0
	if imported {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))))
		count, start = 255, 1
	}
	globals := make([][]byte, count)
	for i := range globals {
		index := i + start
		expr := []byte{0xd2, 0, 0x0b}
		if null {
			expr = []byte{0xd0, 0x70, 0x0b}
		}
		if index != 0 {
			expr = append(append([]byte{0x23}, wasmtest.ULEB(uint32(index-1))...), 0x0b)
		}
		globals[i] = wasmtest.GlobalEntry(wasm.FuncRef, false, expr)
	}
	carrier := []byte{1, 1, 0x70, 0x23, 0xff, 1, 0x21, 0, 0x02, 0x70, 0x20, 0, 0x0c, 0, 0x0b, 0xd1, 0x0b}
	return wasmtest.Module(append(sections,
		wasmtest.Section(3, []byte{3, 0, 1, 1}),
		wasmtest.Section(6, wasmtest.Vec(globals...)),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("identity", 0, 0), wasmtest.ExportEntry("isnull", 0, 1), wasmtest.ExportEntry("carrier", 0, 2), wasmtest.ExportEntry("reference", 3, 255), wasmtest.ExportEntry("first", 3, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x23, 0xff, 1, 0xd1, 0x0b}), append(wasmtest.ULEB(uint32(len(carrier))), carrier...))),
	)...)
}

func TestRV64PublicReferenceGlobalsWithoutTable(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, kind := range []string{"local", "null", "native", "host"} {
				if vector && kind == "host" {
					continue
				}
				null := kind == "null"
				t.Run(fmt.Sprintf("vector%t-artifact%t-%s", vector, artifact, kind), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					source := rv64ReferenceNoTableModule(vector, false, null)
					bindings := NewImports()
					var closeTarget func()
					var hostOwner *HostFuncRef
					if kind == "native" || kind == "host" {
						typ := wasm.I32
						if vector {
							typ = wasm.V128
						}
						types := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}), wasmtest.FuncType(nil, []wasm.ValType{wasm.I32})))
						imports := wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 0)))
						source = rv64ReplaceTableStageSection(t, source, 1, append(types, imports...))
						source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{2, 1, 1}))
						source = rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("isnull", 0, 1), wasmtest.ExportEntry("carrier", 0, 2), wasmtest.ExportEntry("reference", 3, 255), wasmtest.ExportEntry("first", 3, 0))))
						carrier := []byte{1, 1, 0x70, 0x23, 0xff, 1, 0x21, 0, 0x02, 0x70, 0x20, 0, 0x0c, 0, 0x0b, 0xd1, 0x0b}
						source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x23, 0xff, 1, 0xd1, 0x0b}), append(wasmtest.ULEB(uint32(len(carrier))), carrier...))))
						if kind == "native" {
							leafCode := rv64TableStageCompile(t, fmt.Sprintf("ref-no-table-leaf-vector%t", vector), rv64ReferenceNoTableModule(vector, false, false), artifact)
							leaf := rv64NativeTokenInstantiate(t, rt, leafCode, NewImports())
							target, err := leaf.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							bindings.Function("env", "target", target)
							defer leaf.Close()
							defer leafCode.Close()
							closeTarget = func() { leaf.Close(); leafCode.Close() }
						} else {
							host := rv64HostTokenOwner(t, rt, ValI32, slotHostFunc(func(_ HostModule, p, r []uint64) { copy(r, p) }))
							bindings.Function("env", "target", host)
							defer host.Close()
							hostOwner = host
							closeTarget = func() {
								if err := host.Close(); err == nil {
									t.Fatal("host closed with attached importer")
								}
							}
						}
					}
					pc := rv64TableStageCompile(t, fmt.Sprintf("ref-no-table-producer-vector%t-%s", vector, kind), source, artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, bindings)
					if closeTarget != nil {
						closeTarget()
					}
					if pc.tableCount() != 0 || producer.tableDescPtr != 0 {
						t.Fatal("table-free producer has table storage")
					}
					if null && len(producer.funcRefDescs) != 0 {
						t.Fatal("null-only producer allocated descriptors")
					}
					wantNull := uint64(0)
					if null {
						wantNull = 1
					}
					importedGlobalInvoke(t, producer, "carrier", []uint64{wantNull})
					own, err := producer.WasmFunc("carrier")
					if err != nil {
						t.Fatal(err)
					}
					if got, err := own.Invoke(); err != nil || !slices.Equal(got, []uint64{wantNull}) {
						t.Fatal("prepared before publication", got, err)
					}
					g, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					if got, err := own.Invoke(); err != nil || !slices.Equal(got, []uint64{wantNull}) {
						t.Fatal("prepared after publication", got, err)
					}
					c := rv64TableStageCompile(t, fmt.Sprintf("ref-no-table-consumer-vector%t", vector), rv64ReferenceNoTableModule(vector, true, false), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
					if c.tableCount() != 0 || in.tableDescPtr != 0 || len(in.funcRefDescs) != 0 {
						t.Fatal("table-free imported carrier allocated a local descriptor arena")
					}
					producer.Close()
					pc.Close()
					c.Close()
					importedGlobalInvoke(t, in, "isnull", []uint64{wantNull})
					importedGlobalInvoke(t, in, "carrier", []uint64{wantNull})
					fn, err := in.WasmFunc("carrier")
					if err != nil {
						t.Fatal(err)
					}
					for repeat := 0; repeat < 2; repeat++ {
						got, err := fn.Invoke()
						if err != nil || !slices.Equal(got, []uint64{wantNull}) {
							t.Fatal("prepared reference carrier", got, err)
						}
					}
					session, err := fn.OpenSession()
					if err != nil {
						t.Fatal(err)
					}
					got, err := session.Invoke0()
					session.Close()
					if err != nil || !slices.Equal(got, []uint64{wantNull}) {
						t.Fatal("session reference carrier", got, err)
					}
					value, err := in.GlobalValue("reference")
					if err != nil || value.FuncRef().IsNull() != null {
						t.Fatal("table-free copied egress", value, err)
					}
					first, err := in.GlobalValue("first")
					if err != nil || first != value {
						t.Fatal("table-free imported identity", first, err)
					}
					if !null {
						table, err := NewTable(2, 5)
						if err != nil {
							t.Fatal(err)
						}
						defer table.Close()
						copyCell, err := in.ExportedGlobalObject("reference")
						if err != nil {
							t.Fatal(err)
						}
						tc := rv64TableStageCompile(t, fmt.Sprintf("ref-import-table-consumer-vector%t", vector), rv64ReferenceImportModule(vector, true), artifact)
						caller := rv64NativeTokenInstantiate(t, rt, tc, NewImports().Global("env", "ref", copyCell).Table("env", "table", table))
						if _, err := caller.Invoke("install", 0); err != nil {
							t.Fatal(err)
						}
						args, want := []uint64{0xdeadbeef00000007, 0}, []uint64{7}
						if vector {
							args = []uint64{0x0123456789abcdef, 0xfedcba9876543210, 0}
							want = args[:2]
						}
						importedGlobalInvoke(t, caller, "call", want, args...)
						caller.Close()
						tc.Close()
						if err := table.Close(); err != nil {
							t.Fatal(err)
						}
					}
					in.Close()
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					if err := rt.CloseContext(ctx); err != nil {
						t.Fatal(err)
					}
					if hostOwner != nil {
						if err := hostOwner.Close(); err != nil {
							t.Fatal(err)
						}
					}
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
						t.Fatal("table-free reference roots remain")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceGlobalsWithoutTableAuthentication(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprint(artifact), func(t *testing.T) {
			rt := NewRuntime()
			defer rt.Close()
			pc := rv64TableStageCompile(t, "ref-no-table-producer-vectorfalse-local", rv64ReferenceNoTableModule(false, false, false), artifact)
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			g, err := producer.ExportedGlobalObject("reference")
			if err != nil {
				t.Fatal(err)
			}
			c := rv64TableStageCompile(t, "ref-no-table-consumer-vectorfalse", rv64ReferenceNoTableModule(false, true, false), artifact)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
			defer in.Close()
			descriptor := readGlobalObject(g, ValFuncRef)
			producer.Close()
			pc.Close()
			c.Close()
			for _, cached := range []bool{false, true} {
				if cached {
					if _, err := in.GlobalValue("reference"); err != nil {
						t.Fatal(err)
					}
				}
				tokens := len(rt.refStore.byToken)
				for _, offset := range []int{0, 8, 16, 24, 32} {
					entry, ok := rv64PublishedDescriptor(producer, descriptor)
					if !ok {
						t.Fatal("descriptor before corruption")
					}
					saved := binary.LittleEndian.Uint64(entry[offset:])
					binary.LittleEndian.PutUint64(entry[offset:], saved^1)
					_, err := in.GlobalValue("reference")
					binary.LittleEndian.PutUint64(entry[offset:], saved)
					if err == nil || len(rt.refStore.byToken) != tokens {
						t.Fatal("corrupt table-free egress", cached, offset, err)
					}
				}
				writeGlobalObject(g, ValFuncRef, 1)
				_, err := in.GlobalValue("first")
				writeGlobalObject(g, ValFuncRef, descriptor)
				if err == nil {
					t.Fatal("changed frozen reference accepted")
				}
			}
		})
	}
}

func TestRV64PublicReferenceGlobalsWithoutTableAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source and metadata admission")
	}
	for _, vector := range []bool{false, true} {
		source := rv64ReferenceNoTableModule(vector, false, false)
		c := rv64TableStageCompile(t, fmt.Sprintf("ref-no-table-producer-vector%t-local", vector), source, false)
		for name, mutate := range map[string]func(*Compiled){
			"key-count":         func(v *Compiled) { v.FuncTypeID = v.FuncTypeID[:len(v.FuncTypeID)-1] },
			"table-size":        func(v *Compiled) { v.TableSize = 1 },
			"table-type":        func(v *Compiled) { v.TableType = ValFuncRef },
			"table-max":         func(v *Compiled) { v.TableMax = 1 },
			"table-limits":      func(v *Compiled) { v.TableHasMax = true },
			"table-address":     func(v *Compiled) { v.TableAddr64 = true },
			"table-initializer": func(v *Compiled) { v.HasTableInitFunc = true },
			"table-export":      func(v *Compiled) { v.tableExports = map[string]int{"table": 0} },
			"element":           func(v *Compiled) { v.Elems = []ElemInit{{}} },
			"table-abi":         func(v *Compiled) { v.nativeTableContextABI = false },
			"reference-abi":     func(v *Compiled) { v.nativeTableReferenceABI = false },
			"frame":             func(v *Compiled) { v.registerABIDisabled = false },
			"descriptor":        func(v *Compiled) { v.NeedsFuncRefDescs = false },
			"key":               func(v *Compiled) { v.FuncTypeID[0] ^= 1 },
		} {
			v := *c.executionView()
			v.FuncTypeID = slices.Clone(v.FuncTypeID)
			mutate(&v)
			if err := railshotValidateCompiled(&v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("invalid table-free metadata", name, vector, err)
			}
			blob, err := marshalCompiled(&v)
			if err != nil {
				continue
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				normalized := (name == "table-size" || name == "table-type" || name == "table-max" || name == "table-limits" || name == "table-address" || name == "table-initializer") && loaded.TableSize == 0 && loaded.TableType == 0 && loaded.TableMax == 0 && !loaded.TableHasMax && !loaded.TableAddr64 && !loaded.HasTableInitFunc
				loaded.Close()
				if err == nil && normalized {
					continue
				} // Absent-table fields are not serialized.
			}
			if err == nil {
				t.Fatal("invalid table-free artifact", name, vector)
			}
		}
	}
}

func TestRV64PublicReferenceGlobalsWithoutTableUnusedImport(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				typ := wasm.I32
				args := []uint64{0xdeadbeef00000007}
				want := []uint64{7}
				if vector {
					typ = wasm.V128
					args = []uint64{0x0123456789abcdef, 0xfedcba9876543210}
					want = args
				}
				source := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{typ}, []wasm.ValType{typ}))), wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))))
				c := rv64TableStageCompile(t, fmt.Sprintf("ref-no-table-unused-vector%t", vector), source, artifact)
				rt := NewRuntime()
				defer rt.Close()
				g, err := rt.NewFuncRefGlobal(NullFuncRef(), false)
				if err != nil {
					t.Fatal(err)
				}
				defer g.Close()
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
				defer in.Close()
				if in.usesIndependentExecution() || len(in.funcRefDescs) != 0 || in.tableDescPtr != 0 {
					t.Fatal("unused import entry/storage contract")
				}
				importedGlobalInvoke(t, in, "f", want, args...)
			})
		}
	}
}
