//go:build linux && amd64 && !tinygo && !wago_guardpage

package wago

import (
	"context"
	"encoding/binary"
	"math"
	"strings"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/wasm"
	"github.com/wago-org/wago/src/core/runtime"
	"github.com/wago-org/wago/src/core/runtime/abi"
	"github.com/wago-org/wago/testutil/wasmtest"
)

func f64Const(v float64) []byte {
	out := []byte{0x44}
	var bits [8]byte
	binary.LittleEndian.PutUint64(bits[:], math.Float64bits(v))
	return append(out, bits[:]...)
}

func descriptorTailActivationBody(target uint32, param bool) []byte {
	body := []byte{}
	if param {
		body = append(body, 0x20, 0x00)
	}
	body = append(body, 0x12)
	body = append(body, wasmtest.ULEB(target)...)
	return append(body, 0x0b)
}

func localF64DescriptorModule(indirect bool) []byte {
	target := wasmtest.FuncType([]wasm.ValType{wasm.F64}, []wasm.ValType{wasm.F64})
	caller := wasmtest.FuncType(nil, []wasm.ValType{wasm.F64})
	call := f64Const(13.25)
	if indirect {
		call = append(call, 0x41, 0x00, 0x11, 0x00, 0x00) // call_indirect type 0 table 0
	} else {
		call = append(call, 0xd2, 0x00, 0x14, 0x00) // ref.func 0; call_ref type 0
	}
	call = append(call, 0x0b)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(target, caller)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(1), wasmtest.ULEB(0))),
	}
	if indirect {
		sections = append(sections,
			wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})),
		)
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 1)}
	if indirect {
		exports = append(exports, wasmtest.ExportEntry("table", 1, 0))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if indirect {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})))
	} else {
		declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(declared)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0x20, 0x00, 0x0b}),
		wasmtest.Code(call),
		wasmtest.Code(descriptorTailActivationBody(0, true)),
	)))
	return wasmtest.Module(sections...)
}

func localFuncrefResultDescriptorModule(indirect bool) []byte {
	sig := wasmtest.FuncType(nil, []wasm.ValType{wasm.FuncRef})
	call := []byte{}
	if indirect {
		call = append(call, 0x41, 0x00, 0x11, 0x00, 0x00)
	} else {
		call = append(call, 0xd2, 0x00, 0x14, 0x00)
	}
	call = append(call, 0x0b)
	sections := [][]byte{
		wasmtest.Section(1, wasmtest.Vec(sig)),
		wasmtest.Section(3, wasmtest.Vec(wasmtest.ULEB(0), wasmtest.ULEB(0), wasmtest.ULEB(0))),
	}
	if indirect {
		sections = append(sections, wasmtest.Section(4, wasmtest.Vec([]byte{0x70, 0x00, 0x01})))
	}
	exports := [][]byte{wasmtest.ExportEntry("run", 0, 1)}
	if indirect {
		exports = append(exports, wasmtest.ExportEntry("table", 1, 0))
	}
	sections = append(sections, wasmtest.Section(7, wasmtest.Vec(exports...)))
	if indirect {
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec([]byte{0x00, 0x41, 0x00, 0x0b, 0x01, 0x00})))
	} else {
		declared := append([]byte{0x03, 0x00}, wasmtest.Vec(wasmtest.ULEB(0))...)
		sections = append(sections, wasmtest.Section(9, wasmtest.Vec(declared)))
	}
	sections = append(sections, wasmtest.Section(10, wasmtest.Vec(
		wasmtest.Code([]byte{0xd2, 0x00, 0x0b}),
		wasmtest.Code(call),
		wasmtest.Code([]byte{0x12, 0x00, 0x0b}),
	)))
	return wasmtest.Module(sections...)
}

func compileFuncrefEntryABIModule(t testing.TB, module []byte) *Compiled {
	t.Helper()
	cfg := NewRuntimeConfig()
	features := cfg.frontendFeatures()
	features.TypedFunctionReferences = true
	features.TypedTailCalls = true
	features.TailCalls = true
	compiled, err := compileWithFrontendFeatures(cfg, module, features)
	if err != nil {
		t.Fatalf("compile descriptor ABI module: %v", err)
	}
	t.Cleanup(func() { _ = compiled.Close() })
	return compiled
}

func TestFuncrefDescriptorEntryKindDrivesCallABI(t *testing.T) {
	for _, tc := range []struct {
		name    string
		module  []byte
		funcref bool
		wantF64 float64
	}{
		{name: "call-ref-f64", module: localF64DescriptorModule(false), wantF64: 13.25},
		{name: "call-indirect-f64", module: localF64DescriptorModule(true), wantF64: 13.25},
		{name: "call-ref-funcref-result", module: localFuncrefResultDescriptorModule(false), funcref: true},
		{name: "call-indirect-funcref-result", module: localFuncrefResultDescriptorModule(true), funcref: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			compiled := compileFuncrefEntryABIModule(t, tc.module)
			in, err := instantiateCore(compiled, InstantiateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			defer in.Close()
			got, err := in.Call(context.Background(), "run")
			if err != nil || len(got) != 1 {
				t.Fatalf("run = %v, %v", got, err)
			}
			if tc.funcref {
				if got[0].Type() != ValFuncRef || got[0].FuncRef().IsNull() || !in.FuncRefMatchesFunction(got[0].FuncRef(), 0) {
					t.Fatalf("funcref result = %v", got)
				}
			} else if got[0].Type() != ValF64 || got[0].F64() != tc.wantF64 {
				t.Fatalf("f64 result = %v, want %v", got, tc.wantF64)
			}
		})
	}
}

func TestFuncrefDescriptorRejectsConflictingEntryKindTags(t *testing.T) {
	compiled := compileFuncrefEntryABIModule(t, localF64DescriptorModule(false))
	in, err := instantiateCore(compiled, InstantiateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	off := runtime.TableEntryBytes
	home := binary.LittleEndian.Uint64(in.funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:])
	binary.LittleEndian.PutUint64(in.funcRefDescs[off+runtime.TableEntryHomeLinMemOffset:], home|abi.FuncRefLocalWrapperHomeTag)
	if _, err := in.Call(context.Background(), "run"); err == nil || !strings.Contains(err.Error(), "unsupported context") {
		t.Fatalf("conflicting descriptor tags = %v", err)
	}
}
