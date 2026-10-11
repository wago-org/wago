//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCTypeSubtypingRefFuncGlobalsAdmissionIsExact(t *testing.T) {
	for wireIndex, pinned := range rv64GCTypeSubtypingRefFuncGlobalWires {
		variant := uint8(wireIndex + 1)
		data := bytes.Clone(pinned)
		if GCTypeSubtypingRefFuncGlobalsSourceVariant(data) != variant {
			t.Fatal("pinned ref.func-global source was not admitted", variant)
		}
		m, err := wasm.DecodeModule(data)
		if err != nil || admitRV64GCTypeSubtypingRefFuncGlobals(m) != variant {
			t.Fatal("pinned ref.func-global module was not recognized", variant, err)
		}
		metadata, err := frontend.BuildGCTypeMetadata(m)
		if err != nil {
			t.Fatal(err)
		}
		opts := CompileOptions{
			DeferCodeMapping:                    true,
			Interruptible:                       true,
			InvocationContext:                   true,
			ForeignTableCalls:                   true,
			GCTypeSubtypingRefFuncGlobalsSource: variant,
			Codegen: codegen.Options{Module: codegen.ModuleInfo{
				GCTypeDescs:   metadata.Descs,
				GCTypeLayouts: metadata.Layouts,
			}},
		}
		compiled, err := CompileModuleWith(m, opts)
		if err != nil {
			t.Fatal(variant, err)
		}
		if len(compiled.Code) == 0 || len(compiled.Entry) != len(m.Code) || len(compiled.InternalEntry) != len(m.Code) || compiled.CodeImage != nil {
			t.Fatalf("ref.func-global product missing deferred code: variant=%d code=%d entries=%v/%v image=%v", variant, len(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
		}
		t.Logf("variant=%d code=%d sha256=%x entry=%v internal=%v", variant, len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
		withoutWire := opts
		withoutWire.GCTypeSubtypingRefFuncGlobalsSource = 0
		if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded ref.func-global product compiled without exact wire provenance", variant, err)
		}
		for i := range data {
			mutated := bytes.Clone(data)
			mutated[i] ^= 1
			if GCTypeSubtypingRefFuncGlobalsSourceVariant(mutated) != 0 {
				t.Fatalf("wire mutation variant=%d offset=%d remained admitted", variant, i)
			}
		}
		m.Globals[0].Type.Mutable = true
		if admitRV64GCTypeSubtypingRefFuncGlobals(m) != 0 {
			t.Fatal("decoded global mutation remained admitted", variant)
		}
	}
}
