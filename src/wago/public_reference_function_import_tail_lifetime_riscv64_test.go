//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
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

// A numeric global-only importer retains the selected source's owned cells
// after the foreign caller closes, so unwind continuation is observable safely.
func rv64ReferenceImportTailGlobalWitness() []byte {
	return wasmtest.Module(wasmtest.Section(2, wasmtest.Vec(append(append(wasmtest.Name("env"), wasmtest.Name("continued")...), 3, 0x7f, 1))), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("continued", 3, 0))))
}

func TestRV64PublicReferenceFunctionImportedTailsCancellationAndClose(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, parkOwned := range []bool{false, true} {
				for _, closeRoot := range []bool{false, true} {
					t.Run(fmt.Sprintf("artifact%t-vector%t-owned%t-close%t", artifact, vector, parkOwned, closeRoot), func(t *testing.T) {
						rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
						defer rv64HostCertificateCloseRuntime(t, rt)
						nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
						native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
						nf, err := native.ExportedFunc("identity")
						if err != nil {
							t.Fatal(err)
						}
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-import-tails-v%t-ltrue", vector), wasmtest.ClosedReferenceImportTails(vector, true), artifact)
						entered, release := make(chan struct{}), make(chan struct{})
						var parkOnce, releaseOnce sync.Once
						unblock := func() { releaseOnce.Do(func() { close(release) }) }
						defer unblock()
						park := func() { parkOnce.Do(func() { close(entered); <-release }); runtime.GC() }
						shared := func(v int32) int32 {
							if !parkOwned {
								park()
							}
							return v + 1
						}
						owned := rv64DynamicHostBinding(t, rt, true, func(v int32) int32 {
							if parkOwned {
								park()
							}
							return v + 2
						}, pc.importFuncSigs[2])
						selected := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", shared).Function("env.calls", "owned.fn", owned))
						capability := rv64ReferenceImportTailCapability(selected)
						global, err := selected.ExportedGlobalObject("continued")
						if err != nil {
							t.Fatal(err)
						}
						wc := rv64NativeTailCompile(t, "import-tail-global-witness", rv64ReferenceImportTailGlobalWitness(), artifact)
						witness := rv64NativeTokenInstantiate(t, rt, wc, NewImports().Global("env", "continued", global))
						target, err := selected.ExportedFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						cc := rv64NativeTailCompile(t, fmt.Sprintf("import-tail-caller-v%t-sfalse-wtrue-reference-tail", vector), rv64ReferenceImportTailConsumer(vector, false, true, "reference-tail"), artifact)
						caller := rv64NativeTokenInstantiate(t, rt, cc, NewImports().Function("env", "target", target))
						choice := uint64(1)
						if parkOwned {
							choice = 2
						}
						args, payload := rv64ReferenceImportTailArgs(vector, choice, 1)
						want := append(payload, []uint64{0, 0xfffffffe, 0xffffffff}[choice], 113)
						ctx, cancel := context.WithCancel(context.Background())
						defer cancel()
						done, finished := make(chan error, 1), make(chan struct{})
						go func() { defer close(finished); _, err := caller.InvokeContext(ctx, "call", args...); done <- err }()
						defer func() {
							cancel()
							unblock()
							select {
							case <-finished:
							case <-time.After(5 * time.Second):
								t.Error("private imported tail failed to join")
							}
						}()
						select {
						case <-entered:
						case <-time.After(5 * time.Second):
							t.Fatal("private imported tail did not park")
						}
						for _, owner := range []*Instance{selected, native} {
							if err := owner.Close(); err != nil {
								t.Fatal(err)
							}
						}
						for _, code := range []*Compiled{pc, nc} {
							if err := code.Close(); err != nil {
								t.Fatal(err)
							}
						}
						if closeRoot {
							if err := caller.Close(); err != nil {
								t.Fatal(err)
							}
						} else {
							cancel()
						}
						if caller.resourcesClosed || selected.resourcesClosed || native.resourcesClosed || caller.pluginState.Load().railshotInvocation.tailGenerationBudget.count == 0 {
							t.Fatal("parked imported-tail generation lost roots")
						}
						if owned.(*HostFuncRef).Close() == nil {
							t.Fatal("parked imported-tail callback released")
						}
						deadline := time.Now().Add(5 * time.Second)
						for atomic.LoadUint32((*uint32)(unsafe.Pointer(&caller.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
							runtime.Gosched()
						}
						if atomic.LoadUint32((*uint32)(unsafe.Pointer(&caller.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
							t.Fatal("private imported-tail interrupt not delivered")
						}
						unblock()
						select {
						case err := <-done:
							var trap *coreruntime.TrapError
							if closeRoot {
								if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
									t.Fatal("proper-tail Close unwind", err)
								}
							} else if !errors.Is(err, context.Canceled) {
								t.Fatal("proper-tail cancel unwind", err)
							}
						case <-time.After(5 * time.Second):
							t.Fatal("proper-tail unwind timeout")
						}
						if selected.resourcesClosed || readGlobalObject(witness.globalCells[0], ValI32) != 0 || rv64ReferenceImportTailCapability(selected) != capability {
							t.Fatal("unwind continued private caller or lost static capability")
						}
						for _, owner := range []*Instance{caller, selected, native} {
							if owner.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || owner.pluginState.Load().railshotInvocation.definedTailOwner != nil {
								t.Fatal("unwind retained proper-tail generation")
							}
						}
						if !closeRoot {
							if got, err := caller.Invoke("call", args...); err != nil || !slices.Equal(got, want) || readGlobalObject(witness.globalCells[0], ValI32) != 113 {
								t.Fatal("proper-tail reuse after cancellation", got, err)
							}
							if err := caller.Close(); err != nil {
								t.Fatal(err)
							}
						}
						if err := witness.Close(); err != nil {
							t.Fatal(err)
						}
						for _, owner := range []*Instance{caller, selected, native} {
							if !owner.resourcesClosed {
								t.Fatal("proper-tail roots survived final join")
							}
						}
						if err := owned.(*HostFuncRef).Close(); err != nil {
							t.Fatal("proper-tail callback survived final join", err)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceFunctionImportedTailsDeepPrepared(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rv64HostCertificateCloseRuntime(t, rt)
				nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
				native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
				nf, err := native.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				pc := rv64NativeTailCompile(t, fmt.Sprintf("import-tail-recursive-v%t", vector), wasmtest.ClosedReferenceRecursiveImportTail(vector, true), artifact)
				in := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", nf).Function("env.calls", "owned.fn", nf))
				args, payload := rv64ReferenceImportTailArgs(vector, 0, 0)
				args[0] = 0xdeadbeef00001388 // 5000; upper bits are not the counter.
				want := append(payload, 0x80000001, 113)
				if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
					t.Fatal("5000 private local tails then foreign tail", got, err)
				}
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				call, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer call.Close()
				for _, null := range []uint64{0, 1} {
					args[len(args)-1] = 0xcafefeed00000000 | null
					if got, err := call.Invoke(args...); err != nil || !slices.Equal(got, want) {
						t.Fatal("prepared recursive private imported tail", null, got, err)
					}
				}
				if in.eng.RV64InvocationViewPointer()-in.eng.StackLimit() > uintptr(MinNativeStackBytes) {
					t.Fatal("recursive tail did not use minimum stack")
				}
			})
		}
	}
}
