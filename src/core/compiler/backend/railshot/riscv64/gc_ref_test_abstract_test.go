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

func TestRV64GCRefTestAbstractExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCRefTestAbstractHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 626 || sha256.Sum256(source) != rv64GCRefTestAbstractSourceDigest || !admitRV64GCRefTestAbstractWire(source) || !AdmitGCRefTestAbstractSource(source) || !admitRV64GCRefTestAbstract(m) {
		t.Fatal("exact official abstract ref.test module was not admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 4, GCRefTestAbstractSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x46, 0x80, 0x00, 0x62, 0x2e, 0x09, 0xec, 0x52, 0xd3, 0xde, 0xaa, 0x35, 0xee, 0xff, 0xae, 0x93, 0xb8, 0x4e, 0x68, 0x15, 0x94, 0x3c, 0x51, 0x9d, 0xdc, 0x0d, 0x6c, 0x06, 0x22, 0x3d, 0xe3, 0x60}
	wantEntry := []int{0, 576, 4976, 6048, 7184, 8320, 9456, 10592, 11728, 12960, 14192, 15424}
	if len(compiled.Code) != 18172 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected abstract ref.test image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	internalOpts := opts
	internalOpts.production = true
	internalOpts.publicStarts = true
	internalOpts.gcRefTestAbstract = true
	internal, err := compileDecodedModule(m, scalarRegisters[:], internalOpts)
	if err != nil {
		t.Fatal(err)
	}
	if internal.state.capacity < 4 {
		t.Fatalf("abstract ref.test helper argument capacity = %d, want at least 4", internal.state.capacity)
	}
	withoutWire := opts
	withoutWire.GCRefTestAbstractSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("abstract ref.test compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCRefTestAbstractWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}
