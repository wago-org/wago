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

func TestRV64GCStructPackedExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCStructPackedHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 514 || sha256.Sum256(source) != rv64GCStructPackedSourceDigest || !admitRV64GCStructPackedWire(source) || !AdmitGCStructPackedSource(source) || !admitRV64GCStructPacked(m) {
		t.Fatal("exact official packed struct leader was not admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCStructPackedSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xfc, 0x9d, 0x20, 0xbc, 0xd8, 0x67, 0xfe, 0x44, 0xd8, 0x88, 0x77, 0x70, 0x3f, 0x37, 0x97, 0xca, 0x0e, 0xd9, 0x53, 0xfa, 0xf5, 0x46, 0x39, 0x3c, 0xe1, 0x3b, 0xf9, 0xdd, 0x79, 0x17, 0x97, 0x9f}
	wantEntry := []int{0, 784, 1568, 2352, 3136, 3920, 4704, 5488, 6272, 7168}
	if len(compiled.Code) != 8876 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected packed struct image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCStructPackedSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("packed struct compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCStructPackedWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}
