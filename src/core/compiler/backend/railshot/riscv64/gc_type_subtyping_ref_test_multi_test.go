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

func TestRV64GCTypeSubtypingRefTestMultiAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestMultiWire)
	wantSource := [sha256.Size]byte{0x89, 0x3d, 0xcf, 0x05, 0x8c, 0x5b, 0x28, 0x43, 0x65, 0x67, 0x02, 0x8a, 0xb4, 0x1b, 0xfb, 0x40, 0x9c, 0x5f, 0x1a, 0xcc, 0x73, 0x7e, 0x76, 0x4a, 0x3d, 0xfc, 0xc5, 0x1f, 0x6b, 0xe8, 0x20, 0x0e}
	if len(data) != 204 || sha256.Sum256(data) != wantSource || GCTypeSubtypingRefTestSingleSourceVariant(data) != 7 {
		t.Fatal("pinned full multi-result ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 7 {
		t.Fatal("pinned full multi-result ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 7,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x4c, 0xa6, 0x19, 0x75, 0x74, 0x5a, 0x14, 0xc7, 0x03, 0xbf, 0x4d, 0x38, 0x22, 0xfd, 0xec, 0xb6, 0x3a, 0xfa, 0xd6, 0x4c, 0x40, 0x47, 0x3c, 0xdc, 0x73, 0xfa, 0xfe, 0x14, 0xb6, 0x5a, 0xff, 0xb2}
	wantEntries := []int{0, 624, 1248}
	if len(compiled.Code) != 3392 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("full multi-result ref.test image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}

	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded full multi-result ref.test product compiled without exact wire provenance", err)
	}
	for _, variant := range []uint8{1, 2, 3, 4, 5, 6} {
		wrongWire := opts
		wrongWire.GCTypeSubtypingRefTestSingleSource = variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded full multi-result ref.test product compiled with wrong wire provenance", variant, err)
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
