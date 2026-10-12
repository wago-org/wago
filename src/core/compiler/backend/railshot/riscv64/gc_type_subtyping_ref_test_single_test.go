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

func TestRV64GCTypeSubtypingRefTestSingleAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestSingleWire)
	if GCTypeSubtypingRefTestSingleSourceVariant(data) != 1 || !AdmitGCTypeSubtypingRefTestSingleSource(data) {
		t.Fatal("pinned single ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || !admitRV64GCTypeSubtypingRefTestSingle(m) {
		t.Fatal("pinned single ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 1,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x14, 0x74, 0x44, 0xa8, 0x87, 0x0e, 0xaa, 0xe7, 0x7f, 0x6d, 0x79, 0xf4, 0x83, 0x53, 0x0e, 0x51, 0x0a, 0x73, 0x07, 0x38, 0xa0, 0xf7, 0xc2, 0xab, 0xc8, 0xff, 0x94, 0x8d, 0xb6, 0x0e, 0x04, 0x71}
	wantEntries := []int{0, 576}
	if len(compiled.Code) != 1660 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("single ref.test product image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded single ref.test product compiled without exact wire provenance", err)
	}
	for i := range data {
		mutated := bytes.Clone(data)
		mutated[i] ^= 1
		if AdmitGCTypeSubtypingRefTestSingleSource(mutated) {
			t.Fatalf("wire mutation %d remained admitted", i)
		}
	}
	m.Exports[0].Name = "other"
	if admitRV64GCTypeSubtypingRefTestSingle(m) {
		t.Fatal("decoded export mutation remained admitted")
	}
}
