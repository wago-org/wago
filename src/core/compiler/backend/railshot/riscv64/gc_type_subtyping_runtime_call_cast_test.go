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

func TestRV64GCTypeSubtypingRuntimeCallCastAdmissionIsExact(t *testing.T) {
	data := bytes.Clone(rv64GCTypeSubtypingRuntimeCallCastWire)
	if !AdmitGCTypeSubtypingRuntimeCallCastSource(data) {
		t.Fatal("pinned runtime call/cast source was not admitted")
	}
	m, err := wasm.DecodeModule(data)
	if err != nil || !admitRV64GCTypeSubtypingRuntimeCallCast(m) {
		t.Fatal("pinned runtime call/cast module was not recognized", err)
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{
		DeferCodeMapping:                     true,
		Interruptible:                        true,
		InvocationContext:                    true,
		ForeignTableCalls:                    true,
		NativeReferenceCalls:                 true,
		GCTypeSubtypingRuntimeCallCastSource: true,
		Codegen: codegen.Options{Module: codegen.ModuleInfo{
			GCTypeDescs:   metadata.Descs,
			GCTypeLayouts: metadata.Layouts,
		}},
	}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xab, 0x8a, 0x6c, 0xc4, 0xde, 0xb2, 0x04, 0x2d, 0x6a, 0xeb, 0x26, 0x2b, 0xc8, 0x29, 0x01, 0xb6, 0x4b, 0x61, 0xb2, 0xd6, 0x7b, 0x41, 0x75, 0xf5, 0xd1, 0x00, 0x52, 0x04, 0xdf, 0xe7, 0x48, 0x18}
	wantEntries := []int{0, 608, 1216, 1824, 11792, 13216, 14528, 15840, 17136, 18384}
	if len(compiled.Code) != 19628 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || compiled.CodeImage != nil {
		t.Fatalf("runtime call/cast product image changed: code=%d/%x entries=%v/%v image=%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, compiled.CodeImage)
	}
	withoutWire := opts
	withoutWire.GCTypeSubtypingRuntimeCallCastSource = false
	if _, err = CompileModuleWith(m, withoutWire); !errors.Is(err, ErrUnsupported) {
		t.Fatal("decoded runtime call/cast product compiled without exact wire provenance", err)
	}
	for i := range data {
		mutated := bytes.Clone(data)
		mutated[i] ^= 1
		if AdmitGCTypeSubtypingRuntimeCallCastSource(mutated) {
			t.Fatalf("wire mutation %d remained admitted", i)
		}
	}
	m.Exports[0].Name = "other"
	if admitRV64GCTypeSubtypingRuntimeCallCast(m) {
		t.Fatal("decoded export mutation remained admitted")
	}
}
