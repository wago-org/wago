//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ClosedReferenceCountdown(tail bool) []byte {
	call := byte(0x10)
	if tail {
		call = 0x12
	}
	leaf := []byte{0x20, 0, 0x45, 4, 0x70, 0x20, 1, 5, 0x20, 0, 0x41, 1, 0x6b, 0x20, 1, call, 1, 0x0b, 0x0b}
	return wasmtest.Module(wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType([]wasm.ValType{wasm.I32}, []wasm.ValType{wasm.I32}), wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}), wasmtest.FuncType(nil, nil))), wasmtest.Section(3, []byte{3, 0, 1, 2}), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 0))), wasmtest.Section(9, []byte{1, 3, 0, 1, 2}), wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x20, 0, 0xd2, 2, 0x10, 1, 0xd1, 0x0b}), wasmtest.Code(leaf), wasmtest.Code([]byte{0x0b}))))
}
func TestRV64PublicReferenceFunctionsDeepPrepared(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, tail := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-tail%t", artifact, tail), func(t *testing.T) {
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-functions-countdown-%t", tail), rv64ClosedReferenceCountdown(tail), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rt.Close()
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				fn, err := in.WasmFunc("run")
				if err != nil {
					t.Fatal(err)
				}
				session, err := fn.OpenSession()
				if err != nil {
					t.Fatal(err)
				}
				defer session.Close()
				for _, depth := range []uint64{0, 7, 50001} {
					got, err := session.Invoke(0xcafe000000000000 | depth)
					if !tail && depth == 50001 {
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapStackFenceBreached || len(trap.Frames) == 0 || !trap.Frames[0].HasProgramCounter || trap.Frames[0].ProgramCounter != 1 {
							t.Fatal("recursive stack trap/provenance", err)
						}
					} else if err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatal("reference countdown", got, err)
					}
				}
				if got, err := session.Invoke(3); err != nil || !slices.Equal(got, []uint64{0}) {
					t.Fatal("prepared reuse", got, err)
				}
			})
		}
	}
}
func TestRV64PublicReferenceFunctionsCancellationAndClose(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, closeRoot := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-close%t", artifact, closeRoot), func(t *testing.T) {
				c := rv64NativeTailCompile(t, "reference-functions-countdown-true", rv64ClosedReferenceCountdown(true), artifact)
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
				defer rt.Close()
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				if closeRoot {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					done := make(chan error, 1)
					go func() { _, err := in.InvokeContext(ctx, "run", 0xffffffff); done <- err }()
					joined := false
					defer func() {
						if !joined {
							cancel()
							select {
							case <-done:
							case <-time.After(5 * time.Second):
								t.Error("closed invocation did not join")
							}
						}
					}()
					deadline := time.Now().Add(5 * time.Second)
					for in.invocationState.Load()&instanceInvocationCount == 0 && time.Now().Before(deadline) {
						runtime.Gosched()
					}
					if in.invocationState.Load()&instanceInvocationCount == 0 {
						t.Fatal("invocation never admitted")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					select {
					case err := <-done:
						joined = true
						var trap *coreruntime.TrapError
						if !errors.As(err, &trap) || trap.Code != coreruntime.TrapInterrupted {
							t.Fatal("close interruption", err)
						}
					case <-time.After(5 * time.Second):
						t.Fatal("close did not interrupt")
					}
					if in.jm != nil {
						t.Fatal("closed invocation retained storage")
					}
				} else {
					ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
					defer cancel()
					if _, err := in.InvokeContext(ctx, "run", 0xffffffff); !errors.Is(err, context.DeadlineExceeded) {
						t.Fatal("deadline interruption", err)
					}
					if got, err := in.Invoke("run", 5); err != nil || !slices.Equal(got, []uint64{0}) {
						t.Fatal("cancelled instance reuse", got, err)
					}
					if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || rv64EntryCapability(in) != 0 {
						t.Fatal("interrupted owner state retained")
					}
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionsSparseGenerationQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			pc := rv64NativeTailCompile(t, "reference-functions-quota-producer", rv64WideClosedReferenceSource(vector, true), false)
			cc := rv64NativeTailCompile(t, "reference-functions-quota-consumer", rv64PublicReferenceTailModule([]wasm.ValType{wasm.I32}, true), false)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rt.Close()
			producer := rv64NativeTokenInstantiate(t, rt, pc, NewImports())
			export, err := producer.ExportedFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			imports := testImports("env.target", export)
			caller := rv64NativeTokenInstantiate(t, rt, cc, imports)
			// These are real uncached production snapshots under the physical root.
			func() {
				gate := caller.lockInvocation(0)
				defer gate.unlockInvocation()
				entry, err := caller.beginNativeEntry()
				if err != nil {
					t.Fatal(err)
				}
				defer caller.unlockNativeEntry(entry)
				scope := entry.tables.targets
				found := false
				for _, g := range scope.tailActive {
					for i, owner := range g.owners {
						if owner == producer {
							counts, _, err := rv64NativeTailOwnerCounts(owner)
							if err != nil || counts.Functions != 4 || counts.MaskSlots != 4 || counts.NonCallable != 2 || counts.Values != 2 {
								t.Fatal("actual sparse generation counts", counts, err)
							}
							if i >= len(g.snapshots) || g.snapshots[i] == nil || g.snapshots[i].Validate() != nil {
								t.Fatal("actual sparse snapshot")
							}
							found = true
						}
					}
				}
				if !found {
					t.Fatal("closed producer missing from uncached generation")
				}
			}()
			for _, kind := range []string{"exact", "code-below", "metadata-below"} {
				limited := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				t.Cleanup(func() { _ = limited.Close() })
				in := rv64NativeTokenInstantiate(t, limited, cc, imports)
				code, metadata, base, restore := rv64ConfigureDynamicTailQuota(t, in, kind)
				codeLimit, metadataLimit := uint64(code), base+uint64(metadata)
				if kind == "code-below" {
					codeLimit--
				}
				if kind == "metadata-below" {
					metadataLimit--
				}
				got, err := in.Invoke("call", 37, 0)
				restore()
				if kind == "exact" {
					if err != nil || !slices.Equal(got, []uint64{42}) {
						t.Fatal("exact generation quota", got, err)
					}
				} else {
					var quota *coreruntime.ResourceLimitError
					resource, requested, limit := "native code bytes", uint64(code), codeLimit
					if kind == "metadata-below" {
						resource, requested, limit = "instance metadata bytes", base+uint64(metadata), metadataLimit
					}
					if !errors.As(err, &quota) || quota.Resource != resource || quota.Requested != requested || quota.Limit != limit {
						t.Fatal("sparse exact rejection", kind, err)
					}
				}
				for _, owner := range []*Instance{in, producer} {
					s := &owner.pluginState.Load().railshotInvocation
					if s.tailGenerationBudget.count != 0 || s.definedTailOwner != nil {
						t.Fatal("generation charge not refunded")
					}
				}
				in.Close()
				limited.Close()
			}
		})
	}
}
