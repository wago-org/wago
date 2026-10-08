//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
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

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64LocalMutableReferenceModule(vector bool) []byte {
	types := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil)}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	saved := []byte{1, 1, 0x70, 0x23, 0, 0x21, 0, 0x10, 4, 0x20, 0, 0xd1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(3, []byte{6, 0, 1, 0, 2, 2, 2}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("check", 0, 0), wasmtest.ExportEntry("set", 0, 1), wasmtest.ExportEntry("saved", 0, 2), wasmtest.ExportEntry("target", 0, 3), wasmtest.ExportEntry("clear", 0, 4), wasmtest.ExportEntry("trap", 0, 5))),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x04, 0x40, 0xd2, 3, 0x24, 0, 0x05, 0xd0, 0x70, 0x24, 0, 0x0b, 0x23, 0, 0xd1, 0x0b}),
			append(wasmtest.ULEB(uint32(len(saved))), saved...),
			wasmtest.Code([]byte{0x0b}),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0xd2, 3, 0x24, 0, 0x00, 0x0b}),
		)),
	)
}

func TestRV64PublicLocalMutableReferenceCells(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("local-mutable-cell-v%t", vector), rv64LocalMutableReferenceModule(vector), artifact)
				in, err := Instantiate(c, NewImports())
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				c.Close()
				importedGlobalInvoke(t, in, "check", []uint64{1})
				for n := 0; n < 10; n++ {
					importedGlobalInvoke(t, in, "set", []uint64{0}, 0xfeedface00000001)
					descriptor := readGlobalObject(in.globalCells[0], ValFuncRef)
					if index, ok := in.funcrefDescriptorIndex(descriptor); !ok || index != 3 || descriptor>>32 == 0 {
						t.Fatal("full-width own descriptor", index, descriptor, ok)
					}
					importedGlobalInvoke(t, in, "saved", []uint64{0})
					importedGlobalInvoke(t, in, "check", []uint64{1})
					importedGlobalInvoke(t, in, "set", []uint64{1}, 0xfeedface00000000)
				}
				if allocs := testing.AllocsPerRun(20, func() {
					got, err := in.Invoke("set", 1)
					if err != nil || len(got) != 1 || got[0] != 0 {
						panic("warm mutable reference store")
					}
					got, err = in.Invoke("saved")
					if err != nil || len(got) != 1 || got[0] != 0 {
						panic("warm mutable reference load across call")
					}
				}); allocs != 0 {
					t.Fatal("warm local reference allocations", allocs)
				}
				if got, err := in.Invoke("trap"); err == nil || got != nil {
					t.Fatal("cold trap", got, err)
				}
				importedGlobalInvoke(t, in, "check", []uint64{0})
				importedGlobalInvoke(t, in, "set", []uint64{1}, 0)
				for _, name := range []string{"hidden", "0", "target"} {
					if _, err := in.GlobalValue(name); err == nil {
						t.Fatal("unexported reference readable", name)
					}
					if err := in.SetGlobalValue(name, Value{typ: ValFuncRef}); err == nil {
						t.Fatal("unexported reference mutable by host", name)
					}
					if _, err := in.ExportedGlobalObject(name); err == nil {
						t.Fatal("unexported reference handle", name)
					}
				}
			})
		}
	}
}

func TestRV64PublicLocalMutableReferenceMetadataAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TABLE_STAGE_ARTIFACT_MODE") == "load" {
		t.Skip("source and mutable metadata admission only")
	}
	for _, vector := range []bool{false, true} {
		source := rv64LocalMutableReferenceModule(vector)
		c := rv64PublicStartCompile(t, source, false)
		for name, mutate := range map[string]func(*Compiled){
			"export":          func(v *Compiled) { v.GlobalExports = map[string]int{"hidden": 0} },
			"alias":           func(v *Compiled) { v.GlobalExports = map[string]int{"a": 0, "b": 0} },
			"bad-index":       func(v *Compiled) { v.GlobalExports = map[string]int{"bad": 1} },
			"missing-feature": func(v *Compiled) { v.requiredFeatures &^= CoreFeatureReferenceTypes },
			"raw-bits":        func(v *Compiled) { v.Globals[0].Bits = 7 },
			"reference-ABI":   func(v *Compiled) { v.nativeTableReferenceABI = false },
		} {
			copy := *c.executionView()
			copy.Globals = slices.Clone(copy.Globals)
			mutate(&copy)
			if err := railshotValidateCompiled(&copy); err == nil {
				t.Fatal("metadata admitted", vector, name)
			}
			blob, err := marshalCompiled(&copy)
			if err == nil {
				loaded, err := LoadTrustedArtifact(blob)
				// The codec recomputes feature requirements and normalizes unused
				// reference payload bits. Those contradictory fields do not
				// survive serialization as an invalid published artifact.
				canonical := false
				if loaded != nil {
					view := loaded.executionView()
					canonical = (name == "missing-feature" && view.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes)) || (name == "raw-bits" && len(view.Globals) == 1 && view.Globals[0].Bits == 0 && view.Globals[0].Mutable && view.Globals[0].Type == ValFuncRef)
				}
				if loaded != nil {
					loaded.Close()
				}
				if err == nil && !canonical {
					t.Fatal("artifact admitted", vector, name)
				}
			}
		}
		exported := rv64ReplaceTableStageSection(t, source, 7, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("target", 0, 3), wasmtest.ExportEntry("hidden", 3, 0))))
		bad, err := Compile(NewRuntimeConfig(), exported)
		if bad != nil {
			bad.Close()
		}
		if err == nil || bad != nil {
			t.Fatal("mutable reference export source", err)
		}
	}
}

func rv64LocalMutableReferenceImportModule(vector bool) []byte {
	types := [][]byte{wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(nil, nil)}
	if vector {
		types = append(types, wasmtest.FuncType([]wasm.ValType{wasm.V128}, nil))
	}
	saved := []byte{1, 1, 0x70, 0x23, 0, 0x21, 0, 0x10, 0, 0x20, 0, 0xd1, 0x0b}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(types...)),
		wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("target")...), 0, 1))),
		wasmtest.Section(3, []byte{4, 0, 0, 1, 0}),
		wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, true, []byte{0xd0, 0x70, 0x0b}))),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("set", 0, 1), wasmtest.ExportEntry("saved", 0, 2), wasmtest.ExportEntry("clear", 0, 3), wasmtest.ExportEntry("check", 0, 4))),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}), // dropped declaration; no table storage
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0xd2, 0, 0x24, 0, 0x23, 0, 0xd1, 0x0b}),
			append(wasmtest.ULEB(uint32(len(saved))), saved...),
			wasmtest.Code([]byte{0xd0, 0x70, 0x24, 0, 0x0b}),
			wasmtest.Code([]byte{0x23, 0, 0xd1, 0x0b}),
		)),
	)
}

func TestRV64PublicLocalMutableReferenceParkAndNativeRetirement(t *testing.T) {
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			for _, native := range []bool{false, true} {
				t.Run(fmt.Sprintf("vector%t-artifact%t-native%t", vector, artifact, native), func(t *testing.T) {
					rt := NewRuntime()
					defer rt.Close()
					imports := NewImports()
					var in, producer *Instance
					var pc *Compiled
					calls := 0
					panicHost := false
					if native {
						pc = rv64TableStageCompile(t, "local-mutable-provider", rv64PublicNativeIdentityModule(nil), artifact)
						producer = rv64NativeTokenInstantiate(t, rt, pc, NewImports())
						defer producer.Close()
						target, err := producer.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						imports.Function("env", "target", target)
					} else {
						imports.Function("env", "target", slotHostFunc(func(caller HostModule, _, _ []uint64) {
							calls++
							runtime.GC()
							if _, err := in.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
								panic(err)
							}
							if panicHost {
								panic("local reference callback failure")
							}
						}))
					}
					c := rv64TableStageCompile(t, fmt.Sprintf("local-mutable-import-v%t", vector), rv64LocalMutableReferenceImportModule(vector), artifact)
					in = rv64NativeTokenInstantiate(t, rt, c, imports)
					defer in.Close()
					c.Close()
					if native {
						producer.Close()
						pc.Close()
						if !producer.hasPhysicalResources() {
							t.Fatal("producer retired before importer")
						}
					}
					importedGlobalInvoke(t, in, "set", []uint64{0})
					descriptor := readGlobalObject(in.globalCells[0], ValFuncRef)
					if index, ok := in.funcrefDescriptorIndex(descriptor); !ok || index != 0 || descriptor>>32 == 0 {
						t.Fatal("importer-owned full-width thunk", index, descriptor, ok)
					}
					importedGlobalInvoke(t, in, "saved", []uint64{0})
					if native {
						importedGlobalInvoke(t, in, "check", []uint64{0})
					} else {
						importedGlobalInvoke(t, in, "check", []uint64{1})
						if calls != 1 {
							t.Fatal(calls)
						}
						importedGlobalInvoke(t, in, "set", []uint64{0})
						panicHost = true
						var got []uint64
						var callbackPanic any
						func() {
							defer func() { callbackPanic = recover() }()
							got, _ = in.Invoke("saved")
						}()
						if callbackPanic != "local reference callback failure" || got != nil {
							t.Fatal("host panic publication", got, callbackPanic)
						}
						panicHost = false
						importedGlobalInvoke(t, in, "set", []uint64{0})
						importedGlobalInvoke(t, in, "saved", []uint64{0})
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if in.hasPhysicalResources() || native && producer.hasPhysicalResources() {
						t.Fatal("local reference retained mapping after last owner")
					}
				})
			}
		}
	}
}

func TestRV64PublicLocalMutableReferenceParkCancellationAndReuse(t *testing.T) {
	previous := runtime.GOMAXPROCS(1)
	defer runtime.GOMAXPROCS(previous)
	for _, vector := range []bool{false, true} {
		for _, artifact := range []bool{false, true} {
			t.Run(fmt.Sprintf("vector%t-artifact%t", vector, artifact), func(t *testing.T) {
				c := rv64TableStageCompile(t, fmt.Sprintf("local-mutable-import-v%t", vector), rv64LocalMutableReferenceImportModule(vector), artifact)
				entered, release := make(chan struct{}), make(chan struct{})
				var releaseOnce sync.Once
				imports := NewImports()
				imports.Function("env", "target", slotHostFunc(func(HostModule, []uint64, []uint64) { close(entered); <-release }))
				in, err := Instantiate(c, imports)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				c.Close()
				importedGlobalInvoke(t, in, "set", []uint64{0})
				descriptor := readGlobalObject(in.globalCells[0], ValFuncRef)
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				type outcome struct {
					values []uint64
					err    error
				}
				done := make(chan outcome, 1)
				go func() { values, err := in.InvokeContext(ctx, "saved"); done <- outcome{values, err} }()
				joined := false
				defer func() {
					cancel()
					releaseOnce.Do(func() { close(release) })
					if !joined {
						select {
						case <-done:
						case <-time.After(5 * time.Second):
							t.Error("mutable reference invocation did not join")
						}
					}
				}()
				select {
				case <-entered:
				case <-time.After(5 * time.Second):
					t.Fatal("host callback did not park")
				}
				if !in.hasPhysicalResources() {
					t.Fatal("parked owner retired")
				}
				cancel()
				// Observe the active interrupt before letting the callback return;
				// otherwise normal completion may win the cancellation race.
				deadline := time.Now().Add(5 * time.Second)
				for atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
					runtime.Gosched()
				}
				if atomic.LoadUint32((*uint32)(unsafe.Pointer(&in.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
					t.Fatal("parked reference watcher did not interrupt")
				}
				releaseOnce.Do(func() { close(release) })
				select {
				case result := <-done:
					joined = true
					if !errors.Is(result.err, context.Canceled) || result.values != nil {
						t.Fatal("cancelled result publication", result)
					}
				case <-time.After(5 * time.Second):
					t.Fatal("cancelled invocation did not finish")
				}
				if got := readGlobalObject(in.globalCells[0], ValFuncRef); got != descriptor {
					t.Fatal("cancel changed own reference", got, descriptor)
				}
				importedGlobalInvoke(t, in, "check", []uint64{0})
				if _, err := in.Invoke("clear"); err != nil {
					t.Fatal(err)
				}
				importedGlobalInvoke(t, in, "check", []uint64{1})
			})
		}
	}
}
