//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"encoding/binary"
	"fmt"
	"runtime"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64ReferenceStartRows(in *Instance) bool {
	// ref.null-only bodies need the stable context header, rather than a full
	// descriptor directory. Dropped declarations do not manufacture callable rows.
	if !in.c.needsFuncRefDescs() {
		return len(in.funcRefDescs) == coreruntime.FuncRefDescBytes &&
			binary.LittleEndian.Uint64(in.funcRefDescs[coreruntime.FuncRefContextOffset:]) == uint64(in.nativeContext)
	}
	if len(in.funcRefDescs) != 11*coreruntime.FuncRefDescBytes {
		return false
	}
	for _, index := range []int{5, 6, 9} {
		if !rv64ReferenceTargetRowValid(in, index) {
			return false
		}
	}
	row := in.funcRefDescs[4*coreruntime.FuncRefDescBytes : 5*coreruntime.FuncRefDescBytes]
	return binary.LittleEndian.Uint64(row) != 0 && !railshotNonCallableFunction(in.c, 3)
}

func rv64ReferenceStartProvider(t *testing.T, rt *Runtime, artifact, vector, wide bool, grow bool) (*Instance, *Instance, []*Memory, *Imports, []uint64) {
	t.Helper()
	rc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-provider-v%t-w%t", vector, wide), wasmtest.ClosedNumericImportResources(vector, wide), artifact)
	provider := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
	im, memories := rv64ReferenceResourceImportBindings(t, provider)
	for _, memory := range memories {
		t.Cleanup(func() { _ = memory.Close() })
	}
	gc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-grower-w%t", !wide), rv64ReferenceResourceGrower(!wide), artifact)
	grower := rv64NativeTokenInstantiate(t, rt, gc, NewImports().Memory("env", "memory", memories[1]))
	raw := []uint64{0x7ff0000000000099}
	if vector {
		raw = append(raw, 0x80000000aabbccdd)
	}
	if grow {
		if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
			t.Fatal("start provider growth", got, err)
		}
		for i, value := range raw {
			binary.LittleEndian.PutUint64(memories[1].UnsafeBytes()[65536+64+8*i:], value)
		}
	}
	return provider, grower, memories, im, raw
}

func rv64ReferenceStartState(t *testing.T, in *Instance, vector bool, result uint64, raw []uint64) {
	t.Helper()
	if !in.c.HasStart || in.c.StartIsImport || in.c.StartLocalFunc != 0 || in.c.NumImports != 3 || !rv64ReferenceStartRows(in) || rv64ReferenceImportTailCapability(in) == 0 {
		t.Fatal("defined start identity/frame/rows/factory")
	}
	if readGlobalObject(in.globalCells[2], ValI32) != 1 || readGlobalObject(in.globalCells[3], ValI32) != result || readGlobalObject(in.globalCells[5], ValI32) != 113 || readGlobalObject(in.globalCells[6], ValI64) != 2 {
		t.Fatal("defined start private result/count/continuation/alias size")
	}
	if vector {
		want := rv64PublicVector(0x7ff0000000000042, 0x80000000deadbeef)
		if got := readGlobalObjectV128(in.globalCells[4]); got != want {
			t.Fatal("start private vector result", got)
		}
		want = rv64PublicVector(raw[0], raw[1])
		for _, index := range []int{1, 7} {
			if got := readGlobalObjectV128(in.globalCells[index]); got != want {
				t.Fatal("start raw resumed vector", got)
			}
		}
	} else if readGlobalObject(in.globalCells[4], ValF64) != 0x7ff0000000000042 || readGlobalObject(in.globalCells[1], ValF64) != raw[0] || readGlobalObject(in.globalCells[7], ValF64) != raw[0] {
		t.Fatal("start raw private/resumed NaN")
	}
	if in.constructionIsActive() || in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 || in.pluginState.Load().railshotInvocation.definedTailOwner != nil {
		t.Fatal("completed start retained construction/generation")
	}
}

func TestRV64PublicReferenceFunctionDefinedStartSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, tails := range []bool{false, true} {
				for _, wide := range []bool{false, true} {
					for _, choice := range []byte{0, 1, 2} {
						t.Run(fmt.Sprintf("artifact%t-vector%t-tails%t-wide%t-choice%d", artifact, vector, tails, wide, choice), func(t *testing.T) {
							rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
							defer rv64HostCertificateCloseRuntime(t, rt)
							provider, grower, memories, im, raw := rv64ReferenceStartProvider(t, rt, artifact, vector, wide, true)
							nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
							native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
							nf, err := native.ExportedFunc("identity")
							if err != nil {
								t.Fatal(err)
							}
							pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-v%t-t%t-w%t-c%d-pfalse", vector, tails, wide, choice), wasmtest.ClosedReferenceDefinedStart(vector, tails, wide, choice, false, nil), artifact)
							if len(pc.Exports) != 0 {
								t.Fatal("cold start fixture exports a function")
							}
							calls := 0
							beforeCounter := uint64(11)
							host := func(value uint64) CallerHostCallFunc {
								return func(caller Caller, call HostCall) {
									calls++
									in := caller.in
									if !in.constructionIsActive() || in.resourcesClosed || in.invocationState.Load()&instanceInvocationCount == 0 || !rv64ReferenceStartRows(in) || rv64ReferenceImportTailCapability(in) == 0 || in.pluginState.Load().railshotInvocation.tailDispatcher == nil {
										panic("private start scope/rows/static factory not ready")
									}
									if len(call.ParamSlots()) != 1 || len(call.ResultSlots()) != 1 || call.ParamSlots()[0] != 0xfffffffd || caller.Memory()[48] != 7 || caller.Memory()[49] != 42 || caller.Memory()[50] != 0 || readGlobalObject(in.globalCells[0], ValI32) != beforeCounter+1 {
										panic("private start initialization/transport ordering")
									}
									runtime.GC()
									call.ResultSlots()[0] = 0xcafebeef00000000 | value
								}
							}
							owned := rv64DynamicHostBinding(t, rt, true, host(0xffffffff), pc.importFuncSigs[2])
							im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host(0xfffffffe)).Function("env.calls", "owned.fn", owned)
							mod, err := rt.Module(pc)
							if err != nil {
								t.Fatal(err)
							}
							defer mod.Close()
							if calls != 0 {
								t.Fatal("compile/decode/module creation ran start")
							}
							for attempt := range 2 {
								memories[0].UnsafeBytes()[50] = 0
								in, err := rt.Instantiate(context.Background(), mod, WithImports(im))
								if err != nil {
									t.Fatal("private defined start", err)
								}
								rv64ReferenceStartState(t, in, vector, []uint64{0xfffffffd, 0xfffffffe, 0xffffffff}[choice], raw)
								if readGlobalObject(in.globalCells[8], ValI32) != uint64(choice&1) || readGlobalObject(in.globalCells[9], ValI32) != uint64(choice&1) || readGlobalObject(provider.globalCells[1], ValI32) != beforeCounter+2 || memories[0].UnsafeBytes()[50] != 99 {
									t.Fatal("start private reference result/ordered effects")
								}
								for range 3 {
									g, err := in.ExportedGlobalObject("start-count")
									if err != nil || g.Get() != 1 {
										t.Fatal("start reran on state access", err)
									}
								}
								if choice == 0 && calls != 0 || choice != 0 && calls != 2*(attempt+1) {
									t.Fatal("start did not run once per instance", calls)
								}
								if err := in.Close(); err != nil {
									t.Fatal(err)
								}
								if !in.resourcesClosed || in.pluginState.Load().railshotInvocation.tailDispatcher != nil {
									t.Fatal("start instance did not release static factory")
								}
								beforeCounter += 2
							}
							if err := owned.(*HostFuncRef).Close(); err != nil {
								t.Fatal("start callback retained", err)
							}
							for _, owner := range []*Instance{provider, native} {
								if err := owner.Close(); err != nil {
									t.Fatal(err)
								}
							}
							if provider.resourcesClosed {
								t.Fatal("start independent grower lost provider")
							}
							if err := grower.Close(); err != nil {
								t.Fatal(err)
							}
							if !provider.resourcesClosed || !native.resourcesClosed {
								t.Fatal("start roots survived last borrower")
							}
						})
					}
				}
			}
		}
	}
}

func TestRV64PublicReferenceFunctionDefinedStartOtherOrdinals(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			for _, index := range []uint32{7, 10} {
				t.Run(fmt.Sprintf("artifact%t-vector%t-index%d", artifact, vector, index), func(t *testing.T) {
					rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
					defer rv64HostCertificateCloseRuntime(t, rt)
					provider, _, memories, im, _ := rv64ReferenceStartProvider(t, rt, artifact, vector, true, true)
					_, nf := rv64ReferenceStartNative(t, rt, artifact)
					pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-start-ordinal-v%t-i%d", vector, index), wasmtest.ClosedReferenceDefinedStartSelected(vector, index), artifact)
					calls := 0
					host := func(v int32) int32 { calls++; return v }
					in := rv64NativeTokenInstantiate(t, rt, pc, im.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", host))
					if in.c.StartLocalFunc != int(index)-3 || readGlobalObject(in.globalCells[5], ValI32) != 73 || calls != 0 || readGlobalObject(provider.globalCells[1], ValI32) != 11 || readGlobalObject(in.globalCells[2], ValI32) != 0 || memories[0].UnsafeBytes()[48] != 7 || memories[0].UnsafeBytes()[49] != 0 {
						t.Fatal("selected nonzero/final start ordinal")
					}
					in.Close()
				})
			}
		}
	}
}
