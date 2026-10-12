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

func TestRV64GCTypeSubtypingRefTestMultiPairsAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestMultiPairsWire)
	if len(data) != 144 || sha256.Sum256(data) != [sha256.Size]byte{0xb5, 0x61, 0xb7, 0xbc, 0xd1, 0x31, 0x22, 0x3f, 0x57, 0x3b, 0x78, 0x7f, 0xf0, 0x02, 0xce, 0xc3, 0xef, 0x83, 0xd1, 0xcb, 0x90, 0xfc, 0x44, 0x0e, 0xc2, 0x4d, 0x34, 0x7c, 0xc7, 0x89, 0xdf, 0x1d} || GCTypeSubtypingRefTestSingleSourceVariant(data) != 6 {
		t.Fatal("pinned paired multi-result ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || admitRV64GCTypeSubtypingRefTestSingleVariant(m) != 6 {
		t.Fatal("pinned paired multi-result ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 6,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xc5, 0x25, 0x95, 0x83, 0x78, 0xda, 0x46, 0x40, 0x69, 0xdc, 0xe5, 0xdd, 0x12, 0xec, 0xb7, 0x7f, 0x7c, 0x56, 0x6c, 0x97, 0x3e, 0x24, 0x24, 0xce, 0x01, 0xd7, 0x65, 0x81, 0xd6, 0x25, 0x17, 0xe4}
	wantEntries := []int{0, 624, 1248}
	if len(compiled.Code) != 2608 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("paired multi-result ref.test image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}

	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded paired multi-result ref.test product compiled without exact wire provenance", err)
	}
	for _, variant := range []uint8{1, 2, 3, 4, 5} {
		wrongWire := opts
		wrongWire.GCTypeSubtypingRefTestSingleSource = variant
		if _, err = CompileModuleWith(m, wrongWire); !errors.Is(err, ErrUnsupported) {
			t.Fatal("decoded paired multi-result ref.test product compiled with wrong wire provenance", variant, err)
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
