//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionImportedTailsQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
			native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
			nf, err := native.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			pc := rv64NativeTailCompile(t, "import-tail-quota", wasmtest.ClosedReferenceImportTails(vector, true), false)
			calls := 0
			host := func(v int32) int32 { calls++; return v + 1 }
			owned := rv64DynamicHostBinding(t, rt, true, func(v int32) int32 { calls++; return v + 2 }, pc.importFuncSigs[2])
			imports := NewImports().Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", owned)
			view := pc.executionView()
			if err := view.validateCached(); err != nil {
				t.Fatal(err)
			}
			metadata := uint64(view.arenaNeedForImports(imports.bindings, true) + railshotNativeImportMetadataBytes(view, imports.bindings))
			code := railshotNativeCodeBytes(view)
			if code <= uint64(len(view.code)) || metadata == 0 {
				t.Fatal("missing static factory charge")
			}
			if err := pc.prepareCodeMapping(); err != nil {
				t.Fatal(err)
			}
			cache := pc.loadCodeCache()
			base, sealed, refs := cache.base, cache.sealed, cache.refs
			roots := native.referenceLifetime().snapshot().ResourceRoots
			if err := owned.(*HostFuncRef).Close(); err != nil {
				t.Fatal(err)
			}
			for _, kind := range []string{"code-below", "metadata-below", "exact", "exact-retry"} {
				codeLimit, metadataLimit := code, metadata
				if kind == "code-below" {
					codeLimit--
				}
				if kind == "metadata-below" {
					metadataLimit--
				}
				limited := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithMaxNativeCodeBytes(codeLimit).WithMaxInstanceMetadataBytes(metadataLimit)))
				owned := rv64DynamicHostBinding(t, limited, true, func(v int32) int32 { calls++; return v + 2 }, pc.importFuncSigs[2])
				imports := NewImports().Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", owned)
				mod, err := limited.Module(pc)
				var in *Instance
				if err == nil {
					in, err = limited.Instantiate(context.Background(), mod, WithImports(imports))
					mod.Close()
				}
				if kind == "exact" || kind == "exact-retry" {
					if err != nil || in == nil {
						t.Fatal("exact static factory charge/retry", kind, err)
					}
					args, payload := rv64ReferenceImportTailArgs(vector, 1, 0)
					if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, append(payload, 0xfffffffe, 113)) {
						t.Fatal("exact static factory execution", got, err)
					}
					if in.pluginState.Load().railshotInvocation.tailGenerationBudget.count != 0 {
						t.Fatal("static instance unexpectedly charged a generation")
					}
					if err := in.Close(); err != nil {
						t.Fatal(err)
					}
					// Successful construction seals the shared code image; later
					// refunds restore its refs while preserving that publication.
					base, sealed = cache.base, cache.sealed
				} else {
					var quota *coreruntime.ResourceLimitError
					resource, requested, limit := "native code bytes", code, codeLimit
					if kind == "metadata-below" {
						resource, requested, limit = "instance metadata bytes", metadata, metadataLimit
					}
					if in != nil || !errors.As(err, &quota) || quota.Resource != resource || quota.Requested != requested || quota.Limit != limit || calls != 0 {
						t.Fatal("static factory quota rejection", kind, err)
					}
				}
				if cache.base != base || cache.sealed != sealed || cache.refs != refs || native.referenceLifetime().snapshot().ResourceRoots != roots {
					t.Fatal("static factory quota/refund attached or leaked roots", kind)
				}
				if err := owned.(*HostFuncRef).Close(); err != nil {
					t.Fatal("static factory failure/release retained callback", err)
				}
				if err := limited.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestRV64PublicReferenceFunctionImportedTailsNativeWarm(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, vector := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-vector%t", artifact, vector), func(t *testing.T) {
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				nc := rv64NativeTailCompile(t, "reference-imports-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), artifact)
				native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
				nf, err := native.ExportedFunc("identity")
				if err != nil {
					t.Fatal(err)
				}
				pc := rv64NativeTailCompile(t, fmt.Sprintf("reference-import-tails-v%t-ltrue", vector), wasmtest.ClosedReferenceImportTails(vector, true), artifact)
				in := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", nf).Function("env.calls", "owned.fn", nf))
				if err := native.Close(); err != nil {
					t.Fatal(err)
				}
				if err := nc.Close(); err != nil {
					t.Fatal(err)
				}
				for _, choice := range []uint64{0, 1, 2} {
					args, payload := rv64ReferenceImportTailArgs(vector, choice, 0)
					want := append(payload, 0xfffffffd, 113)
					if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
						t.Fatal("native private imported tail", got, err)
					}
					if n := testing.AllocsPerRun(5, func() {
						got, err := in.Invoke("run", args...)
						if err != nil || !slices.Equal(got, want) {
							panic("warm private imported tail")
						}
					}); n != 0 {
						t.Fatal("warm private imported tail allocated", n)
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if !native.resourcesClosed {
					t.Fatal("native proper-tail final root retained")
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionImportedTailsVectorTarget(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			nc := rv64NativeTailCompile(t, "import-tail-vector-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.V128}), artifact)
			native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
			nf, err := native.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			pc := rv64NativeTailCompile(t, "import-tail-vector-target", wasmtest.ClosedReferenceVectorImportTail(), artifact)
			in := rv64NativeTokenInstantiate(t, rt, pc, NewImports().Function("env.calls", "native.fn", nf))
			if err := native.Close(); err != nil {
				t.Fatal(err)
			}
			if err := nc.Close(); err != nil {
				t.Fatal(err)
			}
			for _, null := range []uint64{0, 1} {
				args := []uint64{0x7ff0000000000042, 0x80000000deadbeef, 0xcafefeed00000000 | null}
				want := []uint64{args[0], args[1], 113}
				if got, err := in.Invoke("run", args...); err != nil || !slices.Equal(got, want) {
					t.Fatal("private native vector-tail transport", got, err)
				}
			}
			if err := in.Close(); err != nil {
				t.Fatal(err)
			}
			if !native.resourcesClosed {
				t.Fatal("vector-tail final owner retained")
			}
			// Go callbacks remain scalar even when the source's native-only
			// numeric target is a vector. Reject before callable publication.
			pc2 := rv64NativeTailCompile(t, "import-tail-vector-target", wasmtest.ClosedReferenceVectorImportTail(), artifact)
			mod, err := rt.Module(pc2)
			if err != nil {
				t.Fatal(err)
			}
			bad, err := rt.Instantiate(context.Background(), mod, WithImports(NewImports().Function("env.calls", "native.fn", HostCallFunc(func(HostCall) { panic("vector host called") }))))
			mod.Close()
			if bad != nil {
				bad.Close()
			}
			if bad != nil || err == nil {
				t.Fatal("vector host proper tail published", err)
			}
		})
	}
}
