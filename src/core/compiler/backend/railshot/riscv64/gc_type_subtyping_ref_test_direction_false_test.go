//go:build linux && !tinygo

package riscv64

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCTypeSubtypingRefTestDirectionFalseAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestDirectionFalseWire)
	if len(data) != 122 || sha256.Sum256(data) != [sha256.Size]byte{0x9b, 0x81, 0x11, 0xee, 0x2e, 0x3f, 0xb9, 0x1c, 0xc7, 0x80, 0x1a, 0x63, 0xb0, 0xa5, 0xa8, 0xe9, 0x7e, 0xca, 0x7b, 0x56, 0x65, 0xf7, 0xe6, 0xfe, 0xd5, 0xbe, 0x8a, 0x83, 0x27, 0x53, 0x42, 0x13} || GCTypeSubtypingRefTestSingleSourceVariant(data) != 3 {
		t.Fatal("pinned direction-false ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 3 {
		t.Fatal("pinned direction-false ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 3,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xc1, 0xf5, 0xcd, 0x7c, 0x93, 0xb8, 0x82, 0x7b, 0xd1, 0x7c, 0xa8, 0x62, 0xa8, 0x6c, 0x0f, 0x72, 0x11, 0xd5, 0x52, 0x7b, 0x23, 0xcf, 0x89, 0xb5, 0x1f, 0xdf, 0xa3, 0x23, 0x90, 0xa2, 0x8f, 0x7d}
	wantEntries := []int{0, 576}
	if len(compiled.Code) != 1660 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("direction-false ref.test product image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded direction-false ref.test product compiled without exact wire provenance", err)
	}
	for _, variant := range []uint8{1, 2} {
		wrongWire := opts
		wrongWire.GCTypeSubtypingRefTestSingleSource = variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded direction-false ref.test product compiled with wrong wire provenance", variant, err)
		}
	}
	for i := range data {
		mutated := bytes.Clone(data)
		mutated[i] ^= 1
		if GCTypeSubtypingRefTestSingleSourceVariant(mutated) != 0 {
			t.Fatalf("wire mutation %d remained admitted", i)
		}
	}
	m.Exports[0].Name = "other"
	if admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 0 {
		t.Fatal("decoded export mutation remained admitted")
	}
}
