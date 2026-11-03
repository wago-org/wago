//go:build linux && amd64 && !wago_standalone

package wago

import (
	"context"
	"fmt"
	"reflect"
	"testing"

	backend "github.com/wago-org/wago/src/core/compiler/backend/railshot/amd64"
	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func conservativeUnwindSerializedRoots(plan *shared.GCModuleFrameRootPlan, entries []int) []byte {
	rootMap := &compiledGCFrameRoots{}
	for i := 0; i < plan.FunctionCount(); i++ {
		function := plan.Function(i)
		if function == nil {
			continue
		}
		base := uint32(entries[i])
		if function.AdapterReturnOffset != 0 {
			rootMap.adapterReturnOffsets = append(rootMap.adapterReturnOffsets, base+function.AdapterReturnOffset)
		}
		function.VisitCallsites(func(_ int, site shared.GCFrameCallsite) bool {
			rootMap.callsites = append(rootMap.callsites, compiledGCFrameCallsite{
				returnOffset: base + site.ReturnOffset(), frameBytes: function.FrameBytes,
				stackAdjust: site.StackAdjust(), offsets: append([]uint32(nil), site.Offsets()...),
			})
			return true
		})
	}
	rootMap.adapterReturnOffsets = normalizeAdapterReturnOffsets(rootMap.adapterReturnOffsets)
	var writer compiledWriter
	writer.gcFrameRoots(rootMap)
	return append([]byte(nil), writer.buf...)
}

func normalizeConservativeUnwindPlan(plan *shared.GCModuleFrameRootPlan) {
	for i := 0; i < plan.FunctionCount(); i++ {
		if function := plan.Function(i); function != nil && len(function.Locals) == 0 {
			// The optimized planner intentionally uses nil. Legacy planning used an
			// empty slice; this is the only representation normalization permitted.
			function.Locals = nil
		}
	}
}

func TestConservativeUnwindEmittedMapParity(t *testing.T) {
	for _, fixture := range []struct {
		name         string
		table, chain bool
	}{{"import-plus-one", false, false}, {"unused-funcref-table", true, false}, {"local-call-chain", false, true}} {
		for _, registerABI := range []bool{false, true} {
			for _, workers := range []int{1, 2} {
				t.Run(fmt.Sprintf("%s/reg=%t/workers=%d", fixture.name, registerABI, workers), func(t *testing.T) {
					var baseline []any
					for candidate := false; ; candidate = true {
						m, analysis := unwindPreparationFixture(t, fixture.table, fixture.chain)
						var diagnostic string
						plan := newGCFrameRootPlanMode(m, true, false, candidate, &diagnostic, analysis)
						if plan == nil || diagnostic != "" {
							t.Fatalf("plan=%#v diagnostic=%q", plan, diagnostic)
						}
						for i := 0; i < plan.FunctionCount(); i++ {
							if function := plan.Function(i); function != nil {
								function.UnwindOnly = true
							}
						}
						compiled, err := backend.CompileModuleWith(m, backend.CompileOptions{
							GCFrameRoots: plan, Workers: workers, DeferCodeMapping: true, SyncHostCalls: true,
							ImportBindings: []backend.ImportBinding{{Dynamic: true, ImportIndex: 0}},
							Optimizations:  map[string]bool{"reg-abi": registerABI, "inline": false},
						})
						if err != nil {
							t.Fatal(err)
						}
						serialized := conservativeUnwindSerializedRoots(plan, compiled.Entry)
						normalizeConservativeUnwindPlan(plan)
						snapshot := []any{append([]byte(nil), compiled.Code...), compiled.Entry, compiled.InternalEntry,
							compiled.DirectPrepared, compiled.DirectPreparedLight, compiled.DirectPreparedBounded, plan, serialized}
						if !candidate {
							baseline = snapshot
						} else if !reflect.DeepEqual(baseline, snapshot) {
							t.Fatalf("legacy/candidate emitted output differs")
						}
						for i := 0; i < plan.FunctionCount(); i++ {
							if function := plan.Function(i); function != nil && (!function.Exact || function.FrameBytes == 0 || function.CallsiteCount() < function.CallMaskCount()) {
								t.Fatalf("function %d has invalid physical map: %#v", i, function)
							}
						}
						if candidate {
							break
						}
					}
				})
			}
		}
	}
}

func TestConservativeUnwindPublicLateBinding(t *testing.T) {
	for _, registerABI := range []bool{false, true} {
		for _, chain := range []bool{false, true} {
			t.Run(fmt.Sprintf("reg=%t/chain=%t", registerABI, chain), func(t *testing.T) {
				functionImport := append(append(wasmtest.Name("env"), wasmtest.Name("f")...), byte(wasm.ExternFunc), 0)
				functions := [][]byte{{0}}
				codes := [][]byte{wasmtest.Code([]byte{0x10, 0, 0x41, 1, 0x6a, 0x0b})}
				exported := uint32(1)
				if chain {
					functions = append(functions, []byte{0})
					codes = append(codes, wasmtest.Code([]byte{0x10, 1, 0x0b}))
					exported = 2
				}
				module := wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(2, wasmtest.Vec(functionImport)), wasmtest.Section(3, wasmtest.Vec(functions...)),
					wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, exported))),
					wasmtest.Section(10, wasmtest.Vec(codes...)),
				)
				config := NewRuntimeConfig().WithCoreFeatures(CoreFeaturesV3).WithOptimization("reg-abi", registerABI).WithOptimization("inline", false).WithFunctionWorkers(2)
				runtime := NewRuntime(WithRuntimeConfig(config))
				defer runtime.Close()
				compiled, err := runtime.Compile(module)
				if err != nil {
					t.Fatal(err)
				}
				defer compiled.Close()
				providerCode, err := runtime.Compile(wasmtest.Module(
					wasmtest.Section(1, wasmtest.Vec(wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}))),
					wasmtest.Section(3, wasmtest.Vec([]byte{0})), wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("f", 0, 0))),
					wasmtest.Section(10, wasmtest.Vec(wasmtest.Code([]byte{0x41, 7, 0x0b}))),
				))
				if err != nil {
					t.Fatal(err)
				}
				defer providerCode.Close()
				provider, err := runtime.Instantiate(context.Background(), providerCode)
				if err != nil {
					t.Fatal(err)
				}
				defer provider.Close()
				nativeTarget, err := provider.ExportedFunc("f")
				if err != nil {
					t.Fatal(err)
				}
				hostCalls := 0
				bindings := []any{slotHostFunc(func(_ HostModule, _ []uint64, results []uint64) { hostCalls++; results[0] = 7 }), nativeTarget}
				for _, binding := range bindings {
					instance, err := runtime.Instantiate(context.Background(), compiled, WithImports(testImports("env.f", binding)))
					if err != nil {
						t.Fatal(err)
					}
					result, invokeErr := instance.Invoke("run")
					instance.Close()
					if invokeErr != nil || !reflect.DeepEqual(result, []uint64{8}) {
						t.Fatalf("result=%v err=%v", result, invokeErr)
					}
				}
				if hostCalls != 1 {
					t.Fatalf("host calls=%d, want 1", hostCalls)
				}
			})
		}
	}
}
