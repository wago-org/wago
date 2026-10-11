//go:build linux && !tinygo

package riscv64

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"testing"

	"github.com/wago-org/wago/src/core/compiler/codegen"
	"github.com/wago-org/wago/src/core/compiler/frontend"
	"github.com/wago-org/wago/src/core/compiler/wasm"
)

func rv64GCArrayReferenceBytes(t testing.TB) []byte {
	t.Helper()
	source, err := hex.DecodeString(rv64GCArrayReferenceHex)
	if err != nil {
		t.Fatal(err)
	}
	return source
}

func TestRV64GCArrayReferenceHelperImage(t *testing.T) {
	source := rv64GCArrayReferenceBytes(t)
	if len(source) != 396 || sha256.Sum256(source) != ([sha256.Size]byte{0x19, 0x17, 0x8a, 0x5d, 0xb9, 0xc6, 0xde, 0xd4, 0x1e, 0x18, 0x5a, 0x94, 0x22, 0xc5, 0x58, 0xa6, 0x5d, 0x4b, 0xc1, 0xf1, 0x1e, 0x7b, 0x0d, 0xf1, 0x1a, 0x77, 0x62, 0x26, 0xf2, 0x28, 0x12, 0xa9}) {
		t.Fatal("reference array source identity changed")
	}
	m, err := wasm.DecodeModule(source)
	if err != nil {
		t.Fatal(err)
	}
	if !admitRV64GCArrayReferenceWire(source) || !AdmitGCArrayReferenceSource(source) || !admitRV64GCArrayReference(m) {
		t.Fatal("reference array identity rejected")
	}
	metadata, err := frontend.BuildGCTypeMetadata(m)
	if err != nil {
		t.Fatal(err)
	}
	opts := CompileOptions{DeferCodeMapping: true, Interruptible: true, InvocationContext: true, ForeignTableCalls: true, GCArrayHelpers: true, SyncHostSlots: 4, GCArrayReferenceSource: true, Codegen: codegen.Options{Module: codegen.ModuleInfo{GCTypeDescs: metadata.Descs, GCTypeLayouts: metadata.Layouts}}}
	withoutWire := opts
	withoutWire.GCArrayReferenceSource = false
	if image, err := CompileModuleWith(m, withoutWire); image != nil || !errors.Is(err, ErrUnsupported) {
		t.Fatal("reference array escaped exact wire provenance", err)
	}
	for name, mutate := range map[string]func(*wasm.Module){
		"body": func(v *wasm.Module) {
			v.Code[6].BodyBytes = slices.Clone(v.Code[6].BodyBytes)
			v.Code[6].BodyBytes[12] ^= 1
		},
		"type":                func(v *wasm.Module) { v.Types[2].SubTypes[0].Final = false },
		"element-mode":        func(v *wasm.Module) { v.Elements[0].Mode.Kind = wasm.ElemDeclarative },
		"element-table":       func(v *wasm.Module) { v.Elements[0].Mode.Table = 1 },
		"element-offset-body": func(v *wasm.Module) { v.Elements[0].Mode.Offset.BodyBytes = []byte{0x41, 0, 0x0b} },
		"element-offset-instr": func(v *wasm.Module) {
			v.Elements[0].Mode.Offset.Instrs = []wasm.Instruction{{Kind: wasm.InstrNop}}
		},
		"element-kind": func(v *wasm.Module) { v.Elements[0].Kind.Kind = wasm.ElemFuncExprs },
		"element-ref":  func(v *wasm.Module) { v.Elements[0].Kind.Ref = wasm.AbsRef(wasm.HeapAny) },
		"element-expression": func(v *wasm.Module) {
			v.Elements[0].Kind.Exprs[0].BodyBytes = slices.Clone(v.Elements[0].Kind.Exprs[0].BodyBytes)
			v.Elements[0].Kind.Exprs[0].BodyBytes[1]++
		},
		"export": func(v *wasm.Module) { v.Exports[0].Name = "make" },
	} {
		t.Run("decoded-"+name, func(t *testing.T) {
			candidate, err := wasm.DecodeModule(slices.Clone(source))
			if err != nil {
				t.Fatal(err)
			}
			mutate(candidate)
			if admitRV64GCArrayReference(candidate) {
				t.Fatal("decoded near miss admitted")
			}
			if image, err := CompileModuleWith(candidate, opts); image != nil || !errors.Is(err, ErrUnsupported) {
				t.Fatal("decoded near miss escaped provenance", err)
			}
		})
	}
	image, err := CompileModuleWith(m, opts)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := [sha256.Size]byte{0x6d, 0x16, 0x29, 0x30, 0x5d, 0x94, 0xb7, 0x54, 0x3e, 0xf3, 0xee, 0x2a, 0xd3, 0x63, 0x7b, 0xd7, 0xb1, 0x97, 0x9b, 0x67, 0xc0, 0x7a, 0x63, 0x65, 0x8b, 0xa3, 0x76, 0xd1, 0xee, 0x55, 0xcc, 0x18}
	wantEntry := []int{0, 704, 1424, 2128, 2832, 3632, 4400, 5392, 6224, 6912, 7632}
	if image == nil || len(image.Code) != 9836 || sha256.Sum256(image.Code) != wantDigest || !slices.Equal(image.Entry, wantEntry) || !slices.Equal(image.InternalEntry, wantEntry) {
		t.Fatal("reference array helper image metadata", image)
	}
}
