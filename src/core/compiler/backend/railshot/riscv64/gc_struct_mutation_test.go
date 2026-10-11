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

func TestRV64GCStructMutationExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCStructMutationHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 106 || sha256.Sum256(source) != rv64GCStructMutationSourceDigest || !admitRV64GCStructMutationWire(source) || !AdmitGCStructMutationSource(source) || !admitRV64GCStructMutation(m) {
		t.Fatal("exact numeric struct mutation module was not admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCStructMutationSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x36, 0x94, 0xd7, 0x68, 0x03, 0x66, 0xe5, 0xac, 0x5c, 0x77, 0x15, 0xa4, 0xdd, 0x4d, 0xf6, 0x87, 0x2c, 0x05, 0xce, 0x55, 0x37, 0x88, 0x2b, 0xb8, 0xbc, 0x44, 0x65, 0x82, 0xb7, 0xd0, 0x19, 0x85}
	wantEntry := []int{0, 736}
	if len(compiled.Code) != 2360 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected numeric struct mutation image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCStructMutationSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("numeric struct mutation compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCStructMutationWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}
