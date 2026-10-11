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

func TestRV64GCStructBasicExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCStructBasicHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 373 || sha256.Sum256(source) != rv64GCStructBasicSourceDigest || !admitRV64GCStructBasicWire(source) || !AdmitGCStructBasicSource(source) || !admitRV64GCStructBasic(m) {
		t.Fatal("exact official basic struct module was not admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCStructBasicSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xbb, 0x9b, 0x11, 0x04, 0xff, 0xc5, 0x8c, 0x5d, 0xc1, 0xfd, 0x9d, 0x09, 0x12, 0xbe, 0xa2, 0x4f, 0xbc, 0x57, 0xb5, 0x8f, 0xa5, 0x71, 0xc3, 0x83, 0xf8, 0xc5, 0x50, 0xb3, 0xab, 0x97, 0xf4, 0x4d}
	wantEntry := []int{0, 656, 1360, 2080, 2784, 3504, 4208, 4928, 5632, 6352, 7152, 7904, 8704}
	if len(compiled.Code) != 10232 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected basic struct image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCStructBasicSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("basic struct compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCStructBasicWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}
