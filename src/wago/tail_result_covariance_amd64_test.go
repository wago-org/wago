//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/testutil/wasmtest"
)

type covarianceTailKind uint8

const (
	covarianceDirect covarianceTailKind = iota
	covarianceIndirect
	covarianceRef
)

func covarianceTailModule(kind covarianceTailKind, reverse bool) []byte {
	nonNull := []byte{0x64, 0x70} // (ref func)
	nullable := []byte{0x70}      // funcref / (ref null func)
	calleeResult, callerResult := nonNull, nullable
	if reverse {
		calleeResult, callerResult = nullable, nonNull
	}
	callee := encodedFuncType(nil, [][]byte{calleeResult})
	caller := encodedFuncType(nil, [][]byte{callerResult})
	body := []byte{}
	switch kind {
	case covarianceDirect:
		body = []byte{0x12, 0x00, 0x0b}
	case covarianceIndirect:
		body = []byte{0x41, 0x00, 0x13, 0x00, 0x00, 0x0b}
	case covarianceRef:
		body = []byte{0xd2, 0x00, 0x15, 0x00, 0x0b}
	}
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(callee, caller)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1))),
	}
	if kind == covarianceIndirect {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(wasmtest.ExportEntry("run", 0, 1))))
	if kind == covarianceIndirect {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})))
	} else {
		declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(declared)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		wasmtest.Code(body),
	)))
	return wasmtest.Module(sections...)
}

func compileCovarianceTail(t testing.TB, module []byte) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TailCalls = true
	features.TypedFunctionReferences = true
	features.TypedTailCalls = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("compile covariance tail: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	return compiled
}

func TestTailCallsPreserveValidatedReferenceResultCovariance(t *testing.T) {
	for _, kind := range []covarianceTailKind{covarianceDirect, covarianceIndirect, covarianceRef} {
		t.Run([]string{"direct", "indirect", "ref"}[kind], func(t *testing.T) {
			compiled := compileCovarianceTail(t, covarianceTailModule(kind, false))
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Call(context.Background(), "run")
			if err != nil || len(got) != 1 || got[0].Type() != ValFuncRef || got[0].FuncRef().IsNull() {
				t.Fatalf("covariant tail result = %v, %v", got, err)
			}
			if !in.FuncRefMatchesFunction(got[0].FuncRef(), 0) {
				t.Fatalf("covariant tail lost function identity: %v", got)
			}
		})
	}
}

func TestTailCallsRejectReverseReferenceResultCovariance(t *testing.T) {
	for _, kind := range []covarianceTailKind{covarianceDirect, covarianceIndirect, covarianceRef} {
		module := covarianceTailModule(kind, true)
		decoded, err := wasm.DecodeModule(module)
		if err != nil {
			t.Fatalf("decode reverse covariance: %v", err)
		}
		if err := wasm.ValidateModule(decoded); err == nil || !strings.Contains(err.Error(), "type mismatch") {
			t.Fatalf("AST reverse covariance validation = %v", err)
		}
		cfg := NewRuntimeConfig()
		features := cfg.frontendFeatures()
		features.TailCalls = true
		features.TypedFunctionReferences = true
		features.TypedTailCalls = true
		if compiled, err := compileWithFrontendFeatures(cfg, module, features); err == nil {
			_ = compiled.Close()
			t.Fatalf("byte-backed reverse covariance compiled for kind %d", kind)
		}
	}
}
