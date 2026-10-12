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

func TestRV64GCTypeSubtypingRefTestNominalFalseAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestNominalFalseWire)
	wantSource := [sha256.Size]byte{0x28, 0x41, 0xd0, 0x98, 0xdf, 0xca, 0x12, 0x5c, 0xcd, 0x9c, 0x57, 0x7c, 0xf5, 0x57, 0x62, 0x74, 0x4c, 0x8a, 0x39, 0x11, 0xa1, 0x98, 0x6f, 0x85, 0x7b, 0xe4, 0x8e, 0xbc, 0x0d, 0x51, 0xf7, 0x35}
	if len(data) != 104 || sha256.Sum256(data) != wantSource || GCTypeSubtypingRefTestSingleSourceVariant(data) != 8 {
		t.Fatal("pinned nominal-false ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 8 {
		t.Fatal("pinned nominal-false ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 8,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x0c, 0xa2, 0xa1, 0x9b, 0xab, 0x0b, 0xab, 0xcb, 0xb9, 0xd5, 0x9b, 0xfb, 0x76, 0x09, 0x15, 0xe3, 0xd6, 0x17, 0x82, 0x05, 0xaf, 0xc4, 0xaf, 0xec, 0x5b, 0x3c, 0x48, 0xa8, 0xd9, 0xbf, 0xa5, 0x21}
	if len(compiled.Code) != 1660 || sha256.Sum256(compiled.Code) != wantDigest || len(compiled.Entry) != 2 || compiled.Entry[0] != 0 || compiled.Entry[1] != 576 || len(compiled.InternalEntry) != 2 || compiled.InternalEntry[0] != 0 || compiled.InternalEntry[1] != 576 || compiled.CodeImage != nil {
		t.Fatalf("nominal-false ref.test image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}

	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded nominal-false ref.test compiled without exact wire provenance", err)
	}
	for _, variant := range []uint8{1, 2, 3, 4, 5, 6, 7} {
		wrongWire := opts
		wrongWire.GCTypeSubtypingRefTestSingleSource = variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded nominal-false ref.test compiled with wrong wire provenance", variant, err)
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
