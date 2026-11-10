//go:build linux && riscv64 && !tinygo && !wago_guardpage

package wago

import (
	"fmt"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/backend/railshot/shared"
	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64FunctionStorageSourcePlans(t *testing.T) { testRV64FunctionStorageSourcePlans(t, false) }

func TestRV64FunctionStorageLoweringPlans(t *testing.T) { testRV64FunctionStorageSourcePlans(t, true) }

func testRV64FunctionStorageSourcePlans(t *testing.T, lower bool) {
	for _, foreign := range []bool{false, true} {
		for _, array := range []bool{false, true} {
			mutations := []string{"", "set"}
			if array {
				mutations = append(mutations, "fill", "init")
			}
			for _, mutation := range mutations {
				for _, local := range []bool{false, true} {
					for _, vector := range []bool{false, true} {
						t.Run(fmt.Sprintf("foreign=%v/array=%v/%s/local=%v/vector=%v", foreign, array, mutation, local, vector), func(t *testing.T) {
							m, err := wasm.DecodeModule(rv64FunctionStorageInputFixture(array, local, vector, mutation, foreign))
							if err != nil {
								t.Fatal(err)
							}
							var diagnostic string
							plans := newGCFrameRootPlanOwned(m, true, &diagnostic, &wasm.ValidatedModuleAnalysis{}, false)
							if plans == nil {
								t.Fatal(diagnostic)
							}
							plan := plans.Function(1)
							wantAllocations := 2
							if mutation != "" {
								wantAllocations++
							}
							if plan == nil || plan.AllocationMaskCount() != wantAllocations || plan.CallMaskCount() != 1 || !plan.Conservative || !plan.Exact {
								t.Fatalf("incomplete source plan: %+v", plan)
							}
							wantFunctions := 0
							if local {
								wantFunctions++
							}
							if foreign {
								wantFunctions++
							}
							functions := 0
							for _, root := range plan.Locals {
								if root.Offset&shared.GCFrameFuncRefRootFlag != 0 {
									functions++
								}
							}
							if functions != wantFunctions {
								t.Fatalf("function local roots=%v, want %d", plan.Locals, wantFunctions)
							}
							if !moduleHasGCAllocationSitesWithValidation(m, &wasm.ValidatedModuleAnalysis{}) {
								t.Fatal("function storage allocation summary missing")
							}
							if lower {
								metadata, err := frontend.BuildGCTypeMetadata(m)
								if err != nil {
									t.Fatal(err)
								}
								compiled, err := railshotCompileModuleWith(m, railshotCompileOptions{GCFrameRootsSource: true, GCFrameRoots: plans, GCStructHelpers: true, GCArrayHelpers: array, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, NativeReferenceCalls: true, SyncHostSlots: rv64GenericArrayFixedSlots, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}})
								if err != nil {
									t.Fatal(err)
								}
								if compiled.CodeImage != nil {
									defer compiled.CodeImage.Close()
								}
								if !validGCModuleFrameRootPlanMode(plans, true) || plan.SafepointCount() != wantAllocations || plan.CallsiteCount() != 1 {
									t.Fatalf("backend did not finalize source roots: %+v", plan)
								}
							}
						})
					}
				}
			}
		}
	}
}
