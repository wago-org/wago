//go:build linux && riscv64 && wago_rv64_experimental && !tinygo && !wago_target_tinygo

package wago

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	railshot "github.com/wago-org/wago/src/core/compiler/backend/railshot/riscv64"
	"github.com/wago-org/wago/src/core/compiler/wasm"
	coreruntime "github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/tests/support/wasmtest"
)

// The reference-valued exports remain non-callable descriptor holes. Their
// public wrappers are the only boundary: ref.func can name only the numeric
// target, while id transports an already authenticated descriptor.
func rv64PublicReferenceFunctionBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			wasmtest.FuncType([]wasm.ValType{wasm.FuncRef}, []wasm.ValType{wasm.FuncRef}),
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("id", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x0b}),
		)),
	)
}

func rv64PublicReferenceFunctionCallBoundaryModule() []byte {
	return wasmtest.Module(
		wasmtest.Section(1, wasmtest.Vec(
			wasmtest.FuncType(nil, []wasm.ValType{wasm.I32}),
			wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef}),
			[]byte{0x60, 1, 0x63, 0, 1, 0x7f}, // (ref null type 0) -> i32
		)),
		wasmtest.Section(3, []byte{3, 0, 1, 2}),
		wasmtest.Section(7, wasmtest.Vec(
			wasmtest.ExportEntry("target", 0, 0),
			wasmtest.ExportEntry("get", 0, 1),
			wasmtest.ExportEntry("call", 0, 2),
		)),
		wasmtest.Section(9, []byte{1, 3, 0, 1, 0}),
		wasmtest.Section(10, wasmtest.Vec(
			wasmtest.Code([]byte{0x41, 42, 0x0b}),
			wasmtest.Code([]byte{0xd2, 0, 0x0b}),
			wasmtest.Code([]byte{0x20, 0, 0x14, 0, 0x0b}),
		)),
	)
}

func TestRV64PublicReferenceFunctionBoundarySourceArtifact(t *testing.T) {
	for _, artifact := range []bool{false, true} {
		t.Run(fmt.Sprintf("artifact%t", artifact), func(t *testing.T) {
			if !artifact && os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
				t.Skip("precompiled artifact path only")
			}
			c := rv64NativeTailCompile(t, "reference-function-boundary", rv64PublicReferenceFunctionBoundaryModule(), artifact)
			if !c.nativeReferenceFunctionABI || c.nativeReferenceCallABI || !c.requiredFeatures.IsEnabled(CoreFeatureReferenceTypes) || c.tableCount() != 0 {
				t.Fatal("reference boundary metadata", c.requiredFeatures)
			}
			rt := NewRuntime(WithRuntimeConfig(rv64NativeTailConfig()))
			defer rv64HostCertificateCloseRuntime(t, rt)
			in := rv64NativeTokenInstantiate(t, rt, c, NewImports())
			defer in.Close()
			if len(in.funcRefDescs) != 4*coreruntime.FuncRefDescBytes {
				t.Fatal("reference boundary descriptor extent", len(in.funcRefDescs))
			}
			for _, index := range []int{1, 2} {
				row := in.funcRefDescs[(index+1)*coreruntime.FuncRefDescBytes : (index+2)*coreruntime.FuncRefDescBytes]
				if !slices.Equal(row, make([]byte, len(row))) || !railshotNonCallableFunction(in.c, index) {
					t.Fatal("reference export acquired callable descriptor", index, row)
				}
			}

			got, err := in.Invoke("get")
			if err != nil || len(got) != 1 || got[0] == 0 {
				t.Fatal("reference result", got, err)
			}
			token := got[0]
			if !in.FuncRefMatchesFunction(ValueOf(ValFuncRef, token).FuncRef(), 0) {
				t.Fatal("reference result identity")
			}
			for _, arg := range []uint64{0, token, 0, token} {
				got, err = in.Invoke("id", arg)
				if err != nil || !slices.Equal(got, []uint64{arg}) {
					t.Fatal("reference boundary round trip", got, arg, err)
				}
			}
			typed, err := in.InvokeValues(context.Background(), "id", ValueFuncRef(ValueOf(ValFuncRef, token).FuncRef()))
			if err != nil || len(typed) != 1 || typed[0].Type() != ValFuncRef || typed[0].Bits() != token {
				t.Fatal("typed reference boundary", typed, err)
			}
			if got, err = in.Invoke("id", token^0x9e3779b97f4a7c15); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
				t.Fatal("forged reference ingress", got, err)
			}
			if got, err = in.Invoke("id", token); err != nil || !slices.Equal(got, []uint64{token}) {
				t.Fatal("reuse after rejected ingress", got, err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionBoundaryAdmission(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	if c, err := rv64NativeTailConfig().Compile(rv64PublicReferenceFunctionCallBoundaryModule()); c != nil || !errors.Is(err, railshot.ErrUnsupported) {
		if c != nil {
			_ = c.Close()
		}
		t.Fatal("dynamic reference target owner scope opened", err)
	}
	base := rv64NativeTailCompile(t, "reference-function-boundary-admission", rv64PublicReferenceFunctionBoundaryModule(), false)
	for _, tc := range []struct {
		name   string
		mutate func(*Compiled)
	}{
		{"table", func(c *Compiled) { c.HasTable, c.TableType, c.TableSize = true, ValFuncRef, 1 }},
		{"reference-global", func(c *Compiled) { c.Globals = []GlobalDef{{Type: ValFuncRef}} }},
		{"dynamic-reference-call", func(c *Compiled) { c.nativeReferenceCallABI = true }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c := cloneCompiledMetadata(base)
			c.validateMemo, c.codeCache = &validateMemo{}, nil
			tc.mutate(c)
			if err := railshotValidateCompiled(c); !errors.Is(err, railshot.ErrUnsupported) {
				t.Fatal("non-ephemeral reference export admitted", err)
			}
		})
	}
}

func TestRV64PublicReferenceFunctionBoundaryOwnership(t *testing.T) {
	if os.Getenv("WAGO_RV64_TAIL_ARTIFACT_MODE") == "load" {
		t.Skip("source compiler path only")
	}
	cfg := rv64NativeTailConfig()
	rt := NewRuntime(WithRuntimeConfig(cfg))
	defer rv64HostCertificateCloseRuntime(t, rt)
	c := rv64NativeTailCompile(t, "reference-function-boundary-owner", rv64PublicReferenceFunctionBoundaryModule(), false)
	first := rv64NativeTokenInstantiate(t, rt, c, NewImports())
	second := rv64NativeTokenInstantiate(t, rt, c, NewImports())
	got, err := first.Invoke("get")
	if err != nil || len(got) != 1 || got[0] == 0 {
		t.Fatal("producer reference", got, err)
	}
	token := got[0]
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	if !first.hasPhysicalResources() {
		t.Fatal("public token did not retain producer")
	}
	if got, err = second.Invoke("id", token); err != nil || !slices.Equal(got, []uint64{token}) {
		t.Fatal("same-runtime foreign descriptor round trip", got, err)
	}
	foreignRT := NewRuntime(WithRuntimeConfig(cfg))
	defer rv64HostCertificateCloseRuntime(t, foreignRT)
	foreign := rv64NativeTokenInstantiate(t, foreignRT, c, NewImports())
	if got, err = foreign.Invoke("id", token); err == nil || got != nil || !strings.Contains(err.Error(), "invalid funcref token") {
		t.Fatal("cross-runtime token accepted", got, err)
	}
	if err := foreign.Close(); err != nil {
		t.Fatal(err)
	}
	if err := foreignRT.Close(); err != nil {
		t.Fatal(err)
	}
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	rv64HostCertificateCloseRuntime(t, rt)
	if first.hasPhysicalResources() || second.hasPhysicalResources() {
		t.Fatalf("reference boundary owner retained after Runtime close: first=%t second=%t", first.hasPhysicalResources(), second.hasPhysicalResources())
	}
}
