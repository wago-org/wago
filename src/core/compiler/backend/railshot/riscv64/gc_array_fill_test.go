//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func TestRV64GCArrayFillExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayFillHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 183 || sha256.Sum256(source) != rv64GCArrayFillSourceDigest || !admitRV64GCArrayFillWire(source) || !AdmitGCArrayFillSource(source) || !admitRV64GCArrayFill(m) {
		t.Fatal("exact official array.fill leader was not admitted")
	}
	canonical := canonicalSectionSizes(source)
	canonicalModule, err := wasm.DecodeModule(canonical)
	if err != nil || len(canonical) != 163 || !admitRV64GCArrayFill(canonicalModule) {
		t.Fatalf("canonical alternate did not preserve decoded semantics: len=%d err=%v", len(canonical), err)
	}
	if admitRV64GCArrayFillWire(canonical) || AdmitGCArrayFillSource(canonical) {
		t.Fatal("semantically identical alternate wire encoding was admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 5, GCArrayFillSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x78, 0x5f, 0x70, 0xb7, 0x4d, 0x7f, 0x47, 0x5b, 0x2d, 0xa7, 0x7e, 0x6a, 0x3b, 0x5d, 0x61, 0x61, 0xfc, 0xe5, 0x32, 0x31, 0xfb, 0x91, 0xdc, 0x10, 0x77, 0xdc, 0x2b, 0x06, 0x1f, 0xd6, 0x7f, 0x14}
	wantEntries := []int{0, 720, 1424}
	if len(compiled.Code) != 2716 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) {
		t.Fatalf("unexpected fill image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCArrayFillSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded fill module compiled without exact-wire witness")
	}
	for i := range source {
		candidate := append([]byte(nil), source...)
		candidate[i] ^= 1
		if admitRV64GCArrayFillWire(candidate) {
			t.Fatalf("wire mutation at byte %d was admitted", i)
		}
	}
}
