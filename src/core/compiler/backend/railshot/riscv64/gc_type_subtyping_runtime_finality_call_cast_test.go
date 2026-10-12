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

func TestRV64GCTypeSubtypingRuntimeFinalityCallCastAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRuntimeFinalityCallCastWire)
	if !AdmitGCTypeSubtypingRuntimeFinalityCallCastSource(data) {
		t.Fatal("pinned runtime finality call/cast source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || !admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m) {
		t.Fatal("pinned runtime finality call/cast module was not recognized", err)
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
		GCTypeSubtypingRuntimeFinalityCallCastSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x6d, 0x81, 0xe1, 0x50, 0x2a, 0xed, 0xdf, 0x1b, 0xa8, 0xe8, 0xf7, 0x55, 0x44, 0x65, 0x36, 0xcf, 0x6e, 0x1b, 0x46, 0xf4, 0xb2, 0x5d, 0x42, 0x4a, 0xd6, 0xe8, 0x9f, 0x5f, 0x39, 0x66, 0x38, 0x6d}
	wantEntries := []int{0, 576, 1152, 2448, 3744, 4992}
	if len(compiled.Code) != 6228 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("runtime finality call/cast product image changed: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRuntimeFinalityCallCastSource = false
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded runtime finality call/cast product compiled without exact wire provenance", err)
	}
	for i := range data {
		mutated := bytes.Clone(data)
		mutated[i] ^= 1
		if AdmitGCTypeSubtypingRuntimeFinalityCallCastSource(mutated) {
			t.Fatalf("wire mutation %d remained admitted", i)
		}
	}
	m.Exports[0].Name = "other"
	if admitRV64GCTypeSubtypingRuntimeFinalityCallCast(m) {
		t.Fatal("decoded export mutation remained admitted")
	}
}
