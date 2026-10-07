//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"os"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicImportReferenceTransport(t *testing.T) {
	for _, kind := range []string{"go", "owned", "native", "vector"} {
		for _, borrowed := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s-borrowed%t-shared%t-artifact%t", kind, borrowed, shared, artifact), func(t *testing.T) {
						types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
						if kind == "vector" {
							types = append(types, wasm.V128)
						}
						key := fmt.Sprintf("import-ref-types%d-borrowed%t-shared%t", len(types), borrowed, shared)
						p := rv64TableStageCompile(t, key+"-producer", rv64TableFunctionImportModule(types, borrowed, shared, nil, false, true), artifact)
						c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, shared, false, true, 2), artifact)
						imports, consumerImports := NewImports(), NewImports()
						if shared {
							memory, err := NewSharedMemory(1, 4)
							if err != nil {
								t.Fatal(err)
							}
							defer memory.Close()
							imports.Memory("env", "memory", memory)
							consumerImports.Memory("env", "memory", memory)
						}
						var table *Table
						if borrowed {
							var err error
							table, err = NewTable(2, 2)
							if err != nil {
								t.Fatal(err)
							}
							defer table.Close()
							imports.Table("env", "table", table)
						}
						input, want := rv64NativeVectorInputs(types)
						var producer, consumer, leaf *Instance
						var handle *HostFuncRef
						var rt *Runtime
						calls := 0
						host := CallerHostCallFunc(func(caller Caller, call HostCall) {
							calls++
							if !slices.Equal(call.ParamSlots(), want) {
								panic("import reference typed transport")
							}
							if consumer != nil {
								got, err := consumer.InvokeFromHost(context.Background(), caller, "safe")
								if err != nil || !slices.Equal(got, []uint64{7}) {
									panic(fmt.Sprintf("import reference root reentry: %v %v", got, err))
								}
							}
							runtime.GC()
							copy(call.ResultSlots(), call.ParamSlots())
						})
						switch kind {
						case "native", "vector":
							n := rv64TableStageCompile(t, key+"-leaf", rv64PublicNativeIdentityModule(types), artifact)
							var err error
							leaf, err = Instantiate(n, NewImports())
							if err != nil {
								t.Fatal(err)
							}
							defer leaf.Close()
							ex, err := leaf.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							imports.Function("env", "host", ex)
						case "owned":
							rt = NewRuntime()
							defer rt.Close()
							var err error
							handle, err = rt.NewHostFuncRef(host, FuncSig{Params: []ValType{ValI32, ValI64, ValF32, ValF64}, Results: []ValType{ValI32, ValI64, ValF32, ValF64}})
							if err != nil {
								t.Fatal(err)
							}
							defer handle.Close()
							imports.Function("env", "host", handle)
						default:
							imports.Function("env", "host", host)
						}
						var err error
						if rt != nil {
							mod, e := rt.Module(p)
							if e != nil {
								t.Fatal(e)
							}
							defer mod.Close()
							producer, err = rt.Instantiate(context.Background(), mod, WithImports(imports))
						} else {
							producer, err = Instantiate(p, imports)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer producer.Close()
						if !borrowed {
							table, err = producer.ExportedTable("table")
							if err != nil {
								t.Fatal(err)
							}
						}
						consumerImports.Table("env", "table", table)
						if rt != nil {
							mod, e := rt.Module(c)
							if e != nil {
								t.Fatal(e)
							}
							defer mod.Close()
							consumer, err = rt.Instantiate(context.Background(), mod, WithImports(consumerImports))
						} else {
							consumer, err = Instantiate(c, consumerImports)
						}
						if err != nil {
							t.Fatal(err)
						}
						defer consumer.Close()
						if shared && (producer.jm.LinMemBase() != consumer.jm.LinMemBase() || producer.nativeContext == consumer.nativeContext) {
							t.Fatal("shared-home fixture")
						}
						args := append(slices.Clone(input), 0xfeedface00000000)
						foreignWant := append(slices.Clone(want), 7)
						if got, err := consumer.Invoke("call", args...); err != nil || !slices.Equal(got, foreignWant) {
							t.Fatal("imported element", got, err)
						}
						if _, err := producer.Invoke("install", 0xfeedface00000001); err != nil {
							t.Fatal("imported ref.func", err)
						}
						args[len(args)-1] = 1
						_ = producer.Close()
						_ = p.Close()
						if leaf != nil {
							_ = leaf.Close()
						}
						if handle != nil && handle.Close() == nil {
							t.Fatal("closed retained host binding")
						}
						if got, err := consumer.Invoke("call", args...); err != nil || !slices.Equal(got, foreignWant) || producer.resourcesClosed {
							t.Fatal("retired imported reference", got, err)
						}
						if leaf == nil && calls != 2 {
							t.Fatal("host callback count", calls)
						}
						for _, index := range []uint64{0, 1} {
							if _, err := consumer.Invoke("clear", index); err != nil {
								t.Fatal(err)
							}
						}
						if borrowed {
							if !producer.resourcesClosed || len(producer.importReferenceEntries()) != 0 || leaf != nil && !leaf.resourcesClosed {
								t.Fatal("last row retained import graph")
							}
							if handle != nil {
								if err := handle.Close(); err != nil {
									t.Fatal("final host owner release", err)
								}
							}
						}
					})
				}
			}
		}
	}
}

// Two imports share a signature but retain separate binding ordinals. No active
// initializer overwrites another writer: install0/install1 publish the handles.
func rv64DuplicateImportReferenceModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32}))),
		wasmtest.Section(2, wasmtest.Vec(
			append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 2),
			append(append(wasmtest.Name("env"), wasmtest.Name("a")...), 0, 0),
			append(append(wasmtest.Name("env"), wasmtest.Name("b")...), 0, 0))),
		wasmtest.Section(3, []byte{4, 1, 0, 0, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("clear", 0, 3), wasmtest.ExportEntry("install0", 0, 4), wasmtest.ExportEntry("install1", 0, 5))),
		wasmtest.Section(9, []byte{1, 3, 0, 2, 0, 1}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 0, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x26, 0, 0xfc, 16, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd2, 1, 0x26, 0, 0xfc, 16, 0, 0x0b}))))
}

func TestRV64PublicImportReferenceDuplicateBindings(t *testing.T) {
	for _, owned := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("owned%t-artifact%t", owned, artifact), func(t *testing.T) {
				p := rv64TableStageCompile(t, "import-ref-duplicates", rv64DuplicateImportReferenceModule(), artifact)
				c := rv64TableStageCompile(t, "import-ref-duplicates-consumer", rv64ImportedTableModule([]wasm.ValType{wasm.I32}, 7, false, false, true, 2), artifact)
				rt := NewRuntime()
				defer rt.Close()
				pm, err := rt.Module(p)
				if err != nil {
					t.Fatal(err)
				}
				defer pm.Close()
				cm, err := rt.Module(c)
				if err != nil {
					t.Fatal(err)
				}
				defer cm.Close()
				table, err := NewTable(2, 2)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				var handle *HostFuncRef
				if owned {
					handle, err = rt.NewHostFuncRef(func(v int32) int32 { return v + 11 }, FuncSig{Params: []ValType{ValI32}, Results: []ValType{ValI32}})
					if err != nil {
						t.Fatal(err)
					}
					defer handle.Close()
				}
				var writers [2]*Instance
				for i := range writers {
					var a, b any
					if owned {
						a, b = handle, handle
					} else {
						a = func(v int32) int32 { return v + int32(11+20*i) }
						b = func(v int32) int32 { return v + int32(13+20*i) }
					}
					writers[i], err = rt.Instantiate(context.Background(), pm, WithImports(NewImports().Table("env", "table", table).Function("env", "a", a).Function("env", "b", b)))
					if err != nil {
						t.Fatal(err)
					}
					defer writers[i].Close()
					if _, err := writers[i].Invoke(fmt.Sprintf("install%d", i), uint64(i)); err != nil {
						t.Fatal(err)
					}
				}
				if !owned && writers[0].importReferenceEntries()[0] != writers[1].importReferenceEntries()[0] {
					t.Fatal("ordinary shared thunk fixture")
				}
				consumer, err := rt.Instantiate(context.Background(), cm, WithImports(NewImports().Table("env", "table", table)))
				if err != nil {
					t.Fatal(err)
				}
				defer consumer.Close()
				for _, writer := range writers {
					_ = writer.Close()
				}
				for i := range writers {
					want := uint64(16)
					if !owned && i == 1 {
						want = 38
					}
					got, err := consumer.Invoke("call", 0xeeeeeeee00000005, uint64(i))
					if err != nil || !slices.Equal(got, []uint64{want, 7}) {
						t.Fatal("retired duplicate binding", i, got, err)
					}
				}
				if handle != nil && handle.Close() == nil {
					t.Fatal("released live duplicate host owner")
				}
				if _, err := consumer.Invoke("clear", 0); err != nil {
					t.Fatal(err)
				}
				if !writers[0].resourcesClosed || writers[1].resourcesClosed {
					t.Fatal("writer root separation")
				}
				if handle != nil && handle.Close() == nil {
					t.Fatal("released remaining duplicate host owner")
				}
				if _, err := consumer.Invoke("clear", 1); err != nil {
					t.Fatal(err)
				}
				if !writers[1].resourcesClosed {
					t.Fatal("final writer retained")
				}
				if handle != nil {
					if err := handle.Close(); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestRV64PublicImportReferencePassiveMutation(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			source := rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true)
			// Passive function0 is the imported proxy. A typed reference select and
			// local carry it through table.init, fill, copy, set, get and grow.
			source = rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, []byte{1, 1, 0, 1, 0}))
			source = rv64ReplaceTableStageSection(t, source, 4, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}))
			body := []byte{
				0x41, 0, 0x41, 0, 0x41, 1, 0xfc, 12, 0, 0, // table.init
				0x41, 0, 0x25, 0, 0x22, 1, // table.get, local.tee
				0x41, 1, 0xfc, 15, 0, 0x1a, // table.grow
				0x41, 0, 0x20, 1, 0x41, 2, 0xfc, 17, 0, // table.fill
				0x41, 1, 0x41, 0, 0x41, 1, 0xfc, 14, 0, 0, // table.copy
				0x41, 1, 0xd0, 0x70, 0x20, 1, 0x41, 0, 0x1c, 1, 0x70, 0x26, 0, // typed select -> set
				0xfc, 13, 0, // elem.drop
				0x20, 0, 0x41, 1, 0x11, 1, 0, 0x0b,
			}
			// First defined function has one additional funcref local. Remaining
			// bodies preserve the module's exported call/safe/clear/install signatures.
			withLocal := append([]byte{1, 1, 0x70}, body...)
			withLocal = append(wasmtest.ULEB(uint32(len(withLocal))), withLocal...)
			source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(withLocal,
				wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x11, 1, 0, 0x23, 0, 0x0b}),
				wasmtest.Code([]byte{0x23, 0, 0x0b}),
				wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b}),
				wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0x26, 0, 0xfc, 16, 0, 0x0b}))))
			c := rv64TableStageCompile(t, "import-ref-passive-mutation", source, artifact)
			in, err := Instantiate(c, NewImports().Function("env", "host", func(v int32) int32 { return v + 3 }))
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			if got, err := in.Invoke("target", 0xeeeeeeee00000005); err != nil || !slices.Equal(got, []uint64{8}) {
				t.Fatal("passive import reference mutations", got, err)
			}
			if got, err := in.Invoke("call", 9, 1); err != nil || !slices.Equal(got, []uint64{12, 9}) {
				t.Fatal("persisted grown imported row", got, err)
			}
			if _, err := in.Invoke("target", 5); err == nil {
				t.Fatal("dropped passive element reused")
			}
		})
	}
}

func TestRV64PublicImportReferencePluginState(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			c := rv64TableStageCompile(t, "import-ref-plugin-state", rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true), artifact)
			in, err := Instantiate(c, InstantiateOptions{
				Imports:         NewImports().Function("env", "host", func(v int32) int32 { return v + 1 }),
				pluginGCImports: map[uint32]struct{}{0: {}},
			})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			state := in.importState.Load()
			if state == nil || len(state.pluginGCImports) != 1 || len(in.importReferenceEntries()) != 1 {
				t.Fatal("construction replaced import state")
			}
			if got, err := in.Invoke("call", 5, 0); err != nil || !slices.Equal(got, []uint64{6, 9}) {
				t.Fatal("plugin import proxy authentication", got, err)
			}
		})
	}
}

func TestRV64PublicImportReferenceAuthentication(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source metadata corruption fixture")
	}
	p := rv64TableStageCompile(t, "import-ref-auth", rv64TableFunctionImportModule([]wasm.ValType{wasm.I32}, false, false, nil, false, true), false)
	in, err := Instantiate(p, NewImports().Function("env", "host", func(v int32) int32 { return v }))
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	scope := rv64NativeTableTargets{count: 1}
	scope.owners[0] = in
	desc := in.funcRefDescs[coreruntime.FuncRefDescBytes : 2*coreruntime.FuncRefDescBytes]
	row := slices.Clone(desc[:coreruntime.TableEntryBytes])
	if !scope.authenticatedRow(row) {
		t.Fatal("published import descriptor rejected")
	}
	// Matching mutations to BOTH row and canonical descriptor cannot substitute
	// another aligned executable entry for the independently published wrapper.
	old := slices.Clone(desc)
	defer copy(desc, old)
	binary.LittleEndian.PutUint64(desc, uint64(in.base)+uint64(in.c.Entry[0]))
	copy(row, desc)
	if scope.authenticatedRow(row) {
		t.Fatal("mutated wrapper authority accepted")
	}
}
