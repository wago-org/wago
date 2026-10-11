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

func TestRV64GCRefTestTableExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCRefTestTableHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 168 || sha256.Sum256(source) != rv64GCRefTestTableSourceDigest || !admitRV64GCRefTestTableWire(source) || !AdmitGCRefTestTableSource(source) || !admitRV64GCRefTestTable(m) {
		t.Fatal("exact object-table ref.test module was not admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCRefTestTableSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x6a, 0x0c, 0xce, 0xa7, 0xd2, 0x38, 0x18, 0x9e, 0xc4, 0x4a, 0x79, 0xe4, 0xca, 0x17, 0x65, 0x24, 0x73, 0x17, 0x77, 0xce, 0x8d, 0xd4, 0x7d, 0xf2, 0xcc, 0x8e, 0x5e, 0x05, 0x83, 0xdf, 0xbf, 0x96}
	wantEntry := []int{0, 864, 1728, 2592}
	if len(compiled.Code) != 4232 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected object-table ref.test image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCRefTestTableSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("object-table ref.test compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCRefTestTableWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}
