//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"slices"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// This fixture widens the public owner-scoped call_ref boundary from the
// original no-argument i32 target to the complete shared scalar carrier set.
// The target and wrapper both use (i32, i64, f32, f64) -> (i32, i64, f32,
// f64), while the wrapper takes its authenticated exact funcref last.
func rv64PublicReferenceScalarBoundaryModule(referenceResult bool) []byte {
	callerResult := []byte{4, 0x7f, 0x7e, 0x7d, 0x7c}
	callerBody := []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x14, 0, 0x0b}
	if referenceResult {
		callerResult = []byte{1, 0x70}
		callerBody = []byte{0x20, 0, 0x20, 1, 0x20, 2, 0x20, 3, 0x20, 4, 0x14, 0, 0x1a, 0x1a, 0x1a, 0x1a, 0x20, 4, 0x0b}
	}
	callerType := append([]byte{0x60, 5, 0x7f, 0x7e, 0x7d, 0x7c, 0x63, 0}, callerResult...)
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
			wasmtest.Code(callerBody),
		)),
	)
}

func rv64PublicReferenceScalarArguments(token uint64) (args, want []uint64) {
	args = []uint64{
		0xfeedface89abcdef,
		0xfedcba9876543210,
		0xaabbccdd3f800001,
		math.Float64bits(-17.25),
		token,
	}
	want = []uint64{0x89abcdef, args[1], 0x3f800001, args[3]}
	return
}

func TestRV64PublicReferenceFunctionScalarBoundarySourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		for _, referenceResult := range []bool{false, true} {
			t.Run(fmt.Sprintf("artifact%t-reference-result%t", artifact, referenceResult), func(t *testing.T) {
				if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
					t.Skip("precompiled artifact path only")
				}
				c := rv64NativeTailCompile(t, fmt.Sprintf("reference-function-scalar-boundary-result-%t", referenceResult), rv64PublicReferenceScalarBoundaryModule(referenceResult), artifact)
				if !rv64PublicReferenceCallBoundary(c) || !c.nativeReferenceFunctionABI || !c.nativeReferenceCallABI || !c.nativeReferenceControlABI || c.tableCount() != 0 || len(c.Globals) != 0 {
					t.Fatal("public scalar reference boundary metadata", c.requiredFeatures)
				}
				rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
				defer rv64HostCertificateCloseRuntime(t, rt)
				in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
				defer in.Close()
				got, err := in.Invoke("get")
				if err != nil || len(got) != 1 || got[0] == 0 {
					t.Fatal("scalar reference target token", got, err)
				}
				token := got[0]
				args, want := rv64PublicReferenceScalarArguments(token)
				got, err = in.Invoke("call", args...)
				if referenceResult {
					if err != nil || len(got) != 1 || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[0]).FuncRef(), 0) {
						t.Fatal("scalar call_ref reference result", got, err)
					}
				} else if err != nil || !slices.Equal(got, want) {
					t.Fatal("scalar call_ref result", got, want, err)
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
				got, err = session.Invoke(args...)
				if err != nil {
					t.Fatal("prepared scalar reference boundary", got, err)
				}
				if referenceResult {
					if len(got) != 1 || !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, got[0]).FuncRef(), 0) {
						t.Fatal("prepared scalar call_ref reference result", got)
					}
				} else if !slices.Equal(got, want) {
					t.Fatal("prepared scalar call_ref result", got, want)
				}
				session.Close()
				rv64AssertPublicReferenceArgumentScopesClear(t, in)

				if !referenceResult {
					typed, err := in.InvokeValues(context.Background(), "call",
						ValueI32(-1985229329), ValueI64(-81985529216486896),
						ValueF32(math.Float32frombits(0x3f800001)), ValueF64(-17.25),
						ValueFuncRef(ValueOf(ValFuncRef, token).FuncRef()))
					if err != nil || len(typed) != 4 || typed[0].Type() != ValI32 || typed[0].Bits() != 0x89abcdef || typed[1].Type() != ValI64 || typed[1].Bits() != want[1] || typed[2].Type() != ValF32 || typed[2].Bits() != want[2] || typed[3].Type() != ValF64 || typed[3].Bits() != want[3] {
						t.Fatal("typed scalar reference boundary", typed, err)
					}
					rv64AssertPublicReferenceArgumentScopesClear(t, in)
				}
			})
		}
	}
}

func TestRV64PublicReferenceFunctionScalarBoundaryAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	base := rv64NativeTailCompile(t, "reference-function-scalar-boundary-admission", rv64PublicReferenceScalarBoundaryModule(false), false)
	if !rv64PublicReferenceCallBoundary(base) {
		t.Fatal("scalar public reference boundary rejected")
	}
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"actual-target-param", func(c *Compiled) {
			c.Funcs[0].Params = slices.Clone(c.Funcs[0].Params)
			c.Funcs[0].Params[0] = ValI64
		}},
		{"declared-target-param", func(c *Compiled) {
			c.Types[0].Params = slices.Clone(c.Types[0].Params)
			c.Types[0].Params[0] = ValueTypeDescriptor{Kind: ValueTypeI64}
		}},
		{"declared-caller-param", func(c *Compiled) {
			c.Types[2].Params = slices.Clone(c.Types[2].Params)
			c.Types[2].Params[1] = ValueTypeDescriptor{Kind: ValueTypeI32}
		}},
		{"declared-caller-result", func(c *Compiled) {
			c.Types[2].Results = slices.Clone(c.Types[2].Results)
			c.Types[2].Results[3] = ValueTypeDescriptor{Kind: ValueTypeF32}
		}},
		{"caller-ref-position", func(c *Compiled) {
			c.Funcs[2].Params = slices.Clone(c.Funcs[2].Params)
			c.Funcs[2].Params[3], c.Funcs[2].Params[4] = c.Funcs[2].Params[4], c.Funcs[2].Params[3]
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.Funcs = slices.Clone(c.Funcs)
			c.Types = slices.Clone(c.Types)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-matching scalar public reference boundary admitted", err)
			}
		})
	}
	referenceResult := rv64NativeTailCompile(t, "reference-function-scalar-boundary-reference-result-admission", rv64PublicReferenceScalarBoundaryModule(true), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"actual-result", func(c *Compiled) {
			c.Funcs[2].Results = slices.Clone(c.Funcs[2].Results)
			c.Funcs[2].Results[0] = ValI32
		}},
		{"declared-result-kind", func(c *Compiled) {
			c.Types[2].Results = slices.Clone(c.Types[2].Results)
			c.Types[2].Results[0] = ValueTypeDescriptor{Kind: ValueTypeI32}
		}},
		{"declared-result-nullability", func(c *Compiled) {
			c.Types[2].Results = slices.Clone(c.Types[2].Results)
			c.Types[2].Results[0].Ref.Nullable = false
		}},
	} {
		t.Run("reference-result-"+tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(referenceResult)
			c.Funcs = slices.Clone(c.Funcs)
			c.Types = slices.Clone(c.Types)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-matching scalar reference-result boundary admitted", err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionScalarBoundaryForeignOwner(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-scalar-boundary-foreign-owner", rv64PublicReferenceScalarBoundaryModule(false), artifact)
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			producer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			consumer := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer consumer.Close()
			got, err := producer.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("foreign scalar target token", got, err)
			}
			args, want := rv64PublicReferenceScalarArguments(got[0])
			if err := producer.Close(); err != nil {
				t.Fatal(err)
			}
			if !producer.hasPhysicalResources() {
				t.Fatal("foreign scalar token did not retain its producer")
			}
			if got, err = consumer.Invoke("call", args...); err != nil || !slices.Equal(got, want) {
				t.Fatal("foreign scalar call_ref result", got, want, err)
			}
			rv64AssertPublicReferenceArgumentScopesClear(t, consumer)
			if producer.usesIndependentExecution() || consumer.usesIndependentExecution() || !producer.nativeControlIsShared() || !consumer.nativeControlIsShared() {
				t.Fatal("foreign scalar call_ref did not reconcile native ownership")
			}
		})
	}
}
