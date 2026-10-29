//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"runtime"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceNonNullSource(vector, dead bool) []byte {
	typ, n := wasm.I32, 256
	if vector {
		typ, n = wasm.V128, 128
	}
	hidden := make([]wasm.ValType, n)
	for i := range hidden {
		hidden[i] = typ
	}
	body := []byte{0xd0, 0x70, 0xd4, 0xd1, 0x0b}
	if dead {
		body = append([]byte{0x00}, body...)
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}), wasmtest.FuncType(hidden, hidden))),
		wasmtest.Section(3, []byte{1, 0}),
		wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))),
		wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))),
	)
}

func TestRV64PublicReferenceNonNullStandalone(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, dead := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-dead%t", artifact, vector, dead), func(t *testing.T) {
					c := rv64NativeTailCompile(t, fmt.Sprintf("non-null-standalone-%t-%t", vector, dead), rv64ReferenceNonNullSource(vector, dead), artifact)
					if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || rv64NativeTailModule(c) != nil || !railshotRequiresNativeOwnerScope(c) {
						t.Fatal("non-calling reference witness or retained owner scope")
					}
					// The unused maximal signature must not enlarge call transport merely
					// because the module checks a reference for nullness.
					p, r, err := railshotImportedCallSlots(c, 0, 1)
					if err != nil || p != 0 || r != 1 {
						t.Fatal("non-null check acquired call transport", p, r, err)
					}
					in, err := Instantiate(c)
					if err != nil {
						t.Fatal(err)
					}
					defer in.Close()
					if len(in.funcRefDescs) != 40 {
						t.Fatal("null-only code acquired indexed descriptors", len(in.funcRefDescs))
					}
					for range 3 {
						_, err = in.Invoke("run")
						var tr *coreruntime.TrapError
						want, pc := coreruntime.TrapNullReference, uint32(3)
						if dead {
							want, pc = coreruntime.TrapUnreachable, 1
						}
						if !errors.As(err, &tr) || tr.Code != want || len(tr.Frames) != 1 || tr.Frames[0].FunctionIndex != 0 || !tr.Frames[0].HasProgramCounter || tr.Frames[0].ProgramCounter != pc {
							t.Fatal("non-null source/artifact trap and cold reuse", err)
						}
					}
					if state := &in.pluginState.Load().railshotInvocation; state.tailDispatcher != nil || state.tailGenerationBudget.count != 0 || rv64EntryCapability(in) != 0 {
						t.Fatal("non-null-only entry acquired tail generation")
					}
				})
			}
		}
	}
}

func TestRV64PublicReferenceNonNullCall(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				typ := wasm.I32
				if vector {
					typ = wasm.V128
				}
				sig := []wasm.ValType{typ}
				source := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))),
					wasmtest.Section(3, []byte{2, 0, 0}),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))),
					wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}), wasmtest.Code([]byte{0x20, 0, 0xd2, 0, 0xd4, 0x14, 0, 0x0b}))),
				)
				c := rv64NativeTailCompile(t, fmt.Sprintf("non-null-call-%t", vector), source, artifact)
				if !c.nativeReferenceControlABI || !c.nativeReferenceCallABI {
					t.Fatal("combined reference witnesses")
				}
				in, err := Instantiate(c)
				if err != nil {
					t.Fatal(err)
				}
				defer in.Close()
				args, want := rv64ElementGlobalArguments(vector)
				for range 3 {
					importedGlobalInvoke(t, in, "run", want, args...)
				}
			})
		}
	}
}

func TestRV64PublicReferenceNonNullRetiredGlobalOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, table := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-table%t", artifact, vector, table), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					t.Cleanup(func() { rv64HostCertificateCloseRuntime(t, rt) })
					typ := wasm.I32
					if vector {
						typ = wasm.V128
					}
					sig := []wasm.ValType{typ}
					producerSource := wasmtest.Module(
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))),
						wasmtest.Section(3, []byte{1, 0}),
						wasmtest.Section(6, wasmtest.Vec(wasmtest.GlobalEntry(wasm.FuncRef, false, []byte{0xd2, 0, 0x0b}))),
						wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("reference", 3, 0))),
						wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0x0b}))),
					)
					pc := rv64NativeTailCompile(t, fmt.Sprintf("non-null-producer-%t", vector), producerSource, artifact)
					producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
					g, err := producer.ExportedGlobalObject("reference")
					if err != nil {
						t.Fatal(err)
					}
					sections := [][]byte{
						wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(sig, sig))),
						wasmtest.Section(2, wasmtest.Vec(wasmtest.GlobalImportEntry("env", "ref", wasm.FuncRef, false))),
						wasmtest.Section(3, []byte{1, 0}),
					}
					body := []byte{0x23, 0, 0xd4, 0x1a, 0x20, 0, 0x0b}
					if table {
						sections = append(sections, wasmtest.Section(4, []byte{1, 0x70, 1, 1, 1}))
						body = []byte{0x41, 0, 0x23, 0, 0xd4, 0x26, 0, 0x20, 0, 0x41, 0, 0x11, 0, 0, 0x0b}
					}
					sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code(body))))
					c := rv64NativeTailCompile(t, fmt.Sprintf("non-null-borrower-%t-table%t", vector, table), wasmtest.Module(sections...), artifact)
					in := rv64NativeTokenInstantiate(t, rt, c, NewImports().Global("env", "ref", g))
					if !c.nativeReferenceControlABI || c.nativeReferenceCallABI || !railshotRequiresNativeOwnerScope(c) || (c.tableCount() != 0) != table {
						t.Fatal("foreign carrier witness")
					}
					producer.Close()
					pc.Close()
					c.Close()
					runtime.GC()
					if !producer.hasPhysicalResources() {
						t.Fatal("imported immutable reference owner retired early")
					}
					args, want := rv64ElementGlobalArguments(vector)
					for range 3 {
						importedGlobalInvoke(t, in, "run", want, args...)
					}
					pool := &in.pluginState.Load().railshotInvocation
					if pool.tableTargets[0] == nil || pool.tableDepth != 0 {
						t.Fatal("non-null entry omitted its physical owner scope or leaked activation")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					rv64HostCertificateCloseRuntime(t, rt)
					if producer.hasPhysicalResources() || in.hasPhysicalResources() || pc.codeCache.mem != nil || c.codeCache.mem != nil {
						t.Fatal("reference owner or code survived final runtime retirement")
					}
				})
			}
		}
	}
}
