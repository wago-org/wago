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

func TestRV64GCExternExactAdmission(t *testing.T) {
	source, err := hex.DecodeString(rv64GCExternHex)
	if err != nil {
		t.Fatal(err)
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if len(source) != 286 || sha256.Sum256(source) != rv64GCExternSourceDigest || !admitRV64GCExternWire(source) || !AdmitGCExternSource(source) || !admitRV64GCExtern(m) {
		t.Fatal("exact gc/extern module was rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCStructHelpers: true, GCArrayHelpers: true, SyncHostSlots: 3, GCExternSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	compiled, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0xb4, 0x64, 0x11, 0xb1, 0x4b, 0xaf, 0x1a, 0xe9, 0xc6, 0xd6, 0x5a, 0xb5, 0x76, 0x88, 0x57, 0x17, 0x6c, 0x8e, 0x73, 0x06, 0x13, 0xfe, 0x24, 0xd6, 0x35, 0x52, 0xf9, 0xca, 0x09, 0x4b, 0xe6, 0x30}
	wantEntry := []int{0, 576, 1792, 2464, 3136, 3968}
	if len(compiled.Code) != 6076 || sha256.Sum256(compiled.Code) != wantDigest || !slices.Equal(compiled.Entry, wantEntry) || !slices.Equal(compiled.InternalEntry, wantEntry) {
		t.Fatalf("unexpected gc/extern image: code=%d/%x entries=%v/%v", len(compiled.Code), sha256.Sum256(compiled.Code), compiled.Entry, compiled.InternalEntry)
	}
	withoutWire := opts
	withoutWire.GCExternSource = false
	if _, err := CompileModuleWith(m, withoutWire); err == nil {
		t.Fatal("decoded gc/extern module compiled without exact wire provenance")
	}
	for i := range source {
		candidate := slices.Clone(source)
		candidate[i] ^= 1
		if admitRV64GCExternWire(candidate) {
			t.Fatalf("wire mutation %d retained admission", i)
		}
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"type": func(v *wasm.Module) {
			v.Types[2].SubTypes[0].Comp.Array = wasm.NewFieldType(wasm.StoragePacked(wasm.PackI8), wasm.Var)
		},
		"global":      func(v *wasm.Module) { v.Globals[1].Type.Mutable = true },
		"initializer": func(v *wasm.Module) { v.Globals[0].Init.BodyBytes[1]++ },
		"table":       func(v *wasm.Module) { v.Tables[0].Type.Limits.Min++ },
		"element":     func(v *wasm.Module) { v.Elements[0].Kind.Funcs[0]++ },
		"body":        func(v *wasm.Module) { v.Code[4].BodyBytes[1]++ },
		"export":      func(v *wasm.Module) { v.Exports[3].Name = "other" },
	} {
		t.Run(name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(source)
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCExtern(candidate) {
				t.Fatal("decoded mutation retained gc/extern admission")
			}
		})
	}
}
