//go:build linux && riscv64 && !tinygo && !wago_target_tinygo

package wago

import (
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The call wrapper takes two independently authenticated public reference
// arguments: an abstract nullable funcref forwarded to the target and the
// exact nullable callable target reference. The target reports nullness.
func rv64PublicReferenceArgumentVectorModule() []byte {
	return rv64PublicReferenceArgumentVectorModuleType(wasm.FuncRef)
}

func rv64PublicReferenceArgumentVectorModuleType(argument wasm.ValType) []byte {
	argumentByte := byte(0x70)
	if wasm.EqualValType(argument, wasm.ExternRef) {
		argumentByte = 0x6f
	}
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType([]wasm.ValType{argument}, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			[]byte{0x60, 2, argumentByte, 0x63, 0, 1, 0x7f},
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 0, 0xd1, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x14, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicReferenceFunctionArgumentVectorSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-argument-vector", rv64PublicReferenceArgumentVectorModule(), artifact)
			if !rv64PublicReferenceCallBoundary(c) || !rv64PublicReferenceCarrierTarget(c, 0) || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || rv64NativeTailModule(c) != nil {
				t.Fatal("public reference argument-vector metadata")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("reference argument-vector target token", got, err)
			}
			token := got[0]
			for _, tc := range []struct {
				name string
				args []uint64
				want []uint64
			}{
				{"null-argument", []uint64{0, token}, []uint64{1}},
				{"nonnull-argument", []uint64{token, token}, []uint64{0}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					if got, err = in.Invoke("call", tc.args...); err != nil || !slices.Equal(got, tc.want) {
						t.Fatal("reference argument-vector call", got, tc.want, err)
					}
					rv64AssertPublicReferenceArgumentScopesClear(t, in)
				})
			}
			fn, err := in.WasmFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if got, err = session.Invoke(token, token); err != nil || !slices.Equal(got, []uint64{0}) {
				t.Fatal("prepared reference argument-vector call", got, err)
			}
			session.Close()
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("target", token); err != nil || !slices.Equal(got, []uint64{0}) {
				t.Fatal("direct reference-argument target", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", token^0x9e3779b97f4a7c15, token); err == nil || got != nil {
				t.Fatal("forged forwarded reference argument", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", token, 0); got != nil {
				t.Fatal("null callable target returned values", got, err)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapNullReference {
					t.Fatal("null callable target trap", err)
				}
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
		})
	}
}

func TestRV64PublicReferenceFunctionArgumentVectorForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-argument-vector", rv64PublicReferenceArgumentVectorModule(), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			argumentProducer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			targetProducer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer consumer.Close()
			argumentResult, err := argumentProducer.Invoke("get")
			if err != nil || len(argumentResult) != 1 || argumentResult[0] == 0 {
				t.Fatal("foreign forwarded reference token", argumentResult, err)
			}
			targetResult, err := targetProducer.Invoke("get")
			if err != nil || len(targetResult) != 1 || targetResult[0] == 0 {
				t.Fatal("foreign callable reference token", targetResult, err)
			}
			if err := argumentProducer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := targetProducer.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := consumer.Invoke("call", argumentResult[0], targetResult[0])
			if err != nil || !slices.Equal(got, []uint64{0}) {
				t.Fatal("foreign reference argument-vector call", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			if argumentProducer.usesIndependentExecution() || targetProducer.usesIndependentExecution() || consumer.usesIndependentExecution() || !argumentProducer.nativeControlIsShared() || !targetProducer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign reference argument vector did not reconcile native ownership")
			}
		})
	}
}

func TestRV64PublicReferenceFunctionArgumentVectorAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "reference-function-argument-vector-admission", rv64PublicReferenceArgumentVectorModule(), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"missing-call-owner-abi", func(c *Compiled) { c.nativeReferenceCallABI = false }},
		{"missing-dynamic-escape-witness", func(c *Compiled) { c.dynamicFuncrefEscape = false }},
		{"target-actual-parameter", func(c *Compiled) { c.Funcs[0].Params[0] = ValExternRef }},
		{"target-declared-parameter", func(c *Compiled) { c.Types[0].Params[0].Ref.Heap.Abstract = AbstractHeapExtern }},
		{"caller-forwarded-parameter", func(c *Compiled) { c.Types[2].Params[0].Ref.Heap.Abstract = AbstractHeapExtern }},
		{"caller-target-parameter", func(c *Compiled) { c.Types[2].Params[1].Ref.Heap.TypeIndex = 1 }},
		{"target-signature-key", func(c *Compiled) { c.FuncTypeID[0]++ }},
		{"target-only-export", func(c *Compiled) { c.Exports = map[string]int{"target": 0} }},
		{"no-exports", func(c *Compiled) { c.Exports = map[string]int{} }},
		{"tail-feature", func(c *Compiled) { c.requiredFeatures |= CoreFeatureTailCall }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			c.Funcs = slices.Clone(base.Funcs)
			c.Types = slices.Clone(base.Types)
			c.Funcs[0].Params = slices.Clone(base.Funcs[0].Params)
			c.Types[0].Params = slices.Clone(base.Types[0].Params)
			c.Types[2].Params = slices.Clone(base.Types[2].Params)
			c.FuncTypeID = slices.Clone(base.FuncTypeID)
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-matching public reference argument vector admitted", err)
			}
		})
	}
}
