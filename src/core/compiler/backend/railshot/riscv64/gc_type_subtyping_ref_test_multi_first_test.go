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

func TestRV64GCTypeSubtypingRefTestMultiFirstAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestMultiFirstWire)
	if len(data) != 178 || sha256.Sum256(data) != [sha256.Size]byte{0x5f, 0x08, 0x06, 0x74, 0xa0, 0x0a, 0x73, 0xb3, 0xdb, 0xa3, 0x91, 0xbb, 0x19, 0x67, 0xaa, 0x22, 0xf4, 0xdd, 0x6f, 0x1b, 0x43, 0xb9, 0xb4, 0x9a, 0xff, 0x08, 0x52, 0x8c, 0x33, 0x05, 0xaa, 0x6b} || GCTypeSubtypingRefTestSingleSourceVariant(data) != 5 {
		t.Fatal("pinned first multi-result ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 5 {
		t.Fatal("pinned first multi-result ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 5,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xf1, 0xf0, 0xec, 0x44, 0x47, 0xae, 0x54, 0x72, 0x27, 0x12, 0x01, 0xca, 0xba, 0x8d, 0xba, 0x53, 0xe3, 0x5d, 0x4a, 0x95, 0xdb, 0x91, 0x2e, 0x73, 0xf1, 0xd4, 0xfa, 0x05, 0xbb, 0x78, 0xd8, 0x1d}
	wantEntries := []int{0, 576}
	if len(compiled.Code) != 2152 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("first multi-result ref.test product image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded first multi-result ref.test product compiled without exact wire provenance", err)
	}
	for _, variant := range []uint8{1, 2, 3, 4} {
		wrongWire := opts
		wrongWire.GCTypeSubtypingRefTestSingleSource = variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded first multi-result ref.test product compiled with wrong wire provenance", variant, err)
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
