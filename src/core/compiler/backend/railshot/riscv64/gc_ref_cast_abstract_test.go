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

func TestRV64GCRefCastAbstractExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCRefCastAbstractHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 380 || sha256.Sum256(source) != rv64GCRefCastAbstractSourceDigest || !admitRV64GCRefCastAbstractWire(source) || !AdmitGCRefCastAbstractSource(source) || !admitRV64GCRefCastAbstract(m) {
		t.Fatal("exact abstract gc/ref_cast module was rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 4, GCRefCastAbstractSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x69, 0x58, 0x40, 0x94, 0xc9, 0xbf, 0xbd, 0x3c, 0xcf, 0x8c, 0x6e, 0x66, 0x6d, 0xe3, 0xa0, 0xe2, 0x78, 0x2c, 0xf6, 0x91, 0xf4, 0x31, 0x86, 0x25, 0x3e, 0x63, 0x52, 0xb4, 0x83, 0x30, 0xfb, 0x18}
	wantEntry := []int{0, 576, 2048, 3120, 4944, 6032, 7120}
	if len(compiled.Code) != 9484 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected abstract gc/ref_cast image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCRefCastAbstractSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded abstract gc/ref_cast module compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCRefCastAbstractWire(candidate) {
			t.Fatalf("wire mutation %d retained admission", i)
		}
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"type": func(v *wasm.Module) {
			v.Types[2].SubTypes[0].Comp.Array = wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Var)
		},
		"table":   func(v *wasm.Module) { v.Tables[0].Type.Limits.Min++ },
		"element": func(v *wasm.Module) { v.Elements[0].Kind.Funcs[0]++ },
		"body":    func(v *wasm.Module) { v.Code[5].BodyBytes[6]++ },
		"export":  func(v *wasm.Module) { v.Exports[4].Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCRefCastAbstract(candidate) {
				t.Fatal("decoded mutation retained abstract gc/ref_cast admission")
			}
		})
	}
}
