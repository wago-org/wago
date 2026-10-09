//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"testing"

	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionNumericCallsQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			owners := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, owners)
			provider, _, memories, im, _ := rv64ReferenceStartProvider(t, owners, false, vector, true, true)
			native, nf := rv64ReferenceStartNative(t, owners, false)
			pc := rv64NativeTailCompile(t, "reference-numeric-calls-quota", wasmtest.ClosedReferenceNumericCalls(vector, true, true, 1, 0), false)
			view := pc.executionView()
			if err := view.validateCached(); err != nil {
				t.Fatal(err)
			}
			calls := 0
			start := CallerHostCallFunc(func(caller Caller, _ HostCall) {
				calls++
				if _, err := caller.in.InvokeFromHost(context.Background(), caller, "clear"); err != nil {
					panic(err)
				}
			})
			owned := rv64DynamicHostBinding(t, owners, true, start, pc.importFuncSigs[1])
			resourceBindings := maps.Clone(im.bindings)
			im.Function("env.calls", "unused.fn", nf).Function("env.calls", "finish.fn", func(v int32) int32 { return v + 1 }).Function("env.calls", "start.fn", owned)
			metadata := uint64(view.arenaNeedForImports(im.bindings, true) + railshotNativeImportMetadataBytes(view, im.bindings))
			code := railshotNativeCodeBytes(view)
			if metadata == 0 || code <= uint64(len(view.code)) {
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
					if in != nil || !errors.As(err, &quota) || quota.Resource != resource || quota.Requested != requested || quota.Limit != limit || calls != 0 || readGlobalObject(provider.globalCells[1], ValI32) != before || memories[0].UnsafeBytes()[49] != 0 {
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
