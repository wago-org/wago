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

func TestRV64GCRefCastConcreteExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCRefCastConcreteHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 512 || sha256.Sum256(source) != rv64GCRefCastConcreteSourceDigest || !admitRV64GCRefCastConcreteWire(source) || !AdmitGCRefCastConcreteSource(source) || !admitRV64GCRefCastConcrete(m) {
		t.Fatal("exact concrete gc/ref_cast module was rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCRefCastConcreteSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x71, 0x96, 0xa1, 0x54, 0xb7, 0x13, 0x08, 0xe1, 0xc8, 0x12, 0x00, 0xea, 0x8e, 0x92, 0xcc, 0x17, 0x0c, 0xf1, 0xa4, 0x96, 0x05, 0x76, 0xd1, 0xf7, 0x1c, 0xe1, 0xe6, 0xc6, 0x71, 0x73, 0xd4, 0x85}
	wantEntry := []int{0, 1648, 7488}
	if len(compiled.Code) != 12264 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected concrete gc/ref_cast image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCRefCastConcreteSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded concrete gc/ref_cast module compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCRefCastConcreteWire(candidate) {
			t.Fatalf("wire mutation %d retained admission", i)
		}
	}
}
