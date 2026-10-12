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

func TestRV64GCTypeSubtypingRuntimeTypedTableCallAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRuntimeTypedTableCallWire)
	if !AdmitGCTypeSubtypingRuntimeTypedTableCallSource(data) {
		t.Fatal("pinned runtime typed-table call source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || !admitRV64GCTypeSubtypingRuntimeTypedTableCall(m) {
		t.Fatal("pinned runtime typed-table call module was not recognized", err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping:     true,
		Interruptible:        true,
		InvocationContext:    true,
		ForeignTableCalls:    true,
		NativeReferenceCalls: true,
		GCTypeSubtypingRuntimeTypedTableCallSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x37, 0x59, 0x93, 0x51, 0xb7, 0x27, 0xf8, 0x32, 0xc8, 0xf2, 0x87, 0x7d, 0x04, 0x2e, 0xef, 0x58, 0xf9, 0xfe, 0x47, 0x7d, 0x4f, 0xb3, 0x39, 0x7b, 0xce, 0xce, 0x78, 0x92, 0x90, 0xa2, 0xbc, 0x3e}
	wantEntries := []int{0, 576, 1152, 5856, 7152}
	if len(compiled.Code) != 8444 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("runtime typed-table call product image is incomplete: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRuntimeTypedTableCallSource = false
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded runtime typed-table call product compiled without exact wire provenance", err)
	}
	for i := range data {
		mutated := bytes.Clone(data)
		mutated[i] ^= 1
		if AdmitGCTypeSubtypingRuntimeTypedTableCallSource(mutated) {
			t.Fatalf("wire mutation %d remained admitted", i)
		}
	}
	m.Exports[0].Name = "other"
	if admitRV64GCTypeSubtypingRuntimeTypedTableCall(m) {
		t.Fatal("decoded export mutation remained admitted")
	}
}
