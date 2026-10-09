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

func TestRV64PublicReferenceFunctionResourceImportsCancellationAndClose(t *testing.T) {
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
						pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-imports-v%t-ttrue-wtrue", vector), wasmtest.ClosedReferenceResourceImports(vector, true, true), artifact)
						entered, release := make(chan struct{}), make(chan struct{})
						var parkOnce, releaseOnce sync.Once
						unblock := func() { releaseOnce.Do(func() { close(release) }) }
						defer unblock()
						park := func() { parkOnce.Do(func() { close(entered); <-release }) }
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
						rc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-provider-v%t-wtrue", vector), wasmtest.ClosedNumericImportResources(vector, true), artifact)
						resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
						imports, memories := rv64ReferenceResourceImportBindings(t, resource)
						// The owned callback comes after the resume oracle. Establish its new
						// page through an independent borrower before starting this invocation.
						gc := rv64NativeTailCompile(t, "reference-resource-grower-wfalse", rv64ReferenceResourceGrower(false), artifact)
						grower := rv64NativeTokenInstantiate(t, rt, gc, NewImports().Memory("env", "memory", memories[1]))
						if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
							t.Fatal("cancel pre-growth", got, err)
						}
						if err := grower.Close(); err != nil {
							t.Fatal(err)
						}
						producer := rv64NativeTokenInstantiate(t, rt, pc, imports.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", shared).Function("env.calls", "owned.fn", owned))
						memory, err := producer.ExportedMemory("owned-memory")
						if err != nil {
							t.Fatal(err)
						}
						defer memory.Close()
						wc := rv64NativeTailCompile(t, "reference-resource-grower-wfalse", rv64ReferenceResourceGrower(false), artifact)
						witness := rv64NativeTokenInstantiate(t, rt, wc, NewImports().Memory("env", "memory", memory))
						pf, err := producer.ExportedFunc("run")
						if err != nil {
							t.Fatal(err)
						}
						cc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-caller-v%t-sfalse-wtrue", vector), rv64ReferenceImportsConsumer(vector, false, "reference-tail"), artifact)
						caller := rv64NativeTokenInstantiate(t, rt, cc, NewImports().Function("env", "target", pf))
						args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
						if vector {
							args = append(args, 0x80000000deadbeef)
						}
						want := append(slices.Clone(args[1:]), 9, 0)
						args = append(args, 4)
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
								t.Error("parked reference import did not join")
							}
						}()
						select {
						case <-entered:
						case <-time.After(5 * time.Second):
							t.Fatal("reference import did not park")
						}
						if err := resource.Close(); err != nil {
							t.Fatal(err)
						}
						if err := rc.Close(); err != nil {
							t.Fatal(err)
						}
						if err := producer.Close(); err != nil {
							t.Fatal(err)
						}
						if err := native.Close(); err != nil {
							t.Fatal(err)
						}
						if err := pc.Close(); err != nil {
							t.Fatal(err)
						}
						if err := nc.Close(); err != nil {
							t.Fatal(err)
						}
						if closeRoot {
							if err := caller.Close(); err != nil {
								t.Fatal(err)
							}
						} else {
							cancel()
						}
						if caller.resourcesClosed || producer.resourcesClosed || native.resourcesClosed || resource.resourcesClosed || caller.pluginState.Load().railshotInvocation.tailGenerationBudget.count == 0 {
							t.Fatal("parked reference import lost physical roots")
						}
						if owned.(*HostFuncRef).Close() == nil {
							t.Fatal("parked reference import released owned callback")
						}
						deadline := time.Now().Add(5 * time.Second)
						for atomic.LoadUint32((*uint32)(unsafe.Pointer(&caller.trap[0]))) != uint32(coreruntime.TrapInterrupted) && time.Now().Before(deadline) {
							runtime.Gosched()
						}
						if atomic.LoadUint32((*uint32)(unsafe.Pointer(&caller.trap[0]))) != uint32(coreruntime.TrapInterrupted) {
							t.Fatal("parked reference import interruption not delivered")
						}
						unblock()
						select {
						case err := <-done:
							var trap *coreruntime.TrapError
							if closeRoot {
								if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
									t.Fatal("close unwind", err)
								}
							} else if !errors.Is(err, context.Canceled) {
								t.Fatal("cancel unwind", err)
							}
						case <-time.After(5 * time.Second):
							t.Fatal("reference import unwind timeout")
						}
						if producer.resourcesClosed || readGlobalObject(producer.globalCells[1], ValI32) != 11 || readGlobalObject(producer.globalCells[4], ValI32) != 0 {
							t.Fatal("interrupted reference import ran continuation")
						}
						for _, in := range []*Instance{caller, producer, native} {
							state := &in.pluginState.Load().railshotInvocation
							if state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil {
								t.Fatal("interrupted reference import leaked generation")
							}
						}
						if !closeRoot {
							if got, err := caller.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
								t.Fatal("reference import reuse after cancellation", got, err)
							}
							if err := caller.Close(); err != nil {
								t.Fatal(err)
							}
						}
						if err := witness.Close(); err != nil {
							t.Fatal(err)
						}
						for _, in := range []*Instance{caller, producer, native, resource} {
							if !in.resourcesClosed {
								t.Fatal("reference import roots survived final join")
							}
						}
						if err := owned.(*HostFuncRef).Close(); err != nil {
							t.Fatal("owned callback retained after join", err)
						}
					})
				}
			}
		}
	}
}

func TestRV64PublicReferenceFunctionResourceImportsNativeWarm(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-tails%t", artifact, vector, tails), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rt.Close()
					nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
					native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
					nf, err := native.ExportedFunc("identity")
					if err != nil {
						t.Fatal(err)
					}
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-imports-v%t-t%t-wtrue", vector, tails), wasmtest.ClosedReferenceResourceImports(vector, tails, true), artifact)
					rc := rv64NativeTailCompile(t, fmt.Sprintf("reference-resource-provider-v%t-wtrue", vector), wasmtest.ClosedNumericImportResources(vector, true), artifact)
					resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
					imports, memories := rv64ReferenceResourceImportBindings(t, resource)
					gc := rv64NativeTailCompile(t, "reference-resource-grower-wfalse", rv64ReferenceResourceGrower(false), artifact)
					grower := rv64NativeTokenInstantiate(t, rt, gc, NewImports().Memory("env", "memory", memories[1]))
					if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
						t.Fatal("warm pre-growth", got, err)
					}
					if err := grower.Close(); err != nil {
						t.Fatal(err)
					}
					in := rv64NativeTokenInstantiate(t, rt, pc, imports.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", nf).Function("env.calls", "owned.fn", nf))
					if err := resource.Close(); err != nil {
						t.Fatal(err)
					}
					if err := rc.Close(); err != nil {
						t.Fatal(err)
					}
					if err := native.Close(); err != nil {
						t.Fatal(err)
					}
					if err := nc.Close(); err != nil {
						t.Fatal(err)
					}
					args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
					if vector {
						args = append(args, 0x80000000deadbeef)
					}
					want := append(slices.Clone(args[1:]), 6, 0)
					args = append(args, 2)
					if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
						t.Fatal("all-native reference imports", got, err)
					}
					if allocs := testing.AllocsPerRun(5, func() {
						got, err := in.Invoke("run", args...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm all-native reference imports")
						}
					}); allocs != 0 {
						t.Fatal("all-native reference imports allocated", allocs)
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					if !native.resourcesClosed || !resource.resourcesClosed {
						t.Fatal("all-native physical producer retained")
					}
				})
			}
		}
	}
}
