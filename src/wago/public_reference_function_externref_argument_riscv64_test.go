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
	coreruntime "github.com/wago-org/wago/src/core/runtime"
)

func TestRV64PublicReferenceFunctionExternrefArgumentSourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-externref-argument", rv64PublicReferenceArgumentVectorModuleType(wasm.ExternRef), artifact)
			if !rv64PublicReferenceCallBoundary(c) || !rv64PublicReferenceArgumentTarget(c, 0) || c.Funcs[0].Params[0] != ValExternRef || rv64NativeTailModule(c) != nil {
				t.Fatal("public externref call argument metadata")
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			ref, err := rt.NewExternRef("externref-call-argument")
			if err != nil {
				t.Fatal(err)
			}
			externToken := ValueExternRef(ref).Bits()
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("externref-call target token", got, err)
			}
			callable := got[0]
			for _, tc := range []struct {
				name string
				arg  uint64
				want uint64
			}{
				{"null", 0, 1},
				{"nonnull", externToken, 0},
			} {
				t.Run(tc.name, func(t *testing.T) {
					got, err = in.Invoke("call", tc.arg, callable)
					if err != nil || !slices.Equal(got, []uint64{tc.want}) {
						t.Fatal("externref call argument", got, tc.want, err)
					}
					rv64AssertPublicReferenceArgumentScopesClear(t, in)
				})
			}
			typed, err := in.InvokeValues(context.Background(), "call", ValueExternRef(ref), ValueFuncRef(ValueOf(ValFuncRef, callable).FuncRef()))
			if err != nil || len(typed) != 1 || typed[0].I32() != 0 {
				t.Fatal("typed externref call argument", typed, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			fn, err := in.WasmFunc("call")
			if err != nil {
				t.Fatal(err)
			}
			session, err := fn.OpenSession()
			if err != nil {
				t.Fatal(err)
			}
			if got, err = session.Invoke(externToken, callable); err != nil || !slices.Equal(got, []uint64{0}) {
				t.Fatal("prepared externref call argument", got, err)
			}
			session.Close()
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", externToken^0x9e3779b97f4a7c15, callable); err == nil || got != nil {
				t.Fatal("forged externref call argument", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if got, err = in.Invoke("call", externToken, 0); got != nil {
				t.Fatal("null callable target returned values", got, err)
			} else {
				var trap *coreruntime.TrapError
				if !errors.As(err, &trap) || trap.Code != coreruntime.TrapIndirectOutOfBounds {
					t.Fatal("null callable target trap", err)
				}
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
			if value, ok := rt.ExternRefValue(ref); !ok || value != "externref-call-argument" {
				t.Fatal("externref identity changed", value, ok)
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("externref argument remained pinned after invocation")
			}
			if got, err = in.Invoke("call", externToken, callable); err == nil || got != nil {
				t.Fatal("released externref call argument", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, in)
		})
	}
}

func TestRV64PublicReferenceFunctionExternrefArgumentForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-externref-argument", rv64PublicReferenceArgumentVectorModuleType(wasm.ExternRef), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			ref, err := rt.NewExternRef(struct{ Name string }{"foreign-owner"})
			if err != nil {
				t.Fatal(err)
			}
			producer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer consumer.Close()
			target, err := producer.Invoke("get")
			if err != nil || len(target) != 1 || target[0] == 0 {
				t.Fatal("foreign externref-call target", target, err)
			}
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			got, err := consumer.Invoke("call", ValueExternRef(ref).Bits(), target[0])
			if err != nil || !slices.Equal(got, []uint64{0}) {
				t.Fatal("foreign owner externref call", got, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign externref-call owner did not reconcile shared execution")
			}
			if !rt.ReleaseExternRef(ref) {
				t.Fatal("foreign externref remained pinned")
			}
		})
	}
}

func TestRV64PublicReferenceFunctionExternrefArgumentAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "reference-function-externref-argument-admission", rv64PublicReferenceArgumentVectorModuleType(wasm.ExternRef), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"target-actual-parameter", func(c *Compiled) { c.Funcs[0].Params[0] = ValAnyRef }},
		{"target-declared-parameter", func(c *Compiled) { c.Types[0].Params[0].Ref.Heap.Abstract = AbstractHeapAny }},
		{"caller-forwarded-parameter", func(c *Compiled) { c.Types[2].Params[0].Ref.Heap.Abstract = AbstractHeapAny }},
		{"target-signature-key", func(c *Compiled) { c.FuncTypeID[0]++ }},
		{"target-only-export", func(c *Compiled) { c.Exports = map[string]int{"target": 0} }},
		{"missing-dynamic-witness", func(c *Compiled) { c.dynamicFuncrefEscape = false }},
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
				t.Fatal("non-matching public externref call argument admitted", err)
			}
		})
	}
}
