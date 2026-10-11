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

func TestRV64GCArrayCopyExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCArrayCopyHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 402 || sha256.Sum256(source) != rv64GCArrayCopySourceDigest || !admitRV64GCArrayCopyWire(source) || !AdmitGCArrayCopySource(source) || !admitRV64GCArrayCopy(m) {
		t.Fatal("exact official array.copy leader was not admitted")
	}
	canonical := canonicalSectionSizes(source)
	canonicalModule, err := wasm.DecodeModule(canonical)
	if err != nil || !admitRV64GCArrayCopy(canonicalModule) {
		t.Fatalf("canonical alternate did not preserve decoded semantics: len=%d err=%v", len(canonical), err)
	}
	if admitRV64GCArrayCopyWire(canonical) || AdmitGCArrayCopySource(canonical) {
		t.Fatal("semantically identical alternate wire encoding was admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 7, GCArrayCopySource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x46, 0x4e, 0x41, 0xc2, 0x69, 0x52, 0x87, 0x0f, 0xfc, 0x53, 0x83, 0x2a, 0x2b, 0x5d, 0x13, 0xb2, 0xfb, 0x40, 0xb8, 0xb1, 0xc6, 0xdd, 0x35, 0x61, 0xc2, 0xb1, 0xb9, 0xac, 0x70, 0xca, 0x89, 0x52}
	wantEntries := []int{0, 720, 1456, 2192, 2960, 3824}
	if len(compiled.Code) != 5564 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntries) || !slices.Equal(compiled.InternalEntry, wantEntries) || len(canonical) != 376 {
		t.Fatalf("unexpected copy image: code=%d/%x entries=%v/%v canonical=%d", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry, len(canonical))
	}
	withoutWire := opts
	withoutWire.GCArrayCopySource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded copy module compiled without exact-wire witness")
	}
	for i := range source {
		candidate := append([]byte(nil), source...)
		candidate[i] ^= 1
		if admitRV64GCArrayCopyWire(candidate) {
			t.Fatalf("wire mutation at byte %d was admitted", i)
		}
	}
}
