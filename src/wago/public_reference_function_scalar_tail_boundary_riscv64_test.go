//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	encoder "github.com/wago-org/wago/src/core/encoder/riscv64"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

func rv64PublicReferenceScalarTailBoundaryModule() []byte {
	callerType := []byte{0x60, 5, 0x7f, 0x7e, 0x7d, 0x7c, 0x63, 0, 4, 0x7f, 0x7e, 0x7d, 0x7c}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}, []wasm.ValType{wasm.I32, wasm.I64, wasm.F32, wasm.F64}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			callerType,
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x15, 0, 0x0b}),
		)),
	)
}

func rv64AssertPublicReferenceScalarTailClear(t *testing.T, in *Instance) {
	t.Helper()
	rv64AssertPublicReferenceArgumentScopesClear(t, in)
	state := &in.pluginState.Load().railshotInvocation
	if state.tailGenerationBudget.count != 0 || state.definedTailOwner != nil || rv64EntryCapability(in) != 0 {
		t.Fatal("public scalar reference tail retained invocation state", state.tailGenerationBudget.count, state.definedTailOwner, rv64EntryCapability(in))
	}
}

func TestRV64PublicReferenceFunctionScalarTailBoundarySourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-scalar-tail-boundary", rv64PublicReferenceScalarTailBoundaryModule(), artifact)
			m := rv64NativeTailModule(c)
			if !rv64PublicReferenceCallBoundary(c) || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.requiredFeatures != CoreFeatureBulkMemoryOperations|CoreFeatureMultiValue|CoreFeatureReferenceTypes|CoreFeatureTailCall|CoreFeatureTypedFunctionReferences || m == nil || len(m.Plans) != 1 {
				t.Fatal("public scalar reference tail metadata", c.requiredFeatures, m)
			}
			p := m.Plans[0]
			if p.Kind != encoder.NativeTailReference || p.TypeIndex != 0 || p.FunctionIndex != 2 || p.ImportIndex != 0 || p.SourcePC != 11 || p.FrameBytes != 128 || p.SourceBase != 88 || p.SourceStride != 8 || p.ReferenceBase != 120 {
				t.Fatal("public scalar reference tail plan", p)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig().WithNativeStackBytes(MinNativeStackBytes)))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("scalar reference tail token", got, err)
			}
			args, want := rv64PublicReferenceScalarArguments(got[0])
			for i := 0; i < 3; i++ {
				if got, err = in.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
					t.Fatal("scalar return_call_ref result", i, got, want, err)
				}
				rv64AssertPublicReferenceScalarTailClear(t, in)
			}
			fn, err := in.WasmFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if got, err = session.Invoke(args...); err != nil || !slices.Equal(got, want) {
				t.Fatal("prepared scalar return_call_ref", got, want, err)
			}
			session.Close()
			rv64AssertPublicReferenceScalarTailClear(t, in)

			nullArgs := slices.Clone(args)
			nullArgs[len(nullArgs)-1] = 0
			if got, err = in.Invoke("call", nullArgs...); got != nil {
				t.Fatal("null scalar return_call_ref returned values", got, err)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("null scalar return_call_ref trap", err)
				}
			}
			rv64AssertPublicReferenceScalarTailClear(t, in)
			forged := slices.Clone(args)
			forged[len(forged)-1] ^= 0x9e3779b97f4a7c15
			if got, err = in.Invoke("call", forged...); err == nil || got != nil {
				t.Fatal("forged scalar return_call_ref ingress", got, err)
			}
			rv64AssertPublicReferenceScalarTailClear(t, in)
		})
	}
}

func TestRV64PublicReferenceFunctionScalarTailBoundaryForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-scalar-tail-boundary", rv64PublicReferenceScalarTailBoundaryModule(), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			producer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer consumer.Close()
			got, err := producer.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("foreign scalar tail token", got, err)
			}
			args, want := rv64PublicReferenceScalarArguments(got[0])
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("foreign scalar tail token did not retain producer")
			}
			if got, err = consumer.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
				t.Fatal("foreign scalar return_call_ref", got, want, err)
			}
			rv64AssertPublicReferenceScalarTailClear(t, consumer)
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign scalar tail did not reconcile native ownership")
			}
		})
	}
}

func TestRV64PublicReferenceFunctionScalarTailBoundaryAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "reference-function-scalar-tail-boundary-admission", rv64PublicReferenceScalarTailBoundaryModule(), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"missing-call-owner-abi", func(c *Compiled) { c.nativeReferenceCallABI = false }},
		{"missing-tail-feature", func(c *Compiled) { c.requiredFeatures &^= CoreFeatureTailCall }},
		{"missing-plan", func(c *Compiled) { rv64StoreNativeTailModule(c, nil) }},
		{"plan-kind", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].Kind = encoder.NativeTailIndirect }},
		{"plan-type", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].TypeIndex = 1 }},
		{"plan-function", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].FunctionIndex = 1 }},
		{"plan-source", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].SourcePC++ }},
		{"plan-frame", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].FrameBytes += 16 }},
		{"plan-source-base", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].SourceBase -= 8 }},
		{"plan-source-stride", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].SourceStride = 16 }},
		{"plan-reference-base", func(c *Compiled) { rv64NativeTailModule(c).Plans[0].ReferenceBase -= 8 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			if m := rv64NativeTailModule(base); m != nil {
				rv64StoreNativeTailModule(c, &encoder.NativeTailModule{Plans: slices.Clone(m.Plans)})
			}
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-matching scalar public reference tail admitted", err)
			}
		})
	}
}
