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

func TestRV64GCTypeSubtypingRefTestRecursiveAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRefTestRecursiveWire)
	if len(data) != 162 || sha256.Sum256(data) != [sha256.Size]byte{0x97, 0xaf, 0xdb, 0x1a, 0x9a, 0xd0, 0x42, 0x48, 0x6b, 0x76, 0xad, 0x81, 0x6e, 0x78, 0xa4, 0x3f, 0x93, 0x3e, 0x79, 0xb9, 0x85, 0xc6, 0xfd, 0x20, 0xd0, 0x65, 0x8f, 0x3b, 0x69, 0xc6, 0xe0, 0x22} || GCTypeSubtypingRefTestSingleSourceVariant(data) != 2 || !AdmitGCTypeSubtypingRefTestSingleSource(data) {
		t.Fatal("pinned recursive ref.test source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || !admitRV64GCTypeSubtypingRefTestSingle(m) {
		t.Fatal("pinned recursive ref.test module was not recognized", err)
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
		GCTypeSubtypingRefTestSingleSource: 2,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x8d, 0x3a, 0xcf, 0x80, 0xff, 0x9b, 0xb6, 0xff, 0xb4, 0x5e, 0x93, 0x87, 0x84, 0x0a, 0x91, 0x41, 0x41, 0xe2, 0xbf, 0x98, 0x7d, 0x00, 0x75, 0x6a, 0x7d, 0xc4, 0x29, 0x45, 0xe4, 0xfc, 0x8c, 0x8b}
	wantEntries := []int{0, 576}
	if len(compiled.Code) != 1660 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("recursive ref.test product image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRefTestSingleSource = 0
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded recursive ref.test product compiled without exact wire provenance", err)
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
