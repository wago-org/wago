//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCTypeSubtypingRecursiveFunctionsAdmissionIsExact(t *testing.T) {
	for wireIndex, pinned := range rv64GCTypeSubtypingRecursiveFunctionWires {
		variant := uint8(wireIndex + 1)
		data := bytes.Clone(pinned)
		if GCTypeSubtypingRecursiveFunctionsSourceVariant(data) != variant {
			t.Fatal("pinned recursive-function source was not admitted", variant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingRecursiveFunctions(m) != variant {
			t.Fatal("pinned recursive-function module was not recognized", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping:                        true,
			Interruptible:                           true,
			InvocationContext:                       true,
			ForeignTableCalls:                       true,
			NativeReferenceCalls:                    true,
			GCTypeSubtypingRecursiveFunctionsSource: variant,
			Codegen: codegen.Options{Module: codegen.ModuleInfo{
				GCTypeDescs:   metadata.Descs,
				GCTypeLayouts: metadata.Layouts,
			}},
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(err)
		}
		if len(compiled.Code) == 0 || len(compiled.Entry) != 3 || len(compiled.InternalEntry) != 3 || compiled.CodeImage != nil {
			t.Fatalf("recursive-function product missing deferred code: code=%d entries=%v/%v image=%v", len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		withoutWire := opts
		withoutWire.GCTypeSubtypingRecursiveFunctionsSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded recursive-function product compiled without exact wire provenance", err)
		}
		for i := range data {
			mutated := bytes.Clone(data)
			mutated[i] ^= 1
			if GCTypeSubtypingRecursiveFunctionsSourceVariant(mutated) != 0 {
				t.Fatalf("wire mutation %d remained admitted", i)
			}
		}
		m.Code[0].BodyBytes[0] ^= 1
		if admitRV64GCTypeSubtypingRecursiveFunctions(m) != 0 {
			t.Fatal("decoded body mutation remained admitted")
		}
	}
}
