//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The same witnesses can prepare source artifacts and then run in a binary
// exercises source-compiled modules.
func rv64ForeignTableCompile(t *testing.T, types []wasm.ValType, seed byte, shared, trapping, artifact bool) *Compiled {
	t.Helper()

	// A precompiled runtime cannot execute a source path.

	c := rv64PublicStartCompile(t, rv64ForeignTableCallModule(types, seed, shared, trapping), artifact)

	return c
}

// The first parameter is i32. The target adds its own global, while the caller
// returns its own global after the indirect call to witness context restoration.
func rv64ForeignTableCallModule(types []wasm.ValType, seed byte, shared, trapping bool) []byte {
	callerTypes := append(slices.Clone(types), wasm.I32)
	sections := [][]byte{wasmtest.Section(1, wasmtest.Vec(
		wasmtest.FuncType(callerTypes, callerTypes), wasmtest.FuncType(types, types),
		wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
		wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}),
	))}
	if shared {
		sections = append(sections, wasmtest.Section(2, wasmtest.Vec(rv64VectorMemoryImport())))
	}
	caller, target := []byte{}, []byte{}
	if trapping {
		target = append(target, 0x20, 0, 0x45, 0x04, 0x40, 0x00, 0x0b)
	}
	for index := range types {
		caller = append(caller, 0x20)
		caller = append(caller, wasmtest.ULEB(uint32(index))...)
		target = append(target, 0x20)
		target = append(target, wasmtest.ULEB(uint32(index))...)
		if index == 0 {
			target = append(target, 0x23, 0, 0x6a)
		}
	}
	caller = append(caller, 0x20)
	caller = append(caller, wasmtest.ULEB(uint32(len(types)))...)
	caller = append(caller, 0x11, 1, 0, 0x23, 0, 0x0b)
	target = append(target, 0x0b)
	sections = append(sections,
		wasmtest.Section(3, []byte{4, 0, 1, 2, 3}),
		wasmtest.Section(4, []byte{1, 0x70, 1, 1, 2}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.I32, false, []byte{0x41, seed, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("call", 0, 0), wasmtest.ExportEntry("safe", 0, 2),
			wasmtest.ExportEntry("clear", 0, 3), wasmtest.ExportEntry("table", 1, 0),
			wasmtest.ExportEntry(fmt.Sprintf("target%d", seed), 0, 1))),
		ownedTableElements(0, 1),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(caller), wasmtest.Code(target),
			wasmtest.Code([]byte{0x23, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0xd0, 0x70, 0x26, 0, 0x23, 0, 0x0b}))),
	)
	return wasmtest.Module(sections...)
}

// These earlier owned-table bridge fixtures install a canonical row under the
// real table's lock and physical root, then use normal public entry. Genuine
// public imported-table construction and mutation have separate witnesses.
func rv64InstallForeignTableTarget(t *testing.T, root, producer *Instance) {
	t.Helper()
	rv64InstallForeignTableTargetAt(t, root, producer, 1)
}

func rv64InstallForeignTableTargetAt(t *testing.T, root, producer *Instance, localIndex int) {
	t.Helper()
	table, err := root.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := producer.localFuncrefDescriptor(localIndex)
	if !ok {
		t.Fatal("canonical producer descriptor")
	}
	offset := int(descriptor - uint64(offHeapSlicePtr(producer.funcRefDescs)))
	nativeExecutionMu.Lock()
	table.mu.Lock()
	copy(table.desc[8:8+coreruntime.TableEntryBytes], producer.funcRefDescs[offset:offset+coreruntime.TableEntryBytes])
	table.mu.Unlock()
	nativeExecutionMu.Unlock()
	// The still-open producer owns storage during setup; retain the container
	// root after the row becomes reachable and before retiring either owner.
	if !table.retainProducerInstance(producer) {
		t.Fatal("canonical producer retention")
	}
}

func TestRV64PublicForeignTableCallTypedContextAndRetirement(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, shared := range []bool{false, true} {
			for _, artifact := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-shared%t-artifact%t", vector, shared, artifact), func(t *testing.T) {
					types := []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}
					if vector {
						types = append(types, wasm.V128)
					}
					input, want := rv64NativeVectorInputs(types)
					want[0] = uint64(uint32(want[0] + 9))
					want = append(want, 7)
					input = append(input, 0xfeedface00000000) // dirty i32 table index
					var imports *Imports
					if shared {
						memory, err := NewSharedMemory(1, 4)
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
						imports = NewImports().Memory("env", "memory", memory)
					}
					rootCode := rv64ForeignTableCompile(t, types, 7, shared, false, artifact)
					producerCode := rv64ForeignTableCompile(t, types, 9, shared, false, artifact)
					root, err := Instantiate(rootCode, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer root.Close()
					producer, err := Instantiate(producerCode, imports)
					if err != nil {
						t.Fatal(err)
					}
					defer producer.Close()
					if !root.syncMode || !root.c.nativeTableContextABI || root.nativeContext == producer.nativeContext || (root.jm == producer.jm) != shared {
						t.Fatal("framed foreign context fixture")
					}
					rv64InstallForeignTableTarget(t, root, producer)
					rootCode.nativeTableContextABI = false // detached metadata cannot change execution
					if !root.c.nativeTableContextABI {
						t.Fatal("table ABI capability was not frozen")
					}
					if err := producer.Close(); err != nil {
						t.Fatal(err)
					}
					_ = producerCode.Close()
					_ = rootCode.Close()
					runtime.GC()
					fn, err := root.WasmFunc("call")
					if err != nil {
						t.Fatal(err)
					}
					for range 3 {
						if got, err := root.Invoke("call", input...); err != nil || !slices.Equal(got, want) {
							t.Fatal("foreign transport/context", got, want, err)
						}
						if got, err := fn.Invoke(input...); err != nil || !slices.Equal(got, want) {
							t.Fatal("prepared foreign transport", got, err)
						}
					}
					if n := testing.AllocsPerRun(50, func() {
						got, err := root.Invoke("call", input...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm foreign table entry")
						}
					}); n != 4 {
						t.Fatal("warm foreign table entry allocations", n)
					}
					// Materialized exported tables reconcile retained roots using the
					// existing descriptor-set maps for both owners. Bound that four-allocation cleanup
					// cost independently of entry-scope and bridge transport storage.
					if n := testing.AllocsPerRun(50, func() {
						got, err := fn.Invoke(input...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm foreign table call")
						}
					}); n != 4 {
						t.Fatal("warm foreign table allocations", n)
					}
					if _, err := root.Invoke("clear", 0); err != nil {
						t.Fatal(err)
					}
					if producer.hasPhysicalResources() || root.pluginState.Load().railshotInvocation.tableDepth != 0 {
						t.Fatal("cleared retired producer retained physical storage")
					}
					_, err = root.Invoke("call", input...)
					ownedTableTrap(t, err, coreruntime.TrapIndirectOutOfBounds)
				})
			}
		}
	}
}

func TestRV64PublicForeignTableColdProducerAndCallerReuse(t *testing.T) {
	for _, shared := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("shared%t-artifact%t", shared, artifact), func(t *testing.T) {
				var imports *Imports
				if shared {
					memory, err := NewSharedMemory(1, 4)
					if err != nil {
						t.Fatal(err)
					}
					defer memory.Close()
					imports = NewImports().Memory("env", "memory", memory)
				}
				types := []wasm.ValType{wasm.I32}
				root, err := Instantiate(rv64ForeignTableCompile(t, types, 7, shared, false, artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer root.Close()
				producer, err := Instantiate(rv64ForeignTableCompile(t, types, 9, shared, true, artifact), imports)
				if err != nil {
					t.Fatal(err)
				}
				defer producer.Close()
				rv64InstallForeignTableTarget(t, root, producer)
				_ = producer.Close()
				runtime.GC()
				for range 3 {
					_, err := root.InvokeContext(context.Background(), "call", 0, 0)
					trap := ownedTableTrap(t, err, coreruntime.TrapUnreachable)
					if len(trap.Frames) != 1 || trap.Frames[0].FunctionIndex != 1 || trap.Frames[0].FunctionName != "target9" || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 6 {
						t.Fatal("foreign producer cold provenance", trap.Frames)
					}
					if got, err := root.Invoke("call", 5, 0); err != nil || !slices.Equal(got, []uint64{14, 7}) {
						t.Fatal("foreign reuse after cold unwind", got, err)
					}
				}
				if _, err := root.Invoke("clear", 0); err != nil || producer.hasPhysicalResources() {
					t.Fatal("retired cold producer cleanup", err)
				}
			})
		}
	}
}
