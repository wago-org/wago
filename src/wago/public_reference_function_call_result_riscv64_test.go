//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This is the smallest reference-result extension of the public call_ref
// boundary. The typed target remains reference-free and therefore has a
// published callable descriptor. The wrapper invokes it and returns the same
// authenticated descriptor as a nullable funcref value.
func rv64PublicReferenceFunctionCallResultModule(result byte) []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			[]byte{0x60, 1, 0x63, 0, 1, 0x70}, // (ref null type 0) -> funcref
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, result, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x14, 0, 0x1a, 0x20, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicReferenceFunctionCallResultSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-call-result", rv64PublicReferenceFunctionCallResultModule(42), artifact)
			if !rv64PublicReferenceCallBoundary(c) || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.tableCount() != 0 || len(c.Globals) != 0 {
				t.Fatal("public reference call-result metadata", c.requiredFeatures)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()

			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("reference target token", got, err)
			}
			token := got[0]
			for i := 0; i < 3; i++ {
				got, err = in.Invoke("call", token)
				if err != nil || len(got) != 1 || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[0]).FuncRef(), 0) {
					t.Fatal("public call_ref reference result", i, got, err)
				}
				rv64AssertPublicReferenceArgumentScopesClear(t, in)
			}
			typed, err := in.InvokeValues(context.Background(), "call", ValueFuncRef(ValueOf(ValFuncRef, token).FuncRef()))
			if err != nil || len(typed) != 1 || typed[0].Type() != ValFuncRef || !in.FuncRefMatchesFunction(typed[0].FuncRef(), 0) {
				t.Fatal("typed call_ref reference result", typed, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)

			if got, err = in.Invoke("call", 0); got != nil {
				t.Fatal("null call_ref returned a reference", got, err)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("null call_ref trap", err)
				}
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", token^0x9e3779b97f4a7c15); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatal("forged call_ref ingress", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", token); err != nil || len(got) != 1 || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[0]).FuncRef(), 0) {
				t.Fatal("reuse after rejected call_ref ingress", got, err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionCallResultAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "reference-function-call-result-admission", rv64PublicReferenceFunctionCallResultModule(42), false)
	if !rv64PublicReferenceCallBoundary(base) {
		t.Fatal("exact public reference call-result boundary rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"caller-result-i32", func(c *Compiled) { c.Types[2].Results[0] = ValueTypeDescriptor{Kind: ValueTypeI32} }},
		{"caller-result-non-null", func(c *Compiled) { c.Types[2].Results[0].Ref.Nullable = false }},
		{"caller-result-indexed", func(c *Compiled) {
			c.Types[2].Results[0].Ref.Heap.Defined = true
			c.Types[2].Results[0].Ref.Heap.TypeIndex = 0
		}},
		{"actual-result-i32", func(c *Compiled) { c.Funcs[2].Results[0] = ValI32 }},
		{"missing-control", func(c *Compiled) { c.nativeReferenceControlABI = false }},
		{"reference-global", func(c *Compiled) { c.Globals = []GlobalDef{{Type: ValFuncRef}} }},
		{"extra-export", func(c *Compiled) { c.Exports["alias"] = 0 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-exact public reference call-result boundary admitted", err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionCallResultForeignOwnerLifetime(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			producerCompiled := rv64NativeTailCompile(t, "reference-function-call-result-producer", rv64PublicReferenceFunctionCallBoundaryModule(41), artifact)
			consumerCompiled := rv64NativeTailCompile(t, "reference-function-call-result-consumer", rv64PublicReferenceFunctionCallResultModule(42), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			producer := rv64NativeTokenInstantiate(t, rt, producerCompiled, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, consumerCompiled, NewImports())
			defer consumer.Close()
			got, err := producer.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("foreign producer token", got, err)
			}
			token := got[0]
			descriptor, ok := consumer.refStore.resolve(token)
			if !ok || descriptor == 0 {
				t.Fatal("foreign producer descriptor", descriptor, ok)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("public token did not retain the closed producer")
			}
			for i := 0; i < 3; i++ {
				got, err = consumer.Invoke("call", token)
				returned := uint64(0)
				valid := false
				if len(got) == 1 {
					returned, valid = consumer.refStore.resolve(got[0])
				}
				if err != nil || !valid || returned != descriptor {
					t.Fatal("foreign call_ref reference result", i, got, err)
				}
				rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			}
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign call_ref result did not quiesce both native owners")
			}

			outsideCompiled := rv64NativeTailCompile(t, "reference-function-call-result-outside", rv64PublicReferenceFunctionBoundaryModule(), artifact)
			outside := rv64NativeTokenInstantiate(t, rt, outsideCompiled, NewImports())
			defer outside.Close()
			outsideResult, err := outside.Invoke("get")
			if err != nil || len(outsideResult) != 1 || outsideResult[0] == 0 {
				t.Fatal("outside owner token", outsideResult, err)
			}
			if got, err = consumer.Invoke("call", outsideResult[0]); err == nil || got != nil || !strings.Contains(err.Error(), "outside the public call boundary") {
				t.Fatal("outside owner profile admitted", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			got, err = consumer.Invoke("call", token)
			returned, valid := uint64(0), false
			if len(got) == 1 {
				returned, valid = consumer.refStore.resolve(got[0])
			}
			if err != nil || !valid || returned != descriptor {
				t.Fatal("reuse after rejected foreign owner", got, err)
			}

			foreignRT := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, foreignRT)
			foreign := rv64NativeTokenInstantiate(t, foreignRT, consumerCompiled, NewImports())
			if got, err = foreign.Invoke("call", token); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatal("cross-runtime call_ref token accepted", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, foreign)
			if err := foreign.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}
}
