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

func TestRV64GCTypeSubtypingRefTestNominalFalseDeepAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestNominalFalseDeepWire)
	wantSource := [sha256.Size]byte{0xb0, 0x79, 0x7a, 0x18, 0x25, 0xd0, 0x4b, 0xe4, 0x67, 0xe3, 0x36, 0xf7, 0xf2, 0x36, 0x63, 0x71, 0x84, 0xaa, 0xb4, 0x1a, 0x13, 0xde, 0x20, 0xff, 0x7a, 0x06, 0xeb, 0x1b, 0xb7, 0x88, 0x56, 0x13}
	if len(data) != 117 || sha256.Sum256(data) != wantSource || GCTypeSubtypingRefTestSingleSourceVariant(data) != 9 {
		t.Fatal("pinned deep nominal-false ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 9 {
		t.Fatal("pinned deep nominal-false ref.test module was not recognized", err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping:                   true,
		Interruptible:                      true,
		InvocationContext:                  true,
		ForeignTableCalls:                  true,
		GCTypeSubtypingRefTest:             true,
		GCTypeSubtypingRefTestSingleSource: 9,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xf6, 0xda, 0x1a, 0x04, 0xa2, 0x59, 0xc7, 0x96, 0x71, 0x31, 0x41, 0x86, 0x3a, 0x15, 0x05, 0x7e, 0x9e, 0x9f, 0xba, 0xcf, 0x4e, 0xe7, 0xd9, 0x58, 0xcd, 0x55, 0xe0, 0xb2, 0x17, 0xf5, 0x6e, 0xb8}
	if len(compiled.Code) != 1660 || sha256.Sum256(compiled.Code) != wantDigest || len(compiled.Entry) != 2 || compiled.Entry[0] != 0 || compiled.Entry[1] != 576 || len(compiled.InternalEntry) != 2 || compiled.InternalEntry[0] != 0 || compiled.InternalEntry[1] != 576 || compiled.CodeImage != nil {
		t.Fatalf("deep nominal-false ref.test image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}

	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded deep nominal-false ref.test compiled without exact wire provenance", err)
	}
	for _, variant := range []uint8{1, 2, 3, 4, 5, 6, 7, 8} {
		wrongWire := opts
		wrongWire.GCTypeSubtypingRefTestSingleSource = variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded deep nominal-false ref.test compiled with wrong wire provenance", variant, err)
		}
	}
	for i := range data {
		mutated := bytes.Clone(data)
		mutated[i] ^= 1
		if GCTypeSubtypingRefTestSingleSourceVariant(mutated) != 0 {
			t.Fatalf("wire mutation %d remained admitted", i)
		}
	}
}
