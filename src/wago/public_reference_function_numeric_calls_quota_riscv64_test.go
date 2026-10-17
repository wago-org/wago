//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRV64PublicReferenceFunctionNumericCallsQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 0)
}
func TestRV64PublicReferenceFunctionOwnedTableQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 1)
}
func TestRV64PublicReferenceFunctionTableBulkQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 2)
}
func TestRV64PublicReferenceFunctionProperReferenceTailQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 3)
}
func TestRV64PublicReferenceFunctionProperIndirectTailQuota(t *testing.T) {
	rv64ReferenceFunctionNumericCallsQuota(t, 4)
}
func rv64ReferenceFunctionNumericCallsQuota(t *testing.T, tableStage int) {
	ownedTable := tableStage > 0
	t.Helper()
	product := "numeric-calls"
	if ownedTable {
		product = "owned-table"
	}
	if tableStage == 2 {
		product = "table-bulk"
	}
	if tableStage >= 3 {
		product = "proper-reference-tail"
	}
	if tableStage == 4 {
		product = "proper-indirect-tail"
	}
	if tableStage == 5 {
		product = "expression-elements"
	}
	if tableStage == 6 {
		product = "immutable-cells"
	}
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, owners)
			provider, _, memories, im, _ := rv64ReferenceStartProvider(t, owners, false, vector, true, true)
			native, nf := rv64ReferenceStartNative(t, owners, false)
			pc := rv64NativeTailCompile(t, "reference-"+product+"-quota", rv64ReferenceNumericCallsFixture(tableStage, false, vector, true, true, 1, 0), false)
			view := pc.executionView()
			if err := view.validateCached(); err != nil {
				t.Fatal(err)
			}
			calls := 0
			start := CallerHostCallFunc(func(caller Caller, _ HostCall) {
				calls++
				// This oracle isolates the fresh entry's exact generation charge.
				// Nested generations during target reentry have separate coverage.
				if tableStage >= 3 {
					return
				}
				if _, err := caller.in.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
					panic(err)
				}
			})
			owned := rv64DynamicHostBinding(t, owners, true, start, pc.importFuncSigs[1])
			resourceBindings := maps.Clone(im.bindings)
			im.Function("env.calls", "unused.fn", nf).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 }).Function("env.calls", "start.fn", owned)
			metadata := uint64(view.arenaNeedForImports(im.bindings, true) + railshotNativeImportMetadataBytes(view, im.bindings))
			code := railshotNativeCodeBytes(view)
			if tableStage >= 3 {
				// Mixed direct/reference sites acquire one dynamic generation before
				// the start can run. Charge its retained owner graph as well as the
				// instance metadata; a competing static dispatcher is absent.
				if code != uint64(len(view.code)) || rv64StaticTailPlanCount(rv64NativeTailModule(view)) != 0 {
					t.Fatal("mixed start acquired static dispatcher")
				}
				probe := rv64NativeTokenInstantiate(t, owners, pc, im)
				generationCode, generationMetadata, instanceMetadata := rv64DynamicTailCharge(t, probe)
				if instanceMetadata != metadata || uint64(generationCode) <= code || generationMetadata == 0 {
					t.Fatal("unaccounted mixed start generation", generationCode, generationMetadata, instanceMetadata, code, metadata)
				}
				code, metadata = uint64(generationCode), instanceMetadata+uint64(generationMetadata)
				probe.Close()
				calls = 0
			} else if metadata == 0 || code <= uint64(len(view.code)) {
				t.Fatal("unaccounted static start factory")
			}
			owned.(*HostFuncRef).Close()
			if err := pc.prepareCodeMapping(); err != nil {
				t.Fatal(err)
			}
			cache := pc.loadCodeCache()
			base, sealed, refs := cache.base, cache.sealed, cache.refs
			providerRoots, nativeRoots := provider.referenceLifetime().snapshot().ResourceRoots, native.referenceLifetime().snapshot().ResourceRoots
			for _, kind := range []string{"code-below", "metadata-below", "exact", "exact-retry"} {
				codeLimit, metadataLimit := code, metadata
				if kind == "code-below" {
					codeLimit--
				}
				if kind == "metadata-below" {
					metadataLimit--
				}
				if tableStage >= 3 {
					// Dynamic start generations embed physical caller/import addresses,
					// so another instance's exact byte count is not a stable oracle.
					// Keep metadata exact; use a deterministic code rejection that is
					// below every non-empty dispatcher, and leave successful code
					// admission unbounded. Same-instance tests cover the one-byte edge.
					codeLimit = 0
					if kind == "code-below" {
						codeLimit = 1
					}
				}
				limited := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithMaxNativeCodeBytes(codeLimit).WithMaxInstanceMetadataBytes(metadataLimit)))
				owned := rv64DynamicHostBinding(t, limited, true, start, pc.importFuncSigs[1])
				im := NewImports()
				im.bindings = maps.Clone(resourceBindings)
				im.Function("env.calls", "unused.fn", nf).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 }).Function("env.calls", "start.fn", owned)
				memories[0].UnsafeBytes()[49] = 0
				before := readGlobalObject(provider.globalCells[1], ValI32)
				mod, err := limited.Module(pc)
				var in *Instance
				if err == nil {
					in, err = limited.Instantiate(context.Background(), mod, WithImports(im))
					mod.Close()
				}
				if kind == "exact" || kind == "exact-retry" {
					if err != nil || in == nil {
						t.Fatal("exact start quota/retry", kind, err)
					}
					if tableStage >= 3 {
						if _, err := in.Invoke("clear"); err != nil {
							t.Fatal("fresh generation after start", err)
						}
						if state := &in.pluginState.Load().railshotInvocation; state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || rv64EntryCapability(in) != 0 {
							t.Fatal("start quota generation refund")
						}
					}
					rv64ReferenceNumericCallsCheck(t, in, vector, 3)
					if readGlobalObject(provider.globalCells[1], ValI32) != before+1 || calls != (map[string]int{"exact": 1, "exact-retry": 2}[kind]) {
						t.Fatal("exact quota start execution")
					}
					in.Close()
					base, sealed = cache.base, cache.sealed
				} else {
					var quota *coreruntime.ResourceLimitError
					resource, requested, limit := "native code bytes", code, codeLimit
					if kind == "metadata-below" {
						resource, requested, limit = "instance metadata bytes", metadata, metadataLimit
					}
					quotaMatches := errors.As(err, &quota) && quota.Resource == resource && quota.Limit == limit
					if tableStage >= 3 && kind == "code-below" {
						quotaMatches = quotaMatches && quota.Requested > quota.Limit
					} else {
						quotaMatches = quotaMatches && quota.Requested == requested
					}
					if in != nil || !quotaMatches || calls != 0 || readGlobalObject(provider.globalCells[1], ValI32) != before || memories[0].UnsafeBytes()[49] != 0 {
						t.Fatal("quota failed after start effects", kind, err)
					}
				}
				if cache.base != base || cache.sealed != sealed || cache.refs != refs || provider.referenceLifetime().snapshot().ResourceRoots != providerRoots || native.referenceLifetime().snapshot().ResourceRoots != nativeRoots {
					t.Fatal("start quota/refund code or resource leak", kind)
				}
				if err := owned.(*HostFuncRef).Close(); err != nil {
					t.Fatal(err)
				}
				if err := limited.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
