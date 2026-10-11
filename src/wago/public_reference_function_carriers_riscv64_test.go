//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The target deliberately mixes narrow, wide and both public reference
// carriers in both directions. The wrapper forwards the complete vector to an
// authenticated exact call_ref target and returns it without reinterpretation.
func rv64PublicReferenceCarrierModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(
				[]wasm.ValType{wasm.I32, wasm.FuncRef, wasm.ExternRef, wasm.I64},
				[]wasm.ValType{wasm.ExternRef, wasm.FuncRef, wasm.I32, wasm.I64},
			),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			[]byte{0x60, 5, 0x7f, 0x70, 0x6f, 0x7e, 0x63, 0, 4, 0x6f, 0x70, 0x7f, 0x7e},
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x20, 2, 0x20, 1, 0x20, 0, 0x20, 3, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x14, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicReferenceFunctionCarriersSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-carriers", rv64PublicReferenceCarrierModule(), artifact)
			if !rv64PublicReferenceCallBoundary(c) || !rv64PublicReferenceCarrierTarget(c, 0) || rv64NativeTailModule(c) != nil {
				t.Fatal("public reference carrier metadata")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			ref, err := rt.NewExternRef("reference-carrier")
			if err != nil {
				t.Fatal(err)
			}
			extern := ValueExternRef(ref).Bits()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			callable, err := in.Invoke("get")
			if err != nil || len(callable) != 1 || callable[0] == 0 {
				t.Fatal("reference carrier target token", callable, err)
			}
			args := []uint64{0xfeedface89abcdef, callable[0], extern, 0xfedcba9876543210, callable[0]}
			got, err := in.Invoke("call", args...)
			if err != nil || len(got) != 4 || got[0] != extern || got[2] != 0x89abcdef || got[3] != args[3] || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[1]).FuncRef(), 0) {
				t.Fatal("mixed reference carrier call", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)

			callable, err = in.Invoke("get")
			if err != nil || len(callable) != 1 {
				t.Fatal("fresh typed reference carrier target", callable, err)
			}
			typed, err := in.InvokeValues(context.Background(), "call",
				ValueI32(-1985229329), ValueFuncRef(ValueOf(ValFuncRef, callable[0]).FuncRef()),
				ValueExternRef(ref), ValueI64(-81985529216486896),
				ValueFuncRef(ValueOf(ValFuncRef, callable[0]).FuncRef()))
			if err != nil || len(typed) != 4 || typed[0].Type() != ValExternRef || typed[0].ExternRef() != ref || typed[1].Type() != ValFuncRef || !in.FuncRefMatchesFunction(typed[1].FuncRef(), 0) || typed[2].Type() != ValI32 || typed[2].Bits() != 0x89abcdef || typed[3].Type() != ValI64 {
				t.Fatal("typed mixed reference carrier call", typed, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)

			callable, err = in.Invoke("get")
			if err != nil || len(callable) != 1 {
				t.Fatal("fresh prepared reference carrier target", callable, err)
			}
			fn, err := in.WasmFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if got, err = session.Invoke(7, 0, 0, 9, callable[0]); err != nil || !slices.Equal(got, []uint64{0, 0, 7, 9}) {
				t.Fatal("prepared null reference carrier call", got, err)
			}
			session.Close()
			rv64AssertPublicReferenceArgumentScopesClear(t, in)

			if got, err = in.Invoke("target", 11, callable[0], extern, 13); err != nil || len(got) != 4 || got[0] != extern || got[2] != 11 || got[3] != 13 {
				t.Fatal("direct reference carrier target", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("reference carrier externref remained pinned")
			}
		})
	}
}

func TestRV64PublicReferenceFunctionCarriersForeignOwners(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-carriers-foreign", rv64PublicReferenceCarrierModule(), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			ref, err := rt.NewExternRef(struct{ Name string }{"carrier-owner"})
			if err != nil {
				t.Fatal(err)
			}
			argumentProducer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			targetProducer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer consumer.Close()
			argument, err := argumentProducer.Invoke("get")
			if err != nil || len(argument) != 1 {
				t.Fatal("foreign carrier argument", argument, err)
			}
			target, err := targetProducer.Invoke("get")
			if err != nil || len(target) != 1 {
				t.Fatal("foreign carrier target", target, err)
			}
			argumentDescriptor, argumentOK := consumer.refStore.resolve(argument[0])
			if !argumentOK || argumentDescriptor == 0 {
				t.Fatal("foreign carrier argument descriptor", argumentDescriptor, argumentOK)
			}
			if err := argumentProducer.Close(); err != nil {
				t.Fatal(err)
			}
			if err := targetProducer.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := consumer.Invoke("call", 17, argument[0], ValueExternRef(ref).Bits(), 19, target[0])
			returnedDescriptor, returnedOK := uint64(0), false
			if len(got) == 4 {
				returnedDescriptor, returnedOK = consumer.refStore.resolve(got[1])
			}
			if err != nil || len(got) != 4 || got[0] != ValueExternRef(ref).Bits() || got[2] != 17 || got[3] != 19 || !returnedOK || returnedDescriptor != argumentDescriptor {
				t.Fatal("foreign mixed reference carrier call", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			if argumentProducer.usesIndependentExecution() || targetProducer.usesIndependentExecution() || consumer.usesIndependentExecution() || !argumentProducer.nativeControlIsShared() || !targetProducer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign reference carrier owners did not reconcile shared execution")
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("foreign reference carrier externref remained pinned")
			}
		})
	}
}

func TestRV64PublicReferenceFunctionCarriersAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "reference-function-carriers-admission", rv64PublicReferenceCarrierModule(), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"target-actual-result", func(c *Compiled) { c.Funcs[0].Results[0] = ValFuncRef }},
		{"target-declared-result", func(c *Compiled) { c.Types[0].Results[0].Ref.Heap.Abstract = AbstractHeapFunc }},
		{"caller-actual-result", func(c *Compiled) { c.Funcs[2].Results[1] = ValExternRef }},
		{"caller-declared-result", func(c *Compiled) { c.Types[2].Results[1].Ref.Heap.Abstract = AbstractHeapExtern }},
		{"caller-result-order", func(c *Compiled) {
			c.Funcs[2].Results[2], c.Funcs[2].Results[3] = c.Funcs[2].Results[3], c.Funcs[2].Results[2]
		}},
		{"target-signature-key", func(c *Compiled) { c.FuncTypeID[0]++ }},
		{"missing-dynamic-witness", func(c *Compiled) { c.dynamicFuncrefEscape = false }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			c.Funcs = slices.Clone(base.Funcs)
			c.Types = slices.Clone(base.Types)
			c.Funcs[0].Results = slices.Clone(base.Funcs[0].Results)
			c.Funcs[2].Results = slices.Clone(base.Funcs[2].Results)
			c.Types[0].Results = slices.Clone(base.Types[0].Results)
			c.Types[2].Results = slices.Clone(base.Types[2].Results)
			c.FuncTypeID = slices.Clone(base.FuncTypeID)
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-matching public reference carrier boundary admitted", err)
			}
		})
	}
}
