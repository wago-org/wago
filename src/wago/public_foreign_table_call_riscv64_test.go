//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64ActualForeignTablePrecompiledCompilerGate(t *testing.T) {
	if os.Getenv("WAGO_RV64_FOREIGN_TABLE_ARTIFACT_MODE") != "load" {
		t.Skip("actual precompiled artifact qualification only")
	}
	source := rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, false, false)
	if c, err := Compile(nil, source); c != nil || err == nil || !strings.Contains(err.Error(), "source compilation is unavailable") {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("artifact qualification accidentally used a source compiler", err)
	}
}

// The same witnesses can prepare source artifacts and then run in a binary
// built with wago_precompiled, where source compilation is unavailable.
func rv64ForeignTableCompile(t *testing.T, types []wasm.ValType, seed byte, shared, trapping, artifact bool) *Compiled {
	t.Helper()
	dir := os.Getenv("WAGO_RV64_FOREIGN_TABLE_ARTIFACT_DIR")
	mode := os.Getenv("WAGO_RV64_FOREIGN_TABLE_ARTIFACT_MODE")
	if mode == "load" {
		if !artifact {
			// A precompiled runtime cannot execute a source path.
			t.Skip("actual precompiled runtime: artifact path only")
		}
		if dir == "" {
			t.Fatal("precompiled artifact directory missing")
		}
		blob, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf("types%d-seed%d-shared%t-trap%t.wago", len(types), seed, shared, trapping)))
		if err != nil {
			t.Fatal(err)
		}
		c, err := LoadTrustedArtifact(blob)
		if c != nil {
			t.Cleanup(func() { _ = c.Close() })
		}
		if err != nil {
			t.Fatal(err)
		}
		return c
	}
	c := rv64PublicStartCompile(t, rv64ForeignTableCallModule(types, seed, shared, trapping), artifact)
	if mode == "prepare" {
		if dir == "" {
			t.Fatal("source artifact output directory missing")
		}
		blob, err := c.MarshalBinary()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("types%d-seed%d-shared%t-trap%t.wago", len(types), seed, shared, trapping)), blob, 0600); err != nil {
			t.Fatal(err)
		}
	}
	return c
}

func TestRV64PublicForeignTableArtifactCapabilities(t *testing.T) {
	c := rv64PublicStartCompile(t, rv64ForeignTableCallModule([]wasm.ValType{wasm.I32}, 7, false, false), false)
	for _, flags := range []struct {
		name                    string
		reference, table, traps bool
	}{
		{"legacy-reference", false, true, true},
		{"reference-without-table", true, false, true},
		{"reference-without-traps", true, true, false},
	} {
		t.Run(flags.name, func(t *testing.T) {
			view := *c.executionView()
			view.nativeTableReferenceABI, view.nativeTableContextABI, view.nativeContextTrapABI = flags.reference, flags.table, flags.traps
			blob, err := marshalCompiled(&view)
			if err != nil {
				t.Fatal(err)
			}
			loaded, err := LoadTrustedArtifact(blob)
			if loaded != nil {
				defer loaded.Close()
			}
			if err == nil || loaded != nil && loaded.loadCodeCache() != nil && (loaded.codeCache.base != 0 || loaded.codeCache.sealed) {
				t.Fatal("unqualified reference artifact reached publication", err)
			}
			if !flags.reference && !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("legacy reference ABI error", err)
			}
		})
	}
	// The execution view owns frozen capabilities even if a public mirror changes.
	c.nativeTableReferenceABI = false
	if !c.executionView().nativeTableReferenceABI {
		t.Fatal("reference ABI was not frozen")
	}
	blobFrozen, err := c.MarshalBinary()
	if err != nil {
		t.Fatal(err)
	}
	frozen, err := LoadTrustedArtifact(blobFrozen)
	if frozen != nil {
		defer frozen.Close()
	}
	if err != nil || !frozen.executionView().nativeTableReferenceABI || uint64(frozen.executionView().requiredFeatures)&compiledRV64TableReferenceABI != 0 {
		t.Fatal("frozen reference capability or feature stripping", err)
	}
	view := *c.executionView()
	view.nativeTableContextABI = false
	view.nativeTableReferenceABI = false
	blob, err := marshalCompiled(&view)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err := LoadTrustedArtifact(blob)
	if loaded != nil {
		defer loaded.Close()
	}
	if !errors.Is(err, railshot.ErrUnsupported) || loaded != nil && loaded.loadCodeCache() != nil && (loaded.codeCache.base != 0 || loaded.codeCache.sealed) {
		t.Fatal("legacy table artifact reached executable publication", err)
	}
	view.nativeTableContextABI, view.nativeContextTrapABI = true, false
	blob, err = marshalCompiled(&view)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadTrustedArtifact(blob)
	if loaded != nil {
		defer loaded.Close()
	}
	if err == nil || loaded != nil && loaded.loadCodeCache() != nil && (loaded.codeCache.base != 0 || loaded.codeCache.sealed) {
		t.Fatal("table ABI without native trap context reached executable publication", err)
	}
	// Table-free code has no new bridge dependency; its existing framed artifact
	// contract remains sufficient when the new capability is absent.
	plain := rv64PublicStartCompile(t, rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
	view = *plain.executionView()
	view.nativeTableContextABI = false
	view.nativeTableReferenceABI = false
	blob, err = marshalCompiled(&view)
	if err != nil {
		t.Fatal(err)
	}
	loaded, err = LoadTrustedArtifact(blob)
	if err != nil {
		t.Fatal("table-free artifact compatibility", err)
	}
	defer loaded.Close()
	if loaded.executionView().nativeTableContextABI || loaded.executionView().nativeTableReferenceABI || uint64(loaded.executionView().requiredFeatures)&(compiledRV64TableContextABI|compiledRV64TableReferenceABI) != 0 {
		t.Fatal("internal table capability leaked into core features")
	}
	view.nativeTableContextABI = true
	blob, err = marshalCompiled(&view)
	if err != nil {
		t.Fatal(err)
	}
	legacyReference, err := LoadTrustedArtifact(blob)
	if legacyReference != nil {
		defer legacyReference.Close()
	}
	if err != nil || legacyReference.executionView().nativeTableReferenceABI {
		t.Fatal("table-free bit53-only artifact compatibility", err)
	}
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
	table, err := root.ExportedTable("table")
	if err != nil {
		t.Fatal(err)
	}
	descriptor, ok := producer.localFuncrefDescriptor(1)
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
					}); n != 2 {
						t.Fatal("warm foreign table entry allocations", n)
					}
					// Materialized exported tables reconcile retained roots using the
					// existing descriptor-set map. Bound that two-allocation cleanup
					// cost independently of entry-scope and bridge transport storage.
					if n := testing.AllocsPerRun(50, func() {
						got, err := fn.Invoke(input...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm foreign table call")
						}
					}); n != 2 {
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
