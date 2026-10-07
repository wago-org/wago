//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
	"unsafe"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// Function import 0 uses type 1. Defined table target 1 calls that import;
// reference descriptors and element entries retain the full Wasm ordinal.
func rv64TableFunctionImportModule(types []wasm.ValType, borrowed, shared bool, tail []byte, scalarCallback ...bool) []byte {
	scalarHost := len(scalarCallback) != 0 && scalarCallback[0]
	referenceIndex := byte(1)
	if len(scalarCallback) > 1 && scalarCallback[1] {
		referenceIndex = 0
	}
	importType := byte(1)
	if scalarHost {
		importType = 3
	}
	callerTypes := append(slices.Clone(types), wasm.I32)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(types, types), wasmtest.FuncType(callerTypes, callerTypes), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32})))}
	imports := [][]byte{}
	if borrowed {
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 2))
	}
	if shared {
		imports = append(imports, rv64VectorMemoryImport())
	}
	imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, importType))
	sections = append(sections, wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{5, 1, 2, 0, 3, 3}))
	if !borrowed {
		sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 2, 2}))
	}
	sections = append(sections, wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, 9, 0x0b}))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 1), wasmtest.ExportEntry("call", 0, 2), wasmtest.ExportEntry("safe", 0, 3), wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("install", 0, 5), wasmtest.ExportEntry("table", 1, 0))), ownedTableElements(0, uint32(referenceIndex)))
	var target, caller []byte
	for i := range types {
		target = append(target, 0x20, byte(i))
		caller = append(caller, 0x20, byte(i))
		if i == 0 {
			target = append(target, 0x23, 0, 0x6a)
		}
	}
	if scalarHost {
		target = append(target, 0x20, 0, 0x23, 0, 0x6a, 0x10, 0, 0x1a)
	} else {
		target = append(target, 0x10, 0)
	}
	target = append(target, tail...)
	target = append(target, 0x0b)
	caller = append(caller, 0x20, byte(len(types)), 0x11, 1, 0, 0x23, 0, 0x0b)
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(caller), wasmtest.Code([]byte{0x23, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0xfc, 16, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd2, referenceIndex, 0x26, 0, 0xfc, 16, 0, 0x0b}))))
	return wasmtest.Module(sections...)
}

func TestRV64PublicTableFunctionImportsTransport(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, borrowed := range []bool{false, true} {
			for _, shared := range []bool{false, true} {
				for _, artifact := range []bool{false, true} {
					if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
						continue
					}
					t.Run(fmt.Sprintf("vector%t-borrowed%t-shared%t-artifact%t", vector, borrowed, shared, artifact), func(t *testing.T) {
						types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
						if vector {
							types = append(types, wasm.V128)
						}
						key := fmt.Sprintf("combined-types%d-borrowed%t-shared%t", len(types), borrowed, shared)
						p := rv64TableStageCompile(t, key+"-producer", rv64TableFunctionImportModule(types, borrowed, shared, nil), artifact)
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
						want[0] = uint64(uint32(want[0] + 9))
						var consumer, producer *Instance
						var leaf *Instance
						calls := 0
						if vector {
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
						} else {
							imports.Function("env", "host", CallerHostCallFunc(func(caller Caller, call HostCall) {
								calls++
								if !slices.Equal(call.ParamSlots(), want) {
									panic("mixed table-host raw transport")
								}
								if consumer != nil {
									got, err := consumer.InvokeFromHost(context.Background(), caller, "safe")
									if err != nil || !slices.Equal(got, []uint64{7}) {
										panic(fmt.Sprintf("table root reentry: %v %v", got, err))
									}
								}
								if shared && len(caller.Memory()) != 65536 {
									panic("table target host memory")
								}
								runtime.GC()
								copy(call.ResultSlots(), call.ParamSlots())
							}))
						}
						var err error
						producer, err = Instantiate(p, imports)
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
						consumer, err = Instantiate(c, consumerImports.Table("env", "table", table))
						if err != nil {
							t.Fatal(err)
						}
						defer consumer.Close()
						args := append(slices.Clone(input), 0xfeedface00000000)
						localWant := append(slices.Clone(want), 9)
						foreignWant := append(slices.Clone(want), 7)
						for range 3 {
							if got, err := producer.Invoke("call", args...); err != nil || !slices.Equal(got, localWant) {
								t.Fatal("local table -> import", got, localWant, err)
							}
							if got, err := consumer.Invoke("call", args...); err != nil || !slices.Equal(got, foreignWant) {
								t.Fatal("foreign table -> import", got, foreignWant, err)
							}
						}
						if !vector && calls != 6 {
							t.Fatal("callback count", calls)
						}
						// A defined ref.func follows the imported prefix and stays callable at row1.
						if got, err := producer.Invoke("install", 0xfeedface00000001); err != nil || !slices.Equal(got, []uint64{2}) {
							t.Fatal("defined ref.func", got, err)
						}
						args[len(args)-1] = 1
						_ = producer.Close()
						_ = p.Close()
						if leaf != nil {
							_ = leaf.Close()
						}
						if got, err := consumer.Invoke("call", args...); err != nil || !slices.Equal(got, foreignWant) || producer.resourcesClosed {
							t.Fatal("retired table/import graph", got, err)
						}
						for _, index := range []uint64{0, 1} {
							if _, err := consumer.Invoke("clear", index); err != nil {
								t.Fatal(err)
							}
						}
						// Imported storage has no producer attachment once its last row disappears.
						if borrowed && (!producer.resourcesClosed || p.codeCache.mem != nil || leaf != nil && !leaf.resourcesClosed) {
							t.Fatal("retired graph survived last table row")
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicTableFunctionImportsArtifactKeys(t *testing.T) {
	for _, vector := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32}
		if vector {
			types = append(types, wasm.V128)
		}
		c := rv64PublicStartCompile(t, rv64TableFunctionImportModule(types, false, false, nil), false)
		for _, index := range []int{0, 1, len(c.FuncTypeID) - 1} {
			v := *c.executionView()
			v.FuncTypeID = slices.Clone(v.FuncTypeID)
			v.FuncTypeID[index] ^= 1
			if err := railshotValidateCompiled(&v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("corrupt imported/defined key admitted", index, err)
			}
			blob, err := marshalCompiled(&v)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				loaded.Close()
			}
			if err == nil {
				t.Fatal("corrupt key artifact admitted", index)
			}
		}
		for _, index := range []uint32{uint32(c.NumImports + len(c.Funcs))} {
			v := *c.executionView()
			v.Elems = slices.Clone(v.Elems)
			v.Elems[0].Values = slices.Clone(v.Elems[0].Values)
			v.Elems[0].Values[0].FuncIndex = index
			if err := railshotValidateCompiled(&v); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("out-of-bounds element admitted", index, err)
			}
		}
	}
}

func TestRV64PublicTableFunctionImportsColdAttribution(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
				continue
			}
			types := []wasm.ValType{wasm.I32}
			if vector {
				types = append(types, wasm.V128)
			}
			key := fmt.Sprintf("combined-cold-types%d", len(types))
			p := rv64TableStageCompile(t, key+"-producer", rv64TableFunctionImportModule(types, true, false, []byte{0x00}), artifact)
			n := rv64TableStageCompile(t, key+"-leaf", rv64PublicNativeIdentityModule(types), artifact)
			c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, false, false, true, 2), artifact)
			leaf, err := Instantiate(n, NewImports())
			if err != nil {
				t.Fatal(err)
			}
			defer leaf.Close()
			ex, err := leaf.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			table, err := NewTable(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			producer, err := Instantiate(p, NewImports().Table("env", "table", table).Function("env", "host", ex))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			consumer, err := Instantiate(c, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			input, _ := rv64NativeVectorInputs(types)
			for range 3 {
				_, err := consumer.Invoke("call", append(slices.Clone(input), 0)...)
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].FunctionName != "target" || (!trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != uint32(6+2*len(types))) {
					t.Fatal("table target post-import cold attribution", err)
				}
				got, err := consumer.Invoke("safe")
				if err != nil || !slices.Equal(got, []uint64{7}) {
					t.Fatal("cold reuse", got, err)
				}
			}
		}
	}
}

func TestRV64PublicTableFunctionImportsParkedCancellation(t *testing.T) {
	rv64TableFunctionImportsParkedCancellation(t, false)
}

func TestRV64PublicImportReferenceParkedCancellation(t *testing.T) {
	rv64TableFunctionImportsParkedCancellation(t, true)
}

func rv64TableFunctionImportsParkedCancellation(t *testing.T, importedReference bool) {
	vectors := []bool{false, true}
	if importedReference {
		vectors = []bool{false}
	}
	for _, vector := range vectors {
		for _, artifact := range []bool{false, true} {
			if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
				continue
			}
			for _, closeRoot := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-close%t", vector, artifact, closeRoot), func(t *testing.T) {
					types := []wasm.ValType{wasm.I32, wasm.F32, wasm.F64}
					if vector {
						types = append(types, wasm.V128)
					}
					if importedReference {
						types = []wasm.ValType{wasm.I32}
					}
					key := fmt.Sprintf("combined-park-types%d-ref%t", len(types), importedReference)
					// This post-callback write must not execute after the public root cancels.
					p := rv64TableStageCompile(t, key+"-producer", rv64TableFunctionImportModule(types, true, false, []byte{0x41, 1, 0xd2, 1, 0x26, 0}, true, importedReference), artifact)
					c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, false, false, true, 2, false, importedReference), artifact)
					if importedReference {
						// Establish that this consumer really executes the post-call
						// sentinel when the identical proxy path returns normally.
						func() {
							probeTable, err := NewTable(2, 2)
							if err != nil {
								t.Fatal(err)
							}
							defer probeTable.Close()
							probeProducer, err := Instantiate(p, NewImports().Table("env", "table", probeTable).Function("env", "host", func(v int32) int32 { return v + 1 }))
							if err != nil {
								t.Fatal(err)
							}
							defer probeProducer.Close()
							probeConsumer, err := Instantiate(c, NewImports().Table("env", "table", probeTable))
							if err != nil {
								t.Fatal(err)
							}
							defer probeConsumer.Close()
							if got, err := probeConsumer.Invoke("call", 5, 0); err != nil || !slices.Equal(got, []uint64{6, 7}) {
								t.Fatal("uncancelled proxy control", got, err)
							}
							if bytes.Equal(probeTable.desc[8+coreruntime.TableEntryBytes:8+2*coreruntime.TableEntryBytes], make([]byte, coreruntime.TableEntryBytes)) {
								t.Fatal("uncancelled proxy did not write sentinel")
							}
						}()
					}

					table, err := NewTable(2, 2)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					entered, release := make(chan struct{}), make(chan struct{})
					var once sync.Once
					unblock := func() { once.Do(func() { close(release) }) }
					defer unblock()
					producer, err := Instantiate(p, NewImports().Table("env", "table", table).Function("env", "host", func(v int32) int32 { close(entered); <-release; return v + 1 }))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					consumer, err := Instantiate(c, NewImports().Table("env", "table", table))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					cleaner, err := Instantiate(c, NewImports().Table("env", "table", table))
					if err != nil {
						t.Fatal(err)
					}
					defer cleaner.Close()
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					input, _ := rv64NativeVectorInputs(types)
					done := make(chan error, 1)
					finished := make(chan struct{})
					go func() {
						defer close(finished)
						_, err := consumer.InvokeContext(ctx, "call", append(input, 0)...)
						done <- err
					}()
					defer func() { cancel(); unblock(); <-finished }()
					select {
					case <-entered:
					case <-time.After(5 * time.Second):
						t.Fatal("table target did not park")
					}
					if _, err := cleaner.Invoke("clear", 0); err != nil {
						t.Fatal(err)
					}
					_ = producer.Close()
					_ = p.Close()
					if producer.resourcesClosed || p.codeCache.mem == nil {
						t.Fatal("parked last-row producer released")
					}
					if closeRoot {
						_ = consumer.Close()
					} else {
						cancel()
					}
					deadline := time.Now().Add(5 * time.Second)
					for atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					armed := atomic.LoadUint32((*uint32)(unsafe.Pointer(&consumer.trap[0]))) == uint32(coreruntime.TrapInterrupted)
					unblock()
					select {
					case err := <-done:
						if !armed || err == nil || !closeRoot && !errors.Is(err, context.Canceled) {
							t.Fatal("parked table cancellation", armed, err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("parked table invocation stalled")
					}
					if !bytes.Equal(table.desc[8:8+2*coreruntime.TableEntryBytes], make([]byte, 2*coreruntime.TableEntryBytes)) {
						t.Fatal("post-cancellation table write escaped")
					}
					if !producer.resourcesClosed || p.codeCache.mem != nil {
						t.Fatal("parked scope retained last-row producer after join")
					}
					if got, err := cleaner.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{7}) {
						t.Fatal("parked cold reuse", got, err)
					}
				})
			}
		}
	}
}

func TestRV64PublicTableFunctionImportsCallbackReplacement(t *testing.T) {
	rv64TableFunctionImportsCallbackReplacement(t, false)
}

func TestRV64PublicImportReferenceCallbackReplacement(t *testing.T) {
	rv64TableFunctionImportsCallbackReplacement(t, true)
}

func rv64TableFunctionImportsCallbackReplacement(t *testing.T, importedReference bool) {
	vectors := []bool{false, true}
	if importedReference {
		vectors = []bool{false}
	}
	for _, vector := range vectors {
		for _, artifact := range []bool{false, true} {
			if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
				continue
			}
			for _, panics := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-panic%t", vector, artifact, panics), func(t *testing.T) {
					types := []wasm.ValType{wasm.I32, wasm.F32, wasm.F64}
					if vector {
						types = append(types, wasm.V128)
					}
					if importedReference {
						types = []wasm.ValType{wasm.I32}
					}
					key := fmt.Sprintf("combined-replace-types%d-ref%t", len(types), importedReference)
					p := rv64TableStageCompile(t, key+"-producer", rv64TableFunctionImportModule(types, true, false, nil, true, importedReference), artifact)
					c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, false, false, true, 2), artifact)
					replacement := rv64TableStageCompile(t, key+"-replacement", rv64ImportedTableModule(types, 19, false, true, true, 2), artifact)
					table, err := NewTable(2, 2)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					var producer, next, consumer *Instance
					marker := errors.New("table callback panic")
					producer, err = Instantiate(p, NewImports().Table("env", "table", table).Function("env", "host", CallerHostCallFunc(func(caller Caller, call HostCall) {
						// Last-row replacement happens while the old native target has live FP/SIMD
						// operand values and may still resume. Its scope must retain the old owner.
						var err error
						next, err = Instantiate(replacement, NewImports().Table("env", "table", table))
						if err != nil {
							panic(err)
						}
						_ = producer.Close()
						_ = p.Close()
						if producer.resourcesClosed {
							panic("old target released during callback")
						}
						got, err := consumer.InvokeFromHost(context.Background(), caller, "safe")
						if err != nil || !slices.Equal(got, []uint64{7}) {
							panic("replacement callback reentry")
						}
						runtime.GC()
						if panics {
							panic(marker)
						}
						call.SetI32(0, call.I32(0)+1)
					})))
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					defer func() {
						if next != nil {
							next.Close()
						}
					}()
					consumer, err = Instantiate(c, NewImports().Table("env", "table", table))
					if err != nil {
						t.Fatal(err)
					}
					defer consumer.Close()
					input, want := rv64NativeVectorInputs(types)
					increment := uint64(9)
					if importedReference {
						increment = 1
					}
					want[0] = uint64(uint32(want[0] + increment))
					want = append(want, 7)
					var caught any
					func() {
						defer func() { caught = recover() }()
						got, err := consumer.Invoke("call", append(slices.Clone(input), 0)...)
						if err != nil || !slices.Equal(got, want) {
							t.Fatal("live values across table callback", got, want, err)
						}
					}()
					if panics && caught != marker || !panics && caught != nil {
						t.Fatal("callback panic identity", caught)
					}
					if !producer.resourcesClosed || p.codeCache.mem != nil {
						t.Fatal("replaced target retained after unwinding")
					}
					want[0] = uint64(uint32(input[0] + 19))
					if got, err := consumer.Invoke("call", append(input, 0)...); err != nil || !slices.Equal(got, want) {
						t.Fatal("replacement and cold reuse", got, want, err)
					}
				})
			}
		}
	}
}

func rv64TableFunctionImportBoundaryModule(types []wasm.ValType, globals int) []byte {
	imports := [][]byte{}
	for i := 0; i < globals; i++ {
		imports = append(imports, wasmtest.GlobalImportEntry("env", fmt.Sprintf("g%d", i), wasm.I32, false))
	}
	imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 2), rv64VectorMemoryImport())
	for i := 0; i < 256; i++ {
		typ := byte(0)
		if i == 255 {
			typ = 1
		}
		imports = append(imports, append(append(wasmtest.Name("env"), wasmtest.Name(fmt.Sprintf("f%d", i))...), 0, typ))
	}
	var args []byte
	for i := range types {
		args = append(args, 0x20)
		args = append(args, wasmtest.ULEB(uint32(i))...)
	}
	target := append(slices.Clone(args), 0x10, 0xff, 1, 0x0b)
	caller := append(slices.Clone(args), 0x41, 0, 0x11, 1, 0, 0x0b)
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(types, types))), wasmtest.Section(2, wasmtest.Vec(imports...)), wasmtest.Section(3, []byte{2, 1, 1}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 256), wasmtest.ExportEntry("call", 0, 257))), ownedTableElements(0, 256), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(target), wasmtest.Code(caller))))
}

func TestRV64PublicTableFunctionImportsAggregateBoundary(t *testing.T) {
	rv64TableFunctionImportsAggregateBoundary(t, false)
}

func TestRV64PublicImportReferenceAggregateBoundary(t *testing.T) {
	rv64TableFunctionImportsAggregateBoundary(t, true)
}

func rv64TableFunctionImportsAggregateBoundary(t *testing.T, importedReference bool, expressionReference ...bool) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
				continue
			}
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				types := []wasm.ValType{wasm.I32, wasm.F64}
				if vector {
					for len(types) < 128 {
						types = append(types, wasm.V128)
					}
				}
				key := fmt.Sprintf("combined-bound-types%d-ref%t", len(types), importedReference)
				source := rv64TableFunctionImportBoundaryModule(types, 256)
				if importedReference {
					source = rv64ReplaceTableStageSection(t, source, 9, ownedTableElements(0, 255))
				}
				if len(expressionReference) != 0 && expressionReference[0] {
					key += "-expr"
					source = rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec(rv64ExpressionSegment(6, 0, []byte{0xd2, 0xff, 1, 0x0b}))))
				}
				globalOffset := len(expressionReference) > 1 && expressionReference[1]
				if globalOffset {
					key += "-global-offset"
					source = rv64ReplaceTableStageSection(t, source, 9, wasmtest.Section(9, wasmtest.Vec([]byte{6, 0, 0x23, 0xff, 1, 0x0b, 0x70, 1, 0xd2, 0xff, 1, 0x0b})))
				}
				c := rv64TableStageCompile(t, key+"-caller", source, artifact)
				n := rv64TableStageCompile(t, key+"-leaf", rv64PublicNativeIdentityModule(types), artifact)
				if c.NumImports != 256 || len(c.GlobalImports) != 256 || c.memoryImportCount() != 1 || c.tableImportCount() != 1 || c.FuncTypeID[0] == c.FuncTypeID[255] || c.FuncTypeID[255] != c.FuncTypeID[256] {
					t.Fatal("514-import namespace/keys")
				}
				leaf, err := Instantiate(n, NewImports())
				if err != nil {
					t.Fatal(err)
				}
				defer leaf.Close()
				ex, err := leaf.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				table, err := NewTable(2, 2)
				if err != nil {
					t.Fatal(err)
				}
				defer table.Close()
				memory, err := NewSharedMemory(1, 4)
				if err != nil {
					t.Fatal(err)
				}
				defer memory.Close()
				g := NewGlobalI32(9, false)
				defer g.Close()
				lastGlobal := g
				if globalOffset {
					lastGlobal = NewGlobalI32(0, false)
					defer lastGlobal.Close()
				}
				imports := NewImports().Table("env", "table", table).Memory("env", "memory", memory)
				for i := 0; i < 256; i++ {
					boundGlobal := g
					if i == 255 {
						boundGlobal = lastGlobal
					}
					imports.Global("env", fmt.Sprintf("g%d", i), boundGlobal)
					if i == 255 {
						imports.Function("env", "f255", ex)
					} else {
						imports.Function("env", fmt.Sprintf("f%d", i), HostCallFunc(func(HostCall) { panic("wrong imported function ordinal") }))
					}
				}
				in, err := Instantiate(c, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				input, want := rv64NativeVectorInputs(types)
				if got, err := in.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
					t.Fatal("import255/defined256 execution", got, want, err)
				}
			})
		}
	}
	// The total and independent global caps reject before code publication.
	for _, vector := range []bool{false, true} {
		types := []wasm.ValType{wasm.I32}
		if vector {
			types = append(types, wasm.V128)
		}
		source := rv64TableFunctionImportBoundaryModule(types, 257)
		if err := railshot.AdmitProductionSource(source); !errors.Is(err, railshot.ErrUnsupported) {
			t.Fatal("515 imports admitted", err)
		}
	}
}

func TestRV64PublicTableFunctionImportsDeepestCold(t *testing.T) {
	rv64TableFunctionImportsDeepestCold(t, false)
}

func TestRV64PublicImportReferenceDeepestCold(t *testing.T) {
	rv64TableFunctionImportsDeepestCold(t, true)
}

func rv64TableFunctionImportsDeepestCold(t *testing.T, importedReference bool) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
				continue
			}
			types := []wasm.ValType{wasm.I32, wasm.F64}
			if vector {
				types = append(types, wasm.V128)
			}
			key := fmt.Sprintf("combined-deep-cold-types%d-ref%t", len(types), importedReference)
			leafSource := wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(types, types))), wasmtest.Section(3, []byte{1, 0}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("deepest", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x00, 0x0b}))))
			p := rv64TableStageCompile(t, key+"-producer", rv64TableFunctionImportModule(types, true, false, nil, false, importedReference), artifact)
			c := rv64TableStageCompile(t, key+"-consumer", rv64ImportedTableModule(types, 7, false, false, true, 2), artifact)
			n := rv64TableStageCompile(t, key+"-leaf", leafSource, artifact)
			leaf, err := Instantiate(n, NewImports())
			if err != nil {
				t.Fatal(err)
			}
			defer leaf.Close()
			ex, err := leaf.ExportedFunc("deepest")
			if err != nil {
				t.Fatal(err)
			}
			table, err := NewTable(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			producer, err := Instantiate(p, NewImports().Table("env", "table", table).Function("env", "host", ex))
			if err != nil {
				t.Fatal(err)
			}
			defer producer.Close()
			consumer, err := Instantiate(c, NewImports().Table("env", "table", table))
			if err != nil {
				t.Fatal(err)
			}
			defer consumer.Close()
			_ = leaf.Close()
			_ = n.Close()
			_ = producer.Close()
			_ = p.Close()
			input, _ := rv64NativeVectorInputs(types)
			_, err = consumer.Invoke("call", append(input, 0)...)
			var trap *coreruntime.TrapError
			if !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 0 || trap.Frames[0].FunctionName != "deepest" || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 1 {
				t.Fatal("deepest retired import source", err)
			}
			if got, err := consumer.Invoke("safe"); err != nil || !slices.Equal(got, []uint64{7}) {
				t.Fatal("deepest cold reuse", got, err)
			}
			if _, err := consumer.Invoke("clear", 0); err != nil {
				t.Fatal(err)
			}
			if !producer.resourcesClosed || !leaf.resourcesClosed || p.codeCache.mem != nil || n.codeCache.mem != nil {
				t.Fatal("deepest retired graph retained")
			}
		}
	}
}

func TestRV64PublicTableFunctionImportsFailedConstruction(t *testing.T) {
	rv64TableFunctionImportsFailedConstruction(t, false)
}

func TestRV64PublicImportReferenceFailedConstruction(t *testing.T) {
	rv64TableFunctionImportsFailedConstruction(t, true)
}

func rv64TableFunctionImportsFailedConstruction(t *testing.T, importedReference bool) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
				continue
			}
			types := []wasm.ValType{wasm.I32}
			if vector {
				types = append(types, wasm.V128)
			}
			key := fmt.Sprintf("combined-construction-types%d-ref%t", len(types), importedReference)
			index := uint32(1)
			if importedReference {
				index = 0
			}
			source := rv64ReplaceTableStageSection(t, rv64TableFunctionImportModule(types, true, false, nil), 9, ownedTableElements(2, index))
			p := rv64TableStageCompile(t, key+"-invalid-elements", source, artifact)
			n := rv64TableStageCompile(t, key+"-leaf", rv64PublicNativeIdentityModule(types), artifact)
			leaf, err := Instantiate(n, NewImports())
			if err != nil {
				t.Fatal(err)
			}
			defer leaf.Close()
			ex, err := leaf.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			table, err := NewTable(2, 2)
			if err != nil {
				t.Fatal(err)
			}
			defer table.Close()
			before := leaf.referenceLifetime().snapshot().ResourceRoots
			for range 3 {
				in, err := Instantiate(p, NewImports().Table("env", "table", table).Function("env", "host", ex))
				if in != nil {
					in.Close()
				}
				if err == nil {
					t.Fatal("out-of-bounds active element admitted")
				}
				if table.owner.importers != 0 || table.owner.nativeLeases != 0 || len(table.retained) != 0 || leaf.referenceLifetime().snapshot().ResourceRoots != before {
					t.Fatal("failed construction retained table/native attachments")
				}
			}
			_ = leaf.Close()
			_ = n.Close()
			if !leaf.resourcesClosed || n.codeCache.mem != nil {
				t.Fatal("failed table construction pinned native target")
			}
		}
	}
}

func TestRV64PublicTableFunctionImportsStart(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		if !artifact && os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
			continue
		}
		for _, imported := range []bool{false, true} {
			for _, trapping := range []bool{false, true} {
				if imported && trapping {
					continue
				}
				t.Run(fmt.Sprintf("artifact%t-imported%t-trap%t", artifact, imported, trapping), func(t *testing.T) {
					types := []wasm.ValType{wasm.I32}
					source := rv64TableFunctionImportModule(types, true, false, nil)
					typeSection := wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, types), wasmtest.FuncType(types, types), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I32}, []wasm.ValType{wasm.I32, wasm.I32}), wasmtest.FuncType(types, types), wasmtest.FuncType(nil, nil)))
					source = rv64ReplaceTableStageSection(t, source, 1, typeSection)
					m, err := wasm.DecodeModule(source)
					if err != nil {
						t.Fatal(err)
					}
					var bodies [][]byte
					for _, fn := range m.Code {
						bodies = append(bodies, wasmtest.Code(fn.BodyBytes))
					}
					if imported {
						imports := [][]byte{append(append(wasmtest.Name("env"), wasmtest.Name("table")...), 1, 0x70, 1, 2, 2), append(append(wasmtest.Name("env"), wasmtest.Name("host")...), 0, 1), append(append(wasmtest.Name("env"), wasmtest.Name("start")...), 0, 4)}
						source = rv64ReplaceTableStageSection(t, source, 2, wasmtest.Section(2, wasmtest.Vec(imports...)))
						var exports [][]byte
						for _, ex := range m.Exports {
							index := ex.Index.Index
							if ex.Index.Kind == wasm.ExternFunc {
								index++
							}
							exports = append(exports, wasmtest.ExportEntry(ex.Name, byte(ex.Index.Kind), index))
						}
						source = rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(exports...)))
						source = rv64ReplaceTableStageSection(t, source, 9, append(wasmtest.Section(8, []byte{1}), ownedTableElements(0, 2)...))
						bodies[4] = wasmtest.Code([]byte{0x20, 0, 0xd2, 2, 0x26, 0, 0xfc, 16, 0, 0x0b})
					} else {
						source = rv64ReplaceTableStageSection(t, source, 3, wasmtest.Section(3, []byte{6, 1, 2, 0, 3, 3, 4}))
						body := []byte{0x41, 7, 0x41, 0, 0x10, 2, 0x1a, 0x1a}
						if trapping {
							body = append(body, 0)
						}
						body = append(body, 0x0b)
						bodies = append(bodies, wasmtest.Code(body))
						source = rv64ReplaceTableStageSection(t, source, 9, append(wasmtest.Section(8, []byte{6}), ownedTableElements(0, 1)...))
					}
					source = rv64ReplaceTableStageSection(t, source, 10, wasmtest.Section(10, wasmtest.Vec(bodies...)))
					key := fmt.Sprintf("combined-start-imported%t-trap%t", imported, trapping)
					c := rv64TableStageCompile(t, key, source, artifact)
					table, err := NewTable(2, 2)
					if err != nil {
						t.Fatal(err)
					}
					defer table.Close()
					calls, starts := 0, 0
					imports := NewImports().Table("env", "table", table).Function("env", "host", HostCallFunc(func(call HostCall) { calls++; call.SetI32(0, call.I32(0)+1) }))
					if imported {
						imports.Function("env", "start", HostCallFunc(func(HostCall) { starts++ }))
					}
					in, err := Instantiate(c, imports)
					if in != nil {
						defer in.Close()
					}
					if trapping {
						var trap *coreruntime.TrapError
						if in != nil || !errors.As(err, &trap) || trap.Code != coreruntime.TrapUnreachable || calls != 1 || table.owner.importers != 0 || table.owner.nativeLeases != 0 {
							t.Fatal("table-host start failure", in, err, calls)
						}
						return
					}
					if err != nil || imported && starts != 1 || !imported && calls != 1 {
						t.Fatal("table-host start", err, calls, starts)
					}
					if got, err := in.Invoke("call", 7, 0); err != nil || !slices.Equal(got, []uint64{17, 9}) {
						t.Fatal("post-start table/import", got, err)
					}
					if imported && starts != 1 || !imported && calls != 2 {
						t.Fatal("start repeated", calls, starts)
					}
				})
			}
		}
	}
}
