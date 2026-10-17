//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func TestRV64PublicReferenceFunctionResourceImportsQuota(t *testing.T) {
	for _, vector := range []bool{false, true} {
		t.Run(fmt.Sprintf("vector%t", vector), func(t *testing.T) {
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			nc := rv64NativeTailCompile(t, "reference-resource-imports-quota-native", rv64PublicNativeIdentityModule([]wasm.ValType{wasm.I32}), false)
			native := rv64NativeTokenInstantiate(t, rt, nc, NewImports())
			nf, err := native.ExportedFunc("identity")
			if err != nil {
				t.Fatal(err)
			}
			pc := rv64NativeTailCompile(t, "reference-resource-imports-quota-producer", wasmtest.ClosedReferenceResourceImports(vector, true, true), false)
			calls := 0
			host := func(v int32) int32 { calls++; return v + 1 }
			owned := rv64DynamicHostBinding(t, rt, true, func(v int32) int32 { calls++; return v + 2 }, pc.importFuncSigs[2])
			rc := rv64NativeTailCompile(t, "reference-resource-quota-provider", wasmtest.ClosedNumericImportResources(vector, true), false)
			resource := rv64NativeTokenInstantiate(t, rt, rc, NewImports())
			bindings, memories := rv64ReferenceResourceImportBindings(t, resource)
			gc := rv64NativeTailCompile(t, "reference-resource-quota-grower", rv64ReferenceResourceGrower(false), false)
			grower := rv64NativeTokenInstantiate(t, rt, gc, NewImports().Memory("env", "memory", memories[1]))
			if got, err := grower.Invoke("grow", 1); err != nil || !slices.Equal(got, []uint64{1}) {
				t.Fatal("quota pre-growth", got, err)
			}
			if err := grower.Close(); err != nil {
				t.Fatal(err)
			}
			producer := rv64NativeTokenInstantiate(t, rt, pc, bindings.Function("env.calls", "native.fn", nf).Function("env.calls", "shared.fn", host).Function("env.calls", "owned.fn", owned))
			pf, err := producer.ExportedFunc("run")
			if err != nil {
				t.Fatal(err)
			}
			cc := rv64NativeTailCompile(t, "reference-resource-imports-quota-caller", rv64ReferenceImportsConsumer(vector, false, "reference-tail"), false)
			imports := NewImports().Function("env", "target", pf)
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
				args := []uint64{0xdeadbeeffffffffd, 0x7ff0000000000001}
				if vector {
					args = append(args, 0x80000000deadbeef)
				}
				want := append(slices.Clone(args[1:]), 9, 0)
				args = append(args, 0)
				before := calls
				got, err := in.Invoke("call", args...)
				restore()
				if kind == "exact" {
					if err != nil || !slices.Equal(got, want) || calls != before+2 {
						t.Fatal("exact imported generation quota", got, calls, err)
					}
				} else {
					var quota *coreruntime.ResourceLimitError
					resource, requested, limit := "native code bytes", uint64(code), codeLimit
					if kind == "metadata-below" {
						resource, requested, limit = "instance metadata bytes", base+uint64(metadata), metadataLimit
					}
					if !errors.As(err, &quota) || quota.Resource != resource || quota.Requested != requested || quota.Limit != limit || calls != before {
						t.Fatal("imported quota rejection before calls", kind, err, calls)
					}
				}
				for _, owner := range []*Instance{in, producer, native} {
					state := &owner.pluginState.Load().railshotInvocation
					if state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil {
						t.Fatal("imported generation charge not refunded")
					}
				}
				if err := in.Close(); err != nil {
					t.Fatal(err)
				}
				if err := limited.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}
