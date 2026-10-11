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

func TestRV64GCRefTestConcreteExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCRefTestConcreteHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 976 || sha256.Sum256(source) != rv64GCRefTestConcreteSourceDigest || !admitRV64GCRefTestConcreteWire(source) || !AdmitGCRefTestConcreteSource(source) || !admitRV64GCRefTestConcrete(m) {
		t.Fatal("exact official concrete ref.test module was not admitted")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, SyncHostSlots: 4, GCRefTestConcreteSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x7f, 0x46, 0xfc, 0x65, 0xcf, 0xf1, 0x02, 0x13, 0xaa, 0x3b, 0xb3, 0x83, 0x5b, 0x72, 0x52, 0xf6, 0xaa, 0xb6, 0x4b, 0xc0, 0x38, 0xff, 0xb8, 0x8a, 0x8d, 0xbb, 0x36, 0xc3, 0x9c, 0xce, 0x7f, 0xdb}
	wantEntry := []int{0, 1648, 17232}
	if len(compiled.Code) != 22792 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected concrete ref.test image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	internalOpts := opts
	internalOpts.production = true
	internalOpts.publicStarts = true
	internalOpts.gcRefTestConcrete = true
	internal, err := compileDecodedModule(m, scalarRegisters[:], internalOpts)
	if err != nil {
		t.Fatal(err)
	}
	if internal.state.capacity < 4 {
		t.Fatalf("concrete ref.test helper argument capacity = %d, want at least 4", internal.state.capacity)
	}
	withoutWire := opts
	withoutWire.GCRefTestConcreteSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("concrete ref.test compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCRefTestConcreteWire(candidate) {
			t.Fatalf("wire mutation %d admitted", i)
		}
	}
}
